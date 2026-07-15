package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// tinyOpenAPI is a minimal but derivable spec: one operation, listThings.
const tinyOpenAPI = `{"openapi":"3.1.0","info":{"title":"Tiny","version":"1.0.0"},"paths":{"/things":{"get":{"operationId":"listThings","responses":{"200":{"description":"ok"}}}}}}`

// runOB executes the root command with args, tolerating the ExitResult-as-
// error convention: a zero-code ExitResult is a success.
func runOB(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRoot()
	root.SetArgs(args)
	err := root.Execute()
	if er, ok := err.(app.ExitResult); ok && er.Code == 0 {
		return nil
	}
	return err
}

// TestSynthesizeMachineLane: --input carries the operation's wire input
// (SynthesizeInterfaceInput) wholesale — the argv a delegate invocation
// produces through the bound OBI's machine-lane inputTransform.
func TestSynthesizeMachineLane(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "api.json")
	if err := os.WriteFile(specPath, []byte(tinyOpenAPI), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.obi.json")

	input := fmt.Sprintf(`{"sources":[{"bindingSpec":"openbindings.openapi@1","location":%q}],"name":"Machine"}`, specPath)
	if err := runOB(t, "synthesize", "--input", input, "-o", outPath); err != nil {
		t.Fatalf("synthesize --input: %v", err)
	}

	var doc struct {
		Name       string                     `json:"name"`
		Operations map[string]json.RawMessage `json:"operations"`
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Name != "Machine" {
		t.Errorf("name: got %q, want %q", doc.Name, "Machine")
	}
	if _, ok := doc.Operations["listThings"]; !ok {
		t.Errorf("expected operation listThings, got %v", keysOf(doc.Operations))
	}
}

// TestSynthesizeMachineLane_ContentSource: a wire source may provide content
// directly instead of a location (the schema's anyOf); the synthesized OBI
// carries it inline.
func TestSynthesizeMachineLane_ContentSource(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.obi.json")

	input := fmt.Sprintf(`{"sources":[{"bindingSpec":"openbindings.openapi@1","content":%s}]}`, tinyOpenAPI)
	if err := runOB(t, "synthesize", "--input", input, "-o", outPath); err != nil {
		t.Fatalf("synthesize --input (content source): %v", err)
	}

	var doc struct {
		Operations map[string]json.RawMessage `json:"operations"`
		Sources    map[string]struct {
			Location string `json:"location"`
			Content  any    `json:"content"`
		} `json:"sources"`
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Operations["listThings"]; !ok {
		t.Errorf("expected operation listThings, got %v", keysOf(doc.Operations))
	}
	src, ok := doc.Sources["openapi"]
	if !ok {
		t.Fatalf("expected an openapi source entry, got %v", doc.Sources)
	}
	if src.Content == nil || src.Location != "" {
		t.Errorf("content source: expected inline content and no location, got content=%v location=%q", src.Content != nil, src.Location)
	}
}

// TestSynthesizeMachineLane_Exclusive: --input rejects mixing with the human
// lane (source arguments, metadata flags).
func TestSynthesizeMachineLane_Exclusive(t *testing.T) {
	err := runOB(t, "synthesize", "--input", `{}`, "some.json")
	er, ok := err.(app.ExitResult)
	if !ok || er.Code != 2 {
		t.Fatalf("expected usage error (code 2), got %v", err)
	}
}

// TestInspectMachineLane: --input carries InspectSourceInput ({source: ...})
// wholesale, mirroring the wire schema's wrapped shape.
func TestInspectMachineLane(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "api.json")
	if err := os.WriteFile(specPath, []byte(tinyOpenAPI), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "inspection.json")

	input := fmt.Sprintf(`{"source":{"bindingSpec":"openbindings.openapi@1","location":%q}}`, specPath)
	if err := runOB(t, "inspect", "--input", input, "-o", outPath); err != nil {
		t.Fatalf("inspect --input: %v", err)
	}

	// The machine lane defaults to wire-shaped (JSON) output, not the human
	// rendering — a delegate invocation must round-trip the structure.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var inspection struct {
		Targets []struct {
			Ref string `json:"ref"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(data, &inspection); err != nil {
		t.Fatalf("machine-lane output is not wire-shaped JSON: %v\n%s", err, data)
	}
	if len(inspection.Targets) != 1 {
		t.Fatalf("expected 1 bindable target, got %d", len(inspection.Targets))
	}
	if !strings.Contains(string(data), "listThings") {
		t.Errorf("expected inspection to list listThings, got: %s", data)
	}
}

// TestInspectRequiresSourceOrInput: no positional and no --input is a usage
// error, not a panic or an empty inspection.
func TestInspectRequiresSourceOrInput(t *testing.T) {
	err := runOB(t, "inspect")
	er, ok := err.(app.ExitResult)
	if !ok || er.Code != 2 {
		t.Fatalf("expected usage error (code 2), got %v", err)
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
