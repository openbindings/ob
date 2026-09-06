package app

import (
	"fmt"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
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
	if runtime.SDK.SupportsBindingSpec("openbindings.usage@1") {
		t.Fatal("cohesive runtime incorrectly claimed a split usage provider")
	}
	usageVerdicts := runtime.Synthesizer.CheckBindingSpecs([]string{"openbindings.usage@1"})
	if len(usageVerdicts) != 1 || !usageVerdicts[0].Supported {
		t.Fatalf("combined synthesizer lost usage provider: %#v", usageVerdicts)
	}
}

func TestPreparedProviderCacheUsesRevisionAndIsBounded(t *testing.T) {
	runtime := newDefaultRuntime()
	fixture := func(name string) *openbindings.Interface {
		return &openbindings.Interface{
			OpenBindings: "0.2.0",
			Name:         name,
			Operations:   map[string]openbindings.Operation{"ping": {}},
			Sources: map[string]openbindings.Source{
				"api": {
					BindingSpec: openapibinding.BindingSpecOpenAPI31,
					Content:     []byte(`{"openapi":"3.1.2","info":{"title":"t","version":"1"},"paths":{}}`),
				},
			},
			Bindings: map[string]openbindings.BindingEntry{
				"ping": {Operation: "ping", Source: "api", Selector: "#/paths/~1ping/get"},
			},
		}
	}

	first, err := runtime.prepareProvider(fixture("same"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.prepareProvider(fixture("same"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("identical interface revision did not reuse its prepared provider")
	}

	for index := 0; index <= maxPreparedProviders; index++ {
		if _, err := runtime.prepareProvider(fixture(fmt.Sprintf("revision-%d", index))); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(runtime.preparedProviders); got != maxPreparedProviders {
		t.Fatalf("prepared provider cache size = %d, want %d", got, maxPreparedProviders)
	}
}
