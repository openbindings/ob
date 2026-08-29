package delegates

import "testing"

func TestSupportsFormat_ExactIdentifier(t *testing.T) {
	// Identifiers are exact and opaque (core §6): matching is string
	// equality — no family-permissive or version-range matching survives.
	tests := []struct {
		delegate string
		req      string
		want     bool
	}{
		{"openbindings.usage@1", "openbindings.usage@1", true},
		{"openbindings.openapi-3.1@1", "openbindings.usage@1", false},
		{"usage", "openbindings.usage@1", false},
		{"usage@^2.0.0", "usage@2.1.0", false},
	}
	for _, tt := range tests {
		if got := SupportsFormat(tt.delegate, tt.req); got != tt.want {
			t.Errorf("SupportsFormat(%q, %q) = %v, want %v", tt.delegate, tt.req, got, tt.want)
		}
	}
}
