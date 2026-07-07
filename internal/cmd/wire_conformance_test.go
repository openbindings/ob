package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
)

// TestWireConformance_ExecLane drives operations through the bound CLI OBI's
// exec lane exactly as a delegate registrar would: resolve the bound OBI,
// operation-invoke, exec the real `ob` binary, parse stdout. Every output is
// validated against the operation's output schema (the SDK's OBI-T-08
// machinery via ValidateAgainstSchema, independently of ob's own applyT08
// enforcement), so an op passing here is wire-conformant end to end: input
// field mapping, argv, exec, output shape.
//
// Coverage spans the wire-conformance loop's cohorts
// (ob-pj/wire-conformance.md): A (flat inputs), B (--input machine lanes),
// C (document filters: the read/analysis cases plus the editing chain).
// Cohort F (foreground) and the frame ops (unary realizations of the frame
// contract) are excluded by design, with notes on their binding entries.
func TestWireConformance_ExecLane(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and execs the real binary")
	}

	// The bound OBI, resolved before we chdir into the sandbox. Its schemas
	// judge the outputs below.
	obiPath, err := filepath.Abs("../app/ob.obi.json")
	if err != nil {
		t.Fatal(err)
	}
	obiData, err := os.ReadFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	var bound openbindings.Interface
	if err := json.Unmarshal(obiData, &bound); err != nil {
		t.Fatal(err)
	}

	// Build the real binary; the bound OBI's usage spec names `bin "ob"`, which
	// the usage transport resolves via PATH.
	binDir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(binDir, "ob"), "./cmd/ob")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ob: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// A registrable delegate fixture: the same binary under a different name
	// (registering `exec:ob` itself is refused as the self-delegate).
	binBytes, err := os.ReadFile(filepath.Join(binDir, "ob"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ob-fixture"), binBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	// A resolvable interface fixture for resolveInterface: a minimal OBI
	// served over HTTP from the test process.
	fixtureOBI := `{"openbindings":"0.2.0","name":"fixture","operations":{"ping":{}}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixtureOBI))
	}))
	defer ts.Close()

	// Sandbox: HOME redirects the global config dir (contexts, delegates,
	// global environment); a temp cwd catches local-environment writes.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "") // linux: fall back to HOME/.config
	workDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Document fixtures for the read/analysis family (cohort C). These ops
	// take an interface document as input; the exec-lane binding delivers it
	// out of band (the primary via stdin, a second via a temp file) and the
	// CLI reads it as a `-` locator or file path. See ob-pj/wire-conformance.md
	// batch 3.
	docA := map[string]any{
		"openbindings": "0.2.0",
		"name":         "wire-fixture-a",
		"version":      "1.0.0",
		"operations": map[string]any{
			"ping": map[string]any{},
			"pong": map[string]any{},
		},
		"sources": map[string]any{
			"s": map[string]any{"format": "openapi@3.1", "location": "https://example.com/openapi.yaml"},
		},
		"bindings": map[string]any{
			"ping.s": map[string]any{"operation": "ping", "source": "s", "ref": "#/paths/~1ping/get"},
		},
	}
	docB := map[string]any{
		"openbindings": "0.2.0",
		"name":         "wire-fixture-b",
		"operations": map[string]any{
			"ping": map[string]any{},
		},
	}
	// conform/merge fixtures: a reference to satisfy, an empty target to
	// scaffold into, a source to graft from. These ops ride BOTH documents on
	// temp files (a written-back target cannot be stdin) with -y injected.
	docRef := map[string]any{
		"openbindings": "0.2.0",
		"name":         "wire-ref",
		"operations": map[string]any{
			"createUser": map[string]any{"input": map[string]any{"type": "object"}},
		},
	}
	docSrc := map[string]any{
		"openbindings": "0.2.0",
		"name":         "wire-src",
		"operations": map[string]any{
			"newOp": map[string]any{},
		},
	}

	// A binding-source artifact on disk, used by the cohort-B machine-lane
	// cases below (inspect/synthesize read it from the child's cwd) and by
	// the editing-family chain (source add/pull/bind).
	openapiFixture := `{"openapi":"3.1.0","info":{"title":"wire","version":"1.0.0"},"paths":{"/ping":{"get":{"operationId":"getPing","responses":{"200":{"description":"ok"}}}}}}`
	if err := os.WriteFile(filepath.Join(workDir, "openapi.json"), []byte(openapiFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	// Ordered: later cases depend on earlier state (the initialized
	// environment, the registered delegate, the stored context).
	cases := []struct {
		name  string
		op    string
		input any
		check func(t *testing.T, output any)
	}{
		{"describe", "openbindings.ob.describe", nil, nil},
		{"listFormats", "openbindings.ob.listFormats", nil, nil},
		{"initializeEnvironment", "openbindings.ob.initializeEnvironment", map[string]any{"global": true}, nil},
		{"reportEnvironmentStatus", "openbindings.ob.reportEnvironmentStatus", nil, nil},
		{"listContexts", "openbindings.ob.listContexts", nil, nil},
		{"listDelegates", "openbindings.ob.listDelegates", nil, nil},
		{"resolveDelegate", "openbindings.ob.resolveDelegate", map[string]any{"operation": "openbindings.ob.describe"}, nil},
		{"getDelegateRequirements", "openbindings.ob.getDelegateRequirements", map[string]any{"capability": "invoke"}, nil},
		{"getContext_missing", "openbindings.ob.getContext", map[string]any{"key": "https://missing.example.com"}, func(t *testing.T, output any) {
			if output != nil {
				t.Errorf("expected null for a missing context, got %#v", output)
			}
		}},
		{"setContext", "openbindings.ob.setContext", map[string]any{
			"key":   "https://wire.example.com",
			"value": map[string]any{"headers": map[string]any{"X-Team": "blue"}},
		}, nil},
		{"getContext", "openbindings.ob.getContext", map[string]any{"key": "https://wire.example.com"}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if h, _ := m["headers"].(map[string]any); h == nil || h["X-Team"] != "blue" {
				t.Errorf("expected the stored context back, got %#v", output)
			}
		}},
		{"removeContext", "openbindings.ob.removeContext", map[string]any{"key": "https://wire.example.com"}, nil},
		{"resolveDelegateForFormat", "openbindings.ob.resolveDelegateForFormat", map[string]any{"format": "usage@2.13.1"}, nil},
		{"registerDelegate", "openbindings.ob.registerDelegate", map[string]any{"location": "exec:ob-fixture", "preference": 5}, nil},
		{"setDelegatePreference", "openbindings.ob.setDelegatePreference", map[string]any{"location": "exec:ob-fixture", "preference": 10}, nil},
		{"unregisterDelegate", "openbindings.ob.unregisterDelegate", map[string]any{"location": "exec:ob-fixture"}, nil},
		{"resolveInterface", "openbindings.ob.resolveInterface", map[string]any{"address": ts.URL}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			iface, _ := m["interface"].(map[string]any)
			if iface == nil || iface["name"] != "fixture" {
				t.Errorf("expected the fixture interface in the envelope, got %#v", output)
			}
		}},

		// Read/analysis family (cohort C): document-in filters. The document
		// rides stdin; the CLI reads a `-` locator. Output is judged against
		// each operation's contract output schema above.
		{"validateInterface", "openbindings.ob.validateInterface", map[string]any{"interface": docA}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if m["valid"] != true {
				t.Errorf("expected valid=true for the fixture, got %#v", output)
			}
		}},
		{"reportInterfaceStatus", "openbindings.ob.reportInterfaceStatus", map[string]any{"interface": docA}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if srcs, _ := m["sources"].([]any); len(srcs) != 1 {
				t.Errorf("expected one source in the status report, got %#v", output)
			}
		}},
		{"purifyInterface", "openbindings.ob.purifyInterface", docA, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if m["name"] != "wire-fixture-a" {
				t.Errorf("expected the purified interface back, got %#v", output)
			}
		}},
		{"listSources", "openbindings.ob.listSources", map[string]any{"interface": docA}, func(t *testing.T, output any) {
			if entries, _ := output.([]any); len(entries) != 1 {
				t.Errorf("expected one source entry, got %#v", output)
			}
		}},
		{"listOperations", "openbindings.ob.listOperations", map[string]any{"interface": docA}, func(t *testing.T, output any) {
			if entries, _ := output.([]any); len(entries) != 2 {
				t.Errorf("expected two operation entries, got %#v", output)
			}
		}},
		{"listOperationAliases", "openbindings.ob.listOperationAliases", map[string]any{"interface": docA}, nil},
		{"prepareOperation", "openbindings.ob.prepareOperation", map[string]any{"interface": docA, "operation": "ping"}, nil},
		{"compareInterfaces", "openbindings.ob.compareInterfaces", map[string]any{"baseline": docA, "comparison": docB}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			ops, _ := m["operations"].([]any)
			if len(ops) == 0 {
				t.Errorf("expected a delta for the removed operation, got %#v", output)
			}
		}},
		{"reportCompatibility", "openbindings.ob.reportCompatibility", map[string]any{"target": docA, "candidate": docB}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if _, ok := m["summary"]; !ok {
				t.Errorf("expected a compatibility summary, got %#v", output)
			}
		}},
		{"codegen", "openbindings.ob.codegen", map[string]any{"interface": docA, "language": "go"}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if m["language"] != "go" || m["code"] == "" || m["code"] == nil {
				t.Errorf("expected a go CodegenOutput envelope, got %#v", output)
			}
		}},
		{"conform", "openbindings.ob.conform", map[string]any{"interface": docRef, "target": map[string]any{
			"openbindings": "0.2.0", "name": "wire-tgt", "operations": map[string]any{},
		}}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if m["modified"] != true {
				t.Errorf("expected the target to be modified (createUser scaffolded), got %#v", output)
			}
			if _, ok := m["interface"].(map[string]any); !ok {
				t.Errorf("expected the conformed interface in the report, got %#v", output)
			}
		}},
		{"mergeInterfaces", "openbindings.ob.mergeInterfaces", map[string]any{"target": map[string]any{
			"openbindings": "0.2.0", "name": "wire-mtgt", "operations": map[string]any{"oldOp": map[string]any{}},
		}, "source": docSrc}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if applied, _ := m["applied"].(float64); applied < 1 {
				t.Errorf("expected at least one applied merge entry, got %#v", output)
			}
			if _, ok := m["interface"].(map[string]any); !ok {
				t.Errorf("expected the merged interface in the report, got %#v", output)
			}
		}},

		// Cohort B (machine lane): the delegate-facing derivation and
		// preflight surface. Their whole wire input rides one --input flag
		// as JSON (the batch-5 audit ratified inspect/synthesize as
		// machine-natured alongside binding invoke/prepare), and --input
		// implies wire-shaped JSON output. The frame ops (invokeBinding,
		// invokeOperation) are NOT here: their exec bindings are the
		// documented UNARY REALIZATION of the frame contract — a unary
		// transport cannot carry the frame grammar, so they are excluded
		// like cohort F, with the note stamped on their binding entries.
		{"inspectSource", "openbindings.ob.inspectSource", map[string]any{
			"source": map[string]any{"format": "openapi@3.1", "location": "openapi.json"},
		}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			if targets, _ := m["targets"].([]any); len(targets) != 1 {
				t.Errorf("expected one bindable target in the inspection, got %#v", output)
			}
		}},
		{"synthesizeInterface", "openbindings.ob.synthesizeInterface", map[string]any{
			"name":    "synth-fixture",
			"sources": []any{map[string]any{"format": "openapi@3.1", "location": "openapi.json"}},
		}, func(t *testing.T, output any) {
			m, _ := output.(map[string]any)
			ops, _ := m["operations"].(map[string]any)
			if m["name"] != "synth-fixture" || ops["getPing"] == nil {
				t.Errorf("expected a synthesized interface with getPing, got %#v", output)
			}
		}},
		{"prepareBinding", "openbindings.ob.prepareBinding", map[string]any{
			"source": map[string]any{
				"format":  "openbindings.operation-graph@0.2.0",
				"content": map[string]any{"graphs": map[string]any{"echo": map[string]any{"openbindings.operation-graph": "0.2.0", "nodes": map[string]any{"in": map[string]any{"type": "input"}, "out": map[string]any{"type": "output"}}, "edges": []any{map[string]any{"from": "in", "to": "out"}}}}},
			},
			"ref": "#/graphs/echo",
		}, func(t *testing.T, output any) {
			if output != nil {
				t.Errorf("expected null (no context required for a pure graph), got %#v", output)
			}
		}},
	}

	// invokeConformant drives one operation through the exec lane and judges
	// the output against the operation's contract output schema (OBI-T-08
	// semantics); it returns the (single) output value.
	invokeConformant := func(t *testing.T, op string, input any) any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		ch, _, err := app.InvokeOBIOperation(ctx, obiPath, op, "", input)
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}

		outputSchema := bound.Operations[op].Output

		var out any
		got := 0
		for ev := range ch {
			if ev.Error != nil {
				t.Fatalf("invocation error: %s: %s", ev.Error.Code, ev.Error.Message)
			}
			// The conformance judgment: the output must satisfy the
			// operation's output schema (OBI-T-08 semantics).
			if outputSchema != nil {
				if verr := openbindings.ValidateAgainstSchema(ev.Output, outputSchema, bound.Schemas); verr != nil {
					t.Fatalf("output does not conform to the operation's output schema: %v\noutput: %#v", verr, ev.Output)
				}
			}
			// Belt and braces for permissive output schemas: the usage
			// transport's non-JSON fallback must never leak through.
			if m, ok := ev.Output.(map[string]any); ok {
				if _, leaked := m["stdout"]; leaked {
					t.Fatalf("output is the {stdout} wrapper, not the contract shape: %#v", ev.Output)
				}
			}
			out = ev.Output
			got++
		}
		if got == 0 {
			t.Fatal("invocation produced no output")
		}
		return out
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := invokeConformant(t, tc.op, tc.input)
			if tc.check != nil {
				tc.check(t, out)
			}
		})
	}

	// Editing family (cohort C, batch 4): document-in/document-out filters.
	// The interface document rides stdin as a `-` locator and the modified
	// document comes back on stdout — which IS the contract output for every
	// editing op except pullSource (whose report carries the document in its
	// `interface` member). The chain authors an interface from nothing to a
	// sourced, pulled, bound, edited document entirely over the wire; each
	// step's output feeds the next step's input.
	// Navigation helpers over the wire documents.
	child := func(t *testing.T, v any, path ...string) map[string]any {
		t.Helper()
		m, _ := v.(map[string]any)
		for _, key := range path {
			next, _ := m[key].(map[string]any)
			if next == nil {
				t.Fatalf("document has no object at %v: %#v", path, v)
			}
			m = next
		}
		return m
	}

	var doc map[string]any // the document flowing through the chain
	chain := []struct {
		name  string
		op    string
		input func() any
		check func(t *testing.T, out any)
	}{
		{"newInterface", "openbindings.ob.newInterface", func() any {
			return map[string]any{"name": "edit-fixture", "version": "0.1.0"}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			if doc["name"] != "edit-fixture" {
				t.Errorf("expected the new document back, got %#v", out)
			}
			if ops, ok := doc["operations"].(map[string]any); !ok || len(ops) != 0 {
				t.Errorf("expected an empty operations map, got %#v", doc["operations"])
			}
		}},
		{"setMetadata", "openbindings.ob.setMetadata", func() any {
			return map[string]any{"interface": doc, "description": "edited over the wire"}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			if doc["description"] != "edited over the wire" {
				t.Errorf("expected the description set, got %#v", doc["description"])
			}
		}},
		{"addOperation", "openbindings.ob.addOperation", func() any {
			return map[string]any{
				"interface":   doc,
				"key":         "ping",
				"description": "Ping.",
				"tags":        []any{"t1"},
				"aliases":     []any{"acme.wire.pingAlias"},
				"idempotent":  true,
				"input":       map[string]any{"type": "object"},
				"output":      map[string]any{"type": "string"},
			}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			op := child(t, doc, "operations", "ping")
			if op["idempotent"] != true {
				t.Errorf("expected idempotent=true on the added operation, got %#v", op)
			}
			if in := child(t, op, "input"); in["type"] != "object" {
				t.Errorf("expected the input schema on the added operation, got %#v", op)
			}
		}},
		{"setOperation", "openbindings.ob.setOperation", func() any {
			return map[string]any{"interface": doc, "operation": "ping", "description": "updated", "deprecated": true, "addTags": []any{"t2"}}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			op := child(t, doc, "operations", "ping")
			if op["deprecated"] != true || op["description"] != "updated" {
				t.Errorf("expected the operation edited, got %#v", op)
			}
		}},
		{"addOperationAlias", "openbindings.ob.addOperationAlias", func() any {
			return map[string]any{"interface": doc, "operation": "ping", "aliases": []any{"acme.wire.get"}}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			aliases, _ := child(t, doc, "operations", "ping")["aliases"].([]any)
			if len(aliases) != 2 {
				t.Errorf("expected two aliases after the add, got %#v", aliases)
			}
		}},
		{"removeOperationAlias", "openbindings.ob.removeOperationAlias", func() any {
			return map[string]any{"interface": doc, "operation": "ping", "aliases": []any{"acme.wire.get"}}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			aliases, _ := child(t, doc, "operations", "ping")["aliases"].([]any)
			if len(aliases) != 1 {
				t.Errorf("expected one alias after the removal, got %#v", aliases)
			}
		}},
		{"setOperationCodegenName", "openbindings.ob.setOperationCodegenName", func() any {
			return map[string]any{"interface": doc, "operation": "ping", "codegenName": "Ping"}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			xob := child(t, doc, "operations", "ping", "x-ob")
			if xob["codegenName"] != "Ping" {
				t.Errorf("expected the codegen-name override stored, got %#v", xob)
			}
		}},
		{"setOperationOutputSchema", "openbindings.ob.setOperationOutputSchema", func() any {
			return map[string]any{"interface": doc, "operation": "ping", "outputSchema": map[string]any{"type": "array"}}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			if got := child(t, doc, "operations", "ping", "output"); got["type"] != "array" {
				t.Errorf("expected the elected output schema, got %#v", got)
			}
		}},
		{"renameOperation", "openbindings.ob.renameOperation", func() any {
			return map[string]any{"interface": doc, "oldKey": "ping", "newKey": "pong"}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			ops := child(t, doc, "operations")
			if _, moved := ops["pong"]; !moved {
				t.Errorf("expected the operation renamed to pong, got %#v", ops)
			}
			if _, stale := ops["ping"]; stale {
				t.Error("the old key must be gone after the rename")
			}
		}},
		{"addSource", "openbindings.ob.addSource", func() any {
			return map[string]any{"interface": doc, "source": map[string]any{
				"format":      "openapi@3.1",
				"location":    "openapi.json",
				"name":        "api",
				"description": "Wire fixture API",
			}}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			src := child(t, doc, "sources", "api")
			if src["format"] != "openapi@3.1" || src["description"] != "Wire fixture API" {
				t.Errorf("expected the registered source back, got %#v", src)
			}
		}},
		{"pullSource", "openbindings.ob.pullSource", func() any {
			// Pull re-reads each source from its x-ob ref relative to the
			// document's own directory. The exec lane materializes the
			// document in a temp dir, so a relative ref cannot survive the
			// trip; a wire consumer shipping a document away from its
			// sources must carry refs that still resolve — point the ref at
			// the fixture absolutely.
			xob := child(t, doc, "sources", "api", "x-ob")
			xob["ref"] = filepath.Join(workDir, "openapi.json")
			return map[string]any{"interface": doc}
		}, func(t *testing.T, out any) {
			report := child(t, out)
			if skipped, _ := report["skipped"].([]any); len(skipped) != 0 {
				t.Fatalf("expected no skipped sources, got %#v (warnings: %#v)", skipped, report["warnings"])
			}
			doc = child(t, report, "interface")
			if _, derived := child(t, doc, "operations")["getPing"]; !derived {
				t.Errorf("expected getPing derived by the pull, got %#v", doc["operations"])
			}
		}},
		{"bindOperation", "openbindings.ob.bindOperation", func() any {
			ref, _ := child(t, doc, "bindings", "getPing.api")["ref"].(string)
			if ref == "" {
				t.Fatalf("no derived binding to take the ref from: %#v", doc["bindings"])
			}
			return map[string]any{"interface": doc, "operation": "pong", "source": "api", "ref": ref}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			if _, bound := child(t, doc, "bindings")["pong.api"]; !bound {
				t.Errorf("expected the pong.api binding, got %#v", doc["bindings"])
			}
		}},
		{"unbindOperation", "openbindings.ob.unbindOperation", func() any {
			return map[string]any{"interface": doc, "operation": "pong", "source": "api"}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			if _, still := child(t, doc, "bindings")["pong.api"]; still {
				t.Error("expected the pong.api binding removed")
			}
		}},
		{"detachOperation", "openbindings.ob.detachOperation", func() any {
			return map[string]any{"interface": doc, "operation": "getPing"}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			op := child(t, doc, "operations", "getPing")
			if xob, owned := op["x-ob"].(map[string]any); owned && xob["sourceOwned"] != nil {
				t.Errorf("expected source ownership cleared, got %#v", op["x-ob"])
			}
		}},
		{"removeOperation", "openbindings.ob.removeOperation", func() any {
			return map[string]any{"interface": doc, "keys": []any{"pong"}}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			if _, still := child(t, doc, "operations")["pong"]; still {
				t.Error("expected pong removed")
			}
		}},
		{"removeSource", "openbindings.ob.removeSource", func() any {
			return map[string]any{"interface": doc, "key": "api"}
		}, func(t *testing.T, out any) {
			doc = child(t, out)
			// Empty maps are omitted from the document, so absent == removed.
			if srcs, _ := doc["sources"].(map[string]any); len(srcs) != 0 {
				t.Errorf("expected the source removed, got %#v", srcs)
			}
			bindings, _ := doc["bindings"].(map[string]any)
			if _, still := bindings["getPing.api"]; still {
				t.Error("expected the source's binding removed with it")
			}
		}},
	}

	for _, step := range chain {
		t.Run("edit_"+step.name, func(t *testing.T) {
			if step.name != "newInterface" && doc == nil {
				t.Fatal("chain broken: no document from the previous step")
			}
			out := invokeConformant(t, step.op, step.input())
			step.check(t, out)
		})
	}
}

// repoRoot locates the module root (two levels above internal/cmd).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
