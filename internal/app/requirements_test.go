package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestDelegateCapabilities_DetectedByConformance(t *testing.T) {
	// Every requirement interface conforms to itself.
	for _, cap := range DelegateCapabilities {
		req, err := RequirementInterface(cap)
		if err != nil {
			t.Fatalf("RequirementInterface(%s): %v", cap, err)
		}
		if !satisfiesInterface(req, req) {
			t.Errorf("requirement %s should satisfy itself", cap)
		}
	}

	// The binding-invoker requirement, treated as a candidate delegate OBI,
	// provides ONLY the invoke capability: it has no synthesizeInterface or
	// inspectSource, and its listFormats is keyed to binding-invoker (no alias
	// to the other interfaces), so it does not satisfy create/inspect. This is
	// the invoke-only delegate case.
	bi, _ := RequirementInterface(CapInvoke)
	caps := delegateCapabilities(bi)
	if len(caps) != 1 || caps[0] != CapInvoke {
		t.Errorf("binding-invoker should provide only [invoke], got %v", caps)
	}

	// An interface with no operations provides nothing delegatable.
	if caps := delegateCapabilities(&openbindings.Interface{}); len(caps) != 0 {
		t.Errorf("empty interface should provide no capabilities, got %v", caps)
	}
}

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
	if _, ok := invoke.Operations["openbindings.binding-invoker.prepareBinding"]; ok {
		t.Fatal("invoke capability must not require the independent preflight operation")
	}
}
