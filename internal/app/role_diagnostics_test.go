package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func roleDiagnosticProvider(t *testing.T, tag string) json.RawMessage {
	t.Helper()
	provider, _, err := openbindings.ValidateDocument(roleFrameProvider(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []DelegateCapability{CapSynthesize, CapInspect} {
		expected, err := RequirementInterface(role)
		if err != nil {
			t.Fatal(err)
		}
		extra, _, err := openbindings.ValidateDocument(roleTestProvider(t, expected))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range extra.Operations {
			provider.Operations[k] = v
		}
		for k, v := range extra.Bindings {
			provider.Bindings[k] = v
		}
		for k, v := range extra.Schemas {
			provider.Schemas[k] = v
		}
	}
	for k, b := range provider.Bindings {
		b.Selector += "#" + tag
		provider.Bindings[k] = b
	}
	provider.Description = "sensitive-provider-document-must-not-appear-in-diagnostics"
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRoleDiagnosticPolicies(t *testing.T) {
	for _, test := range []struct {
		role    DelegateCapability
		path    string
		builtin bool
	}{
		{CapInvoke, "", false}, {CapInvoke, "ranked", false}, {CapInvoke, "native-first", true},
		{CapSynthesize, "", true}, {CapInspect, "", true},
	} {
		t.Run(string(test.role)+"/"+test.path, func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			record, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "selected"), Roles: []string{"invoke", "synthesize", "inspect"}, RolePreferences: json.RawMessage(`{"invoke":1e400,"synthesize":1e400,"inspect":1e400}`)})
			if err != nil {
				t.Fatal(err)
			}
			queries := &roleTestInvoker{result: func(selector string, input any) any {
				if selector != "provider."+checkBindingSpecOperationNames(test.role)[0]+"#selected" {
					t.Errorf("unexpected selector: %s", selector)
				}
				if equal, e := jsonvalue.Equal(input, map[string]any{"bindingSpecs": []string{openapi.BindingSpecOpenAPI31}}); e != nil || !equal {
					t.Errorf("not token-only assessment: %v", input)
				}
				return roleOperationVerdicts(t, input, true)
			}}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			out, err := ResolveRoleDelegate(t.Context(), RoleResolutionInput{Role: test.role, BindingSpec: openapi.BindingSpecOpenAPI31, Path: test.path})
			if err != nil || out == nil {
				t.Fatalf("diagnostic: %v %v", out, err)
			}
			wantPath := test.path
			if wantPath == "" {
				wantPath = "native-first"
				if test.role == CapInvoke {
					wantPath = "ranked"
				}
			}
			if !out.Available || out.Builtin != test.builtin || out.Path != wantPath || out.Role != test.role || out.BindingSpec != openapi.BindingSpecOpenAPI31 {
				t.Fatalf("wrong diagnostic: %+v", out)
			}
			if test.builtin && out.RegistrationID != "" || !test.builtin && out.RegistrationID != record.ID {
				t.Fatalf("invented or wrong registration: %+v", out)
			}
			raw, _ := jsonvalue.Marshal(out)
			if strings.Contains(string(raw)+out.Render(), "sensitive-provider-document") || strings.Contains(string(raw), "interface") {
				t.Fatal("diagnostic leaked retained value")
			}
			queries.mu.Lock()
			count := len(queries.calls)
			queries.mu.Unlock()
			wantQueries := 1
			if test.builtin {
				wantQueries = 0
			}
			work.mu.Lock()
			defer work.mu.Unlock()
			if count != wantQueries || len(work.selectors) != 0 {
				t.Fatalf("queries=%d work=%v", count, work.selectors)
			}
		})
	}
}

