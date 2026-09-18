package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func roleOperationFixture() *openbindings.Interface {
	return &openbindings.Interface{
		OpenBindings: "0.2.0", Name: "Independent operation caller",
		Operations: map[string]openbindings.Operation{"echo": {Aliases: []string{"example.echo"}, Input: map[string]any{}, Output: map[string]any{}}},
		Sources:    map[string]openbindings.Source{"work": {BindingSpec: "example.work@1", Location: "https://work.example.invalid/service"}},
		Bindings:   map[string]openbindings.BindingEntry{"echo.work": {Operation: "echo", Source: "work", Selector: "echo"}},
	}
}

func roleOperationVerdicts(t *testing.T, value any, supported bool) any {
	t.Helper()
	raw, err := jsonvalue.Marshal(value)
	if err != nil {
		t.Error(err)
	}
	var request struct {
		BindingSpecs []string `json:"bindingSpecs"`
	}
	if err := jsonvalue.Unmarshal(raw, &request); err != nil {
		t.Error(err)
	}
	rows := make([]any, len(request.BindingSpecs))
	for i, token := range request.BindingSpecs {
		rows[i] = map[string]any{"bindingSpec": token, "supported": supported}
	}
	return rows
}

func roleOperationOutput(t *testing.T, ctx context.Context, run *ConfiguredInvocation) InvocationOutput {
	t.Helper()
	select {
	case event, ok := <-run.Events:
		if !ok {
			t.Fatal("missing output")
		}
		select {
		case _, ok := <-run.Events:
			if ok {
				t.Fatal("unexpected second output")
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return event
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return InvocationOutput{}
	}
}

func TestRoleOperationInvocationRetainsBatchSelection(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordered", true: "explicit"}[explicit], func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			record, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}})
			if err != nil {
				t.Fatal(err)
			}
			queries := &roleTestInvoker{result: func(selector string, value any) any {
				if selector != "provider.openbindings.binding-invoker.checkBindingSpecs" {
					t.Errorf("decoy selected: %s", selector)
				}
				want := []string{"example.other@1", "example.work@1"}
				if explicit {
					want = []string{"example.work@1"}
				}
				if equal, err := jsonvalue.Equal(value, map[string]any{"bindingSpecs": want}); err != nil || !equal {
					t.Errorf("not one token-only batch: %v", value)
				}
				if err := r.unregister(record.ID); err != nil {
					t.Error(err)
				}
				return roleOperationVerdicts(t, value, true)
			}}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work))
			iface := roleOperationFixture()
			iface.Sources["other"] = openbindings.Source{BindingSpec: "example.other@1", Content: json.RawMessage(`{}`)}
			iface.Bindings["echo.other"] = openbindings.BindingEntry{Operation: "echo", Source: "other", Selector: "other"}
			iface.Bindings["echo.duplicate"] = iface.Bindings["echo.work"]
			op, binding := "example.echo", ""
			if explicit {
				op, binding = "", "echo.work"
			}
			config := &InvokeConfig{Selection: []string{"missing", "echo.work"}, Configuration: map[string]any{"private": "caller-only"}}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			input := map[string]any{"n": json.Number("1e400"), "nested": []any{nil, json.Number("9007199254740993")}}
			run, err := invokeOnInterface(ctx, iface, op, binding, input, config)
			if err != nil {
				t.Fatal(err)
			}
			event := roleOperationOutput(t, ctx, run)
			if equal, err := jsonvalue.Equal(event.Output, input); event.Error != nil || err != nil || !equal {
				t.Fatalf("output: %+v", event)
			}
			if run.BindingKey != "echo.work" {
				t.Fatal(run.BindingKey)
			}
			if _, err := invokeOnInterface(ctx, iface, op, binding, input, config); err == nil {
				t.Fatal("fresh lookup retained removed provider")
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			if len(queries.calls) != 1 || len(work.selectors) != 1 || work.selectors[0] != "qualified-work" {
				t.Fatalf("query/work repeated or decoy: %v %v", queries.calls, work.selectors)
			}
			if equal, err := jsonvalue.Equal(work.opened[0].Context, config.context()); err != nil || !equal {
				t.Fatal("caller context changed")
			}
		})
	}
}

