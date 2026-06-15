package app

import (
	"encoding/json"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// mustIface parses an OBI document literal into an Interface for comparison.
func mustIface(t *testing.T, doc string) *openbindings.Interface {
	t.Helper()
	var iface openbindings.Interface
	if err := json.Unmarshal([]byte(doc), &iface); err != nil {
		t.Fatalf("parse interface: %v", err)
	}
	return &iface
}

func compareDocs(t *testing.T, left, right string) ComparisonReport {
	t.Helper()
	return CompareInterfaces(CompareInterfacesInput{
		Left:  resolvedComparisonInput{locator: "left", iface: mustIface(t, left)},
		Right: resolvedComparisonInput{locator: "right", iface: mustIface(t, right)},
	})
}

func pairedDelta(t *testing.T, report ComparisonReport) OperationDelta {
	t.Helper()
	for _, d := range report.Operations {
		if d.Status == "paired" {
			return d
		}
	}
	t.Fatalf("no paired operation in report")
	return OperationDelta{}
}

func hasFindingKind(findings []Finding, kind string) bool {
	for _, f := range findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

// A $ref slot compared against its inline equivalent must not fabricate
// differences: this is the false-breaking case (required.removed reported
// against an unresolved "{$ref}" wrapper that carries no required array).
func TestComparisonRef_ResolvedRefMatchesInline(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "$ref": "#/schemas/In" } } },
	  "schemas": { "In": { "type": "object", "properties": { "url": { "type": "string" } }, "required": ["url"] } }
	}`
	impl := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "type": "object", "properties": { "url": { "type": "string" } }, "required": ["url"] } } }
	}`
	report := compareDocs(t, contract, impl)
	if report.Summary.Verdict != "compatible" && report.Summary.Verdict != "identical" {
		t.Fatalf("verdict = %q, want compatible/identical", report.Summary.Verdict)
	}
	d := pairedDelta(t, report)
	for _, f := range d.Findings {
		if strings.HasPrefix(f.Kind, "required.") {
			t.Errorf("spurious finding behind $ref: %s at %s", f.Kind, f.Location.Pointer)
		}
	}
}

// A real difference behind a $ref must be caught: this is the false-compatible
// case (a tightened input required slips past when both sides are bound by
// reference and the wrappers compare as empty schemas).
func TestComparisonRef_RealDifferenceBehindRefIsCaught(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "$ref": "#/schemas/In" } } },
	  "schemas": { "In": { "type": "object", "required": ["name"] } }
	}`
	impl := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "$ref": "#/schemas/In" } } },
	  "schemas": { "In": { "type": "object", "required": ["name", "age"] } }
	}`
	report := compareDocs(t, contract, impl)
	if report.Summary.Verdict != "incompatible" {
		t.Fatalf("verdict = %q, want incompatible", report.Summary.Verdict)
	}
	if !hasFindingKind(pairedDelta(t, report).Findings, "required.added") {
		t.Errorf("expected required.added behind $ref, got none")
	}
}

// A local $ref that does not resolve is a structural failure, reported as
// profile.ref.resolution_failed and leaving the slot indeterminate.
func TestComparisonRef_UnresolvedLocalRef(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "type": "object", "required": ["x"] } } }
	}`
	impl := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "$ref": "#/schemas/Missing" } } }
	}`
	report := compareDocs(t, contract, impl)
	if !hasFindingKind(pairedDelta(t, report).Findings, "profile.ref.resolution_failed") {
		t.Errorf("expected profile.ref.resolution_failed, got none")
	}
	if report.Summary.Verdict != "indeterminate" {
		t.Errorf("verdict = %q, want indeterminate", report.Summary.Verdict)
	}
}

// An external $ref cannot be followed; it is unverified, not silently
// compatible and not a false break.
func TestComparisonRef_ExternalRefIsUnverified(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "type": "object" } } }
	}`
	impl := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": { "$ref": "https://example.com/schema.json" } } }
	}`
	report := compareDocs(t, contract, impl)
	if !hasFindingKind(pairedDelta(t, report).Findings, "unverified.external_ref") {
		t.Errorf("expected unverified.external_ref, got none")
	}
	if report.Summary.Verdict != "unverified" {
		t.Errorf("verdict = %q, want unverified", report.Summary.Verdict)
	}
}

// A recursive type ($ref back to an ancestor schema) must terminate rather than
// loop, and compare as compatible against itself.
func TestComparisonRef_RecursiveTypeTerminates(t *testing.T) {
	doc := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "output": { "$ref": "#/schemas/Node" } } },
	  "schemas": { "Node": { "type": "object", "properties": { "next": { "$ref": "#/schemas/Node" } } } }
	}`
	report := compareDocs(t, doc, doc)
	if v := report.Summary.Verdict; v != "compatible" && v != "identical" {
		t.Fatalf("verdict = %q, want compatible/identical", v)
	}
}

// An unverifiable finding must not mask a real incompatibility in the same
// slot: the verdict is ranked by severity, not by finding order.
func TestComparisonRef_BreakingNotMaskedByUnverified(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": {
	    "type": "object",
	    "properties": { "note": { "type": "string" } },
	    "required": ["name"]
	  } } }
	}`
	// impl adds a required field (breaking on input) AND carries an external
	// ref on a property (unverified). The verdict must stay incompatible.
	impl := `{
	  "openbindings": "0.2.0",
	  "operations": { "op": { "input": {
	    "type": "object",
	    "properties": { "note": { "$ref": "https://example.com/note.json" } },
	    "required": ["name", "age"]
	  } } }
	}`
	report := compareDocs(t, contract, impl)
	findings := pairedDelta(t, report).Findings
	if !hasFindingKind(findings, "required.added") || !hasFindingKind(findings, "unverified.external_ref") {
		t.Fatalf("expected both required.added and unverified.external_ref, got %+v", findings)
	}
	if report.Summary.Verdict != "incompatible" {
		t.Errorf("verdict = %q, want incompatible (unverified must not mask the break)", report.Summary.Verdict)
	}
}
