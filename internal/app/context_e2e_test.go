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

	_, opts, err := GetContext(targetURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}

	if opts.Headers["X-Custom"] != "custom-value" {
		t.Errorf("header mismatch: %q", opts.Headers["X-Custom"])
	}
	if opts.Cookies["session"] != "abc" {
		t.Errorf("cookie mismatch: %q", opts.Cookies["session"])
	}
	if opts.Environment["STRIPE_ENV"] != "test" {
		t.Errorf("env mismatch: %q", opts.Environment["STRIPE_ENV"])
	}
	if opts.Metadata["baseURL"] != "https://api.stripe.com" {
		t.Errorf("metadata mismatch: %v", opts.Metadata["baseURL"])
	}
}

func TestContextE2E_SourceOverrideMerge(t *testing.T) {
	setupContextTestDir(t)

	targetURL := "https://api.multi.com/spec.json"

	cfg := ContextConfig{
		Headers: map[string]string{"X-Base": "base", "X-Shared": "from-base"},
		SourceOverrides: map[string]*ContextOverride{
			"payments-v2": {
				Headers: map[string]string{"X-Source": "source-only", "X-Shared": "from-source"},
			},
		},
	}
	if err := SaveContextConfig(targetURL, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	_, baseOpts, err := GetContext(targetURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if baseOpts == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}
	if baseOpts.Headers["X-Base"] != "base" {
		t.Errorf("base header mismatch: %q", baseOpts.Headers["X-Base"])
	}
	if baseOpts.Headers["X-Shared"] != "from-base" {
		t.Errorf("shared header should be base-level: %q", baseOpts.Headers["X-Shared"])
	}

	_, mergedOpts, err := GetContextForSource(targetURL, "payments-v2")
	if err != nil {
		t.Fatalf("GetContextForSource: %v", err)
	}
	if mergedOpts == nil {
		t.Fatal("GetContextForSource: expected non-nil ExecutionOptions")
	}
	if mergedOpts.Headers["X-Base"] != "base" {
		t.Errorf("base header should carry through merge: %q", mergedOpts.Headers["X-Base"])
	}
	if mergedOpts.Headers["X-Source"] != "source-only" {
		t.Errorf("source header missing: %q", mergedOpts.Headers["X-Source"])
	}
	if mergedOpts.Headers["X-Shared"] != "from-source" {
		t.Errorf("source override should win: %q", mergedOpts.Headers["X-Shared"])
	}

	_, noOverrideOpts, err := GetContextForSource(targetURL, "nonexistent-source")
	if err != nil {
		t.Fatalf("GetContextForSource (no override): %v", err)
	}
	if noOverrideOpts == nil {
		t.Fatal("GetContextForSource: expected non-nil ExecutionOptions")
	}
	if noOverrideOpts.Headers["X-Shared"] != "from-base" {
		t.Errorf("no override should return base: %q", noOverrideOpts.Headers["X-Shared"])
	}
}

func TestContextE2E_EmptyURLReturnsEmpty(t *testing.T) {
	setupContextTestDir(t)

	bindCtx, opts, err := GetContext("")
	if err != nil {
		t.Fatalf("GetContext(''): %v", err)
	}
	hasCred := len(bindCtx) > 0
	hasOpts := opts != nil && (len(opts.Headers) > 0 || len(opts.Cookies) > 0 || len(opts.Environment) > 0 || len(opts.Metadata) > 0)
	if hasCred || hasOpts {
		t.Errorf("empty URL should return empty context: bindCtx=%+v opts=%+v", bindCtx, opts)
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

	_, opts, err := GetContext(targetURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}
	if opts.Environment["KUBECONFIG"] != "/home/me/.kube/prod" {
		t.Errorf("env mismatch: %q", opts.Environment["KUBECONFIG"])
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

	_, opts, err := GetContext(targetURL)
	if err != nil {
		t.Fatalf("GetContext after delete: %v", err)
	}
	if opts != nil && len(opts.Headers) > 0 {
		t.Errorf("context should be empty after delete: %+v", opts)
	}
}

func TestContextE2E_AutoResolution(t *testing.T) {
	setupContextTestDir(t)

	// Context is now resolved by the executor's key, not the target URL.
	// For hierarchical matching, set context on the petstore origin.
	cfg := ContextConfig{
		Headers: map[string]string{"X-Api-Key": "pet-key-123"},
	}
	if err := SaveContextConfig("https://petstore.swagger.io", cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	// Direct context lookup on the origin should work
	_, opts, err := GetContext("https://petstore.swagger.io")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}
	if opts.Headers["X-Api-Key"] != "pet-key-123" {
		t.Errorf("header mismatch: %q", opts.Headers["X-Api-Key"])
	}

	// resolveBindingAndSource no longer resolves context
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"listPets": {},
		},
		Sources: map[string]openbindings.Source{
			"petstore": {Format: "openapi@3", Location: "https://petstore.swagger.io/v2/swagger.json"},
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

	// Deep URL should match context set on the base
	deepURL := "https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json"
	_, opts, err := GetContext(deepURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}
	if opts.Headers["Authorization"] != "Bearer test-token" {
		t.Errorf("expected hierarchical match, got headers: %v", opts.Headers)
	}

	// Mid-path URL should also match
	midURL := "https://raw.githubusercontent.com/github/rest-api-description"
	_, opts2, err := GetContext(midURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts2 == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}
	if opts2.Headers["Authorization"] != "Bearer test-token" {
		t.Errorf("expected hierarchical match for mid-path, got headers: %v", opts2.Headers)
	}

	// Different domain should NOT match
	otherURL := "https://api.github.com/user"
	_, opts3, err := GetContext(otherURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts3 != nil && len(opts3.Headers) > 0 {
		t.Errorf("different domain should not match: %v", opts3.Headers)
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

	_, opts, err := GetContext(specificURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}
	if opts.Headers["X-Level"] != "specific" {
		t.Errorf("exact match should take precedence, got %q", opts.Headers["X-Level"])
	}

	// A different deep path should fall back to base
	otherPath := "https://api.example.com/v3/other.json"
	_, opts2, err := GetContext(otherPath)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if opts2 == nil {
		t.Fatal("GetContext: expected non-nil ExecutionOptions")
	}
	if opts2.Headers["X-Level"] != "base" {
		t.Errorf("should fall back to base, got %q", opts2.Headers["X-Level"])
	}
}

func TestContextE2E_NoContextReturnsEmptyResolvedBinding(t *testing.T) {
	setupContextTestDir(t)

	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"hello": {},
		},
		Sources: map[string]openbindings.Source{
			"usage": {Format: "usage@1"},
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
