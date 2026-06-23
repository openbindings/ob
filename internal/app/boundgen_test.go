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

// TestBoundServeConformsToContract is the serve drift guard. serve.obi.json is
// a SUBSET realization (it exposes only the served operations), so the check
// runs with the serve OBI as the target: every operation serve exposes must be
// matched and compatible in the contract. That passes for a faithful subset and
// fails on any drift — a divergent schema, or an operation serve exposes that
// the contract doesn't define. If it fails, run `go generate ./internal/app`.
func TestBoundServeConformsToContract(t *testing.T) {
	report := CompatibilityCheck(CompatInput{Target: "../server/serve.obi.json", Candidate: "../../ob.obi.json"})
	if report.Error != nil {
		t.Fatalf("compat error: %s", report.Error.Message)
	}
	if !report.Compatible {
		t.Fatalf("internal/server/serve.obi.json no longer conforms to the contract "+
			"(%d/%d ops compatible) — run `go generate ./internal/app`",
			report.Coverage.Compatible, report.Coverage.Total)
	}
}

func TestGenerateBoundServe_BindsServedSurface(t *testing.T) {
	serve, err := GenerateBoundServe(
		"../../ob.obi.json", "../server/openapi.yaml", "../server/serve.obi.json",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Operations carry the full contract key (not the bare short-name the old
	// hand-maintained file used).
	if _, ok := serve.Operations["openbindings.ob.describe"]; !ok {
		t.Error("expected contract-keyed operation openbindings.ob.describe")
	}
	if _, ok := serve.Operations["describe"]; ok {
		t.Error("did not expect a bare short-name operation key")
	}
	// HTTP ref derives from openapi.yaml; key is <op>.openapi.
	if b, ok := serve.Bindings["openbindings.ob.describe.openapi"]; !ok || b.Source != "openapi" || b.Ref == "" {
		t.Errorf("expected an openapi binding for describe, got %+v (present=%v)", b, ok)
	}
	// invokeBinding is bound over WS (asyncapi).
	if b, ok := serve.Bindings["openbindings.ob.invokeBinding.asyncapi"]; !ok || b.Ref != "#/operations/invokeBinding" {
		t.Errorf("expected asyncapi invoke binding, got %+v (present=%v)", b, ok)
	}
	// MCP is bridged at runtime (`ob mcp <url>`), not a served transport: the
	// OBI carries no mcp source or bindings.
	if _, ok := serve.Sources["mcp"]; ok {
		t.Error("did not expect an mcp source (MCP is bridged, not served)")
	}
	for bk, be := range serve.Bindings {
		if be.Source == "mcp" {
			t.Errorf("did not expect an mcp binding, got %q", bk)
		}
	}
	// Hand-tuned transforms survive the short-name → contract-key rekey.
	if b := serve.Bindings["openbindings.ob.getContext.openapi"]; b.InputTransform == nil {
		t.Error("expected getContext.openapi inputTransform to be preserved from the existing serve OBI")
	}
	// The real wire transports are preserved and embedded as content (not a
	// relative location) so the served discovery OBI is spec-valid (OBI-D-05).
	for _, src := range []string{"openapi", "asyncapi"} {
		s, ok := serve.Sources[src]
		if !ok {
			t.Errorf("expected preserved source %q", src)
			continue
		}
		if s.Content == nil || s.Location != "" {
			t.Errorf("source %q: expected embedded content and no location, got content=%v location=%q", src, s.Content != nil, s.Location)
		}
	}
}

// TestBoundOBIsAreSpecValid guards spec validity of the committed bound OBIs.
// The conformance guards above (assessCompatibility) only check operation/schema
// parity with the contract; they do not catch document-level rule violations such
// as a non-absolute source location (OBI-D-05). Since `ob --openbindings` emits
// the bound CLI OBI and the server serves the bound serve OBI as its discovery
// document, both must validate. If this fails, run `go generate ./internal/app`.
func TestBoundOBIsAreSpecValid(t *testing.T) {
	for _, path := range []string{"ob.obi.json", "../server/serve.obi.json"} {
		report := ValidateInterface(ValidateInput{Locator: path, Strict: true})
		if report.Error != nil {
			t.Fatalf("%s: validate error: %s", path, report.Error.Message)
		}
		if !report.Valid {
			t.Fatalf("%s: not spec-valid: %v", path, report.Problems)
		}
	}
}

func TestGenerateBoundCLI_BindsOpsByShortName(t *testing.T) {
	bound, err := GenerateBoundCLI("../../ob.obi.json", "../cmd/usage.kdl", "usage@2.13.1")
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
	// The usage source is embedded as content (not a relative location) so the
	// emitted OBI is self-contained and spec-valid (OBI-D-05).
	src, ok := bound.Sources["usage"]
	if !ok {
		t.Fatal("expected a usage source entry")
	}
	if src.Content == nil || src.Location != "" {
		t.Errorf("expected embedded usage content and no location, got content=%v location=%q", src.Content != nil, src.Location)
	}
}
