package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSource_Basic(t *testing.T) {
	src, err := ParseSource("openbindings.usage@1:./cli.kdl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.BindingSpec != "openbindings.usage@1" {
		t.Errorf("format = %q, want %q", src.BindingSpec, "openbindings.usage@1")
	}
	if src.Location != "./cli.kdl" {
		t.Errorf("location = %q, want %q", src.Location, "./cli.kdl")
	}
}

func TestParseSource_WithOptions(t *testing.T) {
	src, err := ParseSource("openbindings.usage@1:./cli.kdl?name=cli&embed&description=CLI spec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.BindingSpec != "openbindings.usage@1" {
		t.Errorf("format = %q", src.BindingSpec)
	}
	if src.Name != "cli" {
		t.Errorf("name = %q, want %q", src.Name, "cli")
	}
	if !src.Embed {
		t.Error("embed should be true")
	}
	if src.Description != "CLI spec" {
		t.Errorf("description = %q", src.Description)
	}
}

func TestParseSource_OutputLocation(t *testing.T) {
	src, err := ParseSource("openbindings.openapi@1:/tmp/spec.json?outputLocation=./spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.OutputLocation != "./spec.json" {
		t.Errorf("outputLocation = %q, want %q", src.OutputLocation, "./spec.json")
	}
}

func TestParseSource_BarePath(t *testing.T) {
	src, err := ParseSource("openapi.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.BindingSpec != "" {
		t.Errorf("format = %q, want empty (auto-detect)", src.BindingSpec)
	}
	if src.Location != "openapi.json" {
		t.Errorf("location = %q, want %q", src.Location, "openapi.json")
	}
}

func TestParseSource_URLSchemeNotFormat(t *testing.T) {
	// A scheme://… URL resolves to the whole Location, never split on its
	// scheme as if the scheme were a format token (regression: the parser
	// used to yield BindingSpec="http", Location="//host").
	for _, u := range []string{
		"http://api.example.com/openapi.json",
		"https://api.example.com/openapi.yaml",
		"ws://host:8080/stream",
		"wss://host/stream",
	} {
		src, err := ParseSource(u)
		if err != nil {
			t.Fatalf("ParseSource(%q): unexpected error: %v", u, err)
		}
		if src.BindingSpec != "" {
			t.Errorf("ParseSource(%q): BindingSpec = %q, want empty", u, src.BindingSpec)
		}
		if src.Location != u {
			t.Errorf("ParseSource(%q): Location = %q, want %q", u, src.Location, u)
		}
	}
}

func TestParseSource_BarePathRelative(t *testing.T) {
	src, err := ParseSource("./api.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.BindingSpec != "" {
		t.Errorf("format = %q, want empty", src.BindingSpec)
	}
	if src.Location != "./api.yaml" {
		t.Errorf("location = %q, want %q", src.Location, "./api.yaml")
	}
}

func TestParseSource_BarePathWithOptions(t *testing.T) {
	src, err := ParseSource("openapi.json?name=restApi&embed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.BindingSpec != "" {
		t.Errorf("format = %q, want empty", src.BindingSpec)
	}
	if src.Location != "openapi.json" {
		t.Errorf("location = %q, want %q", src.Location, "openapi.json")
	}
	if src.Name != "restApi" {
		t.Errorf("name = %q, want %q", src.Name, "restApi")
	}
	if !src.Embed {
		t.Error("embed should be true")
	}
}

func TestParseSource_ColonInPath(t *testing.T) {
	// :path with empty prefix — treated as bare path
	src, err := ParseSource(":path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Location != ":path" {
		t.Errorf("location = %q, want %q", src.Location, ":path")
	}
}

func TestParseSource_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"empty path explicit", "usage:"},
		{"unknown option", "usage:path?bogus=val"},
		{"bad option", "usage:path?bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource(tt.input)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestDeriveSourceKey_ExplicitName(t *testing.T) {
	src := SynthesizeInterfaceSource{Name: "myKey", BindingSpec: "openbindings.usage@1", Location: "/foo/bar.kdl"}
	key := DeriveSourceKey(src, 0)
	if key != "myKey" {
		t.Errorf("key = %q, want %q", key, "myKey")
	}
}

