package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/zalando/go-keyring"
)

// skipIfKeychainUnavailable skips the test when OS keychain writes are blocked
// (e.g. in a sandbox, CI container, or headless environment). It probes with
// a real write + delete to catch environments where metadata reads succeed but
// writes fail (macOS sandbox exit status 161).
func skipIfKeychainUnavailable(t *testing.T) {
	t.Helper()
	const probeKey = "__ob_test_probe__"
	if err := keyring.Set(KeychainService, probeKey, "probe"); err != nil {
		t.Skipf("keychain not writable (sandboxed?): %v", err)
	}
	_ = keyring.Delete(KeychainService, probeKey)
}

// TestContextGitHub_ExecutorDrivenExecution tests the full executor-driven
// context resolution pipeline:
//  1. Gets a real GitHub token via `gh auth token`
//  2. Creates a minimal OpenAPI spec and OBI for GET /user
//  3. Sets context for https://api.github.com (the normalizeContextKey-derived key)
//  4. Executes via ExecuteOBIOperation — the executor derives the same key
//     via NormalizeContextKey and looks up context from the store internally
//  5. Validates the response contains the authenticated user's login
//
// Requires: `gh` CLI installed and authenticated.
// Skipped in environments without `gh` or network access.
func TestContextGitHub_ExecutorDrivenExecution(t *testing.T) {
	skipIfKeychainUnavailable(t)
	ghToken := getGitHubToken(t)
	setupContextTestDir(t)

	dir := t.TempDir()

	specContent := `{
  "openapi": "3.0.3",
  "info": { "title": "GitHub User", "version": "1.0.0" },
  "servers": [{ "url": "https://api.github.com" }],
  "paths": {
    "/user": {
      "get": {
        "operationId": "getAuthenticatedUser",
        "summary": "Get the authenticated user",
        "security": [{ "bearer": [] }],
        "responses": { "200": { "description": "OK" } }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearer": { "type": "http", "scheme": "bearer" }
    }
  }
}`

	obiContent := `{
  "openbindings": "0.1.0",
  "name": "github-user-test",
  "version": "1.0.0",
  "operations": {
    "getAuthenticatedUser": { "description": "Get the authenticated user" }
  },
  "sources": {
    "openapi": { "format": "openapi@3.0", "location": "github-user.openapi.json" }
  },
  "bindings": {
    "getAuthenticatedUser.openapi": {
      "operation": "getAuthenticatedUser",
      "source": "openapi",
      "ref": "#/paths/~1user/get"
    }
  }
}`

	specPath := filepath.Join(dir, "github-user.openapi.json")
	obiPath := filepath.Join(dir, "github-user.obi.json")

	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	if err := os.WriteFile(obiPath, []byte(obiContent), 0644); err != nil {
		t.Fatalf("write obi: %v", err)
	}

	// Set context for the API base URL (the key the OpenAPI executor returns).
	cfg := ContextConfig{}
	if err := SaveContextConfig("https://api.github.com", cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}
	if err := SaveContextCredentials("https://api.github.com", map[string]any{"bearerToken": ghToken}); err != nil {
		t.Fatalf("SaveContextCredentials: %v", err)
	}
	t.Cleanup(func() { _ = DeleteContextCredentials("https://api.github.com") })

	ch, err := ExecuteOBIOperation(context.Background(), obiPath, "getAuthenticatedUser", "", nil)
	if err != nil {
		t.Fatalf("ExecuteOBIOperation failed: %v", err)
	}

	var lastData any
	for ev := range ch {
		if ev.Error != nil {
			if ev.Error.Code == openbindings.ErrCodeAuthRequired {
				t.Skipf("GitHub token expired or invalid (code: %s)", ev.Error.Code)
			}
			t.Fatalf("ExecuteOBIOperation stream error: %s (code: %s)", ev.Error.Message, ev.Error.Code)
		}
		lastData = ev.Data
	}

	outputMap, ok := lastData.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", lastData)
	}

	login, _ := outputMap["login"].(string)
	if login == "" {
		t.Fatalf("expected non-empty login in response, got: %v", lastData)
	}
	t.Logf("Authenticated as: %s", login)
}

