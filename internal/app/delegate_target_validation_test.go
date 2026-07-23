package app

import (
	"context"
	"sync"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// This file is the security regression for finding 4.2 (confused-deputy) and
// finding 4.3 (delegate least-privilege). The centerpiece drives driveBinding
// with a stand-in delegate and the REAL store-backed resolver, so the exact
// seam where the fix lives is exercised end to end: a delegate that ASSERTS a
// CONTEXT_REQUIRED target ob never verified used to make ob look up and forward
// another host's stored credentials. Each test pairs the exploit (no guard,
// the pre-fix behavior) with the defense (the guard the delegate path now
// installs) so the before/after is explicit.

// fakeDelegate stands in for an untrusted delegate invoker. Each round it
// records the context it was handed and, until a bearer token arrives, raises a
// terminal CONTEXT_REQUIRED asserting a chosen target — the exact shape
// driveBinding sees from a real delegate (a challenge before any output). Once
// it receives a bearer token it emits one output.
type fakeDelegate struct {
	assertedTarget string

	mu       sync.Mutex
	received []map[string]any
}

func (f *fakeDelegate) invoke(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
	f.mu.Lock()
	snap := make(map[string]any, len(ctxData))
	for k, v := range ctxData {
		snap[k] = v
	}
	f.received = append(f.received, snap)
	f.mu.Unlock()

	impl := openbindings.NewInvocationImpl[any, any](ctx)
	go func() {
		if openbindings.ContextBearerToken(ctxData) == "" {
			impl.FireError(openbindings.NewContextRequiredError(
				"delegate requires a bearer token",
				&openbindings.ContextRequiredDetails{
					Target: f.assertedTarget,
					Alternatives: []openbindings.ContextAlternative{{
						Requirements: []openbindings.ContextRequirement{{Type: "auth.bearer"}},
					}},
				}))
			return
		}
		_ = impl.EmitOutput(map[string]any{"ok": true})
		impl.CloseOutput()
	}()
	return impl
}

func (f *fakeDelegate) rounds() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

func (f *fakeDelegate) round(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.received) {
		return nil
	}
	return f.received[i]
}

// drain collects a driveBinding stream into its outputs and terminal error.
func drain(ch <-chan InvocationOutput) (outputs []any, termErr *openbindings.InvocationError) {
	for ev := range ch {
		switch {
		case ev.Terminal:
		case ev.Error != nil:
			termErr = ev.Error
		default:
			outputs = append(outputs, ev.Output)
		}
	}
	return outputs, termErr
}

// seedContext seeds the sandboxed CLI context store under an origin URL. It
// writes both a config entry (so origin-keyed lookup finds it) and the
// credentials, exactly as ob's own resolver persists.
func seedContext(t *testing.T, originURL string, ctx map[string]any) {
	t.Helper()
	if err := NewCLIContextStore().Set(context.Background(), originURL, ctx); err != nil {
		t.Fatalf("seed context %q: %v", originURL, err)
	}
}

func clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TestDriveBinding_ConfusedDeputy is the 4.2 centerpiece: a delegate whose
// source addresses one host asserts ANOTHER host's target to harvest that
// host's stored credentials.
//
//   - Without the guard (the pre-fix behavior) the delegate DOES receive the
//     victim's stored bearer token — the exfiltration.
//   - With the guard the delegate path now installs, the mismatch is refused
//     loudly before any credential lookup, and no credential is ever forwarded.
func TestDriveBinding_ConfusedDeputy(t *testing.T) {
	setupContextTestDir(t)
	// The operator has stored credentials for the victim host.
	seedContext(t, "https://api.victim.com", map[string]any{"bearerToken": "VICTIM-SECRET"})

	// The source ob is actually invoking addresses the attacker's host; the
	// malicious delegate asserts the victim's target in its challenge.
	attackerSource := InvokeSource{
		BindingSpec: "example.graphql@1",
		Location:    "https://api.attacker.com/graphql",
	}

	t.Run("exploit_without_guard_exfiltrates", func(t *testing.T) {
		del := &fakeDelegate{assertedTarget: "https://api.victim.com"}
		outputs, termErr := drain(driveBinding(
			context.Background(), del.invoke, nil, nil, CLIContextResolver(), nil))

		if termErr != nil {
			t.Fatalf("unguarded path unexpectedly errored: %v", termErr)
		}
		if len(outputs) == 0 {
			t.Fatal("expected the unguarded delegate to succeed after harvesting credentials")
		}
		if del.rounds() < 2 {
			t.Fatalf("expected a retry round after resolution; rounds=%d", del.rounds())
		}
		if got := openbindings.ContextBearerToken(del.round(1)); got != "VICTIM-SECRET" {
			t.Fatalf("exploit precondition not met: unguarded delegate received bearer %q, want the victim secret", got)
		}
	})

	t.Run("guard_refuses_mismatch_no_lookup", func(t *testing.T) {
		del := &fakeDelegate{assertedTarget: "https://api.victim.com"}
		guard := newDelegateProvisionGuard(attackerSource)

		outputs, termErr := drain(driveBinding(
			context.Background(), del.invoke, nil, nil, CLIContextResolver(), guard))

		if termErr == nil {
			t.Fatalf("expected a terminal refusal, got outputs=%v", outputs)
		}
		if termErr.Code != openbindings.ErrCodePermissionDenied {
			t.Fatalf("refusal code = %q, want %q; msg=%q",
				termErr.Code, openbindings.ErrCodePermissionDenied, termErr.Message)
		}
		if len(outputs) != 0 {
			t.Fatalf("a refused invocation must yield no outputs, got %v", outputs)
		}
		// The delegate is challenged once (round 0) and NEVER retried with
		// provisioned credentials.
		if del.rounds() != 1 {
			t.Fatalf("delegate must be invoked once (the challenge) and not retried; rounds=%d", del.rounds())
		}
		for i := 0; i < del.rounds(); i++ {
			if got := openbindings.ContextBearerToken(del.round(i)); got != "" {
				t.Fatalf("confused-deputy NOT closed: delegate received bearer %q in round %d", got, i)
			}
		}
	})
}

// TestDriveBinding_MatchingTargetProvisions confirms the fix does not break the
// legitimate path: a delegate whose source addresses the same host it asserts
// is provisioned normally.
func TestDriveBinding_MatchingTargetProvisions(t *testing.T) {
	setupContextTestDir(t)
	seedContext(t, "https://api.service.com", map[string]any{"bearerToken": "REAL-BEARER"})

	del := &fakeDelegate{assertedTarget: "https://api.service.com"}
	guard := newDelegateProvisionGuard(InvokeSource{
		BindingSpec: "example.graphql@1",
		Location:    "https://api.service.com/graphql",
	})

	outputs, termErr := drain(driveBinding(
		context.Background(), del.invoke, nil, nil, CLIContextResolver(), guard))

	if termErr != nil {
		t.Fatalf("legitimate matching-target invocation failed: %v", termErr)
	}
	if len(outputs) == 0 {
		t.Fatal("expected the matching-target delegate to succeed")
	}
	if got := openbindings.ContextBearerToken(del.round(1)); got != "REAL-BEARER" {
		t.Fatalf("matching-target delegate received bearer %q, want it provisioned", got)
	}
}

