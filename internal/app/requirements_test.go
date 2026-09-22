package app

import (
	"testing"
)

func TestCapabilityRequirementsContainOnlyConsumedOperations(t *testing.T) {
	for cap, keys := range capabilityRequirementOperations {
		req, err := RequirementInterface(cap)
		if err != nil {
			t.Fatalf("RequirementInterface(%s): %v", cap, err)
		}
		if len(req.Operations) != len(keys) {
			t.Fatalf("%s requirement has %d operations, want %d", cap, len(req.Operations), len(keys))
		}
		for _, key := range keys {
			if _, ok := req.Operations[key]; !ok {
				t.Errorf("%s requirement is missing %s", cap, key)
			}
		}
	}

	synth, err := RequirementInterface(CapSynthesize)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := synth.Operations["openbindings.interface-synthesizer.synthesizeInterfaceWithCoverage"]; ok {
		t.Fatal("strict synthesis capability must not require the independent coverage operation")
	}

	invoke, err := RequirementInterface(CapInvoke)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := invoke.Operations["openbindings.binding-invoker.preflightBinding"]; ok {
		t.Fatal("invoke capability must not require the independent preflight operation")
	}
}
