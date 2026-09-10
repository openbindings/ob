package app

import (
	"context"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestApplyTransform_Nil(t *testing.T) {
	input := map[string]any{"foo": "bar"}
	result, err := ApplyTransform(context.Background(), nil, nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(map[string]any)["foo"] != "bar" {
		t.Errorf("expected unchanged input, got %v", result)
	}
}

func TestApplyTransform_SimpleRename(t *testing.T) {
	tor := &openbindings.TransformOrRef{Inline: `{ "to": openbindingsVersion }`}

	input := map[string]any{"openbindingsVersion": "0.1.0"}
	result, err := ApplyTransform(context.Background(), nil, tor, input)
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

func TestApplyTransform_FullSynthesizeInterfaceInput(t *testing.T) {
	// This is the actual transform expression used in ob.obi.json
	tor := &openbindings.TransformOrRef{Inline: `{ "flags": { "to": openbindingsVersion, "id": id, "name": name, "version": version, "description": description }, "args": sources.(bindingSpec & ":" & location & (embed ? "?embed" : "")) }`}

	input := map[string]any{
		"openbindingsVersion": "0.1.0",
		"id":                  "my.interface",
		"name":                "My Interface",
		"sources": []any{
			map[string]any{
				"bindingSpec": "openbindings.usage@1",
				"location":    "./cli.kdl",
			},
			map[string]any{
				"bindingSpec": "openbindings.openapi-3.1@1",
				"location":    "./api.yaml",
				"embed":       true,
			},
		},
	}

	result, err := ApplyTransform(context.Background(), nil, tor, input)
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
	if args[0] != "openbindings.usage@1:./cli.kdl" {
		t.Errorf("expected openbindings.usage@1:./cli.kdl, got %v", args[0])
	}
	if args[1] != "openbindings.openapi-3.1@1:./api.yaml?embed" {
		t.Errorf("expected openbindings.openapi-3.1@1:./api.yaml?embed, got %v", args[1])
	}
}

func TestApplyTransform_ResolveRef(t *testing.T) {
	transforms := map[string]openbindings.Transform{
		"myTransform": `{ "renamed": original }`,
	}
	tor := &openbindings.TransformOrRef{Ref: "#/transforms/myTransform"}

	input := map[string]any{"original": "value"}
	result, err := ApplyTransform(context.Background(), transforms, tor, input)
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

	_, err := ApplyTransform(context.Background(), nil, tor, map[string]any{})
	if err == nil {
		t.Error("expected error for missing ref, got nil")
	}
}

func TestApplyTransform_EmptyExpression(t *testing.T) {
	tor := &openbindings.TransformOrRef{Inline: ""}

	_, err := ApplyTransform(context.Background(), nil, tor, map[string]any{})
	if err == nil {
		t.Error("expected error for empty expression, got nil")
	}
}

func TestApplyTransform_InvalidExpression(t *testing.T) {
	tor := &openbindings.TransformOrRef{Inline: `{ invalid syntax !!!`}

	_, err := ApplyTransform(context.Background(), nil, tor, map[string]any{})
	if err == nil {
		t.Error("expected error for invalid expression, got nil")
	}
}

func TestApplyTransform_NilInput(t *testing.T) {
	tor := &openbindings.TransformOrRef{Inline: `{ "value": $ }`}

	result, err := ApplyTransform(context.Background(), nil, tor, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// JSONata with nil input should still work
	_ = result
}
