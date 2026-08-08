package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
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
	      "ref": "#/greet"
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

// Selected-binding and implementation evidence is available only through the
// caller's explicit diagnostic escape hatch.
func TestRenderInvokeJSON_ExplicitDiagnosticsCarryDisplacedElections(t *testing.T) {
	ch := make(chan app.InvocationOutput, 2)
	ch <- app.InvocationOutput{Output: map[string]any{"ok": true}}
	ch <- app.InvocationOutput{Terminal: true}
	close(ch)
	run := &app.ConfiguredInvocation{
		BindingKey:       "op.usage",
		DisplacedWarning: `2 internal-table election(s) for "op" do not reach delegate "ext"; its own handling governs the binding hop`,
		DisplacedDetail:  []string{"decode=json", "ok-exit=0,1"},
		Events:           ch,
	}
	var buf bytes.Buffer
	if err := renderInvokeJSON(&buf, run, true); err != nil {
		t.Fatalf("render: %v", err)
	}
	var envelope struct {
		Outputs     []any          `json:"outputs"`
		Diagnostics map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, buf.String())
	}
	if len(envelope.Outputs) != 1 {
		t.Errorf("expected one output in the envelope, got %v", envelope.Outputs)
	}
	displaced, _ := envelope.Diagnostics["displacedElections"].(map[string]any)
	warning, _ := displaced["message"].(string)
	if !strings.Contains(warning, "ext") {
		t.Errorf("diagnostics must carry the attributed displacement warning, got %#v", envelope.Diagnostics)
	}
	if _, ok := displaced["details"]; !ok {
		t.Error("diagnostics must carry the displaced-elections detail")
	}
}

func TestRenderInvokeJSON_DefaultIsProtocolBlindAndPreservesPartialOutputs(t *testing.T) {
	ch := make(chan app.InvocationOutput, 3)
	ch <- app.InvocationOutput{Output: map[string]any{"partial": true}}
	ch <- app.InvocationOutput{Error: &openbindings.InvocationError{
		Code:        openbindings.ErrCodeExecutionFailed,
		Message:     "operation completed unsuccessfully",
		Diagnostics: map[string]any{"httpResponse": map[string]any{"status": 500}},
	}}
	close(ch)
	run := &app.ConfiguredInvocation{BindingKey: "op.openapi", Events: ch}
	var buf bytes.Buffer
	err := renderInvokeJSON(&buf, run, false)
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
	if errorValue["code"] != openbindings.ErrCodeExecutionFailed {
		t.Fatalf("abstract unsuccessful completion missing: %#v", envelope)
	}
}

// The data-face flags refuse typos loudly (a misspelled lane or channel can
// never silently change behavior); valid inputs compile per-axis.
func TestBuildInvokeConfig(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		c, err := buildInvokeConfig("json", "0,1", []string{"source=stdin-dash", "extra=file"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Decode != "json" {
			t.Errorf("Decode = %q", c.Decode)
		}
		if len(c.OKExits) != 2 || c.OKExits[0] != 0 || c.OKExits[1] != 1 {
			t.Errorf("OKExits = %v", c.OKExits)
		}
		if c.Routes["source"] != "stdin-dash" || c.Routes["extra"] != "file" {
			t.Errorf("Routes = %v", c.Routes)
		}
	})

	t.Run("unknown decode lane refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("jsonn", "", nil); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Errorf("expected --decode refusal, got %v", err)
		}
	})

	t.Run("unknown channel refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("", "", []string{"f=pipe"}); err == nil || !strings.Contains(err.Error(), "channel") {
			t.Errorf("expected --route channel refusal, got %v", err)
		}
	})

	t.Run("malformed route refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("", "", []string{"noequals"}); err == nil {
			t.Errorf("expected field=channel refusal, got %v", err)
		}
	})

	t.Run("non-integer exit refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("", "0,x", nil); err == nil || !strings.Contains(err.Error(), "ok-exit") {
			t.Errorf("expected --ok-exit refusal, got %v", err)
		}
	})
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
