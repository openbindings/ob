package app

import "context"

// jsonataEvaluator is a CLI-private compatibility composition. Core transforms
// call Evaluate. The only production EvaluateWithBindings caller is the
// deferred Graph engine, whose $input expressions retain its old runtime.
// No numerical mode or document option is introduced. Keep the routing test
// and call-site inventory when changing either consumer.
type jsonataEvaluator struct{}

func (j *jsonataEvaluator) Evaluate(ctx context.Context, expression string, data any) (any, error) {
	return evalTransformContext(ctx, expression, data, nil)
}

func (j *jsonataEvaluator) EvaluateWithBindings(ctx context.Context, expression string, data any, bindings map[string]any) (any, error) {
	// evalTransform maps gnata's undefined result (nil, nil) to
	// invoke.ErrTransformUndefined, which the operation-graph engine
	// detects (errors.Is) to fail a node with TRANSFORM_UNDEFINED while JSON
	// null flows downstream normally.
	return legacyEvalTransformContext(ctx, expression, data, bindings)
}
