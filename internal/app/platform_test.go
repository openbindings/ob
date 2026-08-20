package app

import (
	"testing"

	"github.com/openbindings/openbindings-go/invoke"
)

func platformBoolPointer(value bool) *bool { return &value }

func TestDurableSubsetIsPositiveAndOptIn(t *testing.T) {
	candidate := map[string]any{
		"bearerToken": "one-shot",
		"apiKey":      "reusable",
		"unrelated":   "must-not-persist",
	}
	alt := invoke.ContextAlternative{Requirements: []invoke.ContextRequirement{
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
	first := invoke.ContextRequirement{Type: "auth.apiKey", Name: "headerKey"}
	second := invoke.ContextRequirement{Type: "auth.apiKey", Name: "queryKey"}
	setPromptedCredential(ctx, first, "apiKey", "key-h")
	setPromptedCredential(ctx, second, "apiKey", "key-q")
	details := &invoke.ContextRequiredDetails{
		Target: "api.example.com",
		Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{
			first, second,
		}}},
	}
	if !invoke.ContextSatisfies(ctx, details) {
		t.Fatalf("named context does not satisfy challenge: %#v", ctx)
	}
}

func TestDurableSubsetScopesNamedCredentials(t *testing.T) {
	candidate := map[string]any{"credentials": map[string]any{
		"oneShot":  "temporary",
		"reusable": "stored",
	}}
	alt := invoke.ContextAlternative{Requirements: []invoke.ContextRequirement{
		{Type: "auth.bearer", Name: "oneShot"},
		{Type: "auth.bearer", Name: "reusable", Durable: platformBoolPointer(true)},
	}}
	got := durableSubset(alt, candidate)
	credentials, _ := got["credentials"].(map[string]any)
	if len(credentials) != 1 || credentials["reusable"] != "stored" {
		t.Fatalf("durable named subset = %#v", got)
	}
}

func TestDurableSubsetProjectsConfigValueFragment(t *testing.T) {
	candidate := map[string]any{
		"configuration": map[string]any{
			"server": map[string]any{
				"url":       "https://eu.example",
				"unrelated": "must-not-persist",
			},
			"otherPoint": "must-not-persist",
		},
		"bearerToken": "must-not-persist",
	}
	alt := invoke.ContextAlternative{Requirements: []invoke.ContextRequirement{{
		Type:    "config.value",
		Durable: platformBoolPointer(true),
		Extra:   map[string]any{"point": "server", "path": "/url"},
	}}}
	got := durableSubset(alt, candidate)
	configuration, _ := got["configuration"].(map[string]any)
	point, _ := configuration["server"].(map[string]any)
	if len(got) != 1 || len(configuration) != 1 || len(point) != 1 || point["url"] != "https://eu.example" {
		t.Fatalf("config.value projection = %#v", got)
	}
}

func TestDurableSubsetConfigValueWholePoint(t *testing.T) {
	candidate := map[string]any{
		"configuration": map[string]any{"requestMedia": "application/json"},
	}
	alt := invoke.ContextAlternative{Requirements: []invoke.ContextRequirement{{
		Type:    "config.value",
		Durable: platformBoolPointer(true),
		Extra:   map[string]any{"point": "requestMedia", "path": ""},
	}}}
	got := durableSubset(alt, candidate)
	configuration, _ := got["configuration"].(map[string]any)
	if configuration["requestMedia"] != "application/json" {
		t.Fatalf("empty-pointer projection = %#v", got)
	}
}
