package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// runOBWithStdin executes the root command with args and the given stdin,
// tolerating the ExitResult-as-error convention like runOB.
func runOBWithStdin(t *testing.T, stdin io.Reader, args ...string) error {
	t.Helper()
	root := NewRoot()
	root.SetIn(stdin)
	root.SetArgs(args)
	err := root.Execute()
	if er, ok := err.(app.ExitResult); ok && er.Code == 0 {
		return nil
	}
	return err
}

// TestSynthesizeStdinSource: a source location of `-` reads the artifact from
// stdin. The result must match the file-path equivalent byte-for-byte outside
// the source entry, and the source entry must follow the wire-content
// convention: inline content, no location, no fabricated "-" pull path.
func TestSynthesizeStdinSource(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "api.json")
	if err := os.WriteFile(specPath, []byte(tinyOpenAPI), 0o644); err != nil {
		t.Fatal(err)
	}
	stdinOut := filepath.Join(dir, "stdin.obi.json")
	fileOut := filepath.Join(dir, "file.obi.json")

	// ?name=api pins the source key on both lanes so operations and bindings
	// come out identical.
	if err := runOBWithStdin(t, strings.NewReader(tinyOpenAPI),
		"synthesize", "openbindings.openapi@1:-?name=api", "-o", stdinOut); err != nil {
		t.Fatalf("synthesize from stdin: %v", err)
	}
	if err := runOB(t, "synthesize", "openbindings.openapi@1:"+specPath+"?name=api", "-o", fileOut); err != nil {
		t.Fatalf("synthesize from file: %v", err)
	}

	var stdinDoc, fileDoc map[string]any
	stdinData, err := os.ReadFile(stdinOut)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdinData, &stdinDoc); err != nil {
		t.Fatal(err)
	}
	fileData, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fileData, &fileDoc); err != nil {
		t.Fatal(err)
	}

	// The source entry: content mode, exactly like a wire-supplied content
	// source — inline content, no location, and no "-" recorded anywhere
	// (stdin is content, not a location; there is no pull path).
	sources, _ := stdinDoc["sources"].(map[string]any)
	entry, ok := sources["api"].(map[string]any)
	if !ok {
		t.Fatalf("expected source key %q, got %v", "api", keysOf(sources))
	}
	if entry["content"] == nil {
		t.Error("stdin source: expected embedded content")
	}
	if loc, present := entry["location"]; present {
		t.Errorf("stdin source: expected no location, got %q", loc)
	}
	if xob, _ := entry["x-ob"].(map[string]any); xob != nil {
		if ref, _ := xob["ref"].(string); ref != "" {
			t.Errorf("stdin source: expected no pull path in x-ob.ref, got %q", ref)
		}
	}

	// Everything outside the source entry matches the file-path lane.
	delete(stdinDoc, "sources")
	delete(fileDoc, "sources")
	stdinRest, _ := json.Marshal(stdinDoc)
	fileRest, _ := json.Marshal(fileDoc)
	if string(stdinRest) != string(fileRest) {
		t.Errorf("stdin and file synthesis diverge outside the source entry:\nstdin: %s\nfile:  %s", stdinRest, fileRest)
	}
}

