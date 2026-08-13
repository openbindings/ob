package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func platformBoolPointer(value bool) *bool { return &value }

func TestDurableSubsetIsPositiveAndOptIn(t *testing.T) {
	candidate := map[string]any{
		"bearerToken": "one-shot",
		"apiKey":      "reusable",
		"unrelated":   "must-not-persist",
	}
	alt := openbindings.ContextAlternative{Requirements: []openbindings.ContextRequirement{
		{Type: "auth.bearer"},
		{Type: "auth.apiKey", Durable: platformBoolPointer(true)},
	}}
	got := durableSubset(alt, candidate)
	if len(got) != 1 || got["apiKey"] != "reusable" {
		t.Fatalf("durable subset = %#v", got)
	}
}

func TestPromptedNamedCredentialsCanSatisfyAnANDSet(t *testing.T) {
	ctx := map[string]any{}
	first := openbindings.ContextRequirement{Type: "auth.apiKey", Name: "headerKey"}
	second := openbindings.ContextRequirement{Type: "auth.apiKey", Name: "queryKey"}
	setPromptedCredential(ctx, first, "apiKey", "key-h")
	setPromptedCredential(ctx, second, "apiKey", "key-q")
	details := &openbindings.ContextRequiredDetails{
		Target: "api.example.com",
		Alternatives: []openbindings.ContextAlternative{{Requirements: []openbindings.ContextRequirement{
			first, second,
		}}},
	}
	if !openbindings.ContextSatisfies(ctx, details) {
		t.Fatalf("named context does not satisfy challenge: %#v", ctx)
	}
}

func TestDurableSubsetScopesNamedCredentials(t *testing.T) {
	candidate := map[string]any{"credentials": map[string]any{
		"oneShot":  "temporary",
		"reusable": "stored",
	}}
	alt := openbindings.ContextAlternative{Requirements: []openbindings.ContextRequirement{
		{Type: "auth.bearer", Name: "oneShot"},
		{Type: "auth.bearer", Name: "reusable", Durable: platformBoolPointer(true)},
	}}
	got := durableSubset(alt, candidate)
	credentials, _ := got["credentials"].(map[string]any)
	if len(credentials) != 1 || credentials["reusable"] != "stored" {
		t.Fatalf("durable named subset = %#v", got)
	}
}