func TestDeriveSourceKey_FromFileName(t *testing.T) {
	src := SynthesizeInterfaceSource{BindingSpec: "openbindings.usage@1", Location: "/project/cli.usage.kdl"}
	key := DeriveSourceKey(src, 0)
	if key != "cliUsage" {
		t.Errorf("key = %q, want %q", key, "cliUsage")
	}
}

func TestDeriveSourceKey_NoStutter(t *testing.T) {
	src := SynthesizeInterfaceSource{BindingSpec: "openbindings.asyncapi@1", Location: "/project/asyncapi.json"}
	key := DeriveSourceKey(src, 0)
	if key != "asyncapi" {
		t.Errorf("key = %q, want %q", key, "asyncapi")
	}
}

func TestDeriveSourceKey_FallbackIndex(t *testing.T) {
	src := SynthesizeInterfaceSource{BindingSpec: "openbindings.openapi@1", Location: "/project/this-is-a-very-long-filename-that-exceeds-twenty-chars.json"}
	key := DeriveSourceKey(src, 2)
	if key != "openapi2" {
		t.Errorf("key = %q, want %q", key, "openapi2")
	}
}

// The embed lane reads through ReadSourceContent and parses by FORMAT via
// ParseContentForEmbed — one canonical embed parse across synthesize,
// source add --resolve content, and pull refreshes.
func TestEmbedLane_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	data := map[string]any{"hello": "world"}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}

	raw, err := ReadSourceContent(path, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := ParseContentForEmbed(raw, "openbindings.openapi@1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(result, &obj); err != nil || obj == nil {
		t.Fatalf("expected an embedded JSON object, got %s", result)
	}
	if obj["hello"] != "world" {
		t.Errorf("result = %v", obj)
	}
}

func TestEmbedLane_YAMLFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte("greeting: hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	raw, err := ReadSourceContent(path, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := ParseContentForEmbed(raw, "openbindings.asyncapi@1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(result, &obj); err != nil || obj == nil {
		t.Fatalf("expected an embedded JSON object, got %s", result)
	}
	if obj["greeting"] != "hello" {
		t.Errorf("result = %v", obj)
	}
}