// TestSynthesizeStdinSource_OutputLocation: `?outputLocation=` means
// spec-level `location` on the stdin lane too — the published pointer pairs
// with the embedded artifact (spec §6.4) exactly as on the file lane's
// `?embed&outputLocation=`, and mirrors into x-ob.uri. There is still no
// pull path: stdin remains content, not a location.
func TestSynthesizeStdinSource_OutputLocation(t *testing.T) {
	published := "https://example.com/openapi.json"
	outPath := filepath.Join(t.TempDir(), "out.obi.json")
	if err := runOBWithStdin(t, strings.NewReader(tinyOpenAPI),
		"synthesize", "openbindings.openapi@1:-?name=api&outputLocation="+published, "-o", outPath); err != nil {
		t.Fatalf("synthesize from stdin: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	sources, _ := doc["sources"].(map[string]any)
	entry, ok := sources["api"].(map[string]any)
	if !ok {
		t.Fatalf("expected source key %q, got %v", "api", keysOf(sources))
	}
	if loc, _ := entry["location"].(string); loc != published {
		t.Errorf("spec-level location = %q, want %q", loc, published)
	}
	if entry["content"] == nil {
		t.Error("expected embedded content alongside the published location")
	}
	xob, _ := entry["x-ob"].(map[string]any)
	if xob == nil {
		t.Fatal("expected x-ob metadata")
	}
	if uri, _ := xob["uri"].(string); uri != published {
		t.Errorf("x-ob.uri = %q, want %q (mirrors location, as on every lane)", uri, published)
	}
	if ref, _ := xob["ref"].(string); ref != "" {
		t.Errorf("expected no pull path in x-ob.ref, got %q", ref)
	}
}

// TestSynthesizeInputContentOutputLocation: the machine lane (--input with a
// wire content source) honors outputLocation identically — spec-level
// location, mirrored x-ob.uri.
func TestSynthesizeInputContentOutputLocation(t *testing.T) {
	published := "https://example.com/openapi.json"
	outPath := filepath.Join(t.TempDir(), "out.obi.json")
	input := `{"sources":[{"bindingSpec":"openbindings.openapi@1","name":"api","content":` + tinyOpenAPI + `,"outputLocation":"` + published + `"}]}`
	if err := runOB(t, "synthesize", "--input", input, "-o", outPath); err != nil {
		t.Fatalf("synthesize --input: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	sources, _ := doc["sources"].(map[string]any)
	entry, ok := sources["api"].(map[string]any)
	if !ok {
		t.Fatalf("expected source key %q, got %v", "api", keysOf(sources))
	}
	if loc, _ := entry["location"].(string); loc != published {
		t.Errorf("spec-level location = %q, want %q", loc, published)
	}
	if entry["content"] == nil {
		t.Error("expected the wire-supplied content to remain embedded")
	}
	xob, _ := entry["x-ob"].(map[string]any)
	if xob == nil {
		t.Fatal("expected x-ob metadata")
	}
	if uri, _ := xob["uri"].(string); uri != published {
		t.Errorf("x-ob.uri = %q, want %q", uri, published)
	}
}

// TestSynthesizeStdinSource_DetectsFormat: a bare `-` runs format detection
// over the stdin bytes, the same consensus probe a bare file path gets.
func TestSynthesizeStdinSource_DetectsFormat(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.obi.json")
	if err := runOBWithStdin(t, strings.NewReader(tinyOpenAPI), "synthesize", "-", "-o", outPath); err != nil {
		t.Fatalf("synthesize bare - : %v", err)
	}

	var doc struct {
		Operations map[string]json.RawMessage `json:"operations"`
		Sources    map[string]struct {
			BindingSpec string `json:"bindingSpec"`
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
	if src, ok := doc.Sources["openapi"]; !ok || src.BindingSpec != "openbindings.openapi@5" {
		t.Errorf("expected a detected openbindings.openapi@5 source under key %q, got %v", "openapi", doc.Sources)
	}
}

// TestSynthesizeStdinSource_UndetectableRefused: bytes no format claims are
// refused as a usage error naming the fix (an explicit format).
func TestSynthesizeStdinSource_UndetectableRefused(t *testing.T) {
	err := runOBWithStdin(t, bytes.NewReader([]byte{0x00, 0x01, 0xff}), "synthesize", "-")
	er, ok := err.(app.ExitResult)
	if !ok || er.Code != 2 {
		t.Fatalf("expected usage error (code 2), got %v", err)
	}
	if !strings.Contains(er.Message, "could not detect the format") {
		t.Errorf("expected a detection failure naming the fix, got %q", er.Message)
	}
}

// TestSynthesizeStdinSource_SingleUse: stdin is consumed once; a second `-`
// source in the same invocation is a usage error, not a silent empty parse.
func TestSynthesizeStdinSource_SingleUse(t *testing.T) {
	err := runOBWithStdin(t, strings.NewReader(tinyOpenAPI),
		"synthesize", "openbindings.openapi@1:-", "openbindings.usage@1:-")
	er, ok := err.(app.ExitResult)
	if !ok || er.Code != 2 {
		t.Fatalf("expected usage error (code 2), got %v", err)
	}
	if !strings.Contains(er.Message, "at most one source") {
		t.Errorf("expected the single-stdin-source refusal, got %q", er.Message)
	}
}

// TestSynthesizeStdinSource_ContentOnlyFamilyRefused: a family whose
// synthesis lane cannot work from bytes alone refuses the stdin artifact
// loudly. MCP is that family: content is a pin that still requires the
// server location (MCP-D-02), so `-` alone cannot satisfy it.
func TestSynthesizeStdinSource_ContentOnlyFamilyRefused(t *testing.T) {
	err := runOBWithStdin(t, strings.NewReader(`{"tools":[{"name":"probe"}]}`),
		"synthesize", "openbindings.mcp@1:-")
	er, ok := err.(app.ExitResult)
	if !ok || er.Code != 1 {
		t.Fatalf("expected synthesis failure (code 1), got %v", err)
	}
	if !strings.Contains(er.Message, "MCP-D-02") {
		t.Errorf("expected the MCP-D-02 refusal to surface, got %q", er.Message)
	}
}

// TestInspectStdinSource: inspect shares the source grammar, so `-` reads the
// artifact from stdin there too.
func TestInspectStdinSource(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "inspection.json")
	if err := runOBWithStdin(t, strings.NewReader(tinyOpenAPI),
		"inspect", "openbindings.openapi@1:-", "-F", "json", "-o", outPath); err != nil {
		t.Fatalf("inspect from stdin: %v", err)
	}

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
		t.Fatalf("inspection output is not JSON: %v\n%s", err, data)
	}
	if len(inspection.Targets) != 1 {
		t.Fatalf("expected 1 bindable target, got %d", len(inspection.Targets))
	}
	if !strings.Contains(string(data), "listThings") {
		t.Errorf("expected inspection to list listThings, got: %s", data)
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
