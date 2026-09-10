package app

import (
	"context"
	"errors"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
)

type cancelDuringMarshal struct{ cancel context.CancelFunc }

func (v cancelDuringMarshal) MarshalJSON() ([]byte, error) {
	v.cancel()
	return []byte(`{"id":1}`), nil
}

func TestTransformCancellationAppDrivenBoundaries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	iface := &openbindings.Interface{Operations: map[string]openbindings.Operation{"test": {}}}
	binding := &openbindings.BindingEntry{Operation: "test", InputTransform: &openbindings.TransformOrRef{Inline: "$"}, OutputTransform: &openbindings.TransformOrRef{Inline: "$"}}
	resolved := &resolvedBinding{binding: binding, bindingKey: "test"}
	if err := prepareAppDrivenBindingInput(ctx, iface, "test", resolved, "input"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if v, err := ApplyTransform(ctx, nil, binding.InputTransform, "input"); v != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("%v %v", v, err)
	}
	result := <-unaryChannel(ctx, iface, resolved, InvocationResult{Output: "must not escape"})
	if result.Error == nil {
		t.Fatal("cancelled output transform returned success")
	}
	for _, unreadOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked_source", true: "blocked_sink"}[unreadOutput], func(t *testing.T) {
			streamCtx, stop := context.WithCancel(context.Background())
			defer stop()
			src := make(chan InvocationOutput, 1)
			if unreadOutput {
				src <- InvocationOutput{Output: "input"}
			}
			out := transformEventStream(streamCtx, src, iface, resolved)
			stop()
			select {
			case _, ok := <-out:
				if ok {
					t.Fatal("cancelled stream emitted")
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled stream did not close")
			}
		})
	}
}

func TestTransformCancellationAdapter(t *testing.T) {
	evaluator := &jsonataEvaluator{}
	t.Run("pre_cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		v, err := evaluator.Evaluate(ctx, "not even valid (((", nil)
		if v != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("%v %v", v, err)
		}
	})
	t.Run("cancelled_during_carriage", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		v, err := evaluator.Evaluate(ctx, "$", cancelDuringMarshal{cancel})
		if v != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("%v %v", v, err)
		}
	})
	for _, withBindings := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "deadline_with_bindings"}[withBindings], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			const expression = `$sum($map([1..1000000], function($v){$v*$v}))`
			var v any
			var err error
			if withBindings {
				v, err = evaluator.EvaluateWithBindings(ctx, expression, nil, map[string]any{"input": 1})
			} else {
				v, err = evaluator.Evaluate(ctx, expression, nil)
			}
			if v != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%v %v", v, err)
			}
		})
	}
	t.Run("reuse_and_named_bindings", func(t *testing.T) {
		v, err := evaluator.EvaluateWithBindings(context.Background(), "$input", nil, map[string]any{"input": "unchanged"})
		if err != nil || v != "unchanged" {
			t.Fatalf("%v %v", v, err)
		}
	})
}
