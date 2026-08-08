package cmd

import "testing"

func TestRootUsesCanonicalTagline(t *testing.T) {
	const want = "openbindings: One interface. Any binding."
	if got := NewRoot().Short; got != want {
		t.Fatalf("root summary = %q, want %q", got, want)
	}
}
