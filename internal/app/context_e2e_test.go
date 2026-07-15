package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestContextE2E_URLKeyedRoundTrip(t *testing.T) {
	setupContextTestDir(t)

	targetURL := "https://api.stripe.com/openapi.json"

	cfg := ContextConfig{
		Headers:     map[string]string{"X-Custom": "custom-value"},
		Cookies:     map[string]string{"session": "abc"},
		Environment: map[string]string{"STRIPE_ENV": "test"},
		Metadata:    map[string]any{"baseURL": "https://api.stripe.com"},
	}
	if err := SaveContextConfig(targetURL, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	ctx, err := GetContext(targetURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx == nil {
		t.Fatal("GetContext: expected non-nil context")
	}

	if openbindings.ContextHeaders(ctx)["X-Custom"] != "custom-value" {
		t.Errorf("header mismatch: %q", openbindings.ContextHeaders(ctx)["X-Custom"])
	}
	if openbindings.ContextCookies(ctx)["session"] != "abc" {
		t.Errorf("cookie mismatch: %q", openbindings.ContextCookies(ctx)["session"])
	}
	if openbindings.ContextEnvironment(ctx)["STRIPE_ENV"] != "test" {
		t.Errorf("env mismatch: %q", openbindings.ContextEnvironment(ctx)["STRIPE_ENV"])
	}
	if openbindings.ContextMetadata(ctx)["baseURL"] != "https://api.stripe.com" {
		t.Errorf("metadata mismatch: %v", openbindings.ContextMetadata(ctx)["baseURL"])
	}
}

func TestContextE2E_EmptyURLReturnsEmpty(t *testing.T) {
	setupContextTestDir(t)

	ctx, err := GetContext("")
	if err != nil {
		t.Fatalf("GetContext(''): %v", err)
	}
	if len(ctx) > 0 {
		t.Errorf("empty URL should return empty context: %+v", ctx)
	}
}

func TestContextE2E_ExecURLContext(t *testing.T) {
	setupContextTestDir(t)

	targetURL := "exec:kubectl"
	cfg := ContextConfig{
		Environment: map[string]string{
			"KUBECONFIG": "/home/me/.kube/prod",
		},
	}
	if err := SaveContextConfig(targetURL, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	ctx, err := GetContext(targetURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx == nil {
		t.Fatal("GetContext: expected non-nil context")
	}
	if openbindings.ContextEnvironment(ctx)["KUBECONFIG"] != "/home/me/.kube/prod" {
		t.Errorf("env mismatch: %q", openbindings.ContextEnvironment(ctx)["KUBECONFIG"])
	}
}

func TestContextE2E_DeleteCleansUp(t *testing.T) {
	setupContextTestDir(t)

	targetURL := "https://api.cleanup.com"
	cfg := ContextConfig{
		Headers: map[string]string{"X-Test": "val"},
	}
	if err := SaveContextConfig(targetURL, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	if !ContextExists(targetURL) {
		t.Fatal("context should exist")
	}

	if err := DeleteContext(targetURL); err != nil {
		t.Fatalf("DeleteContext: %v", err)
	}

	if ContextExists(targetURL) {
		t.Error("context should not exist after deletion")
	}

	ctx, err := GetContext(targetURL)
	if err != nil {
		t.Fatalf("GetContext after delete: %v", err)
	}
	if len(openbindings.ContextHeaders(ctx)) > 0 {
		t.Errorf("context should be empty after delete: %+v", ctx)
	}
}

func TestContextE2E_AutoResolution(t *testing.T) {
	setupContextTestDir(t)

	cfg := ContextConfig{
		Headers: map[string]string{"X-Api-Key": "pet-key-123"},
	}
	if err := SaveContextConfig("https://petstore.swagger.io", cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	ctx, err := GetContext("https://petstore.swagger.io")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx == nil {
		t.Fatal("GetContext: expected non-nil context")
	}
	if openbindings.ContextHeaders(ctx)["X-Api-Key"] != "pet-key-123" {
		t.Errorf("header mismatch: %q", openbindings.ContextHeaders(ctx)["X-Api-Key"])
	}

	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"listPets": {},
		},
		Sources: map[string]openbindings.Source{
			"petstore": {BindingSpec: "openbindings.openapi@1", Location: "https://petstore.swagger.io/v2/swagger.json"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.petstore": {
				Operation: "listPets",
				Source:    "petstore",
				Ref:       "#/paths/~1pets/get",
			},
		},
	}

	resolved, err := resolveBindingAndSource(iface, "listPets", "", nil)
	if err != nil {
		t.Fatalf("resolveBindingAndSource: %v", err)
	}
	if resolved.binding == nil {
		t.Fatal("expected resolved binding")
	}
}

func TestContextE2E_HierarchicalURLMatch(t *testing.T) {
	setupContextTestDir(t)

	baseURL := "https://raw.githubusercontent.com"
	cfg := ContextConfig{
		Headers: map[string]string{"Authorization": "Bearer test-token"},
	}
	if err := SaveContextConfig(baseURL, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	deepURL := "https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json"
	ctx, err := GetContext(deepURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx == nil {
		t.Fatal("GetContext: expected non-nil context")
	}
	if openbindings.ContextHeaders(ctx)["Authorization"] != "Bearer test-token" {
		t.Errorf("expected hierarchical match, got headers: %v", openbindings.ContextHeaders(ctx))
	}

	midURL := "https://raw.githubusercontent.com/github/rest-api-description"
	ctx2, err := GetContext(midURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx2 == nil {
		t.Fatal("GetContext: expected non-nil context")
	}
	if openbindings.ContextHeaders(ctx2)["Authorization"] != "Bearer test-token" {
		t.Errorf("expected hierarchical match for mid-path, got headers: %v", openbindings.ContextHeaders(ctx2))
	}

	otherURL := "https://api.github.com/user"
	ctx3, err := GetContext(otherURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if len(openbindings.ContextHeaders(ctx3)) > 0 {
		t.Errorf("different domain should not match: %v", openbindings.ContextHeaders(ctx3))
	}
}

func TestContextE2E_ExactMatchTakesPrecedence(t *testing.T) {
	setupContextTestDir(t)

	baseURL := "https://api.example.com"
	if err := SaveContextConfig(baseURL, ContextConfig{
		Headers: map[string]string{"X-Level": "base"},
	}); err != nil {
		t.Fatalf("SaveContextConfig base: %v", err)
	}

	specificURL := "https://api.example.com/v2/spec.json"
	if err := SaveContextConfig(specificURL, ContextConfig{
		Headers: map[string]string{"X-Level": "specific"},
	}); err != nil {
		t.Fatalf("SaveContextConfig specific: %v", err)
	}

	ctx, err := GetContext(specificURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx == nil {
		t.Fatal("GetContext: expected non-nil context")
	}
	if openbindings.ContextHeaders(ctx)["X-Level"] != "specific" {
		t.Errorf("exact match should take precedence, got %q", openbindings.ContextHeaders(ctx)["X-Level"])
	}

	otherPath := "https://api.example.com/v3/other.json"
	ctx2, err := GetContext(otherPath)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx2 == nil {
		t.Fatal("GetContext: expected non-nil context")
	}
	if openbindings.ContextHeaders(ctx2)["X-Level"] != "base" {
		t.Errorf("should fall back to base, got %q", openbindings.ContextHeaders(ctx2)["X-Level"])
	}
}

func TestContextE2E_NoContextReturnsEmptyResolvedBinding(t *testing.T) {
	setupContextTestDir(t)

	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"hello": {},
		},
		Sources: map[string]openbindings.Source{
			"usage": {BindingSpec: "openbindings.usage@1"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"hello.usage": {
				Operation: "hello",
				Source:    "usage",
				Ref:       "hello",
			},
		},
	}

	resolved, err := resolveBindingAndSource(iface, "hello", "", nil)
	if err != nil {
		t.Fatalf("resolveBindingAndSource: %v", err)
	}
	if resolved.binding == nil {
		t.Fatal("expected resolved binding")
	}
}
