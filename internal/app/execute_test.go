package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// ---------------------------------------------------------------------------
// DefaultBindingForOp
// ---------------------------------------------------------------------------

func TestDefaultBindingForOp_NilInterface(t *testing.T) {
	_, got := DefaultBindingForOp("test", nil)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDefaultBindingForOp_NoBindings(t *testing.T) {
	iface := &openbindings.Interface{}
	_, got := DefaultBindingForOp("test", iface)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDefaultBindingForOp_SingleMatch(t *testing.T) {
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.usage1": {Operation: "listPets", Source: "usage1", Ref: "list pets"},
		},
	}
	key, got := DefaultBindingForOp("listPets", iface)
	if got == nil {
		t.Fatal("expected binding, got nil")
	}
	if got.Ref != "list pets" {
		t.Errorf("expected ref 'list pets', got %q", got.Ref)
	}
	if key != "listPets.usage1" {
		t.Errorf("expected key 'listPets.usage1', got %q", key)
	}
}

func TestDefaultBindingForOp_NoMatch(t *testing.T) {
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"createPet.usage1": {Operation: "createPet", Source: "usage1"},
		},
	}
	_, got := DefaultBindingForOp("listPets", iface)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDefaultBindingForOp_PrioritySelection(t *testing.T) {
	lo := 1.0
	hi := 10.0
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.backup":  {Operation: "listPets", Source: "backup", Ref: "backup-ref", Priority: &hi},
			"listPets.primary": {Operation: "listPets", Source: "primary", Ref: "primary-ref", Priority: &lo},
		},
	}
	_, got := DefaultBindingForOp("listPets", iface)
	if got == nil {
		t.Fatal("expected binding, got nil")
	}
	if got.Source != "primary" {
		t.Errorf("expected source 'primary' (lower priority wins), got %q", got.Source)
	}
}

func TestDefaultBindingForOp_NilPriorityLosesToExplicit(t *testing.T) {
	explicit := 5.0
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.explicit": {Operation: "listPets", Source: "explicit", Ref: "e", Priority: &explicit},
			"listPets.default":  {Operation: "listPets", Source: "default", Ref: "d"}, // nil → +Inf
		},
	}
	_, got := DefaultBindingForOp("listPets", iface)
	if got == nil {
		t.Fatal("expected binding, got nil")
	}
	if got.Source != "explicit" {
		t.Errorf("expected source 'explicit' (nil priority loses to explicit), got %q", got.Source)
	}
}

// ---------------------------------------------------------------------------
// ExecuteOBIOperation
// ---------------------------------------------------------------------------

// writeOBIFile writes a JSON OBI to a temp directory and returns the path.
func writeOBIFile(t *testing.T, dir string, iface map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(iface, "", "  ")
	if err != nil {
		t.Fatalf("marshal OBI: %v", err)
	}
	path := filepath.Join(dir, "interface.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write OBI: %v", err)
	}
	return path
}

func TestExecuteOBIOperation_FileNotFound(t *testing.T) {
	_, err := ExecuteOBIOperation(context.Background(), "/nonexistent/file.json", "test", "", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExecuteOBIOperation_OperationNotFound(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
	})

	_, err := ExecuteOBIOperation(context.Background(), obi, "deletePets", "", nil)
	if err == nil {
		t.Fatal("expected error for missing operation")
	}
}

func TestExecuteOBIOperation_NoBinding(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
	})

	_, err := ExecuteOBIOperation(context.Background(), obi, "listPets", "", nil)
	if err == nil {
		t.Fatal("expected error for missing binding")
	}
}

func TestExecuteOBIOperation_MissingSource(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
		"bindings": map[string]any{
			"listPets.nonexistent": map[string]any{
				"operation": "listPets",
				"source":    "nonexistent",
			},
		},
	})

	_, err := ExecuteOBIOperation(context.Background(), obi, "listPets", "", nil)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestExecuteOBIOperation_BindingKeyResolvesOperation(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
		"sources": map[string]any{
			"usage1": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
			},
		},
		"bindings": map[string]any{
			"listPets.usage1": map[string]any{
				"operation": "listPets",
				"source":    "usage1",
				"ref":       "list pets",
			},
		},
	})

	// Provide only the binding key (no operation key).
	// The operation should be resolved from the binding entry.
	ch, err := ExecuteOBIOperation(context.Background(), obi, "", "listPets.usage1", nil)
	// We expect it to proceed past operation/binding resolution. It will fail
	// at the handler level (no actual cli.kdl file), which is fine — we're
	// testing that the binding-based path resolves the operation correctly.
	if err != nil {
		// Resolution errors that indicate "not_found" or "binding_not_found" are bugs.
		errStr := err.Error()
		if strings.Contains(errStr, "not_found") || strings.Contains(errStr, "binding_not_found") {
			t.Fatalf("binding key should have resolved the operation, got: %s", errStr)
		}
		// Other errors (e.g., no delegate) are expected — resolution succeeded.
		return
	}
	for range ch {
	}
}

func TestExecuteOBIOperation_BindingKeyNotFound(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
	})

	_, err := ExecuteOBIOperation(context.Background(), obi, "", "nonexistent.binding", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent binding key")
	}
}

func TestExecuteOBIOperation_InputTransformError(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
		"sources": map[string]any{
			"usage1": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
			},
		},
		"bindings": map[string]any{
			"listPets.usage1": map[string]any{
				"operation": "listPets",
				"source":    "usage1",
				"ref":       "list pets",
				"inputTransform": map[string]any{
					"type":       "jsonata",
					"expression": "$$$$invalid$$$$",
				},
			},
		},
	})

	_, err := ExecuteOBIOperation(context.Background(), obi, "listPets", "", map[string]any{"limit": 10})
	if err == nil {
		t.Fatal("expected error for bad input transform")
	}
}
