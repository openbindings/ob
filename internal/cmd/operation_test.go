package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// The §4.5.6 machine envelope must carry the displaced-elections warning:
// a machine consumer is never blind to a displaced standing election (the
// §7 carriage pin, invocation-configuration round 5).
func TestRenderInvokeJSON_CarriesDisplacedElections(t *testing.T) {
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
	if err := renderInvokeJSON(&buf, run); err != nil {
		t.Fatalf("render: %v", err)
	}
	var envelope struct {
		Outputs  []any          `json:"outputs"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, buf.String())
	}
	if len(envelope.Outputs) != 1 {
		t.Errorf("expected one output in the envelope, got %v", envelope.Outputs)
	}
	warning, _ := envelope.Metadata["x-ob-displaced-elections"].(string)
	if !strings.Contains(warning, "ext") {
		t.Errorf("metadata must carry the attributed displacement warning, got %#v", envelope.Metadata)
	}
	if _, ok := envelope.Metadata["x-ob-displaced-detail"]; !ok {
		t.Error("metadata must carry the displaced-elections detail")
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
