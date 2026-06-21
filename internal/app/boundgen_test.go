package app

import "testing"

// TestBoundCLIConformsToContract is the drift guard: the committed bound CLI
// OBI (internal/app/ob.obi.json) must conform to the unbound contract
// (../../ob.obi.json). If it fails, regenerate with `go generate ./internal/app`.
func TestBoundCLIConformsToContract(t *testing.T) {
	report := CompatibilityCheck(CompatInput{Target: "../../ob.obi.json", Candidate: "ob.obi.json"})
	if report.Error != nil {
		t.Fatalf("compat error: %s", report.Error.Message)
	}
	if !report.Compatible {
		t.Fatalf("internal/app/ob.obi.json no longer conforms to the contract — run `go generate ./internal/app`")
	}
}

func TestGenerateBoundCLI_BindsOpsByShortName(t *testing.T) {
	bound, err := GenerateBoundCLI("../../ob.obi.json", "../cmd/usage.kdl", "usage@2.13.1", "../cmd/usage.kdl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The contract's keys (and aliases) are preserved.
	if _, ok := bound.Operations["openbindings.ob.describe"]; !ok {
		t.Error("expected contract-keyed operation openbindings.ob.describe")
	}
	// A usage binding is attached by short-name, keyed <op>.usage, ref = command path.
	b, ok := bound.Bindings["openbindings.ob.describe.usage"]
	if !ok {
		t.Fatal("expected a usage binding for describe")
	}
	if b.Operation != "openbindings.ob.describe" || b.Source != "usage" || b.Ref == "" {
		t.Errorf("unexpected binding: %+v", b)
	}
	if _, ok := bound.Sources["usage"]; !ok {
		t.Error("expected a usage source entry")
	}
}
