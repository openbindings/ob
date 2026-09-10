// Package app - transform.go provides JSONata transform execution for OpenBindings.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	gnata "github.com/openbindings/ob/internal/graphjsonata"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"
	jsonataevaluator "github.com/openbindings/openbindings-go/invoke/jsonata"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// ApplyTransform applies a JSONata transform to input data.
// If the transform is nil, returns the input unchanged.
// If the transform is a $ref, it is resolved against the transforms map.
func ApplyTransform(ctx context.Context, transforms map[string]openbindings.Transform, tor *openbindings.TransformOrRef, input any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

	return evalTransformContext(ctx, expression, input, nil)
}

// executeJSONata compiles and evaluates a JSONata expression against input
// data on the direct (non-graph) transform path. An undefined result is
// surfaced as an error here, matching the prior behavior of this path.
func executeJSONata(expression string, input any) (any, error) {
	return evalTransform(expression, input, nil)
}

// evalTransform is the direct Core-transform helper. It uses the same official
// SDK adapter as invocation, with no CLI arithmetic or serialization shortcuts.
func evalTransform(expression string, input any, vars map[string]any) (any, error) {
	return evalTransformContext(context.Background(), expression, input, vars)
}

var coreTransformEvaluator = sync.OnceValue(func() *jsonataevaluator.Evaluator {
	evaluator, err := jsonataevaluator.New(jsonataevaluator.Options{})
	if err != nil {
		panic(err)
	} // constant composition, not caller data
	return evaluator
})

func evalTransformContext(ctx context.Context, expression string, input any, vars map[string]any) (any, error) {
	return coreTransformEvaluator().EvaluateWithBindings(ctx, expression, input, vars)
}

// legacyEvalTransformContext preserves the deferred Graph consumer's prior
// Gnata v0.2.2 engine. Its private relocation has import-path-only changes;
// no candidate number/sequence/regex behavior is silently applied to Graph.
// The CLI compatibility router is not the public SDK evaluator contract.
func legacyEvalTransformContext(ctx context.Context, expression string, input any, vars map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expr, err := gnata.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("compile jsonata expression: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := jsonvalue.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal transform input: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var result any
	if len(vars) > 0 {
		bound := make(map[string]any, len(vars))
		for k, v := range vars {
			bound[k], err = normalizeTransformResult(v)
			if err != nil {
				return nil, fmt.Errorf("normalize transform variable %q: %w", k, err)
			}
		}
		result, err = expr.EvalBytesWithVars(ctx, data, bound)
	} else {
		result, err = expr.EvalBytes(ctx, data)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("evaluate jsonata expression: %w", err)
	}

	if result == nil {
		return nil, invoke.ErrTransformUndefined
	}

	value, err := normalizeTransformResult(result)
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	return value, err
}

// normalizeTransformResult round-trips a raw gnata result through JSON into the
// Go JSON value model. It always round-trips (rather than a fast-path type
// switch) because gnata's order-preserving object type is an internal type that
// can appear nested inside a []any, so a shallow check would miss it.
func normalizeTransformResult(v any) (any, error) {
	// Normalize only the engine's public JSON carriers, then validate their
	// domain BEFORE marshaling. A gnata function is a Go struct and can marshal
	// successfully; successful encoding does not make it a JSONata JSON value.
	v, err := transformJSONValue(v, 0)
	if err != nil {
		return nil, err
	}
	b, err := jsonvalue.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("normalize transform result: %w", err)
	}
	var out any
	if err := jsonvalue.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("normalize transform result: %w", err)
	}
	return out, nil
}

func transformJSONValue(v any, depth int) (any, error) {
	if depth > 512 {
		return nil, fmt.Errorf("transform result exceeds JSON nesting limit")
	}
	// Traverse ordinary containers ourselves so a cyclic host value (notably
	// a supplied variable) reaches our depth guard rather than recursing inside
	// the engine's normalizer. Only engine-owned carriers need that conversion.
	switch v.(type) {
	case []any, map[string]any:
	default:
		v = gnata.NormalizeValue(v)
	}
	switch value := v.(type) {
	case json.Number:
		if !jsonvalue.IsNumber(value) {
			return nil, fmt.Errorf("transform result contains an invalid JSON number")
		}
		return value, nil
	case nil, bool, string,
		float32, float64, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return value, nil // encoding/json checks number syntax and finiteness.
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			var err error
			out[i], err = transformJSONValue(item, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			var err error
			out[key], err = transformJSONValue(item, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("transform result is not a JSON value (%T)", v)
	}
}
