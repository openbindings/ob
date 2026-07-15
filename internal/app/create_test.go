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
	obj, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
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
	obj, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
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
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string for a text format, got %T", result)
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
