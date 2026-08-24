package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func prefOf(v float64) *float64 { return &v }

func TestSelectDelegateFrom(t *testing.T) {
	cand := func(rec DelegateRecord, builtin bool) delegateCandidate {
		return delegateCandidate{
			record:  rec,
			builtin: builtin,
			supportCheck: func(_ context.Context, _ DelegateCapability, bindingSpecs []string) ([]openbindings.BindingSpecVerdict, error) {
				infos := make([]openbindings.BindingSpecInfo, 0, len(rec.BindingSpecs))
				for _, info := range rec.BindingSpecs {
					infos = append(infos, openbindings.BindingSpecInfo{BindingSpec: info.BindingSpec})
				}
				return openbindings.CheckBindingSpecs(bindingSpecs, infos), nil
			},
		}
	}
	selectOne := func(candidates []delegateCandidate, cap DelegateCapability, bindingSpec string) *delegateCandidate {
		t.Helper()
		got, err := selectDelegateFrom(t.Context(), candidates, cap, bindingSpec)
		if err != nil {
			t.Fatalf("select delegate: %v", err)
		}
		return got
	}
	self := cand(DelegateRecord{
		Location: SelfDelegateLocation, Name: "ob",
		Capabilities: []DelegateCapability{CapInvoke, CapSynthesize, CapInspect},
		BindingSpecs: []DelegateBindingSpecInfo{{BindingSpec: "openbindings.openapi@1"}, {BindingSpec: "grpc"}},
	}, true)
	extInvoke := cand(DelegateRecord{
		Location: "exec:x", Name: "x",
		Capabilities: []DelegateCapability{CapInvoke},
		BindingSpecs: []DelegateBindingSpecInfo{{BindingSpec: "grpc"}},
	}, false)
	extSynth := cand(DelegateRecord{
		Location: "exec:y", Name: "y",
		Capabilities: []DelegateCapability{CapSynthesize},
		BindingSpecs: []DelegateBindingSpecInfo{{BindingSpec: "thrift"}},
	}, false)

	invokeOp := capabilityOperation[CapInvoke]
	synthOp := capabilityOperation[CapSynthesize]

	t.Run("native format goes to self even when an external also handles it", func(t *testing.T) {
		got := selectOne([]delegateCandidate{self, extInvoke}, CapInvoke, "grpc")
		if got == nil || !got.builtin {
			t.Fatalf("expected self (builtin) for invoke/grpc on a tie, got %+v", got)
		}
	})

	t.Run("higher preference beats the builtin tie-break", func(t *testing.T) {
		preferred := extInvoke
		preferred.record.Preference = prefOf(5) // user prefers the external for invoke
		got := selectOne([]delegateCandidate{self, preferred}, CapInvoke, "grpc")
		if got == nil || got.builtin {
			t.Fatalf("expected the higher-preference external, got %+v", got)
		}
	})

	t.Run("capability filter: only the synthesize-capable external handles thrift synthesize", func(t *testing.T) {
		got := selectOne([]delegateCandidate{self, extInvoke, extSynth}, CapSynthesize, "thrift")
		if got == nil || got.name() != "y" {
			t.Fatalf("expected the synthesize delegate y for synthesize/thrift, got %+v", got)
		}
	})

	t.Run("no candidate handles the format", func(t *testing.T) {
		if got := selectOne([]delegateCandidate{self, extInvoke}, CapInvoke, "cobol"); got != nil {
			t.Fatalf("expected nil when nothing handles the format, got %+v", got)
		}
	})

	t.Run("capability present but format absent yields nil", func(t *testing.T) {
		// extSynth can synthesize, but only thrift — not openapi.
		if got := selectOne([]delegateCandidate{extSynth}, CapSynthesize, "openbindings.openapi@1"); got != nil {
			t.Fatalf("expected nil (synthesize-capable but wrong format), got %+v", got)
		}
	})

	t.Run("live warrant can support a token omitted from the advisory listing", func(t *testing.T) {
		hidden := cand(DelegateRecord{
			Location: "exec:hidden", Name: "hidden",
			Capabilities: []DelegateCapability{CapInvoke},
		}, false)
		hidden.supportCheck = func(_ context.Context, _ DelegateCapability, bindingSpecs []string) ([]openbindings.BindingSpecVerdict, error) {
			return openbindings.CheckBindingSpecs(bindingSpecs, []openbindings.BindingSpecInfo{{BindingSpec: "hidden@1"}}), nil
		}
		got := selectOne([]delegateCandidate{hidden}, CapInvoke, "hidden@1")
		if got == nil || got.name() != "hidden" {
			t.Fatalf("expected live support warrant to select hidden delegate, got %+v", got)
		}
	})

	t.Run("advisory presence cannot override a live refusal", func(t *testing.T) {
		refusing := cand(DelegateRecord{
			Location: "exec:refusing", Name: "refusing",
			Capabilities: []DelegateCapability{CapInvoke},
			BindingSpecs: []DelegateBindingSpecInfo{{BindingSpec: "listed@1"}},
		}, false)
		refusing.supportCheck = func(_ context.Context, _ DelegateCapability, bindingSpecs []string) ([]openbindings.BindingSpecVerdict, error) {
			return openbindings.CheckBindingSpecs(bindingSpecs, nil), nil
		}
		if got := selectOne([]delegateCandidate{refusing}, CapInvoke, "listed@1"); got != nil {
			t.Fatalf("expected live refusal to prevent selection, got %+v", got)
		}
	})

	t.Run("per-operation preference: X for synthesize, Y for invoke", func(t *testing.T) {
		// Both delegates synthesize and invoke grpc; the operation-scoped
		// preference index routes synthesize to X and invoke to Y.
		x := cand(DelegateRecord{
			Location: "exec:x", Name: "x",
			Capabilities:         []DelegateCapability{CapSynthesize, CapInvoke},
			BindingSpecs:         []DelegateBindingSpecInfo{{BindingSpec: "grpc"}},
			OperationPreferences: map[string]float64{synthOp: 10},
		}, false)
		y := cand(DelegateRecord{
			Location: "exec:y", Name: "y",
			Capabilities:         []DelegateCapability{CapSynthesize, CapInvoke},
			BindingSpecs:         []DelegateBindingSpecInfo{{BindingSpec: "grpc"}},
			OperationPreferences: map[string]float64{invokeOp: 10},
		}, false)
		set := []delegateCandidate{x, y}
		if got := selectOne(set, CapSynthesize, "grpc"); got == nil || got.name() != "x" {
			t.Errorf("synthesize/grpc should route to X, got %+v", got)
		}
		if got := selectOne(set, CapInvoke, "grpc"); got == nil || got.name() != "y" {
			t.Errorf("invoke/grpc should route to Y, got %+v", got)
		}
	})

	t.Run("format-scoped entry beats operation entry beats delegate-level", func(t *testing.T) {
		c := cand(DelegateRecord{
			Location: "exec:z", Name: "z", Preference: prefOf(1),
			Capabilities:           []DelegateCapability{CapInvoke},
			BindingSpecs:           []DelegateBindingSpecInfo{{BindingSpec: "grpc"}},
			OperationPreferences:   map[string]float64{invokeOp: 3},
			BindingSpecPreferences: []BindingSpecPreference{{Operation: invokeOp, BindingSpec: "grpc", Preference: 9}},
		}, false)
		if got := c.effectivePreference(CapInvoke, "grpc"); got != 9 {
			t.Errorf("expected the format-scoped entry (9), got %v", got)
		}
		if got := c.effectivePreference(CapInvoke, "openapi"); got != 3 {
			t.Errorf("expected the operation entry (3) for a non-grpc format, got %v", got)
		}
		if got := c.effectivePreference(CapSynthesize, "grpc"); got != 1 {
			t.Errorf("expected the delegate-level preference (1) when no entry matches, got %v", got)
		}
	})
}

