package app

import (
	"encoding/json"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func TestOrdinaryDocumentCopiesPreserveValues(t *testing.T) {
	raw := `{"openbindings":"0.2.0","operations":{},"schemas":{"Value":{"enum":[9007199254740993,0.10000000000000001,1e-400,1e400]}}}`
	var original map[string]any
	if err := jsonvalue.Unmarshal([]byte(raw), &original); err != nil {
		t.Fatal(err)
	}
	got, ok := normalizeOBIJSON([]byte(raw))
	if !ok {
		t.Error("probe did not recognize document")
	}
	var observed any
	if err := jsonvalue.Unmarshal([]byte(got), &observed); ok && err != nil {
		t.Fatal(err)
	}
	if equal, err := jsonvalue.Equal(original, observed); err != nil || !equal {
		t.Fatalf("probe changed values: %s (%v)", got, err)
	}
	schema := original["schemas"].(map[string]any)["Value"]
	copy := deepCopySchema(schema)
	if equal, err := jsonvalue.Equal(schema, copy); err != nil || !equal {
		t.Fatalf("schema copy changed values: %v (%v)", copy, err)
	}
	copy.(map[string]any)["title"] = "mutation"
	if _, found := schema.(map[string]any)["title"]; found {
		t.Fatal("schema copy aliases caller")
	}
}

func TestSchemaEqualityPreservesInstanceValues(t *testing.T) {
	for _, pair := range [][2]string{{"9007199254740992", "9007199254740993"}, {"0.1", "0.10000000000000001"}, {"0", "1e-400"}} {
		a, b := map[string]any{"const": json.Number(pair[0])}, map[string]any{"const": json.Number(pair[1])}
		if schemasEqual(a, b, nil, nil) {
			t.Errorf("diff equated %s and %s", pair[0], pair[1])
		}
		if strippedCanonicalEqual(a, b) {
			t.Errorf("compat equated %s and %s", pair[0], pair[1])
		}
		if found, err := arrayContains([]any{a["const"]}, b["const"]); err != nil || found {
			t.Errorf("enum equated %s and %s", pair[0], pair[1])
		}
	}
	for _, key := range []string{"default", "description", "examples", "title"} {
		a, b := map[string]any{"const": map[string]any{key: 1}}, map[string]any{"const": map[string]any{key: 2}}
		if strippedCanonicalEqual(a, b) {
			t.Errorf("compat erased instance member %s", key)
		}
		a, b = map[string]any{"properties": map[string]any{key: map[string]any{"const": 1}}}, map[string]any{"properties": map[string]any{key: map[string]any{"const": 2}}}
		if strippedCanonicalEqual(a, b) {
			t.Errorf("compat erased property %s", key)
		}
	}
	a, b := map[string]any{"properties": map[string]any{"x": map[string]any{"type": "number", "description": "a"}}}, map[string]any{"properties": map[string]any{"x": map[string]any{"type": "number", "description": "b"}}}
	if !strippedCanonicalEqual(a, b) {
		t.Fatal("schema-position annotations must remain ignorable")
	}
	if found, err := arrayContains([]any{json.Number("0.10")}, json.Number("1e-1")); err != nil || !found {
		t.Fatal("numeric spelling is not value identity")
	}
	if strippedCanonicalEqual(json.Number("NaN"), json.Number("invalid")) {
		t.Fatal("failed encodings became equal")
	}
}

func TestConflictDisplayPreservesValues(t *testing.T) {
	for _, raw := range []string{`9007199254740993`, `0.10000000000000001`, `1e-400`, `1e400`} {
		if got := compactJSON(json.RawMessage(raw)); got != raw {
			t.Errorf("display %s as %s", raw, got)
		}
	}
}

func TestNumericComparisonReportsExactBounds(t *testing.T) {
	for _, pair := range [][2]string{{"9007199254740992", "9007199254740993"}, {"0.1", "0.10000000000000001"}, {"0", "1e-400"}, {"1e400", "2e400"}} {
		var findings []Finding
		compareNumeric(&findings, "/operations/value/input", "input", "minimum", json.Number(pair[0]), json.Number(pair[1]))
		if len(findings) != 1 || findings[0].Kind != "numeric.minimum.tightened" {
			t.Errorf("missing exact bound finding for %v: %v", pair, findings)
		}
	}
}

func TestComparisonCapabilityIsNotCompatibility(t *testing.T) {
	for _, key := range []string{"minimum", "enum"} {
		left, right := any(json.Number("1e10001")), any(json.Number("2e10001"))
		if key == "enum" {
			left = []any{left}
			right = []any{right}
		}
		makeDoc := func(v any) *openbindings.Interface {
			return &openbindings.Interface{OpenBindings: "0.2.0", Operations: map[string]openbindings.Operation{"value": {Input: map[string]any{"type": "number", key: v}}}}
		}
		report := ComparisonCheck(ComparisonInput{LeftInterface: makeDoc(left), RightInterface: makeDoc(right)})
		if report.Summary.Verdict != "indeterminate" {
			t.Errorf("%s refusal became %s: %+v", key, report.Summary.Verdict, report.Operations)
		}
	}
}
