package app

import "testing"

// Fix B4-1(a): compareSchemaAt did not recurse into "items", so a breaking
// change inside a $ref'd array-item schema went undetected by the structural
// walk. Because the fast-path identity check (b) was ALSO $ref-blind at the
// time, nothing else caught it either: the pair reported "compatible" with
// zero findings despite a string->number break on a contract-required field.
// This is the exact scratchpad repro (target_nested.json vs
// candidate_nested_break.json) reduced to a Go test.
func TestComparisonItems_NestedArrayBreakCaught(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "schemas": {
	    "Delegate": {
	      "type": "object",
	      "properties": { "location": { "type": "string" } },
	      "required": ["location"]
	    }
	  },
	  "operations": { "op": { "output": {
	    "type": "object",
	    "properties": { "delegates": { "type": "array", "items": { "$ref": "#/schemas/Delegate" } } },
	    "required": ["delegates"]
	  } } }
	}`
	candidate := `{
	  "openbindings": "0.2.0",
	  "schemas": {
	    "Delegate": {
	      "type": "object",
	      "properties": { "location": { "type": "number" } },
	      "required": ["location"]
	    }
	  },
	  "operations": { "op": { "output": {
	    "type": "object",
	    "properties": { "delegates": { "type": "array", "items": { "$ref": "#/schemas/Delegate" } } },
	    "required": ["delegates"]
	  } } }
	}`

	report := compareDocs(t, contract, candidate)
	delta := pairedDelta(t, report)
	if !hasFindingKind(delta.Findings, "type.changed") {
		t.Fatalf("expected a type.changed finding inside the array item, got %+v", delta.Findings)
	}
	if report.Summary.Verdict != "incompatible" {
		t.Fatalf("verdict = %q, want incompatible (an array-item break must not pass as identical)", report.Summary.Verdict)
	}
	if delta.Output == nil || delta.Output.Verdict != "incompatible" {
		t.Fatalf("output verdict = %+v, want incompatible", delta.Output)
	}
}

// A genuinely identical nested array/$ref schema must stay
// compatible/identical: the items-recursion fix must not fabricate
// differences where none exist.
func TestComparisonItems_IdenticalNestedArrayStaysCompatible(t *testing.T) {
	doc := `{
	  "openbindings": "0.2.0",
	  "schemas": {
	    "Delegate": {
	      "type": "object",
	      "properties": { "location": { "type": "string" } },
	      "required": ["location"]
	    }
	  },
	  "operations": { "op": { "output": {
	    "type": "object",
	    "properties": { "delegates": { "type": "array", "items": { "$ref": "#/schemas/Delegate" } } },
	    "required": ["delegates"]
	  } } }
	}`

	report := compareDocs(t, doc, doc)
	if v := report.Summary.Verdict; v != "compatible" && v != "identical" {
		t.Fatalf("verdict = %q, want compatible/identical for a genuinely identical pair", v)
	}
	delta := pairedDelta(t, report)
	for _, f := range delta.Findings {
		if hasCategory(f, "breaking") {
			t.Errorf("spurious breaking finding on an identical nested array schema: %+v", f)
		}
	}
}

// The already-working direct case (a $ref AT the slot itself, not nested
// inside an array) must be unaffected by the fast-path guard change: target_ds.json
// vs candidate_direct_break.json from the scratchpad repro, reduced.
func TestComparisonItems_DirectRefBreakStillCaught(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "schemas": { "Delegate": { "type": "object", "properties": { "location": { "type": "string" } }, "required": ["location"] } },
	  "operations": { "op": { "output": { "$ref": "#/schemas/Delegate" } } }
	}`
	candidate := `{
	  "openbindings": "0.2.0",
	  "schemas": { "Delegate": { "type": "object", "properties": { "location": { "type": "number" } }, "required": ["location"] } },
	  "operations": { "op": { "output": { "$ref": "#/schemas/Delegate" } } }
	}`

	report := compareDocs(t, contract, candidate)
	if report.Summary.Verdict != "incompatible" {
		t.Fatalf("verdict = %q, want incompatible", report.Summary.Verdict)
	}
	if !hasFindingKind(pairedDelta(t, report).Findings, "type.changed") {
		t.Errorf("expected type.changed on the direct $ref break")
	}
}

// additionalProperties as a SCHEMA (not a bare true/false) is a structural
// position compareSchemaAt did not walk either — the boolean check only
// catches a true<->false flip. A $ref'd schema there must be compared just
// like "items"/"properties".
func TestComparisonItems_AdditionalPropertiesSchemaBreakCaught(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "schemas": { "Extra": { "type": "object", "properties": { "note": { "type": "string" } } } },
	  "operations": { "op": { "output": {
	    "type": "object",
	    "additionalProperties": { "$ref": "#/schemas/Extra" }
	  } } }
	}`
	candidate := `{
	  "openbindings": "0.2.0",
	  "schemas": { "Extra": { "type": "object", "properties": { "note": { "type": "number" } } } },
	  "operations": { "op": { "output": {
	    "type": "object",
	    "additionalProperties": { "$ref": "#/schemas/Extra" }
	  } } }
	}`

	report := compareDocs(t, contract, candidate)
	delta := pairedDelta(t, report)
	if !hasFindingKind(delta.Findings, "type.changed") {
		t.Fatalf("expected a type.changed finding inside additionalProperties, got %+v", delta.Findings)
	}
	if report.Summary.Verdict != "incompatible" {
		t.Fatalf("verdict = %q, want incompatible", report.Summary.Verdict)
	}
}

// Fix B4-1(b) isolated: the fast path compared raw JSON with $ref left
// unresolved, so two documents whose $ref-wrapped subtree is byte-identical
// (same pointer string) but whose OWN schema registries bind that pointer to
// divergent content were declared identical. This uses oneOf — a position
// the structural walk deliberately does not descend into (see
// TestComparison_SubsumptionMatchesSDKVerdict) — so only the fast-path fix
// (in both compareSchemaSlot AND subsumptionFindings; either alone is not
// enough) can catch it.
func TestComparisonRef_FastPathCatchesDivergentRefTarget(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "schemas": { "Shape": { "type": "string" } },
	  "operations": { "op": { "output": { "oneOf": [ { "$ref": "#/schemas/Shape" } ] } } }
	}`
	candidate := `{
	  "openbindings": "0.2.0",
	  "schemas": { "Shape": { "type": "number" } },
	  "operations": { "op": { "output": { "oneOf": [ { "$ref": "#/schemas/Shape" } ] } } }
	}`

	report := compareDocs(t, contract, candidate)
	if report.Summary.Verdict != "incompatible" {
		t.Fatalf("verdict = %q, want incompatible (same $ref pointer, divergent per-document target)", report.Summary.Verdict)
	}
	delta := pairedDelta(t, report)
	if delta.Output == nil || delta.Output.Verdict == "identical" {
		t.Fatalf("output verdict = %+v; fast path incorrectly declared identical despite divergent $ref targets", delta.Output)
	}
	if !hasFindingKind(delta.Findings, "subsume.violated") {
		t.Errorf("expected subsume.violated once the fast-path shortcut is skipped, got %+v", delta.Findings)
	}
}
