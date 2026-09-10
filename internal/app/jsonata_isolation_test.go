package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

func TestCoreAndDeferredGraphEvaluatorIsolation(t *testing.T) {
	e := &jsonataEvaluator{}
	ctx := context.Background()
	core, err := e.Evaluate(ctx, `0.1+0.2`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if equal, err := jsonvalue.Equal(core, json.Number("0.3")); err != nil || !equal {
		t.Fatalf("Core: %#v %v", core, err)
	}
	graph, err := e.EvaluateWithBindings(ctx, `0.1+0.2`, nil, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if equal, err := jsonvalue.Equal(graph, json.Number("0.30000000000000004")); err != nil || !equal {
		t.Fatalf("Graph changed its prior runtime: %#v %v", graph, err)
	}
	// Empty bindings still identify the Graph entry point; routing must not
	// depend on whether a root input is currently defined.
	for _, bindings := range []map[string]any{nil, {}, {"input": "root"}} {
		v, err := e.EvaluateWithBindings(ctx, `2 ** 3`, nil, bindings)
		if err != nil {
			t.Fatal(err)
		}
		if same, _ := jsonvalue.Equal(v, 8); !same {
			t.Fatalf("legacy control %#v", v)
		}
	}
	if _, err := e.Evaluate(ctx, `2 ** 3`, nil); err == nil {
		t.Fatal("legacy grammar leaked into Core")
	}
	value, err := e.Evaluate(ctx, `id`, map[string]any{"id": json.Number("9007199254740993")})
	if same, _ := jsonvalue.Equal(value, json.Number("9007199254740993")); err != nil || !same {
		t.Fatalf("official value lost after legacy use: %#v %v", value, err)
	}
}
