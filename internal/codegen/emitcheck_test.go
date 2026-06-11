package codegen

import (
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
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off")
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
