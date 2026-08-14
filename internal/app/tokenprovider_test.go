package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- fixtures ---------------------------------------------------------------

// providerOBI writes a token-provider-corresponding OBI to disk: one mint
// operation (aliased to the shared contract key) bound to srvURL's /mint.
func providerOBI(t *testing.T, srvURL string) string {
	t.Helper()
	doc := fmt.Sprintf(`{
  "openbindings": "0.2.0",
  "name": "Test Provider",
  "version": "0.0.1",
  "operations": {
    "test.mint": {
      "description": "Mint a token.",
      "aliases": ["openbindings.token-provider.mint"],
      "input": {
        "type": "object",
        "properties": {"credential": {"type": "string"}},
        "additionalProperties": false
      },
      "output": {
        "type": "object",
        "properties": {
          "accessToken": {"type": "string"},
          "expiresAt": {"type": "string"}
        },
        "required": ["accessToken", "expiresAt"]
      }
    }
  },
  "sources": {
    "api": {
      "bindingSpec": "openbindings.openapi@1",
      "content": {
        "openapi": "3.1.0",
        "info": {"title": "Test Provider", "version": "0.0.1"},
        "servers": [{"url": %q}],
        "paths": {
          "/mint": {
            "post": {
              "operationId": "mint",
              "requestBody": {
                "required": true,
                "content": {"application/json": {"schema": {
                  "type": "object",
                  "properties": {"credential": {"type": "string"}}
                }}}
              },
              "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
                "type": "object",
                "properties": {"accessToken": {"type": "string"}, "expiresAt": {"type": "string"}},
                "required": ["accessToken", "expiresAt"]
              }}}}}
            }
          }
        }
      }
    }
  },
  "bindings": {
    "mint.http": {"operation": "test.mint", "source": "api", "ref": "#/paths/~1mint/post"}
  }
}`, srvURL)
	path := filepath.Join(t.TempDir(), "provider.obi.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// targetOBI writes a bearer-secured target OBI bound to srvURL's /data.
func targetOBI(t *testing.T, srvURL string) string {
	t.Helper()
	doc := fmt.Sprintf(`{
  "openbindings": "0.2.0",
  "name": "Test Target",
  "version": "0.0.1",
  "operations": {
    "test.read": {
      "description": "Read data.",
      "idempotent": true,
      "output": {"type": "object", "properties": {"ok": {"type": "boolean"}}, "required": ["ok"]}
    }
  },
  "sources": {
    "api": {
      "bindingSpec": "openbindings.openapi@1",
      "content": {
        "openapi": "3.1.0",
        "info": {"title": "Test Target", "version": "0.0.1"},
        "servers": [{"url": %q}],
        "components": {"securitySchemes": {"bearer": {"type": "http", "scheme": "bearer"}}},
        "paths": {
          "/data": {
            "get": {
              "operationId": "read",
              "security": [{"bearer": []}],
              "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
                "type": "object", "properties": {"ok": {"type": "boolean"}}, "required": ["ok"]
              }}}}}
            }
          }
        }
      }
    }
  },
  "bindings": {
    "read.http": {"operation": "test.read", "source": "api", "ref": "#/paths/~1data/get"}
  }
}`, srvURL)
	path := filepath.Join(t.TempDir(), "target.obi.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mintServer(t *testing.T, token string, hits *int, wantCredential string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if wantCredential != "" && body["credential"] != wantCredential {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"accessToken":%q,"expiresAt":%q}`,
			token, time.Now().Add(15*time.Minute).UTC().Format(time.RFC3339))
	}))
}

type memStore struct {
	m    map[string]map[string]any
	sets int
}

func (s *memStore) Get(_ context.Context, key string) (map[string]any, error) {
	return s.m[key], nil
}
func (s *memStore) Set(_ context.Context, key string, value map[string]any) error {
	if s.m == nil {
		s.m = map[string]map[string]any{}
	}
	s.m[key] = value
	s.sets++
	return nil
}
func (s *memStore) Delete(_ context.Context, key string) error {
	delete(s.m, key)
	return nil
}

// --- unit: ensurePinnedToken ------------------------------------------------

func TestEnsurePinnedToken_MintsAndCaches(t *testing.T) {
	hits := 0
	srv := mintServer(t, "tok-minted", &hits, "durable-cred")
	defer srv.Close()
	provider := providerOBI(t, srv.URL)

	store := &memStore{}
	stored := map[string]any{"tokenProvider": provider, "tokenCredential": "durable-cred"}

	out, minted := ensurePinnedToken(context.Background(), store, "target-key", stored)
	if !minted {
		t.Fatal("expected a mint")
	}
	if out["bearerToken"] != "tok-minted" {
		t.Fatalf("bearerToken = %v", out["bearerToken"])
	}
	if _, ok := out["bearerTokenExpiresAt"].(string); !ok {
		t.Fatal("expiry not cached")
	}
	if hits != 1 {
		t.Fatalf("provider hits = %d", hits)
	}
	if store.sets != 1 || store.m["target-key"]["bearerToken"] != "tok-minted" {
		t.Fatal("minted token not persisted under the target key")
	}
}

func TestEnsurePinnedToken_FreshTokenSkipsMint(t *testing.T) {
	hits := 0
	srv := mintServer(t, "tok-unused", &hits, "")
	defer srv.Close()
	provider := providerOBI(t, srv.URL)

	stored := map[string]any{
		"tokenProvider":        provider,
		"tokenCredential":      "durable-cred",
		"bearerToken":          "tok-fresh",
		"bearerTokenExpiresAt": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
	}
	out, minted := ensurePinnedToken(context.Background(), &memStore{}, "k", stored)
	if minted || hits != 0 || out["bearerToken"] != "tok-fresh" {
		t.Fatalf("fresh token should not re-mint (minted=%v hits=%d)", minted, hits)
	}
}

