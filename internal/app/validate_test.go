package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestValidateInterface_ConformantDocument(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "conformant.json", map[string]any{
		"openbindings": "0.2.0",
		"operations": map[string]any{
			"greet": map[string]any{"input": map[string]any{"type": "object"}},
		},
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if report.Conclusion != "conformant" || report.Failed() {
		t.Errorf("conclusion = %q, findings %+v; want conformant", report.Conclusion, report.Findings)
	}
	if report.Version != "0.2.0" {
		t.Errorf("expected version 0.2.0, got %q", report.Version)
	}
}

func TestValidateInterface_MissingRequiredFields(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "bad.json", map[string]any{
		"openbindings": "0.2.0",
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if report.Conclusion != "non-conformant" || !report.Failed() {
		t.Fatalf("conclusion = %q; want non-conformant", report.Conclusion)
	}
	found := false
	for _, f := range report.Findings {
		if f.Rule == "OBI-D-02" && f.Status == "violated" && strings.Contains(f.Path+f.Message, "operations") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an OBI-D-02 violation naming operations, got %+v", report.Findings)
	}
}

func TestValidateInterface_UnknownFieldsAreDiagnosedNotViolated(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "unknown.json", map[string]any{
		"openbindings": "0.2.0",
		"operations":   map[string]any{},
		"customField":  "ignored, per OBI-T-02",
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Conclusion != "conformant" {
		t.Fatalf("conclusion = %q; unknown fields must not affect it (OBI-T-02)", report.Conclusion)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Rule != "OBI-T-02" || !strings.Contains(report.Diagnostics[0].Message, "customField") {
		t.Fatalf("diagnostics = %+v; want one OBI-T-02 diagnostic naming customField", report.Diagnostics)
	}
}

func TestValidateInterface_BindingsLeaveConformanceUndetermined(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "bound.json", map[string]any{
		"openbindings": "0.2.0",
		"operations":   map[string]any{"op": map[string]any{}},
		"sources": map[string]any{
			"api": map[string]any{"bindingSpec": "example.rest@1", "location": "https://api.example.com/openapi.json"},
		},
		"bindings": map[string]any{
			"op.api": map[string]any{"operation": "op", "source": "api"},
		},
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Conclusion != "conformance-undetermined" || report.Failed() {
		t.Fatalf("conclusion = %q, failed %v; want undetermined without failing", report.Conclusion, report.Failed())
	}
	if !reflect.DeepEqual(report.Inconclusive, []string{"OBI-D-13"}) {
		t.Fatalf("inconclusive = %v; want OBI-D-13", report.Inconclusive)
	}
}

func TestValidateInterface_BadVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "bad-version.json", map[string]any{
		"openbindings": "not-semver",
		"operations":   map[string]any{},
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Conclusion != "non-conformant" || !reflect.DeepEqual(report.Violated, []string{"OBI-D-02", "OBI-D-12"}) && !reflect.DeepEqual(report.Violated, []string{"OBI-D-12"}) {
		t.Errorf("conclusion %q violated %v; want non-conformant on OBI-D-12", report.Conclusion, report.Violated)
	}
}

func TestValidateInterface_UnsupportedVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "future.json", map[string]any{
		"openbindings": "9.0.0",
		"operations":   map[string]any{},
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Refusal == nil || report.Refusal.Version != "9.0.0" {
		t.Fatalf("refusal = %+v; want a refusal of 9.0.0", report.Refusal)
	}
	if report.Conclusion != "" || !report.Failed() {
		t.Fatalf("a refused document has no conclusion and fails the gate: %+v", report)
	}
	if rendered := report.Render(); !strings.Contains(rendered, "Refused") {
		t.Errorf("expected 'Refused' in render output, got: %s", rendered)
	}
}

func TestValidateInterface_DuplicateKeysAreDecidedOnTheBytes(t *testing.T) {
	report := ValidateInterface(ValidateInput{Document: []byte(`{"openbindings":"0.2.0","operations":{},"operations":{}}`)})
	if report.Conclusion != "non-conformant" || !reflect.DeepEqual(report.Violated, []string{"OBI-D-01"}) {
		t.Fatalf("conclusion %q violated %v; want non-conformant on OBI-D-01", report.Conclusion, report.Violated)
	}
}

func TestValidateInterface_ResolveError(t *testing.T) {
	report := ValidateInterface(ValidateInput{Locator: "/nonexistent/file.json"})

	if report.Error == nil {
		t.Fatal("expected resolve error")
	}
	if report.Error.Code != "resolve_error" {
		t.Errorf("expected code 'resolve_error', got %q", report.Error.Code)
	}
	if !report.Failed() {
		t.Error("a resolve error fails the gate")
	}
}

func TestValidateInterface_JSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "doc.json", minimalInterface(map[string]any{
		"op": map[string]any{},
	}))

	report := ValidateInterface(ValidateInput{Locator: path})

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ValidationReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, report) {
		t.Errorf("round-trip changed the report:\n got %+v\nwant %+v", decoded, report)
	}
}

func TestValidateInterface_DuplicateAliases(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "dup-alias.json", map[string]any{
		"openbindings": "0.2.0",
		"operations": map[string]any{
			"a": map[string]any{"aliases": []any{"shared"}},
			"b": map[string]any{"aliases": []any{"shared"}},
		},
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Conclusion != "non-conformant" {
		t.Errorf("conclusion = %q; want non-conformant (duplicate aliases)", report.Conclusion)
	}
}

func TestValidateInterface_Render(t *testing.T) {
	dir := t.TempDir()

	path := writeInterface(t, dir, "conformant.json", map[string]any{
		"openbindings": "0.2.0",
		"operations":   map[string]any{"op": map[string]any{}},
	})
	rendered := ValidateInterface(ValidateInput{Locator: path}).Render()
	if !contains(rendered, "Conformant") {
		t.Errorf("expected 'Conformant' in render output, got: %s", rendered)
	}

	path = writeInterface(t, dir, "bad.json", map[string]any{"openbindings": "0.2.0"})
	rendered = ValidateInterface(ValidateInput{Locator: path}).Render()
	if !contains(rendered, "Non-conformant") {
		t.Errorf("expected 'Non-conformant' in render output, got: %s", rendered)
	}
}
