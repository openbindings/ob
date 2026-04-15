package cmd

import (
	"testing"
)

func TestParseCurlCommand_BearerToken(t *testing.T) {
	result := parseCurlCommand(`curl -H "Authorization: Bearer ghp_abc123" https://api.github.com`)
	if result.bearerToken != "ghp_abc123" {
		t.Errorf("expected bearer token ghp_abc123, got %q", result.bearerToken)
	}
}

func TestParseCurlCommand_BasicAuth(t *testing.T) {
	result := parseCurlCommand(`curl -u admin:secret https://api.example.com`)
	if result.basic == nil {
		t.Fatal("expected basic auth")
	}
	user, uok := result.basic["username"].(string)
	pass, pok := result.basic["password"].(string)
	if !uok || !pok || user != "admin" || pass != "secret" {
		t.Errorf("basic auth mismatch: %+v", result.basic)
	}
}

func TestParseCurlCommand_Headers(t *testing.T) {
	result := parseCurlCommand(`curl -H "Accept: application/json" -H "X-Custom: value" https://example.com`)
	if result.headers["Accept"] != "application/json" {
		t.Errorf("Accept header: %q", result.headers["Accept"])
	}
	if result.headers["X-Custom"] != "value" {
		t.Errorf("X-Custom header: %q", result.headers["X-Custom"])
	}
}

func TestParseCurlCommand_Cookies(t *testing.T) {
	result := parseCurlCommand(`curl -b "session=abc123; token=xyz" https://example.com`)
	if result.cookies["session"] != "abc123" {
		t.Errorf("session cookie: %q", result.cookies["session"])
	}
	if result.cookies["token"] != "xyz" {
		t.Errorf("token cookie: %q", result.cookies["token"])
	}
}

func TestParseCurlCommand_CookieHeader(t *testing.T) {
	result := parseCurlCommand(`curl -H "Cookie: sid=val1; auth=val2" https://example.com`)
	if result.cookies["sid"] != "val1" {
		t.Errorf("sid cookie: %q", result.cookies["sid"])
	}
	if result.cookies["auth"] != "val2" {
		t.Errorf("auth cookie: %q", result.cookies["auth"])
	}
}

func TestParseCurlCommand_SingleQuotedArgs(t *testing.T) {
	result := parseCurlCommand(`curl -H 'Authorization: Bearer tok_123' https://example.com`)
	if result.bearerToken != "tok_123" {
		t.Errorf("expected bearer token tok_123, got %q", result.bearerToken)
	}
}

func TestParseCurlCommand_IrrelevantFlagsIgnored(t *testing.T) {
	result := parseCurlCommand(`curl -X POST -d '{"key":"val"}' -H "Accept: application/json" https://example.com`)
	if result.headers["Accept"] != "application/json" {
		t.Errorf("Accept header: %q", result.headers["Accept"])
	}
	if result.bearerToken != "" {
		t.Errorf("unexpected bearer token: %q", result.bearerToken)
	}
}

func TestParseCurlCommand_Empty(t *testing.T) {
	result := parseCurlCommand("")
	if result.bearerToken != "" || result.basic != nil || result.headers != nil || result.cookies != nil {
		t.Errorf("empty command should produce empty result: %+v", result)
	}
}

func TestTokenizeCurl(t *testing.T) {
	tokens := tokenizeCurl(`curl -H "Authorization: Bearer tok" -b 'k=v' https://example.com`)
	expected := []string{"curl", "-H", "Authorization: Bearer tok", "-b", "k=v", "https://example.com"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}
