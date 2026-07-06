package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
)

// TestWireConformance_ExecLane drives operations through the bound CLI OBI's
// exec lane exactly as a delegate registrar would: resolve the bound OBI,
// operation-invoke, exec the real `ob` binary, parse stdout. Every output is
// then validated against the operation's output schema (the SDK's OBI-T-08
// machinery via ValidateAgainstSchema — ob's own invoke path does not enforce
// T-08 yet; see the tracker's parked decision), so an op passing here is
// wire-conformant end to end: input field mapping, argv, exec, output shape.
//
// Cohort A of the wire-conformance loop (ob-pj/wire-conformance.md): flat
// wire inputs, -F json forced by the generated binding.
func TestWireConformance_ExecLane(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and execs the real binary")
	}

	// The bound OBI, resolved before we chdir into the sandbox. Its schemas
	// judge the outputs below.
	obiPath, err := filepath.Abs("../app/ob.obi.json")
	if err != nil {
		t.Fatal(err)
	}
	obiData, err := os.ReadFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	var bound openbindings.Interface
	if err := json.Unmarshal(obiData, &bound); err != nil {
		t.Fatal(err)
	}

	// Build the real binary; the bound OBI's usage spec names `bin "ob"`, which
	// the usage transport resolves via PATH.
	binDir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(binDir, "ob"), "./cmd/ob")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ob: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// A registrable delegate fixture: the same binary under a different name
	// (registering `exec:ob` itself is refused as the self-delegate).
	binBytes, err := os.ReadFile(filepath.Join(binDir, "ob"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ob-fixture"), binBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	// A resolvable interface fixture for resolveInterface: a minimal OBI
	// served over HTTP from the test process.
	fixtureOBI := `{"openbindings":"0.2.0","name":"fixture","operations":{"ping":{}}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixtureOBI))
	}))
	defer ts.Close()

	// Sandbox: HOME redirects the global config dir (contexts, delegates,
	// global environment); a temp cwd catches local-environment writes.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "") // linux: fall back to HOME/.config
	workDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Ordered: later cases depend on earlier state (the initialized
	// environment, the registered delegate, the stored context).
	cases := []struct {
		name  string
		op    string
		input any
		check func(t *testing.T, output any)
	}{
		{"describe", "openbindings.ob.describe", nil, nil},
		{"listFormats", "openbindings.ob.listFormats", nil, nil},
		{"initializeEnvironment", "openbindings.ob.initializeEnvironment", map[string]any{"global": true}, nil},
		{"reportEnvironmentStatus", "openbindings.ob.reportEnvironmentStatus", nil, nil},
		{"listContexts", "openbindings.ob.listContexts", nil, nil},
		{"listDelegates", "openbindings.ob.listDelegates", nil, nil},
		{"resolveDelegate", "openbindings.ob.resolveDelegate", map[string]any{"operation": "openbindings.ob.describe"}, nil},
		{"getDelegateRequirements", "openbindings.ob.getDelegateRequirements", map[string]any{"capability": "invoke"}, nil},
		{"getContext_missing", "openbindings.ob.getContext", map[string]any{"key": "https://missing.example.com"}, func(t *testing.T, output any) {
			if output != nil {
				t.Errorf("expected null for a missing context, got %#v", output)
			}
		}},
		{"setContext", "openbindings.ob.setContext", map[string]any{
			"key":   "https://wire.example.com",
			"value": map[string]any{"headers": map[string]any{"X-Team": "blue"}},
		}, nil},
		{"getContext", "openbindings.ob.getContext", map[string]any{"key": "https://wire.example.com"}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if h, _ := m["headers"].(map[string]any); h == nil || h["X-Team"] != "blue" {
				t.Errorf("expected the stored context back, got %#v", output)
			}
		}},
		{"removeContext", "openbindings.ob.removeContext", map[string]any{"key": "https://wire.example.com"}, nil},
		{"resolveDelegateForFormat", "openbindings.ob.resolveDelegateForFormat", map[string]any{"format": "openbindings.usage@0.1.0"}, nil},
		{"registerDelegate", "openbindings.ob.registerDelegate", map[string]any{"location": "exec:ob-fixture", "preference": 5}, nil},
		{"setDelegatePreference", "openbindings.ob.setDelegatePreference", map[string]any{"location": "exec:ob-fixture", "preference": 10}, nil},
		{"unregisterDelegate", "openbindings.ob.unregisterDelegate", map[string]any{"location": "exec:ob-fixture"}, nil},
		{"resolveInterface", "openbindings.ob.resolveInterface", map[string]any{"address": ts.URL}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			iface, _ := m["interface"].(map[string]any)
			if iface == nil || iface["name"] != "fixture" {
				t.Errorf("expected the fixture interface in the envelope, got %#v", output)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			ch, _, err := app.InvokeOBIOperation(ctx, obiPath, tc.op, "", tc.input)
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}

			outputSchema := bound.Operations[tc.op].Output

			got := 0
			for ev := range ch {
				if ev.Error != nil {
					t.Fatalf("invocation error: %s: %s", ev.Error.Code, ev.Error.Message)
				}
				// The conformance judgment: the output must satisfy the
				// operation's output schema (OBI-T-08 semantics).
				if outputSchema != nil {
					if verr := openbindings.ValidateAgainstSchema(ev.Output, outputSchema, bound.Schemas); verr != nil {
						t.Fatalf("output does not conform to the operation's output schema: %v\noutput: %#v", verr, ev.Output)
					}
				}
				// Belt and braces for permissive output schemas: the usage
				// transport's non-JSON fallback must never leak through.
				if m, ok := ev.Output.(map[string]any); ok {
					if _, leaked := m["stdout"]; leaked {
						t.Fatalf("output is the {stdout} wrapper, not the contract shape: %#v", ev.Output)
					}
				}
				if tc.check != nil {
					tc.check(t, ev.Output)
				}
				got++
			}
			if got == 0 {
				t.Fatal("invocation produced no output")
			}
		})
	}
}

// repoRoot locates the module root (two levels above internal/cmd).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
