package app

import "testing"

func prefOf(v float64) *float64 { return &v }

func TestSelectDelegateFrom(t *testing.T) {
	cand := func(rec DelegateRecord, builtin bool) delegateCandidate {
		return delegateCandidate{record: rec, builtin: builtin}
	}
	self := cand(DelegateRecord{
		Location: SelfDelegateLocation, Name: "ob",
		Capabilities: []DelegateCapability{CapInvoke, CapSynthesize, CapInspect},
		Formats:      []DelegateFormatInfo{{Format: "openapi@3.1"}, {Format: "grpc"}},
	}, true)
	extInvoke := cand(DelegateRecord{
		Location: "exec:x", Name: "x",
		Capabilities: []DelegateCapability{CapInvoke},
		Formats:      []DelegateFormatInfo{{Format: "grpc"}},
	}, false)
	extSynth := cand(DelegateRecord{
		Location: "exec:y", Name: "y",
		Capabilities: []DelegateCapability{CapSynthesize},
		Formats:      []DelegateFormatInfo{{Format: "thrift"}},
	}, false)

	invokeOp := capabilityOperation[CapInvoke]
	synthOp := capabilityOperation[CapSynthesize]

	t.Run("native format goes to self even when an external also handles it", func(t *testing.T) {
		got := selectDelegateFrom([]delegateCandidate{self, extInvoke}, CapInvoke, "grpc")
		if got == nil || !got.builtin {
			t.Fatalf("expected self (builtin) for invoke/grpc on a tie, got %+v", got)
		}
	})

	t.Run("higher preference beats the builtin tie-break", func(t *testing.T) {
		preferred := extInvoke
		preferred.record.Preference = prefOf(5) // user prefers the external for invoke
		got := selectDelegateFrom([]delegateCandidate{self, preferred}, CapInvoke, "grpc")
		if got == nil || got.builtin {
			t.Fatalf("expected the higher-preference external, got %+v", got)
		}
	})

	t.Run("capability filter: only the synthesize-capable external handles thrift synthesize", func(t *testing.T) {
		got := selectDelegateFrom([]delegateCandidate{self, extInvoke, extSynth}, CapSynthesize, "thrift")
		if got == nil || got.name() != "y" {
			t.Fatalf("expected the synthesize delegate y for synthesize/thrift, got %+v", got)
		}
	})

	t.Run("no candidate handles the format", func(t *testing.T) {
		if got := selectDelegateFrom([]delegateCandidate{self, extInvoke}, CapInvoke, "cobol"); got != nil {
			t.Fatalf("expected nil when nothing handles the format, got %+v", got)
		}
	})

	t.Run("capability present but format absent yields nil", func(t *testing.T) {
		// extSynth can synthesize, but only thrift — not openapi.
		if got := selectDelegateFrom([]delegateCandidate{extSynth}, CapSynthesize, "openapi@3.1"); got != nil {
			t.Fatalf("expected nil (synthesize-capable but wrong format), got %+v", got)
		}
	})

	t.Run("per-operation preference: X for synthesize, Y for invoke", func(t *testing.T) {
		// Both delegates synthesize and invoke grpc; the operation-scoped
		// preference index routes synthesize to X and invoke to Y.
		x := cand(DelegateRecord{
			Location: "exec:x", Name: "x",
			Capabilities:         []DelegateCapability{CapSynthesize, CapInvoke},
			Formats:              []DelegateFormatInfo{{Format: "grpc"}},
			OperationPreferences: map[string]float64{synthOp: 10},
		}, false)
		y := cand(DelegateRecord{
			Location: "exec:y", Name: "y",
			Capabilities:         []DelegateCapability{CapSynthesize, CapInvoke},
			Formats:              []DelegateFormatInfo{{Format: "grpc"}},
			OperationPreferences: map[string]float64{invokeOp: 10},
		}, false)
		set := []delegateCandidate{x, y}
		if got := selectDelegateFrom(set, CapSynthesize, "grpc"); got == nil || got.name() != "x" {
			t.Errorf("synthesize/grpc should route to X, got %+v", got)
		}
		if got := selectDelegateFrom(set, CapInvoke, "grpc"); got == nil || got.name() != "y" {
			t.Errorf("invoke/grpc should route to Y, got %+v", got)
		}
	})

	t.Run("format-scoped entry beats operation entry beats delegate-level", func(t *testing.T) {
		c := cand(DelegateRecord{
			Location: "exec:z", Name: "z", Preference: prefOf(1),
			Capabilities:         []DelegateCapability{CapInvoke},
			Formats:              []DelegateFormatInfo{{Format: "grpc"}},
			OperationPreferences: map[string]float64{invokeOp: 3},
			FormatPreferences:    []FormatPreference{{Operation: invokeOp, Format: "grpc", Preference: 9}},
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
