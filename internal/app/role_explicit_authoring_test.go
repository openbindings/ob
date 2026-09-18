package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
	"github.com/openbindings/openbindings-go/synthesize"
)

// Explicit source-authoring selection: --registration on inspect, synthesize
// and pull selects exactly one enrolled registration for that role. There is no
// native, locator, display-name or alternate-provider fallback, the selected
// provider's query and work stay on one retained runtime, and unrelated
// registrations receive no query or workload.
func TestRoleExplicitAuthoringSelection(t *testing.T) {
	for _, role := range []DelegateCapability{CapInspect, CapSynthesize} {
		for _, token := range []string{"example.work@1", openapi.BindingSpecOpenAPI31} {
			t.Run(string(role)+"/"+token, func(t *testing.T) {
				r, _ := migrationTestRegistry(t)
				selected, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "selected"), Roles: []string{"invoke", "synthesize", "inspect"}, RolePreferences: json.RawMessage(`{"synthesize":-1e400,"inspect":-1e400}`)})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "other"), Roles: []string{"invoke", "synthesize", "inspect"}, RolePreferences: json.RawMessage(`{"synthesize":1e400,"inspect":1e400}`)}); err != nil {
					t.Fatal(err)
				}
				queries := &roleTestInvoker{result: func(selector string, input any) any {
					if strings.Contains(selector, "checkBindingSpecs") {
						return roleOperationVerdicts(t, input, true)
					}
					if strings.Contains(selector, "inspectSource") {
						return map[string]any{"targets": []any{map[string]any{"selector": "#/x", "operationKey": "x"}}, "exhaustive": true}
					}
					return map[string]any{"openbindings": "0.2.0", "name": "delegated", "operations": map[string]any{"delegated": map[string]any{}}}
				}}
				work := &roleFrameTestInvoker{}
				installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
				ctx := WithExplicitRegistration(t.Context(), role, selected.ID)
				source := openbindings.Source{BindingSpec: token, Content: json.RawMessage(`{"openapi":"3.1.0","info":{"title":"x","version":"1"},"paths":{}}`)}
				switch role {
				case CapInspect:
					result, err := InspectSource(ctx, &source)
					if err != nil || len(result.Targets) != 1 {
						t.Fatalf("explicit inspect: %v %v", result, err)
					}
				case CapSynthesize:
					result, err := SynthesizeInterfaceFromSource(ctx, &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: token, Content: source.Content}}})
					if err != nil || result.Name != "delegated" {
						t.Fatalf("explicit synthesize: %v %v", result, err)
					}
					derived, err := deriveFromSourceContext(ctx, source, "key", "")
					if err != nil || len(derived.Operations) != 1 {
						t.Fatalf("explicit pull derivation: %+v %v", derived, err)
					}
				}
				queries.mu.Lock()
				defer queries.mu.Unlock()
				work.mu.Lock()
				defer work.mu.Unlock()
				for _, selector := range queries.calls {
					if !strings.HasSuffix(selector, "#selected") {
						t.Fatalf("call outside the selected registration: %s", selector)
					}
				}
				if len(work.selectors) != 0 {
					t.Fatalf("invoke workload ran during authoring: %v", work.selectors)
				}
				querySelectors := strings.Join(queries.calls, " ")
				if strings.Count(querySelectors, "checkBindingSpecs") < 1 || !strings.Contains(querySelectors, capabilityOperation[role]) {
					t.Fatalf("expected one support query and the role workload on the selected provider, got %v", queries.calls)
				}
			})
		}
	}
}

