package app

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

func TestTransformResultDomain(t *testing.T) {
	for _, expression := range []string{`function(){1}`, `{"nested":function(){1}}`, `[function(){1}]`, `$sum`, `absent`} {
		t.Run(expression, func(t *testing.T) {
			value, err := executeJSONata(expression, map[string]any{"input": 42})
			if err == nil || value != nil {
				t.Fatalf("non-JSON result accepted: %#v, %v", value, err)
			}
		})
	}
	for _, expression := range []string{`null`, `[]`, `[null]`, `{"Body":1,"Closure":{},"CapturedFocus":42}`, `{"nested":[{"value":null}]}`} {
		t.Run(expression, func(t *testing.T) {
			value, err := executeJSONata(expression, nil)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(value)
			if err != nil || !json.Valid(raw) {
				t.Fatalf("invalid JSON: %s, %v", raw, err)
			}
		})
	}
	// This was previously a binary64-overflow negative control. In the official
	// assigned-decimal domain it is a valid finite result, not Infinity. Host
	// Infinity admission remains rejected in TestTransformVariableNormalizationError.
	wide, err := executeJSONata(`1e308 * 1e308`, nil)
	if same, _ := jsonvalue.Equal(wide, json.Number("1e616")); err != nil || !same {
		t.Fatalf("valid exact result rejected or changed: %#v %v", wide, err)
	}
}

func TestTransformVariableNormalizationError(t *testing.T) {
	cycle := make([]any, 1)
	cycle[0] = cycle
	objectCycle := map[string]any{}
	objectCycle["self"] = objectCycle
	for _, invalid := range []any{math.Inf(1), func() {}, map[string]any{"nested": func() {}}, cycle, objectCycle} {
		if value, err := evalTransform(`$bound`, nil, map[string]any{"bound": invalid}); err == nil || value != nil {
			t.Fatalf("invalid variable accepted: %#v, %v", value, err)
		}
	}
}
