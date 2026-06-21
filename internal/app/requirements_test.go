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
