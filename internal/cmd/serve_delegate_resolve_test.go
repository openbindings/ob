package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// writeResolvableDelegateFixture writes an executable that answers
// --openbindings with a minimal OBI carrying one operation, returning its
// exec: location. A local copy of internal/app's writeFakeDelegate test
// helper: that one lives in a _test.go file in a different package and
// isn't importable from here.
func writeResolvableDelegateFixture(t *testing.T, dir string) string {
	t.Helper()
	obi := `{"openbindings":"0.2.0","name":"resolve-fixture","version":"0.1.0","operations":{"acme.fixture.ping":{}}}`
	path := filepath.Join(dir, "resolve-fixture")
	script := "#!/bin/sh\nif [ \"$1\" = \"--openbindings\" ]; then\ncat <<'OBI'\n" + obi + "\nOBI\nfi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return "exec:" + path
}

// --- /delegates/resolve/{operation} ---

// TestServeResolveDelegate exercises the read-only counterpart of `ob
// delegate resolve` (fix B4-3: resolveDelegate was missing from the served
// HTTP surface despite the ob-lexicon's H marker). A registered scratch
// delegate carrying the requested operation must resolve as a candidate; an
// operation nothing carries must still answer 200 with an empty candidate
// list — resolveDelegate never invokes anything, so "nothing carries this"
// is a literal answer, not an error.
func TestServeResolveDelegate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "") // linux: fall back to HOME/.config
	t.Chdir(dir)
	if _, err := app.Init(false); err != nil {
		t.Fatalf("init environment: %v", err)
	}
	loc := writeResolvableDelegateFixture(t, dir)
	if _, err := app.RegisterDelegate(loc, nil); err != nil {
		t.Fatalf("register delegate: %v", err)
	}

	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/delegates/resolve/acme.fixture.ping", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := mustJSON(t, resp)
	if body["operation"] != "acme.fixture.ping" {
		t.Errorf("operation echoed = %v, want acme.fixture.ping", body["operation"])
	}
	candidates, ok := body["candidates"].([]any)
	if !ok || len(candidates) != 1 {
		t.Fatalf("candidates = %v, want exactly the registered delegate", body["candidates"])
	}
	cand, ok := candidates[0].(map[string]any)
	if !ok || cand["location"] != loc {
		t.Errorf("candidate location = %v, want %q", cand["location"], loc)
	}

	// An operation nothing carries is a literal 200 with empty candidates,
	// not an error.
	resp2, err := authedGet(ts.URL+"/delegates/resolve/acme.nothing.carries.this", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}
	body2 := mustJSON(t, resp2)
	candidates2, ok := body2["candidates"].([]any)
	if !ok || len(candidates2) != 0 {
		t.Errorf("candidates = %v, want an empty array for an operation nothing carries", body2["candidates"])
	}
}
