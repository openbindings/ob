package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/openbindings-go"
)

func TestHashContent(t *testing.T) {
	hash := HashContent([]byte("hello world"))
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash[:7] != "sha256:" {
		t.Errorf("expected sha256: prefix, got %q", hash)
	}

	// Same input → same hash.
	hash2 := HashContent([]byte("hello world"))
	if hash != hash2 {
		t.Errorf("expected same hash for same input")
	}

	// Different input → different hash.
	hash3 := HashContent([]byte("hello world!"))
	if hash == hash3 {
		t.Error("expected different hash for different input")
	}
}

func TestGetSetSourceMeta(t *testing.T) {
	src := openbindings.Source{Format: "usage@2.0.0"}

	// No metadata initially.
	meta, err := GetSourceMeta(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Fatal("expected nil meta for source without x-ob")
	}

	// Set metadata.
	expected := SourceMeta{
		Ref:         "./usage.kdl",
		Resolve:     ResolveModeContent,
		ContentHash: "sha256:abc123",
		LastSynced:  "2026-01-30T12:00:00Z",
		OBVersion:   "0.1.0",
	}
	if err := SetSourceMeta(&src, expected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read it back.
	got, err := GetSourceMeta(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil meta")
	}
	if got.Ref != expected.Ref {
		t.Errorf("ref: got %q, want %q", got.Ref, expected.Ref)
	}
	if got.Resolve != expected.Resolve {
		t.Errorf("resolve: got %q, want %q", got.Resolve, expected.Resolve)
	}
	if got.ContentHash != expected.ContentHash {
		t.Errorf("contentHash: got %q, want %q", got.ContentHash, expected.ContentHash)
	}
	if got.LastSynced != expected.LastSynced {
		t.Errorf("lastSynced: got %q, want %q", got.LastSynced, expected.LastSynced)
	}
}

func TestHasXOB_SetXOB(t *testing.T) {
	lf := openbindings.LosslessFields{}
	if HasXOB(lf) {
		t.Error("expected no x-ob initially")
	}

	SetXOB(&lf)
	if !HasXOB(lf) {
		t.Error("expected x-ob after SetXOB")
	}

	// Verify the value is empty object.
	raw := lf.Extensions[xobKey]
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(obj) != 0 {
		t.Errorf("expected empty object, got %v", obj)
	}
}

func TestStripAllXOB(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"src1": {
				Format:         "usage@2.0.0",
				LosslessFields: openbindings.LosslessFields{Extensions: map[string]json.RawMessage{xobKey: json.RawMessage(`{"ref":"./x"}`)}},
			},
		},
		Operations: map[string]openbindings.Operation{
			"hello": {
				// A floor-stamped output schema (x-ob INSIDE the schema body)
				// plus a nested floor-stamp reached through properties.
				Output: openbindings.JSONSchema{
					"type": "object",
					"x-ob": map[string]any{"floor": "text"},
					"properties": map[string]any{
						"inner": map[string]any{"type": "string", "x-ob": map[string]any{"floor": "text"}},
					},
				},
				LosslessFields: openbindings.LosslessFields{Extensions: map[string]json.RawMessage{xobKey: json.RawMessage(`{}`)}},
			},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"hello.src1": {
				LosslessFields: openbindings.LosslessFields{Extensions: map[string]json.RawMessage{xobKey: json.RawMessage(`{}`)}},
			},
		},
		Schemas: map[string]openbindings.JSONSchema{
			"Pet": {"type": "object", "x-ob": map[string]any{"floor": "text"}},
		},
	}

	StripAllXOB(iface)

	// Verify all x-ob removed.
	if HasXOB(iface.Sources["src1"].LosslessFields) {
		t.Error("expected x-ob stripped from source")
	}
	if HasXOB(iface.Operations["hello"].LosslessFields) {
		t.Error("expected x-ob stripped from operation")
	}
	if HasXOB(iface.Bindings["hello.src1"].LosslessFields) {
		t.Error("expected x-ob stripped from binding")
	}
	// The in-schema floor-stamp must be stripped at every level (the doc
	// comment's "recursively" is load-bearing now).
	out := iface.Operations["hello"].Output
	if _, ok := out["x-ob"]; ok {
		t.Error("expected in-schema x-ob stripped from operation output")
	}
	if inner, _ := out["properties"].(map[string]any)["inner"].(map[string]any); inner["x-ob"] != nil {
		t.Error("expected in-schema x-ob stripped from a nested subschema")
	}
	if _, ok := iface.Schemas["Pet"]["x-ob"]; ok {
		t.Error("expected in-schema x-ob stripped from the shared schemas section")
	}
}

