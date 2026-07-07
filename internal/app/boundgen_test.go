package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openbindings/openbindings-go/formats/usage"
)

// TestBoundCLIConformsToContract is the drift guard: the committed bound CLI
// OBI (internal/app/ob.obi.json) must conform to the unbound contract
// (../../ob.obi.json). If it fails, regenerate with `go generate ./internal/app`.
func TestBoundCLIConformsToContract(t *testing.T) {
	report := ComparisonCheck(ComparisonInput{Left: "../../ob.obi.json", Right: "ob.obi.json"})
	if report.Error != nil {
		t.Fatalf("compat error: %s", report.Error.Message)
	}
	if report.Summary.Verdict != "compatible" {
		t.Fatalf("internal/app/ob.obi.json no longer conforms to the contract (verdict %s) — run `go generate ./internal/app`",
			report.Summary.Verdict)
	}
}

// TestBoundServeConformsToContract is the serve drift guard. serve.obi.json is
// a SUBSET realization (it exposes only the served operations), so the check
// runs with the serve OBI as the target: every operation serve exposes must be
// matched and compatible in the contract. That passes for a faithful subset and
// fails on any drift — a divergent schema, or an operation serve exposes that
// the contract doesn't define. If it fails, run `go generate ./internal/app`.
func TestBoundServeConformsToContract(t *testing.T) {
	report := ComparisonCheck(ComparisonInput{Left: "../server/serve.obi.json", Right: "../../ob.obi.json"})
	if report.Error != nil {
		t.Fatalf("compat error: %s", report.Error.Message)
	}
	if report.Summary.Verdict != "compatible" {
		t.Fatalf("internal/server/serve.obi.json no longer conforms to the contract "+
			"(verdict %s, %d/%d ops paired) — run `go generate ./internal/app`",
			report.Summary.Verdict, report.Summary.Coverage.Paired, report.Summary.Coverage.TotalOperations)
	}
}

