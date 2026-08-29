package app

import (
	"os"
	"path/filepath"
	"testing"
)

// R3 (scoped): the binding view closes "bindings writable but unviewable" —
// list an OBI's bindings, optionally filtered to one operation.
func TestBindingList(t *testing.T) {
	dir := t.TempDir()
	obiPath := filepath.Join(dir, "iface.obi.json")
	doc := `{
	  "openbindings": "0.2.0",
	  "operations": { "getWidget": {}, "listAll": {} },
	  "sources": { "api": { "bindingSpec": "openbindings.openapi-3.1@1", "content": {} } },
	  "bindings": {
	    "getWidget.api": { "operation": "getWidget", "source": "api", "selector": "#/paths/~1widget/get" },
	    "listAll.api":   { "operation": "listAll", "source": "api", "selector": "#/paths/~1all/get", "deprecated": true }
	  }
	}`
	if err := os.WriteFile(obiPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	all, err := BindingList(obiPath, "")
	if err != nil {
		t.Fatalf("BindingList: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d bindings, want 2", len(all))
	}
	// Sorted by key: getWidget.api before listAll.api.
	if all[0].Key != "getWidget.api" || all[0].Operation != "getWidget" || all[0].Source != "api" {
		t.Errorf("binding[0] = %+v", all[0])
	}
	if !all[1].Deprecated {
		t.Errorf("listAll.api should be deprecated: %+v", all[1])
	}

	// Filter to one operation.
	one, err := BindingList(obiPath, "listAll")
	if err != nil {
		t.Fatalf("BindingList(filter): %v", err)
	}
	if len(one) != 1 || one[0].Key != "listAll.api" {
		t.Fatalf("filtered = %+v, want just listAll.api", one)
	}

	// A no-binding filter renders cleanly, not a crash.
	none, _ := BindingList(obiPath, "getWidget")
	if r := none.Render(); r == "" {
		t.Error("render of a single-op filter should be non-empty")
	}
}
