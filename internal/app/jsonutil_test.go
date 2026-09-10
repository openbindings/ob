package app

import (
	"encoding/json"
	"testing"
)

func TestNormalizeJSON_Nil(t *testing.T) {
	result, err := NormalizeJSON(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestNormalizeJSON_BasicTypes(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"map", map[string]any{"key": "value"}},
		{"slice", []any{1, 2, 3}},
		{"string", "hello"},
		{"float64", float64(42.5)},
		{"bool", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NormalizeJSON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Basic types should be returned as-is (same reference)
			// We can't easily test reference equality for all types,
			// so just verify no error and non-nil result
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestNormalizeJSON_Struct(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	input := testStruct{Name: "test", Value: 42}
	result, err := NormalizeJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["name"] != "test" {
		t.Errorf("expected 'test', got %v", m["name"])
	}
	if m["value"] != json.Number("42") {
		t.Errorf("expected 42, got %v", m["value"])
	}
}

func TestNormalizeJSON_NestedStruct(t *testing.T) {
	type inner struct {
		X int `json:"x"`
	}
	type outer struct {
		Inner inner `json:"inner"`
	}

	input := outer{Inner: inner{X: 10}}
	result, err := NormalizeJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	innerMap, ok := m["inner"].(map[string]any)
	if !ok {
		t.Fatalf("expected inner to be map[string]any, got %T", m["inner"])
	}
	if innerMap["x"] != json.Number("10") {
		t.Errorf("expected 10, got %v", innerMap["x"])
	}
}

func TestNormalizeJSON_ExactNumbers(t *testing.T) {
	for _, value := range []any{json.Number("1e400"), uint64(18446744073709551615), struct{ N json.Number }{json.Number("0.10000000000000000001")}} {
		before, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		normalized, err := NormalizeJSON(value)
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(normalized)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("changed %s to %s", before, after)
		}
	}
}

func TestNormalizeJSON_RejectsMalformedTypedNumber(t *testing.T) {
	for _, value := range []any{json.Number(""), struct{ N json.Number }{json.Number("")}} {
		if result, err := NormalizeJSON(value); err == nil {
			t.Fatalf("malformed number normalized as %v", result)
		}
	}
}
