package delegates

import "testing"

func TestSupportsFormat_SemverExactAndCaret(t *testing.T) {
	tests := []struct {
		delegate string
		req      string
		want     bool
	}{
		{"usage@2.0.0", "usage@2.0.0", true},
		{"usage@2.0.0", "usage@2.0.1", false},
		{"usage@^2.0.0", "usage@2.1.0", true},
		{"usage@^2.0.0", "usage@3.0.0", false},
		{"openapi@^3.0.0", "usage@2.1.0", false},
		{"usage", "usage@999.0.0", true}, // name-only = permissive
	}
	for _, tt := range tests {
		if got := SupportsFormat(tt.delegate, tt.req); got != tt.want {
			t.Errorf("SupportsFormat(%q, %q) = %v, want %v", tt.delegate, tt.req, got, tt.want)
		}
	}
}
