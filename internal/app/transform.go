// Package app - transform.go provides JSONata transform execution for OpenBindings.
package app

import (
	"context"
	"encoding/json"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/recolabs/gnata"
)

// ApplyTransform applies a JSONata transform to input data.
// If the transform is nil, returns the input unchanged.
// If the transform is a $ref, it is resolved against the transforms map.
func ApplyTransform(transforms map[string]openbindings.Transform, tor *openbindings.TransformOrRef, input any) (any, error) {
	if tor == nil {
		return input, nil
	}

	// Resolve the transform reference if needed
	expression, ok := tor.Resolve(transforms)
	if !ok {
		if tor.IsRef() {
			return nil, fmt.Errorf("transform reference %q not found", tor.Ref)
		}
		return nil, fmt.Errorf("invalid transform: neither ref nor inline")
	}

	if expression == "" {
		return nil, fmt.Errorf("transform expression is empty")
	}

	return executeJSONata(expression, input)
}

// executeJSONata compiles and evaluates a JSONata expression against input
// data on the direct (non-graph) transform path. An undefined result is
// surfaced as an error here, matching the prior behavior of this path.
func executeJSONata(expression string, input any) (any, error) {
	return evalTransform(expression, input, nil)
}

// evalTransform compiles and evaluates a JSONata expression against input data
// using the gnata engine.
//
// Input is marshaled to JSON bytes and evaluated via EvalBytes/
// EvalBytesWithVars: the byte path preserves object member order through
// evaluation, whereas gnata's Eval(map[string]any) path sorts keys. The raw
// result is then normalized to the Go JSON value model (map[string]any, []any,
// float64, string, bool, nil) — the shape the rest of the invocation pipeline
// (T-08 schema validation, binding-invoker input routing, the operation-graph
// engine) consumes. Optional vars bind JSONata $variables (e.g. $input for
// operation-graph node transforms).
//
// gnata signals an undefined result as (nil, nil), distinct from a JSON null
// result (a non-nil sentinel); this is mapped to ErrTransformUndefined so the
// undefined/null distinction survives (the operation-graph engine maps it to
// TRANSFORM_UNDEFINED, and the direct path treats it as an error).
func evalTransform(expression string, input any, vars map[string]any) (any, error) {
	expr, err := gnata.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("compile jsonata expression: %w", err)
	}

	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal transform input: %w", err)
	}

	var result any
	if len(vars) > 0 {
		bound := make(map[string]any, len(vars))
		for k, v := range vars {
			bound[k], _ = normalizeTransformResult(v)
		}
		result, err = expr.EvalBytesWithVars(context.Background(), data, bound)
	} else {
		result, err = expr.EvalBytes(context.Background(), data)
	}
	if err != nil {
		return nil, fmt.Errorf("evaluate jsonata expression: %w", err)
	}

	if result == nil {
		return nil, openbindings.ErrTransformUndefined
	}

	return normalizeTransformResult(result)
}

// normalizeTransformResult round-trips a raw gnata result through JSON into the
// Go JSON value model. It always round-trips (rather than a fast-path type
// switch) because gnata's order-preserving object type is an internal type that
// can appear nested inside a []any, so a shallow check would miss it.
func normalizeTransformResult(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("normalize transform result: %w", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("normalize transform result: %w", err)
	}
	return out, nil
}