func TestEmbedLane_TextFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.kdl")
	if err := os.WriteFile(path, []byte("node \"value\""), 0644); err != nil {
		t.Fatal(err)
	}

	raw, err := ReadSourceContent(path, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := ParseContentForEmbed(raw, "openbindings.usage@1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var str string
	if err := json.Unmarshal(result, &str); err != nil {
		t.Fatalf("expected a JSON string for a text format, got %s", result)
	}
	if str != "node \"value\"" {
		t.Errorf("result = %q", str)
	}
}

func TestEmbedLane_FileNotFound(t *testing.T) {
	_, err := ReadSourceContent("/nonexistent/test.json", "")
	if err == nil {
		t.Error("expected error")
	}
}

func TestSynthesizeInterface_InvalidVersion(t *testing.T) {
	_, err := SynthesizeInterface(SynthesizeInterfaceInput{
		OpenBindingsVersion: "99.99.99",
	})
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestSynthesizeInterface_NoSources(t *testing.T) {
	iface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Name: "TestInterface",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface == nil {
		t.Fatal("expected interface")
	}
	if iface.Name != "TestInterface" {
		t.Errorf("name = %q", iface.Name)
	}
	if len(iface.Operations) != 0 {
		t.Errorf("expected 0 operations, got %d", len(iface.Operations))
	}
}

func TestSynthesizeInterface_Overrides(t *testing.T) {
	iface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Name:        "Overridden",
		Version:     "1.0.0",
		Description: "A test interface",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface.Name != "Overridden" {
		t.Errorf("name = %q", iface.Name)
	}
	if iface.Version != "1.0.0" {
		t.Errorf("version = %q", iface.Version)
	}
	if iface.Description != "A test interface" {
		t.Errorf("description = %q", iface.Description)
	}
}

// tinyOpenAPIApp is a minimal OpenAPI document for content-lane synthesis
// tests (the app-level twin of the cmd package's tinyOpenAPI fixture).
const tinyOpenAPIApp = `{"openapi":"3.1.0","info":{"title":"Tiny","version":"1.0.0"},"paths":{"/things":{"get":{"operationId":"listThings","responses":{"200":{"description":"ok"}}}}}}`

// TestSynthesizeInterface_ContentSourceOutputLocation: `?outputLocation=`
// means spec-level `location` in EVERY synthesis lane. A wire-supplied
// content source (the shape `--input` decodes to, and what the stdin `-`
// lane produces) given an outputLocation writes it to the source entry's
// spec-level location field, pairing the published pointer with the inline
// artifact exactly like the file lane's `?embed&outputLocation=`, and
// mirrors it in x-ob.uri like every other lane. Previously the value rode
// x-ob.uri only on content sources (an accidental split).
func TestSynthesizeInterface_ContentSourceOutputLocation(t *testing.T) {
	published := "https://example.com/openapi.json"
	content, err := ParseContentForEmbed([]byte(tinyOpenAPIApp), "openbindings.openapi@1")
	if err != nil {
		t.Fatalf("parse content: %v", err)
	}

	iface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Sources: []SynthesizeInterfaceSource{
			{
				BindingSpec:    "openbindings.openapi@1",
				Name:           "api",
				Content:        content,
				OutputLocation: published,
			},
		},
		Name: "Tiny",
	})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}

	src, ok := iface.Sources["api"]
	if !ok {
		t.Fatalf("expected source key %q, got %v", "api", iface.Sources)
	}
	if src.Location != published {
		t.Errorf("spec-level location = %q, want %q", src.Location, published)
	}
	if src.Content == nil {
		t.Error("expected the wire-supplied content to remain embedded")
	}
	meta, err := GetSourceMeta(src)
	if err != nil || meta == nil {
		t.Fatalf("GetSourceMeta: %v", err)
	}
	if meta.URI != published {
		t.Errorf("x-ob.uri = %q, want %q (mirrors location, as in the file lane)", meta.URI, published)
	}
	if meta.Ref != "" {
		t.Errorf("x-ob.ref = %q, want empty (content has no pull path)", meta.Ref)
	}
}

// TestSynthesizeInterface_ContentSourceOutputLocationRelative: the
// relative-value posture is the file lane's, unchanged and shared —
// synthesis records the value VERBATIM (no refusal, no normalization);
// OBI-D-05 absolute-only enforcement lives at the invoke-time gate
// (resolveSourceLocation, see invoke_test.go), which covers content-lane
// entries through the same spec-level location field.
func TestSynthesizeInterface_ContentSourceOutputLocationRelative(t *testing.T) {
	content, err := ParseContentForEmbed([]byte(tinyOpenAPIApp), "openbindings.openapi@1")
	if err != nil {
		t.Fatalf("parse content: %v", err)
	}

	iface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Sources: []SynthesizeInterfaceSource{
			{
				BindingSpec:    "openbindings.openapi@1",
				Name:           "api",
				Content:        content,
				OutputLocation: "./openapi.json",
			},
		},
	})
	if err != nil {
		t.Fatalf("synthesize: %v (the file lane records a relative outputLocation verbatim; content lanes must not diverge)", err)
	}
	if got := iface.Sources["api"].Location; got != "./openapi.json" {
		t.Errorf("spec-level location = %q, want %q (verbatim, matching the file lane)", got, "./openapi.json")
	}
}

func TestSynthesizeInterface_BadSource(t *testing.T) {
	_, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Sources: []SynthesizeInterfaceSource{
			{BindingSpec: "openbindings.usage@1", Location: "/nonexistent/spec.kdl"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing source file")
	}
}
