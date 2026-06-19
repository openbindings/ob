package app

import "testing"

func TestSelectDelegateFrom(t *testing.T) {
	self := delegateCandidate{
		name: "ob", builtin: true,
		capabilities: []DelegateCapability{CapInvoke, CapCreate, CapInspect},
		formats:      []DelegateFormatInfo{{Format: "openapi@3.1"}, {Format: "grpc"}},
	}
	extInvoke := delegateCandidate{
		name: "x", location: "exec:x",
		capabilities: []DelegateCapability{CapInvoke},
		formats:      []DelegateFormatInfo{{Format: "grpc"}},
	}
	extCreate := delegateCandidate{
		name: "y", location: "exec:y",
		capabilities: []DelegateCapability{CapCreate},
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

	t.Run("capability filter: only the create-capable external handles thrift create", func(t *testing.T) {
		got := selectDelegateFrom([]delegateCandidate{self, extInvoke, extCreate}, CapCreate, "thrift")
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
		if got := selectDelegateFrom([]delegateCandidate{extCreate}, CapCreate, "openapi@3.1"); got != nil {
			t.Fatalf("expected nil (create-capable but wrong format), got %+v", got)
		}
	})
}
