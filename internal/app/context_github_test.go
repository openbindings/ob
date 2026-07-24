package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/zalando/go-keyring"
)

const testGitHubToken = "test-github-token"

// newGitHubAPIStub provides the only GitHub behavior these tests depend on:
// /user requires a bearer token and returns a small authenticated-user shape.
// Keeping it local makes the default suite deterministic and offline while
// preserving the OpenAPI and context-resolution paths under test.
func newGitHubAPIStub(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testGitHubToken {
			http.Error(w, `{"message":"Requires authentication"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"openbindings-test"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

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

// TestContextGitHub_OperationInvokerDriven tests the full operation-invoker-driven
// context resolution pipeline:
//  1. Uses a local GitHub-shaped API stub and bearer token
//  2. Creates a minimal OpenAPI spec and OBI for GET /user
//  3. Sets context for the stub base URL (the normalizeContextKey-derived key)
//  4. Invokes via InvokeOBIOperation — the operation invoker derives the same key
//     via NormalizeContextKey and looks up context from the store internally
//  5. Validates the response contains the authenticated user's login
func TestContextGitHub_OperationInvokerDriven(t *testing.T) {
	skipIfKeychainUnavailable(t)
	setupContextTestDir(t)
	github := newGitHubAPIStub(t)

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
        "responses": {
          "200": {
            "description": "OK",
            "content": { "application/json": { "schema": { "type": "object" } } }
          }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearer": { "type": "http", "scheme": "bearer" }
    }
  }
}`
	specContent = strings.ReplaceAll(specContent, "https://api.github.com", github.URL)

	// The source embeds its artifact (the D-05 ruling's local lane; the
	// courtesy lane that resolved relative locations is deleted).
	var embedded map[string]any
	if err := json.Unmarshal([]byte(specContent), &embedded); err != nil {
		t.Fatalf("parse spec fixture: %v", err)
	}
	embeddedJSON, err := json.Marshal(embedded)
	if err != nil {
		t.Fatal(err)
	}
	obiContent := `{
  "openbindings": "0.2.0",
  "name": "github-user-test",
  "version": "1.0.0",
  "operations": {
    "getAuthenticatedUser": { "description": "Get the authenticated user" }
  },
  "sources": {
    "openapi": { "bindingSpec": "openbindings.openapi@1", "content": ` + string(embeddedJSON) + ` }
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

	// Set context for the API base URL (the key the OpenAPI invoker returns).
	cfg := ContextConfig{}
	if err := SaveContextConfig(github.URL, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}
	if err := SaveContextCredentials(github.URL, map[string]any{"bearerToken": testGitHubToken}); err != nil {
		t.Fatalf("SaveContextCredentials: %v", err)
	}
	t.Cleanup(func() { _ = DeleteContextCredentials(github.URL) })

	ch, _, err := InvokeOBIOperation(context.Background(), obiPath, "getAuthenticatedUser", "", nil)
	if err != nil {
		t.Fatalf("InvokeOBIOperation failed: %v", err)
	}

	var lastData any
	for ev := range ch {
		if ev.Error != nil {
			t.Fatalf("InvokeOBIOperation stream error: %s (code: %s)", ev.Error.Message, ev.Error.Code)
		}
		lastData = ev.Output
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
	ghToken := testGitHubToken
	setupContextTestDir(t)

	cfg := ContextConfig{}
	if err := SaveContextConfig("https://api.github.com", cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}
	if err := SaveContextCredentials("https://api.github.com", map[string]any{"bearerToken": ghToken}); err != nil {
		t.Fatalf("SaveContextCredentials: %v", err)
	}

	deepURL := "https://api.github.com/repos/openbindings/openbindings/contents"
	bindCtx, err := GetContext(deepURL)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if openbindings.ContextBearerToken(bindCtx) == "" {
		t.Fatal("hierarchical match should resolve credentials for deep path")
	}
	if openbindings.ContextBearerToken(bindCtx) != ghToken {
		t.Error("token should match the one set on base URL")
	}

	unrelatedCtx, err := GetContext("https://unrelated.example.com/api")
	if err != nil {
		t.Fatalf("GetContext unrelated: %v", err)
	}
	if openbindings.ContextBearerToken(unrelatedCtx) != "" {
		t.Error("unrelated domain should not match")
	}
}

// TestContextGitHub_SecuritySchemeApplication tests that the OpenAPI invoker
// correctly reads securitySchemes and places the bearer token in the
// Authorization header.
func TestContextGitHub_SecuritySchemeApplication(t *testing.T) {
	github := newGitHubAPIStub(t)

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
        "responses": {
          "200": {
            "description": "OK",
            "content": { "application/json": { "schema": { "type": "object" } } }
          }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearer": { "type": "http", "scheme": "bearer" }
    }
  }
}`
	specContent = strings.ReplaceAll(specContent, "https://api.github.com", github.URL)

	specPath := filepath.Join(dir, "github-user.openapi.json")
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	// The invoke lane takes the conformant file:// spelling for a local
	// artifact (OAPI-D-02): bare filesystem paths are relative in form and
	// refused by the loader.
	execInput := InvocationInput{
		Source: InvokeSource{
			BindingSpec: "openbindings.openapi@1",
			Location:    "file://" + specPath,
		},
		Ref:     "#/paths/~1user/get",
		Input:   nil,
		Context: map[string]any{"bearerToken": testGitHubToken},
	}

	result := InvokeOperationWithContext(context.Background(), execInput)
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

// TestContextGitHub_NoCredentialsFails verifies that a GitHub-shaped
// authenticated endpoint returns its 401 without supplied credentials. The
// server is local so this transport/error classification test is hermetic.
func TestContextGitHub_NoCredentialsFails(t *testing.T) {
	github := newGitHubAPIStub(t)

	dir := t.TempDir()

	specContent := `{
  "openapi": "3.0.3",
  "info": { "title": "GitHub User", "version": "1.0.0" },
  "servers": [{ "url": "https://api.github.com" }],
  "paths": {
    "/user": {
      "get": {
        "operationId": "getAuthenticatedUser",
        "responses": {
          "200": {
            "description": "OK",
            "content": { "application/json": { "schema": { "type": "object" } } }
          }
        }
      }
    }
  }
}`
	specContent = strings.ReplaceAll(specContent, "https://api.github.com", github.URL)

	specPath := filepath.Join(dir, "github-user.openapi.json")
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	// The invoke lane takes the conformant file:// spelling for a local
	// artifact (OAPI-D-02): bare filesystem paths are relative in form and
	// refused by the loader.
	execInput := InvocationInput{
		Source: InvokeSource{
			BindingSpec: "openbindings.openapi@1",
			Location:    "file://" + specPath,
		},
		Ref: "#/paths/~1user/get",
	}

	result := InvokeOperationWithContext(context.Background(), execInput)
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
	ctx, err := LoadContext("https://api.example.com")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if openbindings.ContextHeaders(ctx)["X-Test"] != "normalized" {
		t.Errorf("expected normalized lookup to work, got: %v", ctx)
	}

	// Vice versa — load via http:// should also work
	ctx2, err := LoadContext("http://api.example.com")
	if err != nil {
		t.Fatalf("LoadContext (http): %v", err)
	}
	if openbindings.ContextHeaders(ctx2)["X-Test"] != "normalized" {
		t.Errorf("expected http lookup to work, got: %v", ctx2)
	}
}
