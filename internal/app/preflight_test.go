package app

import (
	"context"
	"path/filepath"
	"testing"
)

// obiFromUsage creates an OBI from a single usage spec and writes it, returning
// the OBI path. Used to exercise the operation-level preflight end to end.
func obiFromUsage(t *testing.T, dir, kdl string) string {
	t.Helper()
	writeUsageFile(t, dir, "cli.kdl", kdl)
	iface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Sources: []SynthesizeInterfaceSource{{BindingSpec: usageFormat, Location: filepath.Join(dir, "cli.kdl")}},
		Name:    "app",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	obiPath := filepath.Join(dir, "interface.json")
	if err := WriteInterfaceToPath(obiPath, iface, ""); err != nil {
		t.Fatalf("write OBI: %v", err)
	}
	return obiPath
}

// TestPreflightOperation_ResolvesAndPreflights verifies that PreflightOperation
// resolves an operation to its binding and runs the binding-level preflight.
// A usage (CLI) binding declares no context requirements, so the preflight
// returns nil — the conformant "nothing required" answer.
func TestPreflightOperation_ResolvesAndPreflights(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	obiPath := obiFromUsage(t, dir, `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hi" {}
`)

	details, err := PreflightOperation(context.Background(), obiPath, "greet", "", nil)
	if err != nil {
		t.Fatalf("PreflightOperation: %v", err)
	}
	if details != nil {
		t.Errorf("expected nil context requirements for a usage binding, got %+v", details)
	}
}

// TestPreflightOperation_UnknownOperation verifies a resolution error surfaces
// rather than a nil/no-op result.
func TestPreflightOperation_UnknownOperation(t *testing.T) {
	dir := t.TempDir()
	obiPath := obiFromUsage(t, dir, `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hi" {}
`)

	if _, err := PreflightOperation(context.Background(), obiPath, "nonexistent", "", nil); err == nil {
		t.Fatal("expected error resolving an unknown operation")
	}
}
