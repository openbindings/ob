package app

import (
	"strings"
	"testing"
)

func TestFormatOutput_YAML_SerializesXOBAsObject(t *testing.T) {
	// Structure that mimics how openbindings-go stores x-ob in LosslessFields:
	// when marshaled directly to YAML, json.RawMessage ([]byte) becomes [123, 125].
	// After our JSON round-trip, it should appear as x-ob: {} in YAML.
	v := map[string]any{
		"openbindings": "0.1.0",
		"name":         "test",
		"operations": map[string]any{
			"hello": map[string]any{
				"x-ob": map[string]any{},
			},
		},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "location",
				},
			},
		},
	}

	b, err := FormatOutput(v, OutputFormatYAML)
	if err != nil {
		t.Fatalf("FormatOutput: %v", err)
	}

	yamlStr := string(b)
	// x-ob should appear as a key with an object/map value, not as a sequence of integers.
	if strings.Contains(yamlStr, "x-ob:\n      - 123") || strings.Contains(yamlStr, "x-ob:\n    - 123") {
		t.Error("YAML must not serialize x-ob as integer sequence [123, 125,...]")
	}
	if !strings.Contains(yamlStr, "x-ob:") {
		t.Error("YAML should contain x-ob key")
	}
	if !strings.Contains(yamlStr, "ref:") || !strings.Contains(yamlStr, "resolve:") {
		t.Error("YAML should contain x-ob contents (ref, resolve) as proper keys")
	}
}
