package app

import (
	"encoding/json"
	"testing"
)

func TestValidateInterface_ValidDocument(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "valid.json", minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	}))

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if !report.Valid {
		t.Errorf("expected valid, got invalid: %v", report.Problems)
	}
	if report.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %q", report.Version)
	}
}

func TestValidateInterface_MissingRequiredFields(t *testing.T) {
	dir := t.TempDir()
	// Missing operations and id.
	path := writeInterface(t, dir, "bad.json", map[string]any{
		"openbindings": "0.1.0",
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if report.Valid {
		t.Error("expected invalid")
	}
	if len(report.Problems) == 0 {
		t.Error("expected problems")
	}

	// Should report operations as required.
	foundOps := false
	for _, p := range report.Problems {
		if p == "operations: required" {
			foundOps = true
		}
	}
	if !foundOps {
		t.Error("expected 'operations: required' in problems")
	}
}

func TestValidateInterface_StrictMode(t *testing.T) {
	dir := t.TempDir()
	// Valid document but with unknown fields — strict should catch it.
	path := writeInterface(t, dir, "strict.json", map[string]any{
		"openbindings": "0.1.0",
		"operations":  map[string]any{},
		"customField": "should fail in strict",
	})

	// Non-strict: should pass.
	report := ValidateInterface(ValidateInput{Locator: path})
	if !report.Valid {
		t.Errorf("expected valid in non-strict mode, got: %v", report.Problems)
	}

	// Strict: should fail.
	report = ValidateInterface(ValidateInput{Locator: path, Strict: true})
	if report.Valid {
		t.Error("expected invalid in strict mode")
	}
}

func TestValidateInterface_BadVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "bad-version.json", map[string]any{
		"openbindings": "not-semver",
		"operations":  map[string]any{},
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Valid {
		t.Error("expected invalid")
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
}

func TestValidateInterface_JSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "valid.json", minimalInterface(map[string]any{
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
	if decoded.Valid != report.Valid {
		t.Error("round-trip changed Valid")
	}
	if decoded.Version != report.Version {
		t.Error("round-trip changed Version")
	}
}

func TestValidateInterface_DuplicateAliases(t *testing.T) {
	dir := t.TempDir()
	path := writeInterface(t, dir, "dup-alias.json", map[string]any{
		"openbindings": "0.1.0",
		"operations": map[string]any{
			"a": map[string]any{"aliases": []any{"shared"}},
			"b": map[string]any{"aliases": []any{"shared"}},
		},
	})

	report := ValidateInterface(ValidateInput{Locator: path})

	if report.Valid {
		t.Error("expected invalid (duplicate aliases)")
	}
}

func TestValidateInterface_Render(t *testing.T) {
	dir := t.TempDir()

	// Valid.
	path := writeInterface(t, dir, "valid.json", minimalInterface(map[string]any{
		"op": map[string]any{},
	}))
	report := ValidateInterface(ValidateInput{Locator: path})
	rendered := report.Render()
	if !contains(rendered, "Valid") {
		t.Errorf("expected 'Valid' in render output, got: %s", rendered)
	}

	// Invalid.
	path = writeInterface(t, dir, "bad.json", map[string]any{"openbindings": "0.1.0"})
	report = ValidateInterface(ValidateInput{Locator: path})
	rendered = report.Render()
	if !contains(rendered, "Invalid") {
		t.Errorf("expected 'Invalid' in render output, got: %s", rendered)
	}
}
