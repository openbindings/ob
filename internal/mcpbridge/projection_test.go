package mcpbridge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
)

func TestProjectGenericTool_ObjectInputStaysDirect(t *testing.T) {
	op := openbindings.Operation{
		Input: map[string]any{
			"type":       "object",
			"properties": map[string]any{"message": map[string]any{"type": "string"}},
		},
		Output: map[string]any{"type": "string"},
	}
	projection := projectGenericTool(op, nil)
	inputSchema := projection.InputSchema.(map[string]any)
	if inputSchema["type"] != "object" || inputSchema["properties"] == nil {
		t.Fatalf("object input was not preserved: %#v", inputSchema)
	}
	input, err := projection.decodeInput(json.RawMessage(`{"message":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !input.Present || input.Value.(map[string]any)["message"] != "hello" {
		t.Fatalf("wrong decoded input: %#v", input)
	}
	if _, err := projection.decodeInput(json.RawMessage(`null`)); err == nil {
		t.Fatal("null MCP arguments must not pass an object input schema")
	}
	assertOutputSequenceSchema(t, projection.OutputSchema, "string")
}

func TestProjectGenericTool_NonObjectInputUsesReversibleEnvelope(t *testing.T) {
	op := openbindings.Operation{
		Input:  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		Output: map[string]any{"type": "boolean"},
	}
	projection := projectGenericTool(op, nil)
	root := projection.InputSchema.(map[string]any)
	properties := root["properties"].(map[string]any)
	wrapped := properties["input"].(map[string]any)
	if wrapped["type"] != "array" {
		t.Fatalf("array input schema was not preserved under input: %#v", root)
	}
	input, err := projection.decodeInput(json.RawMessage(`{"input":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	if values, ok := input.Value.([]any); !input.Present || !ok || len(values) != 2 {
		t.Fatalf("wrong decoded input: %#v", input)
	}
	input, err = projection.decodeInput(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if input.Present {
		t.Fatalf("empty envelope must mean no input value: %#v", input)
	}

	input, err = projection.decodeInput(json.RawMessage(`{"input":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if !input.Present || input.Value != nil {
		t.Fatalf("explicit null was confused with absent input: %#v", input)
	}
	if _, err := projection.decodeInput(json.RawMessage(`null`)); err == nil {
		t.Fatal("null MCP arguments must not be confused with an absent input envelope")
	}
}

func TestProjectGenericTool_UnspecifiedInputSupportsAbsentOrAnyValue(t *testing.T) {
	projection := projectGenericTool(openbindings.Operation{}, nil)
	root := projection.InputSchema.(map[string]any)
	properties := root["properties"].(map[string]any)
	if _, ok := properties["input"]; !ok {
		t.Fatalf("unspecified input lost the generic value lane: %#v", root)
	}
	if _, required := root["required"]; required {
		t.Fatalf("unspecified per-value contract must not assert input cardinality: %#v", root)
	}

	absent, err := projection.decodeInput(json.RawMessage(`{}`))
	if err != nil || absent.Present {
		t.Fatalf("absent input changed: input=%#v err=%v", absent, err)
	}
	scalar, err := projection.decodeInput(json.RawMessage(`{"input":"value"}`))
	if err != nil || !scalar.Present || scalar.Value != "value" {
		t.Fatalf("unspecified scalar input changed: input=%#v err=%v", scalar, err)
	}
}

func TestProjectGenericTool_FalseInputPermitsOnlyAbsentValue(t *testing.T) {
	projection := projectGenericTool(openbindings.Operation{Input: false}, nil)
	root := projection.InputSchema.(map[string]any)
	inputSchema := root["properties"].(map[string]any)["input"]
	if inputSchema != false {
		t.Fatalf("false per-value contract changed: %#v", root)
	}
	if _, required := root["required"]; required {
		t.Fatalf("false per-value contract was mistaken for required cardinality: %#v", root)
	}
}

func TestProjectGenericTool_LiftsBundledDefinitionsToEnvelopeRoot(t *testing.T) {
	schemas := map[string]openbindings.JSONSchema{
		"Message": map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
		},
	}
	projection := projectGenericTool(openbindings.Operation{
		Input: map[string]any{
			"type":  "array",
			"items": map[string]any{"$ref": "#/schemas/Message"},
		},
		Output: map[string]any{
			"type":  "array",
			"items": map[string]any{"$ref": "#/schemas/Message"},
		},
	}, schemas)

	inputRoot := projection.InputSchema.(map[string]any)
	if _, ok := inputRoot["$defs"].(map[string]any)["Message"]; !ok {
		t.Fatalf("input definitions were not lifted to the MCP schema root: %#v", inputRoot)
	}
	nestedInput := inputRoot["properties"].(map[string]any)["input"].(map[string]any)
	if _, nested := nestedInput["$defs"]; nested {
		t.Fatalf("input definitions remained below a root-fragment reference: %#v", nestedInput)
	}

	outputRoot := projection.OutputSchema.(map[string]any)
	if _, ok := outputRoot["$defs"].(map[string]any)["Message"]; !ok {
		t.Fatalf("output definitions were not lifted to the MCP schema root: %#v", outputRoot)
	}
	itemSchema := outputRoot["properties"].(map[string]any)["outputs"].(map[string]any)["items"].(map[string]any)
	if _, nested := itemSchema["$defs"]; nested {
		t.Fatalf("output definitions remained below a root-fragment reference: %#v", itemSchema)
	}
}

func TestGenericToolResultPreservesCardinalityAndPartialFailure(t *testing.T) {
	for _, outputs := range [][]any{
		{},
		{nil},
		{"one"},
		{"one", "two"},
	} {
		result := genericToolResult(drainedOperation{Outputs: outputs})
		if result.IsError {
			t.Fatalf("successful sequence marked as error: %#v", result)
		}
		envelope := result.StructuredContent.(map[string]any)
		got := envelope["outputs"]
		if !reflect.DeepEqual(got, outputs) {
			t.Fatalf("output cardinality changed: got %#v, want %#v", got, outputs)
		}
		var textEnvelope map[string]any
		text := result.Content[0].(*mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &textEnvelope); err != nil {
			t.Fatalf("text fallback is not JSON: %v", err)
		}
		if !reflect.DeepEqual(textEnvelope["outputs"], outputs) {
			t.Fatalf("structured/text sequence mismatch: structured=%#v text=%#v", outputs, textEnvelope["outputs"])
		}
	}

	result := genericToolResult(drainedOperation{
		Outputs: []any{map[string]any{"n": 1}, nil},
		Error: &openbindings.InvocationError{
			Code:    openbindings.ErrCodeResponseError,
			Message: "stream failed",
			Details: map[string]any{"offset": 2},
		},
	})
	if !result.IsError {
		t.Fatal("terminal operation error was not surfaced")
	}
	envelope := result.StructuredContent.(map[string]any)
	if outputs := envelope["outputs"].([]any); len(outputs) != 2 || outputs[1] != nil {
		t.Fatalf("output sequence changed: %#v", envelope)
	}
	failure := envelope["error"].(map[string]any)
	if failure["code"] != openbindings.ErrCodeResponseError || failure["details"] == nil {
		t.Fatalf("error detail changed: %#v", failure)
	}
}

func TestRegisterInterfaceWithReport_RefusesImplicitBindingSelection(t *testing.T) {
	iface := &openbindings.Interface{
		OpenBindings: "0.2.0",
		Operations:   map[string]openbindings.Operation{"echo": {}},
		Sources: map[string]openbindings.Source{
			"a": {BindingSpec: "test.a", Location: "https://a.example"},
			"b": {BindingSpec: "test.b", Location: "https://b.example"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"echo.a": {Operation: "echo", Source: "a"},
			"echo.b": {Operation: "echo", Source: "b"},
		},
	}
	invoker := newRegistrationTestInvoker("test.a", "test.b")
	report := RegisterInterfaceWithReport(
		newTestMCPServer(),
		iface,
		invoker,
		nil,
		RegisterOptions{},
	)
	if report.Registered != 0 || report.Excluded != 1 {
		t.Fatalf("ambiguous operation was advertised: %#v", report)
	}

	report = RegisterInterfaceWithReport(
		newTestMCPServer(),
		iface,
		invoker,
		map[string]any{"configuration": map[string]any{"selection": []any{"echo.b"}}},
		RegisterOptions{},
	)
	if report.Registered != 1 || report.Excluded != 0 {
		t.Fatalf("explicit selection was not honored: %#v", report)
	}
}

func TestRegisterInterfaceWithReport_MatchesInvokerWiringSelection(t *testing.T) {
	tests := []struct {
		name       string
		iface      *openbindings.Interface
		context    map[string]any
		registered int
		reason     string
	}{
		{
			name: "no binding",
			iface: &openbindings.Interface{
				OpenBindings: "0.2.0",
				Operations:   map[string]openbindings.Operation{"echo": {}},
			},
			reason: "no binding",
		},
		{
			name: "unavailable binding spec",
			iface: &openbindings.Interface{
				OpenBindings: "0.2.0",
				Operations:   map[string]openbindings.Operation{"echo": {}},
				Sources: map[string]openbindings.Source{
					"main": {BindingSpec: "test.unavailable", Location: "https://example.test"},
				},
				Bindings: map[string]openbindings.BindingEntry{
					"echo.main": {Operation: "echo", Source: "main"},
				},
			},
			reason: "requires unavailable",
		},
		{
			name: "sole missing source",
			iface: &openbindings.Interface{
				OpenBindings: "0.2.0",
				Operations:   map[string]openbindings.Operation{"echo": {}},
				Bindings: map[string]openbindings.BindingEntry{
					"echo.main": {Operation: "echo", Source: "missing"},
				},
			},
			reason: "references missing source",
		},
		{
			name: "missing source participates in ambiguity",
			iface: &openbindings.Interface{
				OpenBindings: "0.2.0",
				Operations:   map[string]openbindings.Operation{"echo": {}},
				Sources: map[string]openbindings.Source{
					"main": {BindingSpec: "test.a", Location: "https://example.test"},
				},
				Bindings: map[string]openbindings.BindingEntry{
					"echo.main":    {Operation: "echo", Source: "main"},
					"echo.missing": {Operation: "echo", Source: "missing"},
				},
			},
			reason: "binding selection required",
		},
		{
			name: "explicit valid selection bypasses malformed alternative",
			iface: &openbindings.Interface{
				OpenBindings: "0.2.0",
				Operations:   map[string]openbindings.Operation{"echo": {}},
				Sources: map[string]openbindings.Source{
					"main": {BindingSpec: "test.a", Location: "https://example.test"},
				},
				Bindings: map[string]openbindings.BindingEntry{
					"echo.main":    {Operation: "echo", Source: "main"},
					"echo.missing": {Operation: "echo", Source: "missing"},
				},
			},
			context: map[string]any{
				"configuration": map[string]any{"selection": []string{"echo.main"}},
			},
			registered: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := RegisterInterfaceWithReport(
				newTestMCPServer(),
				test.iface,
				newRegistrationTestInvoker("test.a"),
				test.context,
				RegisterOptions{},
			)
			if report.Registered != test.registered {
				t.Fatalf("registered = %d, want %d: %#v", report.Registered, test.registered, report)
			}
			if test.reason != "" && (len(report.Entries) != 1 || !strings.Contains(report.Entries[0].Reason, test.reason)) {
				t.Fatalf("reason does not contain %q: %#v", test.reason, report)
			}
		})
	}
}

func assertOutputSequenceSchema(t *testing.T, schema any, itemType string) {
	t.Helper()
	root := schema.(map[string]any)
	properties := root["properties"].(map[string]any)
	outputs := properties["outputs"].(map[string]any)
	items := outputs["items"].(map[string]any)
	if root["type"] != "object" || outputs["type"] != "array" || items["type"] != itemType {
		t.Fatalf("wrong output sequence schema: %#v", root)
	}
}

func newTestMCPServer() *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{Name: "projection-test", Version: "test"}, nil)
}
