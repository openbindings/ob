package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	openapibinding "github.com/openbindings/openbindings-go/formats/openapi"
)

func TestOpenAPICandidateCollisionSurvivesOBTransformRuntime(t *testing.T) {
	type observedRequest struct {
		pathID  string
		queryID string
		bodyID  string
	}
	observed := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- observedRequest{
			pathID:  strings.TrimPrefix(r.URL.Path, "/items/"),
			queryID: r.URL.Query().Get("id"),
			bodyID:  fmt.Sprint(body["id"]),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	artifact := fmt.Sprintf(`{
		"openapi":"3.1.0",
		"info":{"title":"Collision","version":"1"},
		"servers":[{"url":%q}],
		"paths":{"/items/{id}":{"post":{
			"operationId":"updateItem",
			"parameters":[
				{"in":"path","name":"id","required":true,"schema":{"type":"string"}},
				{"in":"query","name":"id","schema":{"type":"string"}}
			],
			"requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}}},
			"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}}}}
		}}}
	}`, server.URL)

	synthesis, err := openapibinding.NewSynthesizer().SynthesizeInterfaceWithCoverage(
		context.Background(),
		&openbindings.SynthesizeInput{Sources: []openbindings.SynthesizeSource{{
			BindingSpec: openapibinding.BindingSpec,
			Content:     openbindings.TextContent(artifact),
		}}},
	)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}

	invoker := openbindings.NewOperationInvoker(openapibinding.NewInvoker())
	invoker.TransformEvaluator = &jsonataEvaluator{}
	call := openbindings.Invoke(
		context.Background(),
		invoker,
		synthesis.Interface,
		openbindings.NewOperationSignature[any, any]("updateItem"),
	)
	if err := call.Write(context.Background(), map[string]any{
		"id": "path-value", "id_2": "query-value", "id_3": "body-value",
	}); err != nil {
		t.Fatalf("write operation input: %v", err)
	}
	_ = call.Close()
	output, err := openbindings.Single(context.Background(), call.Outputs())
	if err != nil {
		t.Fatalf("invoke through ob transform runtime: %v", err)
	}
	if got := output.(map[string]any)["ok"]; got != true {
		t.Fatalf("output ok = %#v, want true", got)
	}
	got := <-observed
	if got != (observedRequest{pathID: "path-value", queryID: "query-value", bodyID: "body-value"}) {
		t.Fatalf("wire/application semantics changed: %#v", got)
	}
}
