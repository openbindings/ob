package app

import (
	"errors"
	"fmt"

	"github.com/blues/jsonata-go"
	openbindings "github.com/openbindings/openbindings-go"
)

// jsonataEvaluator implements openbindings.TransformEvaluatorWithBindings
// using the jsonata-go library.
type jsonataEvaluator struct{}

func (j *jsonataEvaluator) Evaluate(expression string, data any) (any, error) {
	return j.EvaluateWithBindings(expression, data, nil)
}

func (j *jsonataEvaluator) EvaluateWithBindings(expression string, data any, bindings map[string]any) (any, error) {
	expr, err := jsonata.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("compile jsonata: %w", err)
	}

	normalized, err := NormalizeJSON(data)
	if err != nil {
		return nil, fmt.Errorf("normalize data: %w", err)
	}

	// Register bindings as JSONata variables (e.g., $input for graph context).
	if len(bindings) > 0 {
		vars := make(map[string]any, len(bindings))
		for k, v := range bindings {
			nv, _ := NormalizeJSON(v)
			vars[k] = nv
		}
		if err := expr.RegisterVars(vars); err != nil {
			return nil, fmt.Errorf("register jsonata vars: %w", err)
		}
	}

	result, err := expr.Eval(normalized)
	if err != nil {
		// An undefined result is a distinct signal, not an evaluation
		// failure: the operation-graph engine fails the node with
		// TRANSFORM_UNDEFINED while null flows downstream normally.
		if errors.Is(err, jsonata.ErrUndefined) {
			return nil, openbindings.ErrTransformUndefined
		}
		return nil, fmt.Errorf("evaluate jsonata: %w", err)
	}
	return result, nil
}
