package cmd

import (
	"context"
	"encoding/json"
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

	cases := []struct {
		op    string
		input any
	}{
		{"openbindings.ob.describe", nil},
		{"openbindings.ob.listFormats", nil},
		{"openbindings.ob.initializeEnvironment", map[string]any{"global": true}},
		{"openbindings.ob.reportEnvironmentStatus", nil},
		{"openbindings.ob.listContexts", nil},
		{"openbindings.ob.listDelegates", nil},
		{"openbindings.ob.resolveDelegate", map[string]any{"operation": "openbindings.ob.describe"}},
		{"openbindings.ob.getDelegateRequirements", map[string]any{"capability": "invoke"}},
	}

	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
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
