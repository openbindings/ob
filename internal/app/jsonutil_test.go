package app

import "testing"

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
	if m["value"] != float64(42) { // JSON numbers are float64
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
	if innerMap["x"] != float64(10) {
		t.Errorf("expected 10, got %v", innerMap["x"])
	}
}
