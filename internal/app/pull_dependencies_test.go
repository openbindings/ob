package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func sourceOwnedDependency(de openbindings.DependencyEntry) openbindings.DependencyEntry {
	SetXOB(&de.LosslessFields)
	return de
}

// A synthesizer emits a dependency for every inbound unit it represents -- an
// OpenAPI callback or webhook, whose operation the described component
// CONSUMES rather than serves (Core Section 5.6). Before 2026-09-01 the pull
// path dropped them at the derive boundary: DeriveResult carried no
// Dependencies field, so a pulled OBI held an unbound operation with nothing
// saying it was a consumption point -- the one fact distinguishing it from an
// operation nothing has bound yet.
func TestPullSourceInto_DependenciesCreateOverwritePruneLeaveAuthored(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"onA":  sourceOwnedOp(openbindings.Operation{Description: "old"}),
			"onB":  sourceOwnedOp(openbindings.Operation{}),
			"mine": sourceOwnedOp(openbindings.Operation{Description: "authored consumption point"}),
		},
		Dependencies: map[string]openbindings.DependencyEntry{
			"a":     sourceOwnedDependency(openbindings.DependencyEntry{Operation: "onA"}),
			"b":     sourceOwnedDependency(openbindings.DependencyEntry{Operation: "onB"}),
			"byHand": {Operation: "mine"}, // hand-authored: must survive
		},
	}
	derived := DeriveResult{
		Operations: map[string]openbindings.Operation{
			"onA": {Description: "new"},
			"onC": {Description: "fresh"},
		},
		Dependencies: map[string]openbindings.DependencyEntry{
			"a": {Operation: "onA"},
			"c": {Operation: "onC"},
		},
	}

	var out SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &out)

	if _, ok := iface.Dependencies["c"]; !ok {
		t.Error("dependency \"c\" should be added")
	}
	if _, ok := iface.Dependencies["b"]; ok {
		t.Error("dependency \"b\" should be pruned: the source no longer derives it")
	}
	if _, ok := iface.Dependencies["byHand"]; !ok {
		t.Error("a hand-authored dependency must survive a pull")
	}
	if iface.Dependencies["a"].Operation != "onA" {
		t.Errorf("dependency \"a\" = %q, want onA", iface.Dependencies["a"].Operation)
	}

	// An operation a dependency names is REFERENCED even though nothing binds
	// it, so the orphan sweep must not read "no binding" as "unreferenced".
	if _, ok := iface.Operations["onC"]; !ok {
		t.Error("onC is named by a derived dependency and must survive")
	}
	if _, ok := iface.Operations["mine"]; !ok {
		t.Error("an operation a hand-authored dependency names must survive")
	}
	// onB lost its only reference and is no longer derived: it goes.
	if _, ok := iface.Operations["onB"]; ok {
		t.Error("onB lost its dependency and is no longer derived; it should be pruned")
	}

	if len(out.DependenciesAdded) != 1 || out.DependenciesAdded[0] != "c" {
		t.Errorf("DependenciesAdded = %v, want [c]", out.DependenciesAdded)
	}
	if len(out.DependenciesPruned) != 1 || out.DependenciesPruned[0] != "b" {
		t.Errorf("DependenciesPruned = %v, want [b]", out.DependenciesPruned)
	}
}

// A second pull over an unchanged source rewrites nothing: the dependency
// merge has to be as churn-free as the binding merge beside it.
func TestPullSourceInto_DependencyPullIsIdempotent(t *testing.T) {
	iface := &openbindings.Interface{
		Operations:   map[string]openbindings.Operation{"onA": sourceOwnedOp(openbindings.Operation{})},
		Dependencies: map[string]openbindings.DependencyEntry{},
	}
	derived := DeriveResult{
		Operations:   map[string]openbindings.Operation{"onA": {}},
		Dependencies: map[string]openbindings.DependencyEntry{"a": {Operation: "onA"}},
	}

	var first SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &first)
	if len(first.DependenciesAdded) != 1 {
		t.Fatalf("first pull DependenciesAdded = %v, want one", first.DependenciesAdded)
	}

	var second SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &second)
	if len(second.DependenciesAdded) != 0 || len(second.DependenciesUpdated) != 0 || len(second.DependenciesPruned) != 0 {
		t.Errorf("second pull reported churn: added=%v updated=%v pruned=%v",
			second.DependenciesAdded, second.DependenciesUpdated, second.DependenciesPruned)
	}
}

// End to end through the real OpenAPI synthesizer, on the document
// conformance scenario OAPI30-SS-45 uses -- the scenario whose description
// reads "A callback operation becomes a targetless Core dependency". Before
// the fix ob produced the operation and no dependency at all, so the OBI said
// nothing about the consumption point the scenario is named for.
func TestSourcePull_CallbackBecomesADependency(t *testing.T) {
	dir := t.TempDir()

	doc := `{
	  "openapi": "3.0.4",
	  "info": {"title": "Callback dependency", "version": "1"},
	  "servers": [{"url": "https://api.example"}],
	  "paths": {"/jobs": {"post": {
	    "operationId": "createJob",
	    "callbacks": {"done": {"{$request.body#/url}": {"post": {
	      "operationId": "receiveDone",
	      "requestBody": {"required": true, "content": {"application/json": {"schema": {"type": "object"}}}},
	      "responses": {"200": {"description": "accepted", "content": {"application/json": {"schema": {"type": "string"}}}}}
	    }}}},
	    "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object"}}}}}
	  }}}
	}`
	if err := os.WriteFile(filepath.Join(dir, "api.json"), []byte(doc), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	obiPath := writeInterface(t, dir, "interface.json", map[string]any{
		"openbindings": "0.2.0",
		"name":         "cb",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"api": map[string]any{
				"bindingSpec": "openbindings.openapi-3.0@1",
				"location":    "./api.json",
				"x-ob":        map[string]any{"ref": "./api.json", "resolve": "location"},
			},
		},
	})

	out, err := SourcePull(SourcePullInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(out.DependenciesAdded) != 1 {
		t.Fatalf("DependenciesAdded = %v, want exactly one callback dependency", out.DependenciesAdded)
	}

	data, err := os.ReadFile(obiPath)
	if err != nil {
		t.Fatalf("read OBI: %v", err)
	}
	var parsed struct {
		Operations   map[string]any `json:"operations"`
		Bindings     map[string]any `json:"bindings"`
		Dependencies map[string]struct {
			Operation string `json:"operation"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse OBI: %v", err)
	}
	if len(parsed.Dependencies) != 1 {
		t.Fatalf("dependencies = %v, want one", parsed.Dependencies)
	}
	for key, dep := range parsed.Dependencies {
		if dep.Operation != "receiveDone" {
			t.Errorf("dependency %q names operation %q, want receiveDone", key, dep.Operation)
		}
	}
	// The callback's operation is a consumption point, never a binding target:
	// its runtime-expression destination is not an address this document can
	// dispatch to.
	for key, binding := range parsed.Bindings {
		if op, _ := binding.(map[string]any)["operation"].(string); op == "receiveDone" {
			t.Errorf("binding %q targets the callback operation; a dependency has no binding", key)
		}
	}
	if _, ok := parsed.Operations["receiveDone"]; !ok {
		t.Error("the consumed operation contract must be emitted")
	}
}
