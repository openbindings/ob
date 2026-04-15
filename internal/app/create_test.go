package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSource_Basic(t *testing.T) {
	src, err := ParseSource("usage@2.13.1:./cli.kdl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Format != "usage@2.13.1" {
		t.Errorf("format = %q, want %q", src.Format, "usage@2.13.1")
	}
	if src.Location != "./cli.kdl" {
		t.Errorf("location = %q, want %q", src.Location, "./cli.kdl")
	}
}

func TestParseSource_WithOptions(t *testing.T) {
	src, err := ParseSource("usage@2.13.1:./cli.kdl?name=cli&embed&description=CLI spec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Format != "usage@2.13.1" {
		t.Errorf("format = %q", src.Format)
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
	src, err := ParseSource("openapi@3.1:/tmp/spec.json?outputLocation=./spec.json")
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
	if src.Format != "" {
		t.Errorf("format = %q, want empty (auto-detect)", src.Format)
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
	if src.Format != "" {
		t.Errorf("format = %q, want empty", src.Format)
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
	if src.Format != "" {
		t.Errorf("format = %q, want empty", src.Format)
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
	src := CreateInterfaceSource{Name: "myKey", Format: "usage@2.0.0", Location: "/foo/bar.kdl"}
	key := DeriveSourceKey(src, 0)
	if key != "myKey" {
		t.Errorf("key = %q, want %q", key, "myKey")
	}
}

func TestDeriveSourceKey_FromFileName(t *testing.T) {
	src := CreateInterfaceSource{Format: "usage@2.0.0", Location: "/project/cli.usage.kdl"}
	key := DeriveSourceKey(src, 0)
	if key != "cliUsage" {
		t.Errorf("key = %q, want %q", key, "cliUsage")
	}
}

func TestDeriveSourceKey_NoStutter(t *testing.T) {
	src := CreateInterfaceSource{Format: "asyncapi@3.0", Location: "/project/asyncapi.json"}
	key := DeriveSourceKey(src, 0)
	if key != "asyncapi" {
		t.Errorf("key = %q, want %q", key, "asyncapi")
	}
}

func TestDeriveSourceKey_FallbackIndex(t *testing.T) {
	src := CreateInterfaceSource{Format: "openapi@3.1", Location: "/project/this-is-a-very-long-filename-that-exceeds-twenty-chars.json"}
	key := DeriveSourceKey(src, 2)
	if key != "openapi2" {
		t.Errorf("key = %q, want %q", key, "openapi2")
	}
}

func TestRenderInterface(t *testing.T) {
	t.Run("nil interface", func(t *testing.T) {
		r := RenderInterface(nil)
		if r == "" {
			t.Error("expected non-empty render")
		}
	})
}

func TestReadEmbedContent_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	data := map[string]any{"hello": "world"}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := readEmbedContent(path)
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

func TestReadEmbedContent_YAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte("greeting: hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := readEmbedContent(path)
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

func TestReadEmbedContent_KDL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.kdl")
	if err := os.WriteFile(path, []byte("node \"value\""), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := readEmbedContent(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string for KDL, got %T", result)
	}
	if str != "node \"value\"" {
		t.Errorf("result = %q", str)
	}
}

func TestReadEmbedContent_FileNotFound(t *testing.T) {
	_, err := readEmbedContent("/nonexistent/test.json")
	if err == nil {
		t.Error("expected error")
	}
}

func TestCreateInterface_InvalidVersion(t *testing.T) {
	_, err := CreateInterface(CreateInterfaceInput{
		OpenBindingsVersion: "99.99.99",
	})
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestCreateInterface_NoSources(t *testing.T) {
	iface, err := CreateInterface(CreateInterfaceInput{
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

func TestCreateInterface_Overrides(t *testing.T) {
	iface, err := CreateInterface(CreateInterfaceInput{
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

func TestCreateInterface_BadSource(t *testing.T) {
	_, err := CreateInterface(CreateInterfaceInput{
		Sources: []CreateInterfaceSource{
			{Format: "usage@2.0.0", Location: "/nonexistent/spec.kdl"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing source file")
	}
}
