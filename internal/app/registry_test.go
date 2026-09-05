package app

import (
	"testing"

	openapibinding "github.com/openbindings/openbindings-go/formats/openapi"
)

func TestOperationGraphIsInvocationOnly(t *testing.T) {
	const operationGraph = "openbindings.operation-graph@1"
	runtime := newDefaultRuntime()

	invocationSpecs := make(map[string]bool)
	for _, info := range runtime.SDK.OperationInvoker().BindingSpecs() {
		invocationSpecs[info.BindingSpec] = true
	}
	if !invocationSpecs[operationGraph] {
		t.Fatalf("default invoker does not advertise %q", operationGraph)
	}

	synthesisSpecs := make(map[string]bool)
	for _, info := range runtime.Synthesizer.BindingSpecs() {
		synthesisSpecs[info.BindingSpec] = true
	}
	if synthesisSpecs[operationGraph] {
		t.Fatalf("default synthesizer advertises invocation-only %q", operationGraph)
	}
}

func TestDefaultRuntimeRegistersEachOpenAPISiblingExactlyOnce(t *testing.T) {
	runtime := newDefaultRuntime()
	want := map[string]bool{
		openapibinding.BindingSpecOpenAPI20: false,
		openapibinding.BindingSpecOpenAPI30: false,
		openapibinding.BindingSpecOpenAPI31: false,
		openapibinding.BindingSpecOpenAPI32: false,
	}
	for _, info := range runtime.SDK.OperationInvoker().BindingSpecs() {
		seen, openAPI := want[info.BindingSpec]
		if !openAPI {
			continue
		}
		if seen {
			t.Fatalf("OpenAPI binding specification %q registered more than once", info.BindingSpec)
		}
		want[info.BindingSpec] = true
	}
	for bindingSpec, seen := range want {
		if !seen {
			t.Errorf("OpenAPI binding specification %q is not registered", bindingSpec)
		}
	}
}

func TestDefaultRuntimeOwnsOneOpenAPIAdapter(t *testing.T) {
	runtime := newDefaultRuntime()
	if runtime.OpenAPI == nil {
		t.Fatal("OpenAPI adapter is nil")
	}
	verdicts := runtime.SDK.CheckBindingSpecs([]string{openapibinding.BindingSpecOpenAPI31})
	if len(verdicts) != 1 || !verdicts[0].Supported {
		t.Fatal("SDK runtime does not own OpenAPI 3.1 synthesis")
	}
}