// The output-schema election writes into op.Output and stamps the marker;
// a pull re-applies it onto a floor derivation but a grown source schema
// displaces it; purify strips the marker while the elected value stays.
func TestOutputSchemaElection(t *testing.T) {
	elected := openbindings.JSONSchema{"type": "array", "items": map[string]any{"type": "object"}}

	var lf openbindings.LosslessFields
	if err := SetOutputSchemaElection(&lf, elected); err != nil {
		t.Fatal(err)
	}
	got, err := GetOutputSchemaElection(lf)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "array" {
		t.Errorf("round-trip = %#v", got)
	}

	// Pull re-application onto a floor derivation.
	existing := openbindings.Operation{LosslessFields: lf}
	fresh := openbindings.Operation{Output: openbindings.JSONSchema{"type": "string", "x-ob": map[string]any{"floor": "text"}}}
	var warns []string
	carryOutputSchemaElection(existing, &fresh, "op", &warns)
	if fresh.Output["type"] != "array" {
		t.Errorf("election should re-apply onto a floor derivation, got %#v", fresh.Output)
	}
	if len(warns) != 0 {
		t.Errorf("no warning expected re-applying onto a floor, got %v", warns)
	}

	// A grown, non-floor source schema wins and displaces the election.
	fresh2 := openbindings.Operation{Output: openbindings.JSONSchema{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer"}}}}
	var warns2 []string
	carryOutputSchemaElection(existing, &fresh2, "op", &warns2)
	if fresh2.Output["type"] != "object" {
		t.Errorf("a grown source schema must win, got %#v", fresh2.Output)
	}
	if len(warns2) == 0 {
		t.Error("expected a loud displacement warning when the source grows a real schema")
	}
}

func TestParseContentForEmbed(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		format     string
		wantString bool
	}{
		{
			name:       "JSON format returns object",
			data:       `{"key": "value"}`,
			format:     "openapi@3.1",
			wantString: false,
		},
		{
			name:       "KDL format returns string",
			data:       `bin "hello" { cmd "greet" }`,
			format:     "usage@2.0.0",
			wantString: true,
		},
		{
			name:       "Protobuf format returns string",
			data:       `syntax = "proto3";`,
			format:     "protobuf@3",
			wantString: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseContentForEmbed([]byte(tt.data), tt.format)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			_, isString := result.(string)
			if isString != tt.wantString {
				t.Errorf("isString: got %v, want %v (value: %v)", isString, tt.wantString, result)
			}
		})
	}
}

func TestReadSourceContent_File(t *testing.T) {
	dir := t.TempDir()
	content := "hello world"
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte(content), 0644)

	// Absolute path.
	data, err := ReadSourceContent(path, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != content {
		t.Errorf("got %q, want %q", string(data), content)
	}

	// Relative path with obiDir.
	data2, err := ReadSourceContent("test.txt", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data2) != content {
		t.Errorf("got %q, want %q", string(data2), content)
	}
}

func TestResolveSourceSpec_Location(t *testing.T) {
	dir := t.TempDir()
	src := openbindings.Source{Format: "usage@2.0.0"}
	meta := SourceMeta{Ref: "./usage.kdl", Resolve: ResolveModeLocation}
	data := []byte("irrelevant for location mode")

	err := ResolveSourceSpec(&src, meta, data, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Location == "" {
		t.Error("expected location to be set")
	}
	if src.Content != nil {
		t.Error("expected content to be nil in location mode")
	}
}

func TestResolveSourceSpec_LocationWithURI(t *testing.T) {
	src := openbindings.Source{Format: "usage@2.0.0"}
	meta := SourceMeta{
		Ref:     "./usage.kdl",
		Resolve: ResolveModeLocation,
		URI:     "https://cdn.example.com/usage.kdl",
	}
	data := []byte("irrelevant")

	err := ResolveSourceSpec(&src, meta, data, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Location != "https://cdn.example.com/usage.kdl" {
		t.Errorf("expected URI override, got %q", src.Location)
	}
}

func TestResolveSourceSpec_Content(t *testing.T) {
	src := openbindings.Source{Format: "usage@2.0.0"}
	meta := SourceMeta{Ref: "./usage.kdl", Resolve: ResolveModeContent}
	data := []byte(`bin "hello" { }`)

	err := ResolveSourceSpec(&src, meta, data, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Content == nil {
		t.Fatal("expected content to be set")
	}
	str, ok := src.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", src.Content)
	}
	if str != `bin "hello" { }` {
		t.Errorf("unexpected content: %q", str)
	}
	if src.Location != "" {
		t.Error("expected location to be empty in content mode")
	}
}
