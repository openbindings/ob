package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/openbindings-go/invoke"
)

func TestOperationUnbind_ExactBindingKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interface.obi.json")
	document := `{
	  "openbindings": "0.2.0",
	  "operations": {"greet": {}},
	  "sources": {
	    "api": {"bindingSpec": "example.api@1", "content": {}}
	  },
	  "bindings": {
	    "friendly-name": {
	      "operation": "greet",
	      "source": "api",
	      "selector": "#/greet"
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runOB(
		t,
		"operation",
		"unbind",
		path,
		"--binding",
		"friendly-name",
	); err != nil {
		t.Fatalf("unbind exact binding: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var updated struct {
		Operations map[string]json.RawMessage `json:"operations"`
		Bindings   map[string]json.RawMessage `json:"bindings"`
	}
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.Operations["greet"]; !ok {
		t.Error("operation should remain after exact unbind")
	}
	if _, ok := updated.Bindings["friendly-name"]; ok {
		t.Error("exact binding key should be removed")
	}
}

func TestRenderInvokeJSON_SuccessEnvelopeIsMinimal(t *testing.T) {
	ch := make(chan app.InvocationOutput, 1)
	ch <- app.InvocationOutput{Output: map[string]any{"ok": true}}
	close(ch)
	run := &app.ConfiguredInvocation{
		BindingKey: "op.usage",
		Events:     ch,
	}
	var buf bytes.Buffer
	if err := renderInvokeJSON(&buf, run); err != nil {
		t.Fatalf("render: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, buf.String())
	}
	if outputs, _ := envelope["outputs"].([]any); len(outputs) != 1 {
		t.Errorf("expected one output in the envelope, got %v", envelope)
	}
	if len(envelope) != 1 {
		t.Fatalf("aggregate success envelope leaked non-invocation fields: %#v", envelope)
	}
}

func TestRenderInvokeJSON_DefaultIsProtocolBlindAndPreservesPartialOutputs(t *testing.T) {
	ch := make(chan app.InvocationOutput, 3)
	ch <- app.InvocationOutput{Output: map[string]any{"partial": true}}
	ch <- app.InvocationOutput{Error: invoke.NewInvocationErrorWithData(
		invoke.ErrCodeExecutionFailed, map[string]any{"reason": "declined"})}
	close(ch)
	run := &app.ConfiguredInvocation{BindingKey: "op.openapi", Events: ch}
	var buf bytes.Buffer
	err := renderInvokeJSON(&buf, run)
	if exit, ok := err.(app.ExitResult); !ok || exit.Code != 1 {
		t.Fatalf("render error = %#v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, buf.String())
	}
	outputs, _ := envelope["outputs"].([]any)
	if len(outputs) != 1 {
		t.Fatalf("partial outputs were lost: %#v", envelope)
	}
	if _, present := envelope["diagnostics"]; present {
		t.Fatalf("native diagnostics leaked by default: %#v", envelope)
	}
	if _, present := envelope["metadata"]; present {
		t.Fatalf("legacy metadata leaked by default: %#v", envelope)
	}
	errorValue, _ := envelope["error"].(map[string]any)
	if errorValue["code"] != invoke.ErrCodeExecutionFailed {
		t.Fatalf("abstract unsuccessful completion missing: %#v", envelope)
	}
	if data, _ := errorValue["data"].(map[string]any); data["reason"] != "declined" {
		t.Fatalf("application-authored failure data changed: %#v", envelope)
	}
}

// The --input house grammar: inline JSON, @file, and - (stdin). Inline
// invalid JSON is refused.
func TestReadInvokeInput_Inline(t *testing.T) {
	v, err := readInvokeInput(`{"limit":10}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok || m["limit"] != float64(10) {
		t.Errorf("parsed = %#v", v)
	}

	if _, err := readInvokeInput("{bad"); err == nil {
		t.Error("expected invalid inline JSON to be refused")
	}

	if v, err := readInvokeInput(""); err != nil || v != nil {
		t.Errorf("empty input should be nil/no-error, got %v / %v", v, err)
	}
}

func TestReadInvokeConfiguration_RequiresObjectAndCarriesNamedPoints(t *testing.T) {
	configuration, err := readInvokeConfiguration(`{
		"document":{"source":"query Viewer { viewer { id } }","operationName":"Viewer"},
		"protocolFields":{"httpHeaders":{"X-Tenant":"acme"}}
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	document, ok := configuration["document"].(map[string]any)
	if !ok || document["operationName"] != "Viewer" {
		t.Fatalf("document configuration = %#v", configuration["document"])
	}

	for _, invalid := range []string{`["query { viewer { id } }"]`, `"query { viewer { id } }"`, `null`} {
		if _, err := readInvokeConfiguration(invalid); err == nil {
			t.Errorf("expected non-object %s to be refused", invalid)
		}
	}

	config := &app.InvokeConfig{
		Selection:     []string{"viewer.graphql"},
		Configuration: configuration,
	}
	context := config.Context()
	got := context["configuration"].(map[string]any)
	if got["selection"].([]string)[0] != "viewer.graphql" {
		t.Fatalf("selection was not merged: %#v", got)
	}
	if _, ok := got["protocolFields"]; !ok {
		t.Fatalf("binding configuration was not carried: %#v", got)
	}
}

func TestInvocationStdinConflict(t *testing.T) {
	for _, tc := range []struct {
		obi, input, configuration string
		conflict                  bool
	}{
		{"interface.json", "-", "", false},
		{"interface.json", "", "-", false},
		{"-", "", "", false},
		{"interface.json", "-", "-", true},
		{"-", "-", "", true},
		{"-", "", "-", true},
		{"-", "-", "-", true},
	} {
		got := invocationStdinConflict(tc.obi, tc.input, tc.configuration)
		if (got != "") != tc.conflict {
			t.Errorf("invocationStdinConflict(%q, %q, %q) = %q", tc.obi, tc.input, tc.configuration, got)
		}
	}
}

// The remedy line for a config.value challenge is the copy-pasteable
// --config command against the challenge's asserted target (never a
// full-replace --value, which could clobber sibling points).
func TestContextSetHint_ConfigValue(t *testing.T) {
	details := &invoke.ContextRequiredDetails{
		Target: "https://host.example/openapi.json",
		Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{
			Type:  "config.value",
			Extra: map[string]any{"point": "server", "path": "/url"},
		}}}},
	}
	got := contextSetHint(details)
	want := "ob context set https://host.example/openapi.json --config server='<json>'"
	if got != want {
		t.Errorf("contextSetHint = %q, want %q", got, want)
	}

	// Still suppressed when the challenge asserts no target.
	details.Target = ""
	if got := contextSetHint(details); got != "" {
		t.Errorf("empty-target hint = %q, want suppression", got)
	}
}