func TestRoleOperationInvocationNativeRanking(t *testing.T) {
	for _, name := range []string{"no-environment", "native-tie", "preferred-external"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if name != "no-environment" {
				r, _ := migrationTestRegistry(t)
				preference := json.RawMessage(`{"invoke":0}`)
				if name == "preferred-external" {
					preference = json.RawMessage(`{"invoke":1e400}`)
				}
				if _, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}, RolePreferences: preference}); err != nil {
					t.Fatal(err)
				}
			}
			queries := &roleTestInvoker{result: func(_ string, v any) any { return roleOperationVerdicts(t, v, true) }}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"native":true}`))
			}))
			defer server.Close()
			iface := roleOperationFixture()
			iface.Sources["work"] = openbindings.Source{BindingSpec: openapi.BindingSpecOpenAPI31, Content: json.RawMessage(`{"openapi":"3.1.0","info":{"title":"Native operation","version":"1"},"servers":[{"url":"` + server.URL + `"}],"paths":{"/echo":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`)}
			iface.Bindings["echo.work"] = openbindings.BindingEntry{Operation: "echo", Source: "work", Selector: "#/paths/~1echo/get"}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			input := map[string]any{}
			run, err := invokeOnInterface(ctx, iface, "echo", "", input, nil)
			if err != nil {
				t.Fatal(err)
			}
			event := roleOperationOutput(t, ctx, run)
			var want any = map[string]any{"native": true}
			wantNative, wantWork, wantQuery := int32(1), 0, 1
			if name == "no-environment" {
				wantQuery = 0
			}
			if name == "preferred-external" {
				want, wantNative, wantWork = input, 0, 1
			}
			if equal, err := jsonvalue.Equal(event.Output, want); event.Error != nil || err != nil || !equal {
				t.Fatalf("winner: %+v", event)
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			if requests.Load() != wantNative || len(work.selectors) != wantWork || len(queries.calls) != wantQuery {
				t.Fatalf("native=%d work=%v queries=%v", requests.Load(), work.selectors, queries.calls)
			}
		})
	}
}

func TestRoleOperationInvocationRefusals(t *testing.T) {
	for _, mode := range []string{"legacy", "corrupt", "malformed-support", "unsupported", "ambiguous", "wrong-role", "input-validation", "output-validation", "transforms", "malicious-target"} {
		t.Run(mode, func(t *testing.T) {
			r, unbound := migrationTestRegistry(t)
			provider := roleFrameProvider(t)
			roles := []string{"invoke"}
			if mode == "wrong-role" {
				base, _ := openbindings.ValidateDocument(provider)
				expected, _ := RequirementInterface(CapInspect)
				extra, _ := openbindings.ValidateDocument(roleTestProvider(t, expected))
				for key, op := range extra.Operations {
					base.Operations[key] = op
				}
				for key, b := range extra.Bindings {
					base.Bindings[key] = b
				}
				for key, s := range extra.Schemas {
					base.Schemas[key] = s
				}
				var err error
				provider, err = jsonvalue.Marshal(base)
				if err != nil {
					t.Fatal(err)
				}
				roles = []string{"inspect"}
			}
			if _, err := r.register(RoleRegistrationInput{Interface: provider, Roles: roles}); err != nil {
				t.Fatal(err)
			}
			if mode == "legacy" {
				seedLegacyMigration(t, r, unbound)
			}
			if mode == "corrupt" {
				if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), []byte(`{"delegateRegistry":null}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			queries := &roleTestInvoker{result: func(_ string, v any) any {
				if mode == "malformed-support" {
					return []any{map[string]any{"bindingSpec": "example.work@1"}}
				}
				return roleOperationVerdicts(t, v, mode != "unsupported")
			}}
			work := &roleFrameTestInvoker{}
			if mode == "malicious-target" {
				work.terminal = invoke.NewContextRequiredError(&invoke.ContextRequiredDetails{Target: "https://unrelated.example.invalid", Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{Type: "auth.bearer"}}}}})
			}
			engine := invoke.NewOperationInvoker(queries, work)
			var resolutions atomic.Int32
			engine.ContextResolver = func(context.Context, *invoke.ContextRequiredDetails) (map[string]any, error) {
				resolutions.Add(1)
				return map[string]any{"bearerToken": "must-not-leak"}, nil
			}
			installRoleFrameRuntime(t, engine)
			iface := roleOperationFixture()
			var input any = "value"
			if mode == "ambiguous" {
				iface.Bindings["echo.two"] = iface.Bindings["echo.work"]
			}
			if mode == "input-validation" || mode == "output-validation" || mode == "transforms" {
				op := iface.Operations["echo"]
				if mode == "input-validation" {
					op.Input = map[string]any{"type": "integer"}
				} else {
					op.Output = map[string]any{"type": "integer"}
				}
				iface.Operations["echo"] = op
				if mode == "transforms" {
					input = json.Number("1")
					b := iface.Bindings["echo.work"]
					b.InputTransform = &openbindings.TransformOrRef{Inline: "$ + 1"}
					b.OutputTransform = &openbindings.TransformOrRef{Inline: "$ + 1"}
					iface.Bindings["echo.work"] = b
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			run, err := invokeOnInterface(ctx, iface, "echo", "", input, nil)
			wantWork := 0
			switch mode {
			case "transforms", "output-validation", "malicious-target":
				if err != nil {
					t.Fatal(err)
				}
				event := roleOperationOutput(t, ctx, run)
				wantWork = 1
				if mode == "transforms" {
					if equal, e := jsonvalue.Equal(event.Output, json.Number("3")); event.Error != nil || e != nil || !equal {
						t.Fatalf("transforms not exactly once: %+v", event)
					}
				} else if event.Error == nil {
					t.Fatal("unsafe result accepted")
				} else if mode == "malicious-target" && event.Error.Code != errCodeDelegateTargetRefused {
					t.Fatalf("target refusal: %+v", event.Error)
				}
			default:
				if err == nil {
					t.Fatal("expected refusal")
				}
				if mode == "ambiguous" && !errors.Is(err, invoke.ErrBindingSelectionRequired) {
					t.Fatal(err)
				}
				if mode == "legacy" && !strings.Contains(err.Error(), "explicit migration") {
					t.Fatal(err)
				}
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			wantQueries := 1
			if mode == "legacy" || mode == "corrupt" || mode == "wrong-role" {
				wantQueries = 0
			}
			if len(queries.calls) != wantQueries || len(work.selectors) != wantWork || resolutions.Load() != 0 {
				t.Fatalf("unexpected disclosure/retry: queries=%v work=%v resolution=%d", queries.calls, work.selectors, resolutions.Load())
			}
		})
	}
}

