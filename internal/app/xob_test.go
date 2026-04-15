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
				LosslessFields: openbindings.LosslessFields{Extensions: map[string]json.RawMessage{xobKey: json.RawMessage(`{}`)}},
			},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"hello.src1": {
				LosslessFields: openbindings.LosslessFields{Extensions: map[string]json.RawMessage{xobKey: json.RawMessage(`{}`)}},
			},
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
