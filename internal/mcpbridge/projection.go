package mcpbridge

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
)

// genericToolProjection is the protocol-neutral mapping from one OBI
// operation boundary to MCP's object-only tool boundary.
type genericToolProjection struct {
	InputSchema  any
	OutputSchema any
	decodeInput  func(json.RawMessage) (operationInput, error)
}

type operationInput struct {
	Value   any
	Present bool
}

func projectGenericTool(op openbindings.Operation, schemas map[string]openbindings.JSONSchema) genericToolProjection {
	inputSchema := bundleValueSchema(op.Input, schemas)
	directObject := false
	if root, ok := inputSchema.(map[string]any); ok {
		directObject = root["type"] == "object"
	}

	decodeDirect := func(raw json.RawMessage) (operationInput, error) {
		if len(raw) == 0 {
			return operationInput{}, nil
		}
		var input map[string]any
		if err := json.Unmarshal(raw, &input); err != nil {
			return operationInput{}, fmt.Errorf("invalid arguments: %w", err)
		}
		if input == nil {
			return operationInput{}, fmt.Errorf("invalid arguments: MCP tool arguments must be an object")
		}
		return operationInput{Value: input, Present: true}, nil
	}

	outputValueSchema, outputDefs := nestedBundledValue(bundleValueSchema(op.Output, schemas))
	outputEnvelope := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"outputs": map[string]any{
				"type":  "array",
				"items": outputValueSchema,
			},
		},
		"required":             []any{"outputs"},
		"additionalProperties": false,
	}
	if outputDefs != nil {
		outputEnvelope["$defs"] = outputDefs
	}
	projection := genericToolProjection{
		OutputSchema: outputEnvelope,
	}

	// MCP tool arguments are always an object. Preserve an object-valued OBI
	// input directly for the common API case. Every other JSON value uses one
	// explicit adapter member; this is reversible and avoids lying about a
	// scalar/array schema by changing its root type to object.
	if directObject {
		projection.InputSchema = inputSchema
		projection.decodeInput = decodeDirect
		return projection
	}

	nestedInputSchema, inputDefs := nestedBundledValue(inputSchema)
	inputEnvelope := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": nestedInputSchema,
		},
		"additionalProperties": false,
	}
	if inputDefs != nil {
		inputEnvelope["$defs"] = inputDefs
	}
	projection.InputSchema = inputEnvelope
	projection.decodeInput = func(raw json.RawMessage) (operationInput, error) {
		if len(raw) == 0 {
			return operationInput{}, nil
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return operationInput{}, fmt.Errorf("invalid arguments: %w", err)
		}
		if envelope == nil {
			return operationInput{}, fmt.Errorf("invalid arguments: MCP tool arguments must be an object")
		}
		value, ok := envelope["input"]
		if !ok {
			return operationInput{}, nil
		}
		var input any
		if err := json.Unmarshal(value, &input); err != nil {
			return operationInput{}, fmt.Errorf(`invalid arguments member "input": %w`, err)
		}
		return operationInput{Value: input, Present: true}, nil
	}
	return projection
}

// nestedBundledValue moves a bundled schema's definitions to the enclosing
// MCP schema root. Rewritten references are document-root fragments
// (#/$defs/...), so leaving $defs beside a nested property/items schema would
// make every such reference point at the wrong location.
func nestedBundledValue(schema any) (any, map[string]any) {
	root, ok := schema.(map[string]any)
	if !ok {
		return schema, nil
	}
	defs, ok := root["$defs"].(map[string]any)
	if !ok {
		return schema, nil
	}
	nested := make(map[string]any, len(root)-1)
	for key, value := range root {
		if key != "$defs" {
			nested[key] = value
		}
	}
	return nested, defs
}

// genericToolResult preserves the complete OpenBindings output sequence.
// Cardinality belongs to the selected binding and is intentionally absent
// from the OBI operation, so one stable sequence envelope is the only
// protocol-neutral schema that is true before invocation.
func genericToolResult(result drainedOperation) *mcp.CallToolResult {
	outputs := result.Outputs
	if outputs == nil {
		outputs = []any{}
	}
	envelope := map[string]any{
		"outputs": outputs,
	}
	if result.Error != nil {
		failure := map[string]any{"code": result.Error.Code}
		if result.Error.HasData() {
			failure["data"] = result.Error.Data
		}
		envelope["error"] = failure
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to marshal operation result: %v", err)}},
		}
	}
	return &mcp.CallToolResult{
		IsError:           result.Error != nil,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(data)}},
		StructuredContent: envelope,
	}
}
