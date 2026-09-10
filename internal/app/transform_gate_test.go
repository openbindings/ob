package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Core's language/ corpus governs the documented binding-transform checks.
// agree/ and known-divergence/ are historical runtime observations, retained as
// regression controls on the unmigrated private Graph engine ONLY. Neither the
// reference's old behavior nor its known bugs define current Core conformance.
//
// The corpus is located under OB_SPEC_CORPUS (the conformance root), or in the
// sibling spec checkout used by the monorepo development layout. Absence is a
// local skip unless OB_CORPUS_REQUIRED is set, in which case it is a failure.

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
		root = filepath.Clean(filepath.Join("..", "..", "..", "spec", "conformance"))
	}
	dir := root
	if filepath.Base(root) != "transforms" {
		dir = filepath.Join(root, "transforms")
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if os.Getenv("OB_CORPUS_REQUIRED") != "" {
			t.Fatalf("transforms corpus not found at %s (OB_CORPUS_REQUIRED is set; set OB_SPEC_CORPUS)", dir)
		}
		t.Skipf("transforms corpus not found at %s", dir)
	}
	return dir
}

// evalOutcome observes the frozen legacy engine, not current binding transforms.
func evalOutcome(expr string, input any) gateOutcome {
	result, err := legacyEvalTransformContext(context.Background(), expr, input, nil)
	switch {
	case errors.Is(err, invoke.ErrTransformUndefined):
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

// Historical agreement remains a regression control for deferred Graph wiring.
func TestLegacyTransformGate_HistoricalAgreement(t *testing.T) {
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
	t.Logf("legacy isolation: %d historical agreement cases retained", total)
}

// Known legacy differences must not accidentally migrate the deferred engine.
// This is not a requirement to preserve those defects in the new evaluator.
func TestLegacyTransformGate_HistoricalDifferences(t *testing.T) {
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
	t.Logf("legacy isolation: %d historical differences retained", total)
}

func TestTransformGate_DocumentedLanguage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(transformCorpusDir(t), "language", "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			ID        string `json:"id"`
			Expr      string `json:"expr"`
			InputJSON string `json:"inputJSON"`
			Expected  struct {
				Status string `json:"status"`
				JSON   string `json:"json"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("no documented language cases")
	}
	for _, c := range corpus.Cases {
		t.Run(c.ID, func(t *testing.T) {
			var input any
			if err := jsonvalue.Unmarshal([]byte(c.InputJSON), &input); err != nil {
				t.Fatal(err)
			}
			value, err := evalTransform(c.Expr, input, nil)
			if c.Expected.Status == "failure" {
				if err == nil {
					t.Fatalf("expected boundary failure; received %#v", value)
				}
				return
			}
			if c.Expected.Status != "json" {
				t.Fatalf("unrecognized expected status %q", c.Expected.Status)
			}
			if err != nil {
				t.Fatal(err)
			}
			var want any
			if err := jsonvalue.Unmarshal([]byte(c.Expected.JSON), &want); err != nil {
				t.Fatal(err)
			}
			equal, err := jsonvalue.Equal(value, want)
			if err != nil || !equal {
				t.Fatalf("language result %#v, want %s (comparison: %v)", value, c.Expected.JSON, err)
			}
		})
	}
	t.Logf("current binding transforms: %d documented language/boundary cases", len(corpus.Cases))
}
