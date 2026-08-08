package app

import "testing"

func TestOperationGraphIsInvocationOnly(t *testing.T) {
	const operationGraph = "openbindings.operation-graph@1"

	invocationSpecs := make(map[string]bool)
	for _, info := range newDefaultInvoker().BindingSpecs() {
		invocationSpecs[info.BindingSpec] = true
	}
	if !invocationSpecs[operationGraph] {
		t.Fatalf("default invoker does not advertise %q", operationGraph)
	}

	synthesisSpecs := make(map[string]bool)
	for _, info := range newDefaultSynthesizer().BindingSpecs() {
		synthesisSpecs[info.BindingSpec] = true
	}
	if synthesisSpecs[operationGraph] {
		t.Fatalf("default synthesizer advertises invocation-only %q", operationGraph)
	}
}