func TestRoleDiagnosticRefusals(t *testing.T) {
	for _, mode := range []string{"unknown-role", "empty-token", "unknown-path", "ranked-authoring", "explicit-without-id", "explicit-and-ranked", "missing-id", "wrong-role", "unbound", "false", "malformed", "legacy", "corrupt", "cancelled", "pinned"} {
		t.Run(mode, func(t *testing.T) {
			r, raw := migrationTestRegistry(t)
			roles := []string{"invoke"}
			if mode == "wrong-role" {
				roles = []string{"inspect"}
			}
			provider := roleDiagnosticProvider(t, "selected")
			if mode == "unbound" {
				iface, _, err := openbindings.ValidateDocument(provider)
				if err != nil {
					t.Fatal(err)
				}
				iface.Bindings = nil
				provider, err = jsonvalue.Marshal(iface)
				if err != nil {
					t.Fatal(err)
				}
			}
			record, err := r.register(RoleRegistrationInput{Interface: provider, Roles: roles})
			if err != nil {
				t.Fatal(err)
			}
			input := RoleResolutionInput{Role: CapInvoke, BindingSpec: openapi.BindingSpecOpenAPI31, RegistrationID: record.ID}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			switch mode {
			case "unknown-role":
				input.Role = "storage"
			case "empty-token":
				input.BindingSpec = ""
			case "unknown-path":
				input.RegistrationID = ""
				input.Path = "imaginary"
			case "ranked-authoring":
				input.Role = CapInspect
				input.RegistrationID = ""
				input.Path = "ranked"
			case "explicit-without-id":
				input.RegistrationID = ""
				input.Path = "explicit"
			case "explicit-and-ranked":
				input.Path = "ranked"
			case "missing-id":
				input.RegistrationID = "https://provider.example.invalid/obi"
			case "legacy":
				seedLegacyMigration(t, r, raw)
			case "corrupt":
				if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), []byte(`{"delegateRegistry":null}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			case "pinned":
				ctx = context.WithValue(ctx, nativeInvocationRoutingKey{}, true)
			}
			queries := &roleTestInvoker{result: func(_ string, value any) any {
				if mode == "malformed" {
					return []any{map[string]any{"bindingSpec": input.BindingSpec}}
				}
				return roleOperationVerdicts(t, value, false)
			}}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			out, err := ResolveRoleDelegate(ctx, input)
			if mode == "false" {
				if err != nil || out == nil || out.Available || out.Builtin || out.RegistrationID != "" || out.Path != "explicit" {
					t.Fatalf("false fell back or failed: %+v %v", out, err)
				}
			} else if err == nil || out != nil {
				t.Fatalf("refusal swallowed: %+v %v", out, err)
			}
			wantQueries := 0
			if mode == "false" || mode == "malformed" {
				wantQueries = 1
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			if len(queries.calls) != wantQueries || len(work.selectors) != 0 {
				t.Fatalf("unexpected disclosure: %v %v", queries.calls, work.selectors)
			}
		})
	}
}

func TestRoleDiagnosticNoEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, role := range DelegateCapabilities {
		out, err := ResolveRoleDelegate(t.Context(), RoleResolutionInput{Role: role, BindingSpec: openapi.BindingSpecOpenAPI31})
		if err != nil || out == nil || !out.Available || !out.Builtin || out.RegistrationID != "" {
			t.Fatalf("native without environment: %+v %v", out, err)
		}
		out, err = ResolveRoleDelegate(t.Context(), RoleResolutionInput{Role: role, BindingSpec: "example.unsupported@1"})
		if err != nil || out == nil || out.Available {
			t.Fatalf("empty inventory: %+v %v", out, err)
		}
	}
}

func TestRoleExplicitSelectionRetainsWork(t *testing.T) {
	for _, role := range DelegateCapabilities {
		t.Run(string(role), func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			wanted, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "wanted"), Roles: []string{string(role)}, RolePreferences: json.RawMessage(`{"` + string(role) + `":-1e400}`)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "other"), Roles: []string{string(role)}, RolePreferences: json.RawMessage(`{"` + string(role) + `":1e400}`)}); err != nil {
				t.Fatal(err)
			}
			query := "provider." + checkBindingSpecOperationNames(role)[0] + "#wanted"
			queries := &roleTestInvoker{result: func(selector string, value any) any {
				if selector == query {
					if err := r.unregister(wanted.ID); err != nil {
						t.Error(err)
					}
					return roleOperationVerdicts(t, value, true)
				}
				if selector != "provider."+capabilityOperation[role]+"#wanted" {
					t.Errorf("wrong provider/decoy: %s", selector)
				}
				if role == CapInspect {
					return map[string]any{"targets": []any{}, "exhaustive": true}
				}
				return map[string]any{"openbindings": "0.2.0", "operations": map[string]any{}, "name": "wanted"}
			}}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			input := RoleResolutionInput{Role: role, BindingSpec: openapi.BindingSpecOpenAPI31, RegistrationID: wanted.ID}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			selected, path, err := resolveRoleSelection(ctx, input)
			if err != nil || selected == nil || selected.Builtin || selected.Runtime.candidate.Record.ID != wanted.ID || path != "explicit" {
				t.Fatalf("explicit selection: %+v %s %v", selected, path, err)
			}
			if role == CapInvoke {
				out := invokeViaGuardedDelegate(ctx, &roleBindingInvoker{spec: input.BindingSpec, route: selected.Work}, InvocationInput{Source: InvokeSource{BindingSpec: input.BindingSpec, Content: json.RawMessage(`{}`)}, Selector: "echo", Input: json.Number("1e400")})
				if equal, e := jsonvalue.Equal(out.Output, json.Number("1e400")); out.Error != nil || e != nil || !equal {
					t.Fatalf("work: %+v", out)
				}
			} else {
				source := map[string]any{"bindingSpec": input.BindingSpec, "content": map[string]any{}}
				var workInput any = map[string]any{"source": source}
				if role == CapSynthesize {
					workInput = map[string]any{"sources": []any{source}}
				}
				if _, err := selected.unary(ctx, workInput); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ResolveRoleDelegate(ctx, input); err == nil {
				t.Fatal("removed explicit ID fell back to another provider/native")
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			wantQueries, wantWork := 2, 0
			if role == CapInvoke {
				wantQueries, wantWork = 1, 1
			}
			if len(queries.calls) != wantQueries || len(work.selectors) != wantWork {
				t.Fatalf("query/work counts: %v %v", queries.calls, work.selectors)
			}
			if wantWork == 1 && work.selectors[0] != "qualified-work#wanted" {
				t.Fatal(work.selectors)
			}
		})
	}
}

func TestRoleExplicitSelectionRoleBoundary(t *testing.T) {
	for _, role := range DelegateCapabilities {
		t.Run(string(role), func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			other := "invoke"
			if role == CapInvoke {
				other = "inspect"
			}
			record, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "unenrolled"), Roles: []string{other}})
			if err != nil {
				t.Fatal(err)
			}
			queries := &roleTestInvoker{}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			for _, id := range []string{record.ID, "exec:must-not-run", "https://provider.example.invalid/obi", "ob"} {
				selected, _, err := resolveRoleSelection(t.Context(), RoleResolutionInput{Role: role, BindingSpec: openapi.BindingSpecOpenAPI31, RegistrationID: id})
				if err == nil || selected != nil {
					t.Fatalf("unenrolled/locator/self hint accepted: %v %v", selected, err)
				}
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			if len(queries.calls) != 0 || len(work.selectors) != 0 {
				t.Fatalf("unenrolled provider called: %v %v", queries.calls, work.selectors)
			}
		})
	}
}

func TestRoleSourceProvenanceDoesNotEnroll(t *testing.T) {
	for _, role := range DelegateCapabilities {
		for _, hintKind := range []string{"locator", "registration-id"} {
			t.Run(string(role)+"/"+hintKind, func(t *testing.T) {
				r, _ := migrationTestRegistry(t)
				other := "inspect"
				if role == CapInspect {
					other = "synthesize"
				}
				record, err := r.register(RoleRegistrationInput{Interface: roleDiagnosticProvider(t, "unenrolled"), Roles: []string{other}})
				if err != nil {
					t.Fatal(err)
				}
				hint := "exec:must-not-run"
				if hintKind == "registration-id" {
					hint = record.ID
				}
				source := openbindings.Source{BindingSpec: "example.work@1", Content: json.RawMessage(`{}`)}
				if err := SetSourceMeta(&source, SourceMeta{Delegate: hint}); err != nil {
					t.Fatal(err)
				}
				queries := &roleTestInvoker{}
				work := &roleFrameTestInvoker{}
				installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work))
				switch role {
				case CapSynthesize:
					_, err = DeriveFromSource(source, "work", "")
				case CapInspect:
					_, err = InspectSource(t.Context(), &source)
				case CapInvoke:
					iface := roleOperationFixture()
					iface.Sources["work"] = source
					_, err = invokeOnInterface(t.Context(), iface, "echo", "", "secret-workload", nil)
				}
				if err == nil {
					t.Fatal("source metadata activated an unenrolled role")
				}
				queries.mu.Lock()
				defer queries.mu.Unlock()
				work.mu.Lock()
				defer work.mu.Unlock()
				if len(queries.calls) != 0 || len(work.selectors) != 0 {
					t.Fatalf("hint disclosed work/query: %v %v", queries.calls, work.selectors)
				}
			})
		}
	}
}
