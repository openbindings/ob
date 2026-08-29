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
      "bindingSpec": "openbindings.openapi-3.1@1",
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
    "mint.http": {"operation": "test.mint", "source": "api", "selector": "#/paths/~1mint/post", "inputTransform": "{\"body\":$}"}
  }
}`, srvURL)
	path := filepath.Join(t.TempDir(), "provider.obi.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// bearerMintProviderOBI writes a token-provider OBI whose mint operation is
// itself bearer-secured — invoking it raises CONTEXT_REQUIRED(auth.bearer)
// for srvURL's origin (the invoker reads the requirement from the artifact
// before dispatch). Used to exercise a provider whose own mint challenges.
func bearerMintProviderOBI(t *testing.T, srvURL string) string {
	t.Helper()
	doc := fmt.Sprintf(`{
  "openbindings": "0.2.0",
  "name": "Bearer-Secured Provider",
  "version": "0.0.1",
  "operations": {
    "test.mint": {
      "description": "Mint a token (itself bearer-secured).",
      "aliases": ["openbindings.token-provider.mint"],
      "input": {"type": "object", "properties": {"credential": {"type": "string"}}, "additionalProperties": false},
      "output": {"type": "object", "properties": {"accessToken": {"type": "string"}, "expiresAt": {"type": "string"}}, "required": ["accessToken", "expiresAt"]}
    }
  },
  "sources": {
    "api": {
      "bindingSpec": "openbindings.openapi-3.1@1",
      "content": {
        "openapi": "3.1.0",
        "info": {"title": "Bearer-Secured Provider", "version": "0.0.1"},
        "servers": [{"url": %q}],
        "components": {"securitySchemes": {"bearer": {"type": "http", "scheme": "bearer"}}},
        "paths": {
          "/mint": {
            "post": {
              "operationId": "mint",
              "security": [{"bearer": []}],
              "requestBody": {"required": false, "content": {"application/json": {"schema": {
                "type": "object", "properties": {"credential": {"type": "string"}}
              }}}},
              "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
                "type": "object", "properties": {"accessToken": {"type": "string"}, "expiresAt": {"type": "string"}},
                "required": ["accessToken", "expiresAt"]
              }}}}}
            }
          }
        }
      }
    }
  },
  "bindings": {
    "mint.http": {"operation": "test.mint", "source": "api", "selector": "#/paths/~1mint/post", "inputTransform": "{\"body\":$}"}
  }
}`, srvURL)
	path := filepath.Join(t.TempDir(), "bearer-provider.obi.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// locatedMintProviderOBI writes a token-provider OBI whose mint source is a
// LOCATED artifact at docURL (not embedded), so credentialOrigin can inspect
// the fetch transport of the artifact describing where the credential goes.
func locatedMintProviderOBI(t *testing.T, docURL string) string {
	t.Helper()
	doc := fmt.Sprintf(`{
  "openbindings": "0.2.0",
  "name": "Located Provider",
  "version": "0.0.1",
  "operations": {
    "test.mint": {
      "description": "Mint a token.",
      "aliases": ["openbindings.token-provider.mint"],
      "input": {"type": "object", "properties": {"credential": {"type": "string"}}, "additionalProperties": false},
      "output": {"type": "object", "properties": {"accessToken": {"type": "string"}, "expiresAt": {"type": "string"}}, "required": ["accessToken", "expiresAt"]}
    }
  },
  "sources": {
    "api": {"bindingSpec": "openbindings.openapi-3.1@1", "location": %q}
  },
  "bindings": {
    "mint.http": {"operation": "test.mint", "source": "api", "selector": "#/paths/~1mint/post", "inputTransform": "{\"body\":$}"}
  }
}`, docURL)
	path := filepath.Join(t.TempDir(), "located-provider.obi.json")
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
      "bindingSpec": "openbindings.openapi-3.1@1",
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
    "read.http": {"operation": "test.read", "source": "api", "selector": "#/paths/~1data/get"}
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

// A provider that returns a non-RFC3339 expiresAt must be REJECTED, not
// cached — otherwise the freshness parse fails on every later challenge and
// ob re-mints on every invocation (a silent storm of real provider calls).
func TestEnsurePinnedToken_UnparseableExpiryRejected(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		// A Unix timestamp instead of an RFC 3339 instant.
		_, _ = w.Write([]byte(`{"accessToken":"tok","expiresAt":"1786743000"}`))
	}))
	defer srv.Close()
	provider := providerOBI(t, srv.URL)

	store := &memStore{}
	stored := map[string]any{"tokenProvider": provider, "tokenCredential": "c"}
	out, minted := ensurePinnedToken(context.Background(), store, "k", stored)
	if minted {
		t.Fatal("an unparseable expiresAt must not count as a successful mint")
	}
	if _, cached := out["bearerToken"]; cached {
		t.Fatal("a token with an unparseable expiry must not be cached (would storm)")
	}
	if store.sets != 0 {
		t.Fatal("nothing should be persisted when the expiry is invalid")
	}
	if hits != 1 {
		t.Fatalf("expected exactly one mint attempt, got %d", hits)
	}
}

// M5 transport policy: the classifier that decides refuse/exempt/note.
func TestCredentialOrigin(t *testing.T) {
	cases := []struct {
		in        string
		plaintext bool
		isNetwork bool
	}{
		{"https://auth.example.com/obi", false, true},
		{"https://auth.example.com:8443", false, true},
		{"http://auth.example.com/obi", true, true},  // plaintext remote → refuse
		{"http://localhost:8787", false, true},       // loopback exempt
		{"http://127.0.0.1:8787", false, true},       // loopback exempt
		{"http://[::1]:8787", false, true},           // loopback exempt
		{"http://api.localhost:3000", false, true},   // *.localhost exempt
		{"wss://stream.example.com", false, true},    // TLS ws
		{"ws://stream.example.com", true, true},      // plaintext ws remote
		{"grpc.example.com:443", false, true},        // bare host:port: network, no TLS verdict
		{"/home/me/provider.obi.json", false, false}, // file path: not a network endpoint
		{"file:///etc/provider.json", false, false},  // file scheme: not network
		{"", false, false},                           // empty
	}
	for _, c := range cases {
		_, plaintext, isNet := credentialOrigin(c.in)
		if plaintext != c.plaintext || isNet != c.isNetwork {
			t.Errorf("credentialOrigin(%q) = (plaintext %v, isNet %v), want (%v, %v)",
				c.in, plaintext, isNet, c.plaintext, c.isNetwork)
		}
	}
}

// The locator floor refuses a plaintext-remote pinned locator BEFORE any fetch
// — a durable credential must never depend on an OBI retrieved insecurely.
func TestMint_RefusesPlaintextRemoteLocator(t *testing.T) {
	_, err := mintFromPinnedProvider(context.Background(), "http://provider.example.com/obi", "cred")
	if err == nil {
		t.Fatal("expected refusal of a plaintext-remote locator")
	}
	if !strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("expected a plaintext-refusal message, got: %v", err)
	}
}

// The destination floor refuses when the mint's LOCATED source artifact is
// fetched over plaintext to a remote host (the artifact describing where the
// credential goes is itself MITM-injectable).
func TestMint_RefusesPlaintextRemoteDestination(t *testing.T) {
	// Locator is a local file (exempt); its mint source is a plaintext-remote
	// artifact. No network call happens — the refusal precedes invocation.
	provider := locatedMintProviderOBI(t, "http://insecure.example.com/openapi.json")
	_, err := mintFromPinnedProvider(context.Background(), provider, "cred")
	if err == nil {
		t.Fatal("expected refusal of a plaintext-remote mint destination")
	}
	if !strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("expected a plaintext-refusal message, got: %v", err)
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

// TestReentrancyGuardPropagates proves the marker actually threads through the
// real invoke path (invokeOnInterface → driveBinding → the live
// CLIContextResolver), not merely that ensurePinnedToken short-circuits when
// the marker is pre-set.
//
// Topology: mint from provider P, whose own mint is bearer-secured, so
// invoking it raises CONTEXT_REQUIRED and consults the CLI resolver. Under
// P's mint-server origin — the challenge target the resolver keys on — a
// RECURSION DECOY provider is pinned. If the marker propagates, the resolver's
// nested ensurePinnedToken sees it and returns early, and the decoy is never
// contacted. If it did NOT propagate, the resolver would auto-mint from the
// decoy — so a nonzero decoy hit count is a direct falsification.
func TestReentrancyGuardPropagates(t *testing.T) {
	dir := t.TempDir()
	contextsDirFunc = func() (string, error) { return dir, nil }
	t.Cleanup(func() { contextsDirFunc = defaultContextsDir })
	t.Setenv(EnvCredentialsFile, filepath.Join(dir, "creds.json"))

	decoyHits := 0
	decoySrv := mintServer(t, "tok-decoy", &decoyHits, "")
	defer decoySrv.Close()
	decoyProvider := providerOBI(t, decoySrv.URL)

	// P's mint is bearer-secured; its server is never reached (the challenge
	// is raised from the artifact before dispatch), but it must resolve.
	pSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer pSrv.Close()
	p := bearerMintProviderOBI(t, pSrv.URL)

	// Pin the decoy under P's mint-server origin — the exact key the resolver
	// derives from P's bearer challenge. A broken guard mints from it here.
	if err := SaveUnifiedContext(pSrv.URL, map[string]any{
		"tokenProvider":   decoyProvider,
		"tokenCredential": "decoy-cred",
	}); err != nil {
		t.Fatal(err)
	}

	// Mint from P with a clean (no-marker) context, as the outer resolver would.
	minted, err := mintFromPinnedProvider(context.Background(), p, "outer-cred")
	if err == nil {
		t.Fatalf("P's own mint is bearer-secured and unsatisfiable; expected failure, got token %v", minted)
	}
	if decoyHits != 0 {
		t.Fatalf("RECURSION: the decoy was minted from (%d hits) — the re-entrancy marker did not propagate through the invoke path to the resolver", decoyHits)
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
