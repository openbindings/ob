package codegen

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEmittedGoCompiles generates the demo invoker and compiles it against
// the local SDK checkout, proving the emitted shape is real code. Skipped
// when the sibling openbindings-go checkout is not present (e.g. CI building
// ob in isolation).
func TestEmittedGoCompiles(t *testing.T) {
	if _, err := os.Stat(mustAbs(t, "../../../openbindings-go/go.mod")); err != nil {
		t.Skip("sibling openbindings-go checkout not present")
	}
	iface := loadTestInterface(t, "../demo/api/openbindings.json")
	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	code := EmitGo(result, "demoapi")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "client.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	gomod := `module demoapi

go 1.25.0

require github.com/openbindings/openbindings-go v0.2.0

replace github.com/openbindings/openbindings-go => ` + mustAbs(t, "../../../openbindings-go") + `
`
	// A coordinated unpublished SDK candidate can depend on a coordinated
	// runtime candidate. Resolve that selected module before disabling GOWORK;
	// do not accidentally test the candidate SDK against a registry's latest
	// evaluator. This remains a source-checkout test, not publication evidence.
	selected := exec.Command("go", "list", "-m", "-json", "github.com/openbindings/jsonata-runtime/go")
	if raw, err := selected.Output(); err != nil {
		t.Fatalf("resolve selected runtime: %v", err)
	} else {
		var module struct {
			Dir     string
			Main    bool
			Replace *struct{ Dir string }
		}
		if err := json.Unmarshal(raw, &module); err != nil {
			t.Fatal(err)
		}
		if module.Replace != nil {
			module.Dir = module.Replace.Dir
		}
		if (module.Main || module.Replace != nil) && module.Dir != "" {
			gomod += fmt.Sprintf("\nreplace github.com/openbindings/jsonata-runtime/go => %q\n", module.Dir)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	// Resolve the generated consumer's complete module graph before proving
	// that the resulting source builds without network access. A fresh module
	// cache does not necessarily contain superseded transitive go.mod files.
	tidy.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOSUMDB=off")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("emitted Go does not compile:\n%s\n--- emitted code ---\n%s", out, code)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
