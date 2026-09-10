package app

import (
	"context"
	"encoding/json"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func TestTransformStringFidelity(t *testing.T) {
	for _, source := range []string{`{"\ud800":"\udc00","value":"\ud800"}`, `$`, `$ ~> |$|{}|`, `$eval($string($))`} {
		t.Run(source, func(t *testing.T) {
			var input any
			raw := []byte(`{"\ud800":"\udc00","value":"\ud800"}`)
			if err := jsonvalue.Unmarshal(raw, &input); err != nil {
				t.Fatal(err)
			}
			got, err := ApplyTransform(context.Background(), nil, &openbindings.TransformOrRef{Inline: source}, input)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := jsonvalue.Marshal(got)
			if err != nil || !json.Valid(encoded) {
				t.Fatalf("invalid result: %s %v", encoded, err)
			}
			if same, err := jsonvalue.Equal(got, input); err != nil || !same {
				t.Fatalf("changed assigned string value: %s %v", encoded, err)
			}
		})
	}
}

func TestBindingTransformDoesNotExposeCompatibilityClone(t *testing.T) {
	for _, source := range []string{`$clone($)`, `$eval("$clone($)")`} {
		if value, err := ApplyTransform(context.Background(), nil, &openbindings.TransformOrRef{Inline: source}, map[string]any{}); err == nil {
			t.Fatalf("nonstandard helper escaped through CLI: %#v", value)
		}
	}
}