func TestGenerateBoundServe_BindsServedSurface(t *testing.T) {
	serve, err := GenerateBoundServe(
		"../../ob.obi.json", "../server/openapi.yaml", "../server/serve.obi.json",
		"http://127.0.0.1:20290",
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
	// The served OBI points at this server's own live spec endpoints via absolute
	// URLs (not embedded content): the discovery doc is always fetched from a
	// running server, which rewrites the host:port per request (handleOBI).
	for _, src := range []string{"openapi", "asyncapi"} {
		s, ok := serve.Sources[src]
		if !ok {
			t.Errorf("expected preserved source %q", src)
			continue
		}
		if s.Content != nil || s.Location == "" {
			t.Errorf("source %q: expected an absolute location and no content, got content=%v location=%q", src, s.Content != nil, s.Location)
		}
		if !strings.HasPrefix(s.Location, "http://") && !strings.HasPrefix(s.Location, "https://") {
			t.Errorf("source %q: expected an absolute http(s) URL, got %q", src, s.Location)
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
	bound, err := GenerateBoundCLI("../../ob.obi.json", "../cmd/usage.kdl")
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
	if b.Operation != "openbindings.ob.describe" || b.Source != "usage" || b.Ref != "describe" {
		t.Errorf("unexpected binding: %+v", b)
	}
	// The usage source is the PRISTINE artifact embedded as content (not a
	// relative location) so the emitted OBI is self-contained and
	// spec-valid (OBI-D-05).
	src, ok := bound.Sources["usage"]
	if !ok {
		t.Fatal("expected a usage source entry")
	}
	if src.Content == nil || src.Location != "" {
		t.Errorf("expected embedded usage content and no location, got content=%v location=%q", src.Content != nil, src.Location)
	}
	if src.Format != "usage@"+usage.MaxTestedVersion {
		t.Errorf("source format = %q, want the bare usage token", src.Format)
	}
	if text, _ := src.Content.(string); text == "" || !strings.Contains(text, `bin "ob"`) {
		t.Error("expected the pristine kdl text as embedded content")
	}
	// The elections the document no longer carries live in ob's own
	// site-guarded hook table (consumer configuration): JSON machine lane
	// everywhere, diff(1)-convention exits, filter routing.
	table := BoundCLIHookTable(bound)
	if oks := table.OKExits["openbindings.ob.validateInterface"]; len(oks) != 2 {
		t.Errorf("validateInterface should elect exit ok [0,1], got %v", oks)
	}
	foundDescribe := false
	for _, op := range table.DecodeJSON {
		if op == "openbindings.ob.describe" {
			foundDescribe = true
		}
	}
	if !foundDescribe {
		t.Error("describe should elect the JSON machine lane")
	}
	if r := table.Routes["openbindings.ob.validateInterface"]; r["locator"] != usage.RouteStdinDash {
		t.Errorf("validateInterface should route locator via stdin-dash, got %v", r)
	}
}

// TestGenerateBoundCLI_AttachesWireInputTransforms: machine-natured commands
// (WireInputByShort — the relocated wireInput props) carry a machine-lane
// inputTransform that JSON-serializes the operation's wire input into the
// named flag, so generic operation-invocation of the bound CLI OBI produces
// argv the CLI parses.
func TestGenerateBoundCLI_AttachesWireInputTransforms(t *testing.T) {
	bound, err := GenerateBoundCLI("../../ob.obi.json", "../cmd/usage.kdl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wire := map[string]any{
		"source": map[string]any{"format": "openapi@3.1", "location": "api.yaml"},
		"ref":    "#/x",
	}
	for _, short := range []string{"invokeBinding", "prepareBinding", "synthesizeInterface", "inspectSource"} {
		key := "openbindings.ob." + short + ".usage"
		b, ok := bound.Bindings[key]
		if !ok {
			t.Errorf("%s: binding missing", key)
			continue
		}
		if b.InputTransform == nil {
			t.Errorf("%s: expected a machine-lane inputTransform", key)
			continue
		}
		out, terr := ApplyTransform(bound.Transforms, b.InputTransform, wire)
		if terr != nil {
			t.Errorf("%s: transform failed: %v", key, terr)
			continue
		}
		m, ok := out.(map[string]any)
		if !ok {
			t.Errorf("%s: transform produced %T, want an object", key, out)
			continue
		}
		s, ok := m["input"].(string)
		if !ok || len(m) != 1 {
			t.Errorf("%s: expected exactly {input: <json string>}, got %#v", key, m)
			continue
		}
		var roundTrip map[string]any
		if uerr := json.Unmarshal([]byte(s), &roundTrip); uerr != nil {
			t.Errorf("%s: --input payload is not JSON: %v", key, uerr)
			continue
		}
		if roundTrip["ref"] != "#/x" {
			t.Errorf("%s: payload did not round-trip: %#v", key, roundTrip)
		}
	}
	// Ops without wireInput get the -F json forcing transform (exec-lane
	// output conformance): nil input becomes {format: json}, object input is
	// merged with it.
	b := bound.Bindings["openbindings.ob.describe.usage"]
	if b.InputTransform == nil {
		t.Fatal("describe: expected the -F json forcing inputTransform")
	}
	out, terr := ApplyTransform(bound.Transforms, b.InputTransform, nil)
	if terr != nil {
		t.Fatalf("describe: transform on nil input failed: %v", terr)
	}
	if m, ok := out.(map[string]any); !ok || m["format"] != "json" || len(m) != 1 {
		t.Errorf("describe: nil input should become {format: json}, got %#v", out)
	}
	b = bound.Bindings["openbindings.ob.resolveDelegate.usage"]
	if b.InputTransform == nil {
		t.Fatal("resolveDelegate: expected the -F json forcing inputTransform")
	}
	out, terr = ApplyTransform(bound.Transforms, b.InputTransform, map[string]any{"operation": "x"})
	if terr != nil {
		t.Fatalf("resolveDelegate: transform failed: %v", terr)
	}
	if m, ok := out.(map[string]any); !ok || m["format"] != "json" || m["operation"] != "x" {
		t.Errorf("resolveDelegate: expected merged {operation, format}, got %#v", out)
	}
	// Ops whose wire field names the CLI spells differently (or that a root
	// flag shadows) carry an adaptation transform: the BINDING adapts the
	// wire shape to the CLI's natural surface, leaving the CLI untouched.
	b = bound.Bindings["openbindings.ob.resolveDelegateForFormat.usage"]
	if b.InputTransform == nil {
		t.Fatal("resolveDelegateForFormat: expected an adaptation transform")
	}
	out, terr = ApplyTransform(bound.Transforms, b.InputTransform, map[string]any{"format": "usage@2.0.0"})
	if terr != nil {
		t.Fatalf("resolveDelegateForFormat: transform failed: %v", terr)
	}
	if m, ok := out.(map[string]any); !ok || m["format-token"] != "usage@2.0.0" || m["format"] != "json" {
		t.Errorf("resolveDelegateForFormat: expected {format-token, format: json}, got %#v", out)
	}
	// setDelegatePreference: format scopes to --source-format; other fields
	// pass through untouched.
	b = bound.Bindings["openbindings.ob.setDelegatePreference.usage"]
	if b.InputTransform == nil {
		t.Fatal("setDelegatePreference: expected an adaptation transform")
	}
	out, terr = ApplyTransform(bound.Transforms, b.InputTransform, map[string]any{
		"location": "exec:x", "preference": 5, "operation": "op.key", "format": "grpc",
	})
	if terr != nil {
		t.Fatalf("setDelegatePreference: transform failed: %v", terr)
	}
	if m, ok := out.(map[string]any); !ok || m["source-format"] != "grpc" || m["location"] != "exec:x" ||
		m["operation"] != "op.key" || m["format"] != "json" {
		t.Errorf("setDelegatePreference: unexpected adaptation output %#v", out)
	}
	// getContext: the wire key rides the CLI's natural <url> argument.
	b = bound.Bindings["openbindings.ob.getContext.usage"]
	out, terr = ApplyTransform(bound.Transforms, b.InputTransform, map[string]any{"key": "https://x"})
	if terr != nil {
		t.Fatalf("getContext: transform failed: %v", terr)
	}
	if m, ok := out.(map[string]any); !ok || m["url"] != "https://x" {
		t.Errorf("getContext: expected {url, format}, got %#v", out)
	}
}
