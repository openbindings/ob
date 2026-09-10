package mcpbridge

import (
	"encoding/json"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func TestGenericToolValueFidelity(t *testing.T) {
	for _, raw := range []string{`9007199254740993`, `0.10000000000000001`, `1e-400`, `1e400`, `null`, `[[],{"rawJSON":"2"}]`} {
		for _, object := range []bool{false, true} {
			schema := any(map[string]any{})
			arguments, expected := `{"input":`+raw+`}`, raw
			if object {
				schema = map[string]any{"type": "object"}
				arguments = `{"value":` + raw + `}`
				expected = arguments
			}
			projection := projectGenericTool(openbindings.Operation{Input: schema}, nil)
			input, err := projection.decodeInput(json.RawMessage(arguments))
			if err != nil || !input.Present {
				t.Fatalf("decode: %v", err)
			}
			var want any
			if err := jsonvalue.Unmarshal([]byte(expected), &want); err != nil {
				t.Fatal(err)
			}
			if eq, err := jsonvalue.Equal(want, input.Value); err != nil || !eq {
				t.Errorf("tool changed %s: %v (%v)", arguments, input.Value, err)
			}
			copy := deepCopyJSON(want)
			if eq, err := jsonvalue.Equal(want, copy); err != nil || !eq {
				t.Errorf("copy changed %s: %v (%v)", expected, copy, err)
			}
			var result struct {
				Value any `json:"value"`
			}
			if err := remarshal(map[string]any{"value": want}, &result); err != nil {
				t.Fatal(err)
			}
			if eq, err := jsonvalue.Equal(want, result.Value); err != nil || !eq {
				t.Errorf("bridge remarshal changed %s: %v (%v)", expected, result.Value, err)
			}
		}
	}
}

func TestGenericToolRefusesMalformedNumber(t *testing.T) {
	result := genericToolResult(drainedOperation{Outputs: []any{json.Number("")}})
	if !result.IsError {
		t.Fatal("malformed output was presented as a successful result")
	}
}
