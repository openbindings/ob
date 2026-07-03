package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveToken_FlagWins(t *testing.T) {
	token, err := resolveToken("flag-token", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "flag-token" {
		t.Errorf("expected flag-token, got %q", token)
	}
}

func TestResolveToken_FileRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := resolveToken("", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "file-token" {
		t.Errorf("expected file-token, got %q", token)
	}
}

func TestResolveToken_UnreadableFileErrors(t *testing.T) {
	// An unreadable --token-file must error, not silently fall through: the
	// caller asked for that token, and proceeding without it would fail later
	// with an opaque 401.
	_, err := resolveToken("", "/nonexistent/path/token")
	if err == nil {
		t.Error("expected error for unreadable token file")
	}
}
