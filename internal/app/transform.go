// Package app - transform.go provides JSONata transform execution for OpenBindings.
package app

import (
	"fmt"

	"github.com/blues/jsonata-go"
	openbindings "github.com/openbindings/openbindings-go"
)

// ApplyTransform applies a JSONata transform to input data.
// If the transform is nil, returns the input unchanged.
// If the transform is a $ref, it is resolved against the transforms map.
func ApplyTransform(transforms map[string]openbindings.Transform, tor *openbindings.TransformOrRef, input any) (any, error) {
	if tor == nil {
		return input, nil
	}

	// Resolve the transform reference if needed
	transform := tor.Resolve(transforms)
	if transform == nil {
		if tor.IsRef() {
			return nil, fmt.Errorf("transform reference %q not found", tor.Ref)
		}
		return nil, fmt.Errorf("invalid transform: neither ref nor inline")
	}

	// Validate transform type
	if transform.Type != "jsonata" {
		return nil, fmt.Errorf("unsupported transform type %q (only 'jsonata' is supported)", transform.Type)
	}

	if transform.Expression == "" {
		return nil, fmt.Errorf("transform expression is empty")
	}

	return executeJSONata(transform.Expression, input)
}

// executeJSONata compiles and executes a JSONata expression against input data.
func executeJSONata(expression string, input any) (any, error) {
	// Compile the expression
	expr, err := jsonata.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("compile jsonata expression: %w", err)
	}

	// Convert input to JSON-compatible format if needed
	// The jsonata-go library works with any Go values, but for consistency
	// we ensure we're working with map[string]any / []any types
	normalizedInput, err := NormalizeJSON(input)
	if err != nil {
		return nil, fmt.Errorf("normalize input: %w", err)
	}

	// Execute the expression
	result, err := expr.Eval(normalizedInput)
	if err != nil {
		return nil, fmt.Errorf("evaluate jsonata expression: %w", err)
	}

	return result, nil
}
