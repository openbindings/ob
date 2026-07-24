package app

import (
	"testing"

	"github.com/openbindings/ob/internal/servecontract"
	"gopkg.in/yaml.v3"
)

func TestGenerateServeOpenAPI_PublishesAuthoritativeErrorVocabulary(t *testing.T) {
	data, err := GenerateServeOpenAPI("../../ob.obi.json", "http://localhost:20290")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	schema := doc.Components.Schemas["ErrorResponse"]
	required, _ := schema["required"].([]any)
	if !containsYAMLString(required, "error") || !containsYAMLString(required, "code") {
		t.Fatalf("ErrorResponse required = %#v, want error and code", required)
	}
	properties, _ := schema["properties"].(map[string]any)
	code, _ := properties["code"].(map[string]any)
	enum, _ := code["enum"].([]any)
	got := make([]string, len(enum))
	for i := range enum {
		got[i], _ = enum[i].(string)
	}
	want := servecontract.ErrorCodes()
	if len(got) != len(want) {
		t.Fatalf("published %d error codes, catalog has %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("published error code %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func containsYAMLString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