// A batch can choose different registrations for different tokens without
// collapsing equal documents, conflating their overrides, or re-querying them.
func TestRoleOperationInvocationBatchRanksEachToken(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	provider := roleFrameProvider(t)
	ids := []string{}
	for _, preference := range []string{`{"invoke":1}`, `{"invoke":2}`} {
		record, err := r.register(RoleRegistrationInput{Interface: provider, Roles: []string{"invoke"}, RolePreferences: json.RawMessage(preference)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, record.ID)
	}
	high, zero := json.Number("1e400"), json.Number("0")
	if err := r.preferBindingSpec(ids[0], "invoke", "example.one@1", &high); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := r.preferBindingSpec(id, "invoke", "example.native@1", &zero); err != nil {
			t.Fatal(err)
		}
	}
	for _, policy := range []roleRoutingPath{roleRanked, roleNativeFirst} {
		t.Run(string(policy), func(t *testing.T) {
			handlers := map[string]*roleTestInvoker{}
			factory := func(candidate roleCandidate) (invoke.ProviderRuntime, invoke.RealizationSelector) {
				h := &roleTestInvoker{result: func(_ string, value any) any {
					input, err := decodeOutput[struct {
						BindingSpecs []string `json:"bindingSpecs"`
					}](value)
					if err != nil {
						t.Error(err)
					}
					return openbindings.CheckBindingSpecs(input.BindingSpecs, []openbindings.BindingSpecInfo{{BindingSpec: "example.one@1"}, {BindingSpec: "example.two@1"}, {BindingSpec: "example.native@1"}})
				}}
				handlers[candidate.Record.ID] = h
				return invoke.NewOperationInvoker(h, &roleFrameTestInvoker{}), nil
			}
			tokens := []string{"example.one@1", "example.two@1", "example.one@1", "example.native@1", "example.unknown@1"}
			selected, err := selectRoleRuntimes(t.Context(), r, CapInvoke, tokens, policy, func(token string) bool { return token == "example.native@1" }, factory)
			if err != nil {
				t.Fatal(err)
			}
			for token, id := range map[string]string{"example.one@1": ids[0], "example.two@1": ids[1]} {
				if selected[token] == nil || selected[token].Builtin || selected[token].Runtime.candidate.Record.ID != id {
					t.Fatalf("wrong token winner for %s", token)
				}
			}
			if selected["example.native@1"] == nil || !selected["example.native@1"].Builtin || selected["example.unknown@1"] != nil {
				t.Fatal("native tie or explicit false changed")
			}
			if selected["example.one@1"].Work.ProviderKey == selected["example.two@1"].Work.ProviderKey {
				t.Fatal("equal documents collapsed registration identity")
			}
			want := []string{"example.one@1", "example.two@1", "example.native@1", "example.unknown@1"}
			if policy == roleNativeFirst {
				want = []string{"example.one@1", "example.two@1", "example.unknown@1"}
			}
			for _, h := range handlers {
				h.mu.Lock()
				if len(h.calls) != 1 || len(h.inputs) != 1 {
					t.Errorf("repeated assessment: %v", h.calls)
				} else if equal, e := jsonvalue.Equal(h.inputs[0], map[string]any{"bindingSpecs": want}); e != nil || !equal {
					t.Errorf("wrong assessment: %v", h.inputs)
				}
				h.mu.Unlock()
			}
		})
	}
}

func TestRoleOperationInvocationPinnedMintNeverDelegates(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	if _, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}, RolePreferences: json.RawMessage(`{"invoke":1e400}`)}); err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(_ string, value any) any { return roleOperationVerdicts(t, value, true) }}
	work := &roleFrameTestInvoker{}
	engine := invoke.NewOperationInvoker(queries, work, openapi.NewAdapter())
	engine.TransformEvaluator = &jsonataEvaluator{}
	installRoleFrameRuntime(t, engine)
	hits := 0
	server := mintServer(t, "pinned-token", &hits, "durable-pinned-credential")
	defer server.Close()
	provider := providerOBI(t, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	minted, err := mintFromPinnedProvider(ctx, provider, "durable-pinned-credential")
	queries.mu.Lock()
	defer queries.mu.Unlock()
	work.mu.Lock()
	defer work.mu.Unlock()
	if len(queries.calls) != 0 || len(work.selectors) != 0 {
		t.Errorf("pinned mint consulted delegates: queries=%d work=%d", len(queries.calls), len(work.selectors))
	}
	if err != nil || minted == nil || minted.accessToken != "pinned-token" || hits != 1 {
		t.Fatalf("pinned provider not used: token=%v err=%v hits=%d", minted, err, hits)
	}
}
