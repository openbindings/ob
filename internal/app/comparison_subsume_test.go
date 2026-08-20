package app

import (
	"testing"

	"github.com/openbindings/openbindings-go/compare"
)

// The comparison's schema verdicts run the SDK's compatibility engine, so
// `ob compat` and CheckInterfaceCompatibility reach the same verdict on the
// same pair — the shared-semantics promise. Regression: the slot verdict
// only distinguished identical-vs-different (no subsumption), so a breaking
// change the structural walk does not descend into — a union combinator's
// variant type flipping inside oneOf — reported "compatible" while the SDK
// said output_incompatible.
//
// (Array items were the original vehicle for this regression; the
// items-recursion fix in compareSchemaAt now catches that case directly via
// a type.changed finding, so slotAlreadyFaulted correctly stops the engine
// from double-reporting it — see TestComparisonItems_NestedArrayBreakCaught.
// oneOf remains a position the structural walk deliberately does not
// descend into, so it still isolates the subsumption-engine safety net this
// test exists to cover.)
func TestComparison_SubsumptionMatchesSDKVerdict(t *testing.T) {
	contract := `{
	  "openbindings": "0.2.0",
	  "operations": { "getMenu": { "output": {
	    "type": "object",
	    "properties": { "price": { "oneOf": [ { "type": "number" } ] } }
	  } } }
	}`
	changed := `{
	  "openbindings": "0.2.0",
	  "operations": { "getMenu": { "output": {
	    "type": "object",
	    "properties": { "price": { "oneOf": [ { "type": "string" } ] } }
	  } } }
	}`

	report := compareDocs(t, contract, changed)
	delta := pairedDelta(t, report)
	if delta.Output == nil || delta.Output.Verdict != "incompatible" {
		t.Fatalf("ob compat output verdict = %+v, want incompatible", delta.Output)
	}
	if !hasFindingKind(delta.Findings, "subsume.violated") {
		t.Errorf("expected a subsume.violated finding, got %+v", delta.Findings)
	}
	for _, f := range delta.Findings {
		if f.Kind == "subsume.violated" && f.Detail == "" {
			t.Errorf("subsume.violated finding must carry the engine's reason")
		}
	}

	// The SDK's verdict on the identical pair.
	issues := compare.CheckInterfaceCompatibility(mustIface(t, contract), mustIface(t, changed))
	sdkIncompatible := false
	for _, is := range issues {
		if is.Operation == "getMenu" && is.Kind == compare.CompatibilityOutputIncompatible {
			sdkIncompatible = true
		}
	}
	if !sdkIncompatible {
		t.Fatalf("SDK verdict missing output_incompatible: %+v", issues)
	}

	// And a genuinely compatible widening (output narrows nothing; candidate
	// output is the same oneOf shape) must NOT be flagged: strictness parity,
	// not just failure parity.
	widened := `{
	  "openbindings": "0.2.0",
	  "operations": { "getMenu": { "output": {
	    "type": "object",
	    "properties": { "price": { "oneOf": [ { "type": "number" } ] } },
	    "required": ["price"]
	  } } }
	}`
	report2 := compareDocs(t, contract, widened)
	delta2 := pairedDelta(t, report2)
	if delta2.Output != nil && delta2.Output.Verdict == "incompatible" {
		t.Errorf("tightened-but-satisfying output flagged incompatible: %+v", delta2.Findings)
	}
	if issues2 := compare.CheckInterfaceCompatibility(mustIface(t, contract), mustIface(t, widened)); len(issues2) != 0 {
		t.Errorf("SDK disagrees on the satisfying pair: %+v", issues2)
	}
}
