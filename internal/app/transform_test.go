package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestApplyTransform_Nil(t *testing.T) {
	input := map[string]any{"foo": "bar"}
	result, err := ApplyTransform(nil, nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(map[string]any)["foo"] != "bar" {
		t.Errorf("expected unchanged input, got %v", result)
	}
}

func TestApplyTransform_SimpleRename(t *testing.T) {
	transform := &openbindings.Transform{
		Type:       "jsonata",
		Expression: `{ "to": openbindingsVersion }`,
	}
	tor := &openbindings.TransformOrRef{Transform: transform}

	input := map[string]any{"openbindingsVersion": "0.1.0"}
	result, err := ApplyTransform(nil, tor, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if resultMap["to"] != "0.1.0" {
		t.Errorf("expected to=0.1.0, got %v", resultMap["to"])
	}
}

func TestApplyTransform_FullCreateInterfaceInput(t *testing.T) {
	// This is the actual transform expression used in ob.obi.json
	transform := &openbindings.Transform{
		Type:       "jsonata",
		Expression: `{ "flags": { "to": openbindingsVersion, "id": id, "name": name, "version": version, "description": description }, "args": sources.(format & ":" & location & (embed ? "?embed" : "")) }`,
	}
	tor := &openbindings.TransformOrRef{Transform: transform}

	input := map[string]any{
		"openbindingsVersion": "0.1.0",
		"id":                  "my.interface",
		"name":                "My Interface",
		"sources": []any{
			map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
			},
			map[string]any{
				"format":   "openapi@3.1",
				"location": "./api.yaml",
				"embed":    true,
			},
		},
	}

	result, err := ApplyTransform(nil, tor, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}

	flags, ok := resultMap["flags"].(map[string]any)
	if !ok {
		t.Fatalf("expected flags map, got %T", resultMap["flags"])
	}

	if flags["to"] != "0.1.0" {
		t.Errorf("expected to=0.1.0, got %v", flags["to"])
	}
	if flags["id"] != "my.interface" {
		t.Errorf("expected id=my.interface, got %v", flags["id"])
	}
	if flags["name"] != "My Interface" {
		t.Errorf("expected name='My Interface', got %v", flags["name"])
	}

	args, ok := resultMap["args"].([]any)
	if !ok {
		t.Fatalf("expected args array, got %T", resultMap["args"])
	}

	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if args[0] != "usage@2.0.0:./cli.kdl" {
		t.Errorf("expected usage@2.0.0:./cli.kdl, got %v", args[0])
	}
	if args[1] != "openapi@3.1:./api.yaml?embed" {
		t.Errorf("expected openapi@3.1:./api.yaml?embed, got %v", args[1])
	}
}

func TestApplyTransform_ResolveRef(t *testing.T) {
	transforms := map[string]openbindings.Transform{
		"myTransform": {
			Type:       "jsonata",
			Expression: `{ "renamed": original }`,
		},
	}
	tor := &openbindings.TransformOrRef{Ref: "#/transforms/myTransform"}

	input := map[string]any{"original": "value"}
	result, err := ApplyTransform(transforms, tor, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if resultMap["renamed"] != "value" {
		t.Errorf("expected renamed=value, got %v", resultMap["renamed"])
	}
}

func TestApplyTransform_RefNotFound(t *testing.T) {
	tor := &openbindings.TransformOrRef{Ref: "#/transforms/nonexistent"}

	_, err := ApplyTransform(nil, tor, map[string]any{})
	if err == nil {
		t.Error("expected error for missing ref, got nil")
	}
}

func TestApplyTransform_UnsupportedType(t *testing.T) {
	transform := &openbindings.Transform{
		Type:       "xslt",
		Expression: `<xsl:template/>`,
	}
	tor := &openbindings.TransformOrRef{Transform: transform}

	_, err := ApplyTransform(nil, tor, map[string]any{})
	if err == nil {
		t.Error("expected error for unsupported type, got nil")
	}
}

func TestApplyTransform_EmptyExpression(t *testing.T) {
	transform := &openbindings.Transform{
		Type:       "jsonata",
		Expression: "",
	}
	tor := &openbindings.TransformOrRef{Transform: transform}

	_, err := ApplyTransform(nil, tor, map[string]any{})
	if err == nil {
		t.Error("expected error for empty expression, got nil")
	}
}

func TestApplyTransform_InvalidExpression(t *testing.T) {
	transform := &openbindings.Transform{
		Type:       "jsonata",
		Expression: `{ invalid syntax !!!`,
	}
	tor := &openbindings.TransformOrRef{Transform: transform}

	_, err := ApplyTransform(nil, tor, map[string]any{})
	if err == nil {
		t.Error("expected error for invalid expression, got nil")
	}
}

func TestApplyTransform_NilInput(t *testing.T) {
	transform := &openbindings.Transform{
		Type:       "jsonata",
		Expression: `{ "value": $ }`,
	}
	tor := &openbindings.TransformOrRef{Transform: transform}

	result, err := ApplyTransform(nil, tor, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// JSONata with nil input should still work
	_ = result
}
