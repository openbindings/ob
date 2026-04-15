package cmd

import (
	"context"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestTokenOverlayStore_Get_OverlayWins(t *testing.T) {
	inner := openbindings.NewMemoryStore()
	inner.Set(context.Background(), "http://example.com", map[string]any{"bearerToken": "inner-token"})

	overlay := &tokenOverlayStore{
		inner: inner,
		creds: map[string]map[string]any{
			"http://example.com": {"bearerToken": "overlay-token"},
		},
	}

	cred, err := overlay.Get(context.Background(), "http://example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cred["bearerToken"] != "overlay-token" {
		t.Errorf("expected overlay-token, got %v", cred["bearerToken"])
	}
}

func TestTokenOverlayStore_Get_FallsThrough(t *testing.T) {
	inner := openbindings.NewMemoryStore()
	inner.Set(context.Background(), "http://other.com", map[string]any{"bearerToken": "inner-token"})

	overlay := &tokenOverlayStore{
		inner: inner,
		creds: map[string]map[string]any{
			"http://example.com": {"bearerToken": "overlay-token"},
		},
	}

	cred, err := overlay.Get(context.Background(), "http://other.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cred["bearerToken"] != "inner-token" {
		t.Errorf("expected inner-token, got %v", cred["bearerToken"])
	}
}

func TestTokenOverlayStore_Get_NoInner(t *testing.T) {
	overlay := &tokenOverlayStore{
		inner: nil,
		creds: map[string]map[string]any{
			"http://example.com": {"bearerToken": "token"},
		},
	}

	cred, err := overlay.Get(context.Background(), "http://example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cred["bearerToken"] != "token" {
		t.Errorf("expected token, got %v", cred["bearerToken"])
	}

	// Miss with no inner should return nil.
	cred, err = overlay.Get(context.Background(), "http://other.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cred != nil {
		t.Errorf("expected nil for miss, got %v", cred)
	}
}

func TestTokenOverlayStore_Set_PassesThrough(t *testing.T) {
	inner := openbindings.NewMemoryStore()
	overlay := &tokenOverlayStore{inner: inner, creds: map[string]map[string]any{}}

	err := overlay.Set(context.Background(), "http://example.com", map[string]any{"apiKey": "k"})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	cred, _ := inner.Get(context.Background(), "http://example.com")
	if cred["apiKey"] != "k" {
		t.Errorf("Set did not pass through to inner store")
	}
}

func TestTokenOverlayStore_Delete_PassesThrough(t *testing.T) {
	inner := openbindings.NewMemoryStore()
	inner.Set(context.Background(), "http://example.com", map[string]any{"token": "x"})

	overlay := &tokenOverlayStore{inner: inner, creds: map[string]map[string]any{}}
	overlay.Delete(context.Background(), "http://example.com")

	cred, _ := inner.Get(context.Background(), "http://example.com")
	if cred != nil {
		t.Error("Delete did not pass through")
	}
}

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
