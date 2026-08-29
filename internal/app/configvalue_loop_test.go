package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openbindings/openbindings-go/invoke"
)

// multiServerTargetOBI writes an OBI whose located OpenAPI source declares
// TWO servers (v1 and v2 prefixes of srvURL) and no selection default, so
// invoking challenges CONTEXT_REQUIRED (config.value, point server) with the
// declared URLs as the engine-asserted enum.
func multiServerTargetOBI(t *testing.T, docURL string) string {
	t.Helper()
	doc := fmt.Sprintf(`{
  "openbindings": "0.2.0",
  "name": "Multi-Server Target",
  "version": "0.0.1",
  "operations": {
    "test.read": {
      "description": "Read data.",
      "idempotent": true,
      "output": {"type": "object", "properties": {"ok": {"type": "boolean"}}, "required": ["ok"]}
    }
  },
  "sources": {
    "api": {"bindingSpec": "openbindings.openapi-3.1@1", "location": %q}
  },
  "bindings": {
    "read.http": {"operation": "test.read", "source": "api", "selector": "#/paths/~1data/get"}
  }
}`, docURL)
	path := filepath.Join(t.TempDir(), "multi-server.obi.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestConfigValueLoop_ServerSelection is the acceptance test for the
// config.value consumer loop: a multi-server document with no stored context
// refuses with CONTEXT_REQUIRED whose rendering lists the declared servers
// as a numbered picker, applying the equivalent of
// `ob context set <target> --config server=...` files the answer under the
// exact asserted target (the source URL, not its origin), and re-invoking
// succeeds against the selected server without any prompt.
func TestConfigValueLoop_ServerSelection(t *testing.T) {
	setupContextTestDir(t)
	t.Setenv(EnvCredentialsFile, filepath.Join(t.TempDir(), "creds.json"))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverV1 := srv.URL + "/v1"
	serverV2 := srv.URL + "/v2"
	openapiDoc := fmt.Sprintf(`{
  "openapi": "3.1.0",
  "info": {"title": "Multi-Server", "version": "0.0.1"},
  "servers": [{"url": %q}, {"url": %q}],
  "paths": {
    "/data": {
      "get": {
        "operationId": "read",
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
          "type": "object", "properties": {"ok": {"type": "boolean"}}, "required": ["ok"]
        }}}}}
      }
    }
  }
}`, serverV1, serverV2)
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openapiDoc))
	})
	// Only the SECOND declared server carries the data: success after
	// configuration proves the stored selection was actually applied.
	mux.HandleFunc("/v2/data", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/v1/data", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	docURL := srv.URL + "/openapi.json"
	obiPath := multiServerTargetOBI(t, docURL)

	// 1. No context: the invocation refuses with the config.value challenge.
	run, err := InvokeOBIOperationConfigured(context.Background(), obiPath, "test.read", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := <-run.Events
	if !ok || ev.Error == nil {
		t.Fatalf("expected a terminal error, got %#v", ev)
	}
	if ev.Error.Code != invoke.ErrCodeContextRequired {
		t.Fatalf("expected CONTEXT_REQUIRED, got %s", ev.Error.Code)
	}
	details := invoke.ContextRequiredFrom(ev.Error)
	if details == nil {
		t.Fatal("CONTEXT_REQUIRED without details")
	}
	// The engine asserts the artifact's own identity as the scope for the
	// server point (context-scope model): the source URL, not its origin.
	if details.Target != docURL {
		t.Fatalf("challenge target = %q, want the source URL %q", details.Target, docURL)
	}

	// 2. Its rendering is a numbered picker of the declared server URLs.
	rendered := RenderContextRequirements(details)
	for _, want := range []string{"point: server", "1. " + serverV1, "2. " + serverV2} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered challenge missing %q:\n%s", want, rendered)
		}
	}

	// 3. Apply the equivalent of the remedy line the CLI prints
	// (`ob context set <target> --config server='{"url":...}'`): the
	// --config lane merges configuration.<point> under the exact target.
	if err := ApplyContextUpdate(docURL, ContextUpdate{
		Configuration: map[string]any{"server": map[string]any{"url": serverV2}},
	}); err != nil {
		t.Fatal(err)
	}

	// 4. Re-invoke: the resolver finds the durable answer under the exact
	// asserted target and the call succeeds against the selected server.
	run, err = InvokeOBIOperationConfigured(context.Background(), obiPath, "test.read", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev, ok = <-run.Events
	if !ok {
		t.Fatal("no output")
	}
	if ev.Error != nil {
		t.Fatalf("re-invoke after context set failed: %s %v", ev.Error.Code, ev.Error.Data)
	}
	output, _ := ev.Output.(map[string]any)
	if output["ok"] != true {
		t.Fatalf("unexpected output: %#v", ev.Output)
	}
}

// TestConfigValueLoop_ExactTargetKeying pins the keying rule's negative
// space: an answer stored under the source's ORIGIN must not satisfy a
// config-only challenge asserting the source URL — many artifacts can live
// on one host, and one artifact's configuration must not resolve another's
// challenge.
func TestConfigValueLoop_ExactTargetKeying(t *testing.T) {
	setupContextTestDir(t)
	t.Setenv(EnvCredentialsFile, filepath.Join(t.TempDir(), "creds.json"))

	target := "https://host.example/specs/a.json"
	if err := ApplyContextUpdate("https://host.example", ContextUpdate{
		Configuration: map[string]any{"server": map[string]any{"url": "https://host.example"}},
	}); err != nil {
		t.Fatal(err)
	}

	durable := true
	details := &invoke.ContextRequiredDetails{
		Target: target,
		Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{
			Type:    "config.value",
			Durable: &durable,
			Extra:   map[string]any{"point": "server", "path": "/url"},
		}}}},
	}
	resolved, err := CLIContextResolver()(context.Background(), details)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != nil {
		t.Fatalf("origin-stored configuration resolved an exact-target challenge: %#v", resolved)
	}

	// Stored under the exact target, the same challenge resolves — scoped to
	// only the addressed fragment.
	if err := ApplyContextUpdate(target, ContextUpdate{
		Configuration: map[string]any{"server": map[string]any{"url": "https://eu.example"}},
	}); err != nil {
		t.Fatal(err)
	}
	resolved, err = CLIContextResolver()(context.Background(), details)
	if err != nil {
		t.Fatal(err)
	}
	configuration, _ := resolved["configuration"].(map[string]any)
	point, _ := configuration["server"].(map[string]any)
	if point["url"] != "https://eu.example" {
		t.Fatalf("exact-target configuration did not resolve: %#v", resolved)
	}
}
