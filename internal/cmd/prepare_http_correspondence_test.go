package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
	openapiformat "github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
)

type prepareHTTPTransforms struct{}

func (prepareHTTPTransforms) Evaluate(ctx context.Context, expression string, data any) (any, error) {
	return app.ApplyTransform(ctx, nil, &openbindings.TransformOrRef{Inline: expression}, data)
}

// This drives the published binding and its real transform, rather than writing
// a request that merely agrees with the handler's private implementation.
func TestServeOperationPrepare_PublishedHTTPBinding(t *testing.T) {
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()
	ts := testEnv(t)
	defer ts.Close()
	response, err := http.Get(ts.URL + "/.well-known/openbindings")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var document openbindings.Interface
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	client := invoke.NewOperationInvoker(openapiformat.NewInvoker())
	client.TransformEvaluator = prepareHTTPTransforms{}
	for _, withContext := range []bool{false, true} {
		t.Run(map[bool]string{false: "bare", true: "context"}[withContext], func(t *testing.T) {
			input := map[string]any{"interface": echoOperationInterface(), "operation": "echo"}
			if withContext {
				input["context"] = map[string]any{"configuration": map[string]any{"example": true}}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			call := invoke.Invoke(ctx, client, &document, invoke.NewOperationSignature[any, any]("openbindings.ob.prepareOperation"),
				invoke.WithBindingKey("openbindings.ob.prepareOperation.openapi"),
				invoke.WithContext(map[string]any{"configuration": map[string]any{"security": map[string]any{"index": 1}}, "credentials": map[string]any{"bearerAuth": "test-token"}}))
			if err := call.Write(ctx, input); err != nil {
				t.Fatalf("prepare invocation: %#v", err)
			}
			if err := call.Close(); err != nil {
				t.Fatal(err)
			}
			value, err := invoke.Single(ctx, call.Outputs())
			if err != nil {
				t.Fatal(err)
			}
			if value != nil {
				t.Fatalf("prepare output = %#v, want null", value)
			}
		})
	}
}