// TestContextGitHub_HierarchicalAPIBaseURL tests that context set on the
// API base URL (https://api.github.com) is resolved via hierarchical matching
// when the target URL is a deeper path.
func TestContextGitHub_HierarchicalAPIBaseURL(t *testing.T) {
	skipIfKeychainUnavailable(t)
	ghToken := getGitHubToken(t)
	setupContextTestDir(t)

	cfg := ContextConfig{}
	if err := SaveContextConfig("https://api.github.com", cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}
	if err := SaveContextCredentials("https://api.github.com", map[string]any{"bearerToken": ghToken}); err != nil {
		t.Fatalf("SaveContextCredentials: %v", err)
	}

	deepURL := "https://api.github.com/repos/openbindings/openbindings/contents"
	bindCtx, _, err := GetContext(deepURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if openbindings.ContextBearerToken(bindCtx) == "" {
		t.Fatal("hierarchical match should resolve credentials for deep path")
	}
	if openbindings.ContextBearerToken(bindCtx) != ghToken {
		t.Error("token should match the one set on base URL")
	}

	unrelatedCtx, _, err := GetContext("https://unrelated.example.com/api")
	if err != nil {
		t.Fatalf("GetContext unrelated: %v", err)
	}
	if openbindings.ContextBearerToken(unrelatedCtx) != "" {
		t.Error("unrelated domain should not match")
	}
}

// TestContextGitHub_SecuritySchemeApplication tests that the OpenAPI executor
// correctly reads securitySchemes and places the bearer token in the
// Authorization header.
func TestContextGitHub_SecuritySchemeApplication(t *testing.T) {
	skipIfKeychainUnavailable(t)
	ghToken := getGitHubToken(t)
	setupContextTestDir(t)

	dir := t.TempDir()

	specContent := `{
  "openapi": "3.0.3",
  "info": { "title": "GitHub User", "version": "1.0.0" },
  "servers": [{ "url": "https://api.github.com" }],
  "paths": {
    "/user": {
      "get": {
        "operationId": "getAuthenticatedUser",
        "summary": "Get the authenticated user",
        "security": [{ "bearer": [] }],
        "responses": { "200": { "description": "OK" } }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearer": { "type": "http", "scheme": "bearer" }
    }
  }
}`

	specPath := filepath.Join(dir, "github-user.openapi.json")
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	execInput := ExecuteOperationInput{
		Source: ExecuteSource{
			Format:   "openapi@3.0",
			Location: specPath,
		},
		Ref:     "#/paths/~1user/get",
		Input:   nil,
		Context: map[string]any{"bearerToken": ghToken},
	}

	result := ExecuteOperationWithContext(context.Background(), execInput)
	if result.Error != nil {
		t.Fatalf("execution failed: %s", result.Error.Message)
	}

	outputJSON, _ := json.Marshal(result.Output)
	var user map[string]any
	if err := json.Unmarshal(outputJSON, &user); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	login, _ := user["login"].(string)
	if login == "" {
		t.Fatalf("expected authenticated response with login, got: %s", string(outputJSON))
	}
	t.Logf("Authenticated as: %s (via direct execution)", login)
}

// TestContextGitHub_NoCredentialsFails verifies that calling the GitHub
// authenticated endpoint without credentials returns a 401.
func TestContextGitHub_NoCredentialsFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	skipIfKeychainUnavailable(t)
	setupContextTestDir(t)

	// Ensure no stored credentials from prior tests are picked up by
	// the executor's context resolution.
	_ = DeleteContextCredentials("https://api.github.com")
	t.Cleanup(func() { _ = DeleteContextCredentials("https://api.github.com") })

	dir := t.TempDir()

	specContent := `{
  "openapi": "3.0.3",
  "info": { "title": "GitHub User", "version": "1.0.0" },
  "servers": [{ "url": "https://api.github.com" }],
  "paths": {
    "/user": {
      "get": {
        "operationId": "getAuthenticatedUser",
        "responses": { "200": { "description": "OK" } }
      }
    }
  }
}`

	specPath := filepath.Join(dir, "github-user.openapi.json")
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	execInput := ExecuteOperationInput{
		Source: ExecuteSource{
			Format:   "openapi@3.0",
			Location: specPath,
		},
		Ref: "#/paths/~1user/get",
	}

	result := ExecuteOperationWithContext(context.Background(), execInput)
	if result.Error == nil {
		t.Fatal("expected error without credentials")
	}
	if result.Status != 401 {
		t.Errorf("expected 401 status, got %d", result.Status)
	}
}

// TestContext_CLIContextStore tests that the CLI context store correctly
// wraps the keychain-based credential persistence.
func TestContext_CLIContextStore(t *testing.T) {
	setupContextTestDir(t)

	store := NewCLIContextStore()

	// Store should return nil for unknown keys.
	got, err := store.Get(context.Background(), "https://api.example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown key, got %v", got)
	}
}

// TestContext_HTTPSNormalization tests that http:// and https:// are treated
// as equivalent keys for context storage and lookup.
func TestContext_HTTPSNormalization(t *testing.T) {
	setupContextTestDir(t)

	cfg := ContextConfig{
		Headers: map[string]string{"X-Test": "normalized"},
	}
	if err := SaveContextConfig("http://api.example.com", cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	// Should be able to load via https:// even though set via http://
	_, opts, err := LoadContext("https://api.example.com")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if opts == nil || opts.Headers["X-Test"] != "normalized" {
		t.Errorf("expected normalized lookup to work, got: %v", opts)
	}

	// Vice versa — load via http:// should also work
	_, opts2, err := LoadContext("http://api.example.com")
	if err != nil {
		t.Fatalf("LoadContext (http): %v", err)
	}
	if opts2 == nil || opts2.Headers["X-Test"] != "normalized" {
		t.Errorf("expected http lookup to work, got: %v", opts2)
	}
}

func getGitHubToken(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		t.Skip("gh CLI not available or not authenticated; skipping")
	}
	token := string(out)
	if len(token) > 0 && token[len(token)-1] == '\n' {
		token = token[:len(token)-1]
	}
	if token == "" {
		t.Skip("empty gh token; skipping")
	}
	return token
}