// TestDriveBinding_DelegateLeastPrivilege is the 4.3 regression: on the delegate
// path, ob scopes every provisioned context to the challenge, so a delegate
// never receives credentials outside its own challenge's scope — neither an
// unrelated STORED credential (already scoped by the resolver) nor an unrelated
// per-call credential the caller attached (scoped by driveBinding).
func TestDriveBinding_DelegateLeastPrivilege(t *testing.T) {
	setupContextTestDir(t)
	// Stored context carries the challenged bearer token AND an unrelated key.
	seedContext(t, "https://api.service.com", map[string]any{
		"bearerToken": "REAL-BEARER",
		"apiKey":      "UNRELATED-STORED-KEY",
	})

	// Caller's per-call context: a header plus an unrelated per-call
	// credential the bearer challenge does not scope.
	perCall := map[string]any{
		"headers": map[string]any{"X-Trace": "abc"},
		"apiKey":  "CALLER-UNRELATED-KEY",
	}
	source := InvokeSource{BindingSpec: "example.graphql@1", Location: "https://api.service.com/graphql"}

	t.Run("guard_scopes_to_challenge", func(t *testing.T) {
		del := &fakeDelegate{assertedTarget: "https://api.service.com"}
		guard := newDelegateProvisionGuard(source)
		if _, err := drain(driveBinding(
			context.Background(), del.invoke, clone(perCall), nil, CLIContextResolver(), guard)); err != nil {
			t.Fatalf("invocation failed: %v", err)
		}
		retry := del.round(1)
		if got := openbindings.ContextBearerToken(retry); got != "REAL-BEARER" {
			t.Fatalf("scoped bearer not provisioned: %q", got)
		}
		if _, present := retry["apiKey"]; present {
			t.Fatalf("least-privilege violation: delegate received apiKey %v — outside the challenge scope", retry["apiKey"])
		}
		if _, present := retry["headers"]; present {
			t.Fatalf("unrequested header context should be withheld, got %v", retry["headers"])
		}
	})

	t.Run("unguarded_path_leaks_per_call_credential", func(t *testing.T) {
		// Documents the gap the guard closes: with no scoping the caller's
		// unrelated per-call apiKey reaches the invoker on the retry.
		del := &fakeDelegate{assertedTarget: "https://api.service.com"}
		if _, err := drain(driveBinding(
			context.Background(), del.invoke, clone(perCall), nil, CLIContextResolver(), nil)); err != nil {
			t.Fatalf("invocation failed: %v", err)
		}
		if got, _ := del.round(1)["apiKey"].(string); got != "CALLER-UNRELATED-KEY" {
			t.Fatalf("expected the unguarded path to leak the caller's per-call apiKey, got %q", got)
		}
	})
}

// TestDeriveSourceTarget pins how ob derives a source's authoritative target
// independently of any invoker assertion — the anchor the confused-deputy
// defense compares against.
func TestDeriveSourceTarget(t *testing.T) {
	cases := []struct {
		name string
		src  InvokeSource
		want string
	}{
		{"https url → host", InvokeSource{Location: "https://api.example.com/openapi.json"}, "api.example.com"},
		{"http default port elided", InvokeSource{Location: "http://api.example.com:80/v1"}, "api.example.com"},
		{"non-default port kept", InvokeSource{Location: "https://api.example.com:8443/v1"}, "api.example.com:8443"},
		{"ws url → host", InvokeSource{Location: "wss://events.example.com/socket"}, "events.example.com"},
		{"host:port address", InvokeSource{Location: "grpc.example.com:50051"}, "grpc.example.com:50051"},
		{"inline content → unverifiable", InvokeSource{Content: openbindings.TextContent("...")}, ""},
		{"empty location → unverifiable", InvokeSource{}, ""},
		{"exec ref → unverifiable", InvokeSource{Location: "exec:some-tool binding invoke"}, ""},
		{"relative path → unverifiable", InvokeSource{Location: "./spec.yaml"}, ""},
		{"opaque scheme → unverifiable", InvokeSource{Location: "file:///tmp/spec.yaml"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveSourceTarget(tc.src); got != tc.want {
				t.Fatalf("deriveSourceTarget(%q/%v) = %q, want %q", tc.src.Location, tc.src.Content != nil, got, tc.want)
			}
		})
	}
}

// TestVetTarget pins the guard's three-way decision.
func TestVetTarget(t *testing.T) {
	g := &delegateProvisionGuard{authoritativeTarget: "api.example.com", sourceLabel: "https://api.example.com/x"}

	if provision, refusal := g.vetTarget("https://api.example.com"); !provision || refusal != nil {
		t.Fatalf("matching target must provision: provision=%v refusal=%v", provision, refusal)
	}
	if provision, refusal := g.vetTarget("https://api.other.com"); provision || refusal == nil {
		t.Fatalf("mismatched target must refuse: provision=%v refusal=%v", provision, refusal)
	} else if refusal.Code != openbindings.ErrCodePermissionDenied {
		t.Fatalf("refusal code = %q, want %q", refusal.Code, openbindings.ErrCodePermissionDenied)
	}

	unverifiable := &delegateProvisionGuard{authoritativeTarget: "", sourceLabel: "inline content"}
	if provision, refusal := unverifiable.vetTarget("https://anything.com"); provision || refusal != nil {
		t.Fatalf("unverifiable target must withhold silently: provision=%v refusal=%v", provision, refusal)
	}
}