func TestEnsurePinnedToken_ExpiringTokenRemints(t *testing.T) {
	hits := 0
	srv := mintServer(t, "tok-renewed", &hits, "")
	defer srv.Close()
	provider := providerOBI(t, srv.URL)

	stored := map[string]any{
		"tokenProvider":        provider,
		"tokenCredential":      "durable-cred",
		"bearerToken":          "tok-stale",
		"bearerTokenExpiresAt": time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339), // inside skew
	}
	out, minted := ensurePinnedToken(context.Background(), &memStore{}, "k", stored)
	if !minted || out["bearerToken"] != "tok-renewed" {
		t.Fatalf("expiring token should re-mint (minted=%v token=%v)", minted, out["bearerToken"])
	}
	if hits != 1 {
		t.Fatalf("provider hits = %d", hits)
	}
}

func TestEnsurePinnedToken_UserSetBearerNeverOverwritten(t *testing.T) {
	hits := 0
	srv := mintServer(t, "tok-unwanted", &hits, "")
	defer srv.Close()
	provider := providerOBI(t, srv.URL)

	// A bearerToken with no recorded expiry is user-set, not policy-cached.
	stored := map[string]any{
		"tokenProvider": provider,
		"bearerToken":   "user-durable-bearer",
	}
	out, minted := ensurePinnedToken(context.Background(), &memStore{}, "k", stored)
	if minted || hits != 0 || out["bearerToken"] != "user-durable-bearer" {
		t.Fatal("user-set bearer must never be overwritten by the pin policy")
	}
}

func TestEnsurePinnedToken_NoPinNeverMints(t *testing.T) {
	// The decoy criterion's first half: with no pin, nothing is contacted —
	// even though a mint-capable provider exists and is reachable, and even
	// though a delegate registry might carry the mint alias (the pin path
	// never consults delegates at all; only stored["tokenProvider"] is read).
	hits := 0
	srv := mintServer(t, "tok-decoy", &hits, "")
	defer srv.Close()
	_ = providerOBI(t, srv.URL) // exists, reachable, never consulted

	stored := map[string]any{"tokenCredential": "durable-cred"}
	_, minted := ensurePinnedToken(context.Background(), &memStore{}, "k", stored)
	if minted || hits != 0 {
		t.Fatal("no pin must mean no mint, regardless of reachable providers")
	}
}

func TestEnsurePinnedToken_ReentrancyGuard(t *testing.T) {
	hits := 0
	srv := mintServer(t, "tok", &hits, "")
	defer srv.Close()
	provider := providerOBI(t, srv.URL)

	ctx := context.WithValue(context.Background(), mintingContextKey{}, true)
	stored := map[string]any{"tokenProvider": provider, "tokenCredential": "c"}
	_, minted := ensurePinnedToken(ctx, &memStore{}, "k", stored)
	if minted || hits != 0 {
		t.Fatal("mint-in-flight context must never recurse into another mint")
	}
}

// --- end to end: the resolver ladder with a pin ------------------------------

// TestPinnedProviderEndToEnd is the capstone acceptance test: a bearer-secured
// target is invoked with NO explicit credentials anywhere; the CLI context
// resolver finds the pin, mints from the PINNED provider only, and the target
// receives the minted token. A second, decoy provider carrying the same
// contract alias is up and reachable throughout — and must never be contacted:
// capability never implies use.
func TestPinnedProviderEndToEnd(t *testing.T) {
	dir := t.TempDir()
	contextsDirFunc = func() (string, error) { return dir, nil }
	t.Cleanup(func() { contextsDirFunc = defaultContextsDir })
	t.Setenv(EnvCredentialsFile, filepath.Join(dir, "creds.json"))

	pinnedHits, decoyHits := 0, 0
	pinnedSrv := mintServer(t, "tok-pinned", &pinnedHits, "durable-cred")
	defer pinnedSrv.Close()
	decoySrv := mintServer(t, "tok-decoy", &decoyHits, "")
	defer decoySrv.Close()

	pinnedProvider := providerOBI(t, pinnedSrv.URL)
	_ = providerOBI(t, decoySrv.URL) // the decoy: alias-carrying, reachable, unpinned

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-pinned" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer targetSrv.Close()
	target := targetOBI(t, targetSrv.URL)

	// Pin the provider for the target's origin — the challenge key the
	// resolver derives is the normalized endpoint (host:port).
	if err := SaveUnifiedContext(targetSrv.URL, map[string]any{
		"tokenProvider":   pinnedProvider,
		"tokenCredential": "durable-cred",
	}); err != nil {
		t.Fatal(err)
	}

	run, err := InvokeOBIOperationConfigured(context.Background(), target, "test.read", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := <-run.Events
	if !ok {
		t.Fatal("no output")
	}
	if ev.Error != nil {
		t.Fatalf("invocation failed: %s", ev.Error.Code)
	}
	out, _ := ev.Output.(map[string]any)
	if out["ok"] != true {
		t.Fatalf("unexpected output: %v", ev.Output)
	}
	if pinnedHits != 1 {
		t.Fatalf("pinned provider hits = %d, want 1", pinnedHits)
	}
	if decoyHits != 0 {
		t.Fatalf("DECOY PROVIDER WAS CONTACTED (%d hits) — pinning rule violated", decoyHits)
	}

	// The minted token is cached for subsequent runs.
	cached, err := LoadContext(targetSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if cached["bearerToken"] != "tok-pinned" {
		t.Fatal("minted token not cached under the target context")
	}
	if !strings.HasPrefix(fmt.Sprint(cached["bearerTokenExpiresAt"]), "20") {
		t.Fatal("expiry not cached")
	}
}