func TestRoleExplicitAuthoringRefusals(t *testing.T) {
	for _, mode := range []string{"missing", "copied-id", "wrong-role", "unsupported", "malformed", "unbound", "no-token"} {
		t.Run(mode, func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			roles := []string{"invoke", "synthesize", "inspect"}
			if mode == "wrong-role" {
				roles = []string{"synthesize"}
			}
			provider := roleDiagnosticProvider(t, "selected")
			if mode == "unbound" {
				// The accepted interface itself: admissible for inspect, but it
				// carries no bindings and so has no executable realization.
				expected, _ := RequirementInterface(CapInspect)
				provider, _ = jsonvalue.Marshal(expected)
				roles = []string{"inspect"}
			}
			record, err := r.register(RoleRegistrationInput{Interface: provider, Roles: roles})
			if err != nil {
				t.Fatal(err)
			}
			id := record.ID
			switch mode {
			case "missing":
				id = "dlg_00000000000000000000000000000000_1"
			case "copied-id":
				// A registration ID copied from another environment is not authority here.
				other := &roleRegistry{path: envConfigTestEnv(t), catalogue: r.catalogue}
				foreign, err := other.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "foreign"), Roles: roles})
				if err != nil {
					t.Fatal(err)
				}
				id = foreign.ID
				r = &roleRegistry{path: r.path, catalogue: r.catalogue}
				t.Chdir(r.path + "/..")
			}
			queries := &roleTestInvoker{result: func(selector string, input any) any {
				switch mode {
				case "unsupported":
					return roleOperationVerdicts(t, input, false)
				case "malformed":
					return []any{map[string]any{"supported": "yes"}}
				}
				return roleOperationVerdicts(t, input, true)
			}}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			token := "example.work@1"
			if mode == "no-token" {
				token = ""
			}
			ctx := WithExplicitRegistration(t.Context(), CapInspect, id)
			_, err = InspectSource(ctx, &openbindings.Source{BindingSpec: token, Content: json.RawMessage(`{}`)})
			if err == nil {
				t.Fatal("explicit selection fell back or succeeded without support")
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			wantQueries := 1
			if mode == "missing" || mode == "copied-id" || mode == "wrong-role" || mode == "unbound" || mode == "no-token" {
				wantQueries = 0
			}
			if len(queries.calls) != wantQueries || len(work.selectors) != 0 {
				t.Fatalf("%s: queries=%v work=%v err=%v", mode, queries.calls, work.selectors, err)
			}
			for _, selector := range queries.calls {
				if strings.Contains(selector, "inspectSource") {
					t.Fatalf("%s: workload ran after a negative/failed assessment: %v", mode, queries.calls)
				}
			}
		})
	}
}

func TestDetectSourceCandidatesViaRegistration(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	record, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "inspector"), Roles: []string{"inspect"}})
	if err != nil {
		t.Fatal(err)
	}
	synthOnly, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "synth"), Roles: []string{"synthesize"}})
	if err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(selector string, input any) any {
		switch {
		case strings.Contains(selector, "listBindingSpecs"):
			return []any{map[string]any{"bindingSpec": "example.custom@1"}, map[string]any{"bindingSpec": "example.other@1"}, map[string]any{"bindingSpec": "example.custom@1"}}
		case strings.Contains(selector, "checkBindingSpecs"):
			raw, _ := jsonvalue.Marshal(input)
			var request struct{ BindingSpecs []string }
			_ = jsonvalue.Unmarshal(raw, &request)
			rows := []any{}
			for _, token := range request.BindingSpecs {
				rows = append(rows, map[string]any{"bindingSpec": token, "supported": token == "example.custom@1"})
			}
			return rows
		default:
			raw, _ := jsonvalue.Marshal(input)
			if !strings.Contains(string(raw), `"example.custom@1"`) || !strings.Contains(string(raw), `"content"`) {
				t.Errorf("inspection probe must carry the supported token and the file content: %s", raw)
			}
			return map[string]any{"targets": []any{map[string]any{"selector": "#/a", "operationKey": "a"}, map[string]any{"selector": "#/b", "operationKey": "b"}}, "exhaustive": true}
		}
	}}
	work := &roleFrameTestInvoker{}
	installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work))
	file := t.TempDir() + "/thing.custom"
	if err := writeFile(file, []byte("custom artifact bytes")); err != nil {
		t.Fatal(err)
	}
	claims, err := DetectSourceCandidatesVia(t.Context(), record.ID, file)
	if err != nil || len(claims) != 1 || claims[0].BindingSpec != "example.custom@1" || claims[0].DelegateID != record.ID || claims[0].OperationCount != 2 {
		t.Fatalf("claims: %+v %v", claims, err)
	}
	queries.mu.Lock()
	calls := strings.Join(queries.calls, " ")
	queries.mu.Unlock()
	if strings.Count(calls, "listBindingSpecs") != 1 || strings.Count(calls, "checkBindingSpecs") != 1 || strings.Count(calls, "inspectSource") != 1 || strings.Contains(calls, "#synth") {
		t.Fatalf("detection calls: %v", queries.calls)
	}
	if _, err := DetectSourceCandidatesVia(t.Context(), synthOnly.ID, file); err == nil || !strings.Contains(err.Error(), "not enrolled") {
		t.Fatalf("synthesize-only registration must not detect: %v", err)
	}
	if _, err := DetectSourceCandidatesVia(t.Context(), "exec:must-not-run", file); err == nil {
		t.Fatal("locator accepted as a registration")
	}
	work.mu.Lock()
	defer work.mu.Unlock()
	if len(work.selectors) != 0 {
		t.Fatalf("detection ran invoke workload: %v", work.selectors)
	}
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
