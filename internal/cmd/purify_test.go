package cmd

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
)

// A managed interface carrying x-ob at all four structural levels the stripper
// touches: document root, each source, each operation, each binding.
const managedFixture = `{
  "openbindings": "0.2.0",
  "name": "fixture",
  "version": "0.1.0",
  "x-ob": {"obVersion": "0.1.0"},
  "sources": {
    "api": {"format": "openapi@3.1", "location": "http://example.test/openapi.json", "x-ob": {"ref": "http://example.test/openapi.json"}}
  },
  "operations": {
    "getThing": {"description": "d", "output": {"type": "object"}, "x-ob": {"base": {"description": "d"}}}
  },
  "bindings": {
    "getThing.api": {"operation": "getThing", "source": "api", "ref": "#/paths/thing/get", "x-ob": {"base": {}}}
  }
}`

// TestPurifyGraphMatchesStripAllXOB is the correctness bar for the purify graph:
// running the operation-graph must produce exactly what the Go reference stripper
// (app.StripAllXOB) produces. If jsonata-go or the graph drifts, this fails.
func TestPurifyGraphMatchesStripAllXOB(t *testing.T) {
	var doc any
	if err := json.Unmarshal([]byte(managedFixture), &doc); err != nil {
		t.Fatal(err)
	}
	result := app.InvokeOperationWithContext(context.Background(), app.InvokeOperationInput{
		Source: app.InvokeSource{Format: "openbindings.operation-graph@0.2.0", Content: purifyGraph},
		Ref:    "#/graphs/purify",
		Input:  doc,
	})
	if result.Error != nil {
		t.Fatalf("purify graph invocation failed: %s", result.Error.Message)
	}

	var iface openbindings.Interface
	if err := json.Unmarshal([]byte(managedFixture), &iface); err != nil {
		t.Fatal(err)
	}
	app.StripAllXOB(&iface)

	gotJSON, _ := json.Marshal(result.Output)
	wantJSON, _ := json.Marshal(&iface)
	var gotV, wantV any
	_ = json.Unmarshal(gotJSON, &gotV)
	_ = json.Unmarshal(wantJSON, &wantV)
	if !reflect.DeepEqual(gotV, wantV) {
		t.Errorf("purify graph output != StripAllXOB output\n graph: %s\n strip: %s", gotJSON, wantJSON)
	}
	if strings.Contains(string(gotJSON), "x-ob") {
		t.Errorf("purify left x-ob behind: %s", gotJSON)
	}
}
