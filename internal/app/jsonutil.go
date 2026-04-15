// Package app - jsonutil.go provides JSON normalization and conversion utilities.
package app

import "encoding/json"

// NormalizeJSON converts a Go value to a JSON-normalized form (map[string]any, []any, etc).
// This ensures consistent handling of structs, typed maps, and other Go values
// when working with JSON-based transformations.
//
// Returns the input unchanged if it's already a basic JSON type.
// Otherwise, round-trips through JSON marshaling to normalize.
func NormalizeJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}

	// If already a basic JSON type, return as-is
	switch v.(type) {
	case map[string]any, []any, string, float64, bool:
		return v, nil
	}

	// Round-trip through JSON to normalize
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}