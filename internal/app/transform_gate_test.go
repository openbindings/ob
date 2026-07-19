package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// This is the Go side of the JSONata transform differential-conformance gate
// (spec/conformance/transforms). The `agree/` corpus pins the NORMATIVE
// jsonata-js output that every conformant engine MUST reproduce; this test
// runs every agree case through ob's adopted engine (gnata, via the same
// evalTransform path invocation uses) and asserts it matches. It is the
// cross-SDK parity gate on the Go side and the regression guard on the engine.
//
// The corpus is located under OB_SPEC_CORPUS (the conformance root); the test
// skips when that is unset, matching the other corpus-backed tests.

type gateOutcome struct {
	Status                string          `json:"status"` // value | undefined | error
	Value                 json.RawMessage `json:"value"`
	ErrorContains         string          `json:"errorContains"`
	OrderNondeterministic bool            `json:"orderNondeterministic"`
}

type gateCase struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Expr        string          `json:"expr"`
	Input       json.RawMessage `json:"input"`
	Expected    gateOutcome     `json:"expected"`
	Actual      gateOutcome     `json:"actual"` // known-divergence only
}

type gateFile struct {
	Kind           string     `json:"kind"`
	ExpectedEngine string     `json:"expectedEngine"`
	Cases          []gateCase `json:"cases"`
}

func transformCorpusDir(t *testing.T) string {
	t.Helper()
	root := os.Getenv("OB_SPEC_CORPUS")
	if root == "" {
		t.Skip("OB_SPEC_CORPUS not set; skipping transform differential-conformance gate")
	}
	dir := root
	if filepath.Base(root) != "transforms" {
		dir = filepath.Join(root, "transforms")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("transforms corpus not found at %s", dir)
	}
	return dir
}

// evalOutcome runs an expression through ob's engine and reduces it to the
// corpus outcome envelope (undefined stays distinct from a null value).
func evalOutcome(expr string, input any) gateOutcome {
	result, err := evalTransform(expr, input, nil)
	switch {
	case errors.Is(err, openbindings.ErrTransformUndefined):
		return gateOutcome{Status: "undefined"}
	case err != nil:
		return gateOutcome{Status: "error", ErrorContains: err.Error()}
	default:
		b, _ := json.Marshal(result)
		return gateOutcome{Status: "value", Value: b}
	}
}

// jsonEqual compares two JSON values structurally, optionally treating a
// top-level array as a multiset (for order-nondeterministic actuals — not
// expected in the agree set, honored for completeness).
func gateJSONEqual(a, b json.RawMessage, multiset bool) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	if multiset {
		aa, aok := av.([]any)
		bb, bok := bv.([]any)
		if aok && bok {
			return multisetEqual(aa, bb)
		}
	}
	return reflect.DeepEqual(av, bv)
}

func multisetEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(v any) string { s, _ := json.Marshal(v); return string(s) }
	ak := make([]string, len(a))
	bk := make([]string, len(b))
	for i := range a {
		ak[i] = key(a[i])
		bk[i] = key(b[i])
	}
	sort.Strings(ak)
	sort.Strings(bk)
	return reflect.DeepEqual(ak, bk)
}

func loadGateFiles(t *testing.T, dir string) []gateFile {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []gateFile
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var gf gateFile
		if err := json.Unmarshal(b, &gf); err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		files = append(files, gf)
	}
	return files
}

// TestTransformGate_AgreeMatchesNormative runs the agree corpus through gnata
// and asserts every case reproduces the normative jsonata-js output. A failure
// means the Go engine has drifted from the cross-SDK parity contract.
func TestTransformGate_AgreeMatchesNormative(t *testing.T) {
	dir := filepath.Join(transformCorpusDir(t), "agree")
	files := loadGateFiles(t, dir)
	total := 0
	for _, gf := range files {
		for _, c := range gf.Cases {
			total++
			t.Run(c.ID, func(t *testing.T) {
				var input any
				if err := json.Unmarshal(c.Input, &input); err != nil {
					t.Fatalf("bad input: %v", err)
				}
				got := evalOutcome(c.Expr, input)
				if got.Status != c.Expected.Status {
					t.Fatalf("expr %q: status = %q, want %q (err=%q)", c.Expr, got.Status, c.Expected.Status, got.ErrorContains)
				}
				if c.Expected.Status == "value" && !gateJSONEqual(got.Value, c.Expected.Value, c.Expected.OrderNondeterministic) {
					t.Fatalf("expr %q: value = %s, want %s", c.Expr, got.Value, c.Expected.Value)
				}
			})
		}
	}
	if total == 0 {
		t.Fatal("no agree cases loaded")
	}
	t.Logf("transform gate: %d agree cases matched the normative engine on gnata", total)
}

// TestTransformGate_KnownDivergenceStillHolds runs the catalogued gnata
// divergences through gnata and asserts each still produces the recorded
// `actual`. This is the inverse guard: when a gnata upgrade CLOSES a
// divergence (its output shifts toward the normative `expected`), this test
// fails, prompting the catalog to be updated and the case promoted to agree.
func TestTransformGate_KnownDivergenceStillHolds(t *testing.T) {
	dir := filepath.Join(transformCorpusDir(t), "known-divergence")
	files := loadGateFiles(t, dir)
	total := 0
	for _, gf := range files {
		for _, c := range gf.Cases {
			total++
			t.Run(c.ID, func(t *testing.T) {
				var input any
				if err := json.Unmarshal(c.Input, &input); err != nil {
					t.Fatalf("bad input: %v", err)
				}
				got := evalOutcome(c.Expr, input)
				if got.Status != c.Actual.Status {
					t.Fatalf("expr %q: gnata status = %q, catalogued actual = %q — divergence changed; update catalog", c.Expr, got.Status, c.Actual.Status)
				}
				if c.Actual.Status == "value" && !gateJSONEqual(got.Value, c.Actual.Value, false) {
					t.Fatalf("expr %q: gnata value = %s, catalogued actual = %s — divergence changed; update catalog", c.Expr, got.Value, c.Actual.Value)
				}
			})
		}
	}
	if total == 0 {
		t.Fatal("no known-divergence cases loaded")
	}
	t.Logf("transform gate: %d catalogued gnata divergences still hold", total)
}