func TestAvailableBindingSpecsBatchesOneCallPerDelegate(t *testing.T) {
	type observation struct {
		calls int
		input []string
	}
	first, second := &observation{}, &observation{}
	candidate := func(name string, seen *observation, supported string) delegateCandidate {
		return delegateCandidate{
			record: DelegateRecord{
				Location:     "exec:" + name,
				Name:         name,
				Capabilities: []DelegateCapability{CapInvoke},
			},
			supportCheck: func(_ context.Context, _ DelegateCapability, bindingSpecs []string) ([]openbindings.BindingSpecVerdict, error) {
				seen.calls++
				seen.input = append([]string(nil), bindingSpecs...)
				return openbindings.CheckBindingSpecs(bindingSpecs, []openbindings.BindingSpecInfo{{BindingSpec: supported}}), nil
			},
		}
	}

	got, err := availableBindingSpecs(t.Context(), []delegateCandidate{
		candidate("first", first, "a@1"),
		candidate("second", second, "b@1"),
	}, CapInvoke, []string{"b@1", "a@1", "b@1", "c@1"})
	if err != nil {
		t.Fatal(err)
	}
	wantInput := []string{"b@1", "a@1", "c@1"}
	for name, seen := range map[string]*observation{"first": first, "second": second} {
		if seen.calls != 1 {
			t.Errorf("%s delegate calls = %d, want 1", name, seen.calls)
		}
		if !slices.Equal(seen.input, wantInput) {
			t.Errorf("%s delegate input = %v, want one deduplicated batch %v", name, seen.input, wantInput)
		}
	}
	if !got["a@1"] || !got["b@1"] || got["c@1"] {
		t.Errorf("support union = %v, want a@1 and b@1 only", got)
	}
}

func TestDelegateMissingSupportQueryFailsLoudly(t *testing.T) {
	candidate := delegateCandidate{
		record: DelegateRecord{
			Location:     "exec:legacy",
			Name:         "legacy",
			Capabilities: []DelegateCapability{CapInvoke},
		},
		iface: &openbindings.Interface{Operations: map[string]openbindings.Operation{}},
	}
	_, err := candidate.checkBindingSpecs(t.Context(), CapInvoke, []string{"a@1"})
	if err == nil {
		t.Fatal("expected a delegate without checkBindingSpecs to fail")
	}
	for _, want := range []string{"registered without checkBindingSpecs", "re-register", "ob delegate register exec:legacy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
