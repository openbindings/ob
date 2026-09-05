package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	openapiformat "github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/synthesize"
)

type singleRetryInvoker struct {
	prepareCalls int
	invokeCalls  int
	details      *invoke.ContextRequiredDetails
}

func (i *singleRetryInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: openapiformat.BindingSpecOpenAPI31}}
}

func (i *singleRetryInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, i.BindingSpecs())
}

func (i *singleRetryInvoker) PrepareBinding(_ context.Context, args *invoke.BindingInvocationArgs) (*invoke.ContextRequiredDetails, error) {
	i.prepareCalls++
	if invoke.ContextSatisfies(args.Context, i.details) {
		return nil, nil
	}
	return i.details, nil
}

func (i *singleRetryInvoker) InvokeBinding(ctx context.Context, args *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	i.invokeCalls++
	call := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		if !invoke.ContextSatisfies(args.Context, i.details) {
			call.FireError(invoke.NewContextRequiredError(i.details))
			return
		}
		_ = call.EmitOutput(map[string]any{"ok": true})
		call.CloseOutput()
	}()
	return call
}

func TestOpenAPIOperationHasOneContextRetryOwner(t *testing.T) {
	durable := true
	details := &invoke.ContextRequiredDetails{
		Target: "https://api.example",
		Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{
			Type: "auth.bearer", Durable: &durable,
		}}}},
	}
	bindingInvoker := &singleRetryInvoker{details: details}
	operationInvoker := invoke.NewOperationInvoker(bindingInvoker)
	resolverCalls := 0
	operationInvoker.ContextResolver = func(context.Context, *invoke.ContextRequiredDetails) (map[string]any, error) {
		resolverCalls++
		return map[string]any{"bearerToken": "secret"}, nil
	}
	cleanup := OverrideInvokerForTest(operationInvoker)
	defer cleanup()

	iface := &openbindings.Interface{
		OpenBindings: "0.2.0",
		Name:         "Single retry",
		Operations: map[string]openbindings.Operation{
			"read": {},
		},
		Sources: map[string]openbindings.Source{
			"api": {BindingSpec: openapiformat.BindingSpecOpenAPI31, Content: openbindings.TextContent("{}")},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"read.api": {Operation: "read", Source: "api", Selector: "read"},
		},
	}

	run, err := invokeOnInterface(context.Background(), iface, "read", "", nil, nil)
	if err != nil {
		t.Fatalf("invoke operation: %v", err)
	}
	event, ok := <-run.Events
	if !ok || event.Error != nil {
		t.Fatalf("operation failed: %#v", event)
	}
	if resolverCalls != 1 {
		t.Fatalf("context resolver calls = %d, want exactly 1", resolverCalls)
	}
	if bindingInvoker.invokeCalls != 1 {
		t.Fatalf("binding invocation calls = %d, want exactly 1 after preflight", bindingInvoker.invokeCalls)
	}
}

func TestOpenAPIFamilyUsesOneRuntimeVertically(t *testing.T) {
	ResetDefaultInvoker()
	t.Cleanup(ResetDefaultInvoker)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	oas2 := fmt.Sprintf(`{
		"swagger":"2.0",
		"info":{"title":"Vertical","version":"1"},
		"schemes":["http"],
		"host":%q,
		"paths":{"/ping":{"get":{"operationId":"ping","produces":["application/json"],"responses":{"200":{"description":"ok","schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}}}}}}
	}`, parsed.Host)
	oas3 := func(version string) string {
		return fmt.Sprintf(`{
			"openapi":%q,
			"info":{"title":"Vertical","version":"1"},
			"servers":[{"url":%q}],
			"paths":{"/ping":{"get":{"operationId":"ping","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}}}}}}}}
		}`, version, server.URL)
	}

	cases := []struct {
		name        string
		bindingSpec string
		document    string
	}{
		{"OpenAPI 2.0", openapiformat.BindingSpecOpenAPI20, oas2},
		{"OpenAPI 3.0", openapiformat.BindingSpecOpenAPI30, oas3("3.0.4")},
		{"OpenAPI 3.1", openapiformat.BindingSpecOpenAPI31, oas3("3.1.2")},
		{"OpenAPI 3.2", openapiformat.BindingSpecOpenAPI32, oas3("3.2.0")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := openbindings.Source{
				BindingSpec: test.bindingSpec,
				Content:     json.RawMessage(test.document),
			}
			inspection, err := InspectSource(context.Background(), &source)
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}
			if !inspection.Exhaustive || len(inspection.Targets) != 1 {
				t.Fatalf("inspection = %#v, want one exhaustive target", inspection)
			}

			iface, err := SynthesizeInterfaceFromSource(context.Background(), &synthesize.SynthesizeInput{
				Sources: []synthesize.SynthesizeSource{{
					BindingSpec: test.bindingSpec,
					Name:        "api",
					Content:     json.RawMessage(test.document),
				}},
			})
			if err != nil {
				t.Fatalf("synthesize: %v", err)
			}
			preflight, err := PrepareInterfaceOperation(context.Background(), iface, "ping", "", nil)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if preflight != nil {
				t.Fatalf("prepare requirements = %#v, want none", preflight)
			}

			before := requests.Load()
			run, err := invokeOnInterface(context.Background(), iface, "ping", "", nil, nil)
			if err != nil {
				t.Fatalf("invoke wiring: %v", err)
			}
			event, ok := <-run.Events
			if !ok || event.Error != nil {
				t.Fatalf("invoke event = %#v", event)
			}
			output, ok := event.Output.(map[string]any)
			if !ok || output["ok"] != true {
				t.Fatalf("invoke output = %#v, want {ok:true}", event.Output)
			}
			if got := requests.Load(); got != before+1 {
				t.Fatalf("requests = %d, want exactly one more than %d", got, before)
			}
		})
	}
}
