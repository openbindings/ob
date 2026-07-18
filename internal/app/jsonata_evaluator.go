package app

// jsonataEvaluator implements openbindings.TransformEvaluatorWithBindings
// using the gnata JSONata engine (github.com/recolabs/gnata).
type jsonataEvaluator struct{}

func (j *jsonataEvaluator) Evaluate(expression string, data any) (any, error) {
	return evalTransform(expression, data, nil)
}

func (j *jsonataEvaluator) EvaluateWithBindings(expression string, data any, bindings map[string]any) (any, error) {
	// evalTransform maps gnata's undefined result (nil, nil) to
	// openbindings.ErrTransformUndefined, which the operation-graph engine
	// detects (errors.Is) to fail a node with TRANSFORM_UNDEFINED while JSON
	// null flows downstream normally.
	return evalTransform(expression, data, bindings)
}
