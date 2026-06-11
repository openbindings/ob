package cmd

import "testing"

func TestResolveToken_FlagWins(t *testing.T) {
	token := resolveToken("flag-token", "")
	if token != "flag-token" {
		t.Errorf("expected flag-token, got %q", token)
	}
}

func TestResolveToken_FileReadsFallback(t *testing.T) {
	// Non-existent file falls through to empty.
	token := resolveToken("", "/nonexistent/path/token")
	if token != "" {
		t.Errorf("expected empty for bad file, got %q", token)
	}
}
