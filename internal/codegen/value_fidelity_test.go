package codegen

import (
	"encoding/json"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestCodegenReferenceValueFidelity(t *testing.T) {
	iface := &openbindings.Interface{OpenBindings: "0.2.0", Operations: map[string]openbindings.Operation{"value": {Input: map[string]any{"$ref": "#/schemas/Value"}}}, Schemas: map[string]openbindings.JSONSchema{"Value": map[string]any{"type": "number", "enum": []any{json.Number("9007199254740993"), json.Number("1e-400")}}}}
	result, err := Generate(iface)
	if err != nil {
		t.Fatal(err)
	}
	code := EmitTypeScript(result)
	if !containsExactCodegenTokens(code) {
		t.Fatalf("reference values lost: %s", code)
	}
}

func containsExactCodegenTokens(code string) bool {
	return strings.Contains(code, "9007199254740993") && strings.Contains(code, "1e-400")
}

func TestCodegenEnumIntersectionValueFidelity(t *testing.T) {
	if got, err := intersectEnums([]any{json.Number("0.10")}, []any{json.Number("1e-1")}); err != nil || len(got) != 1 {
		t.Fatalf("equal values lost: %v", got)
	}
	// JSON enum members may be compound values; the comparison must not panic.
	if got, err := intersectEnums([]any{map[string]any{"id": json.Number("9007199254740993")}}, []any{map[string]any{"id": json.Number("9007199254740993")}}); err != nil || len(got) != 1 {
		t.Fatalf("compound enum lost: %v", got)
	}
}

func TestCodegenComparisonCapabilityIsExplicit(t *testing.T) {
	iface := &openbindings.Interface{OpenBindings: "0.2.0", Operations: map[string]openbindings.Operation{"value": {Input: map[string]any{"allOf": []any{map[string]any{"type": "number", "enum": []any{json.Number("1e10001")}}, map[string]any{"type": "number", "enum": []any{json.Number("2e10001")}}}}}}}
	if result, err := Generate(iface); err == nil || result != nil {
		t.Fatalf("unavailable enum comparison returned code: %+v, %v", result, err)
	}
}
