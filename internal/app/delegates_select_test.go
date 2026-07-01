package app

import "testing"

func TestSelectDelegateFrom(t *testing.T) {
	self := delegateCandidate{
		name: "ob", builtin: true,
		capabilities: []DelegateCapability{CapInvoke, CapSynthesize, CapInspect},
		formats:      []DelegateFormatInfo{{Format: "openapi@3.1"}, {Format: "grpc"}},
	}
	extInvoke := delegateCandidate{
		name: "x", location: "exec:x",
		capabilities: []DelegateCapability{CapInvoke},
		formats:      []DelegateFormatInfo{{Format: "grpc"}},
	}
	extCreate := delegateCandidate{
		name: "y", location: "exec:y",
		capabilities: []DelegateCapability{CapSynthesize},
		formats:      []DelegateFormatInfo{{Format: "thrift"}},
	}

	t.Run("native format goes to self even when an external also handles it", func(t *testing.T) {
		got := selectDelegateFrom([]delegateCandidate{self, extInvoke}, CapInvoke, "grpc")
		if got == nil || !got.builtin {
			t.Fatalf("expected self (builtin) for invoke/grpc on a tie, got %+v", got)
		}
	})

	t.Run("higher preference beats the builtin tie-break", func(t *testing.T) {
		preferred := extInvoke
		preferred.preference = 5 // user prefers the external for invoke
		got := selectDelegateFrom([]delegateCandidate{self, preferred}, CapInvoke, "grpc")
		if got == nil || got.builtin {
			t.Fatalf("expected the higher-preference external, got %+v", got)
		}
	})

	t.Run("capability filter: only the synthesize-capable external handles thrift synthesize", func(t *testing.T) {
		got := selectDelegateFrom([]delegateCandidate{self, extInvoke, extCreate}, CapSynthesize, "thrift")
		if got == nil || got.name != "y" {
			t.Fatalf("expected the create delegate y for create/thrift, got %+v", got)
		}
	})

	t.Run("no candidate handles the format", func(t *testing.T) {
		if got := selectDelegateFrom([]delegateCandidate{self, extInvoke}, CapInvoke, "cobol"); got != nil {
			t.Fatalf("expected nil when nothing handles the format, got %+v", got)
		}
	})

	t.Run("capability present but format absent yields nil", func(t *testing.T) {
		// extCreate can create, but only thrift — not openapi.
		if got := selectDelegateFrom([]delegateCandidate{extCreate}, CapSynthesize, "openapi@3.1"); got != nil {
			t.Fatalf("expected nil (synthesize-capable but wrong format), got %+v", got)
		}
	})

	t.Run("per-offering preference: X for create, Y for invoke", func(t *testing.T) {
		// One delegate does both create and invoke for grpc, with per-offering
		// preferences that route create to X and invoke to Y.
		x := delegateCandidate{
			name: "x", location: "exec:x",
			capabilities: []DelegateCapability{CapSynthesize, CapInvoke},
			formats:      []DelegateFormatInfo{{Format: "grpc"}},
			perOffering: []OfferingPreference{
				{Capability: CapSynthesize, Preference: 10}, // prefer X for create
			},
		}
		y := delegateCandidate{
			name: "y", location: "exec:y",
			capabilities: []DelegateCapability{CapSynthesize, CapInvoke},
			formats:      []DelegateFormatInfo{{Format: "grpc"}},
			perOffering: []OfferingPreference{
				{Capability: CapInvoke, Preference: 10}, // prefer Y for invoke
			},
		}
		set := []delegateCandidate{x, y}
		if got := selectDelegateFrom(set, CapSynthesize, "grpc"); got == nil || got.name != "x" {
			t.Errorf("create/grpc should route to X, got %+v", got)
		}
		if got := selectDelegateFrom(set, CapInvoke, "grpc"); got == nil || got.name != "y" {
			t.Errorf("invoke/grpc should route to Y, got %+v", got)
		}
	})

	t.Run("format-specific override beats capability-only", func(t *testing.T) {
		c := delegateCandidate{
			name: "z", location: "exec:z", preference: 1,
			capabilities: []DelegateCapability{CapInvoke},
			formats:      []DelegateFormatInfo{{Format: "grpc"}},
			perOffering: []OfferingPreference{
				{Capability: CapInvoke, Preference: 3},                 // capability-only
				{Capability: CapInvoke, Format: "grpc", Preference: 9}, // more specific
			},
		}
		if got := c.effectivePreference(CapInvoke, "grpc"); got != 9 {
			t.Errorf("expected the format-specific override (9), got %v", got)
		}
		if got := c.effectivePreference(CapInvoke, "openapi"); got != 3 {
			t.Errorf("expected the capability-only override (3) for a non-grpc format, got %v", got)
		}
		if got := c.effectivePreference(CapSynthesize, "grpc"); got != 1 {
			t.Errorf("expected the delegate-level preference (1) when no offering matches, got %v", got)
		}
	})
}
