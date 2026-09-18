package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
	"github.com/openbindings/openbindings-go/synthesize"
)

// These enter through the production authoring functions, not a private selector.
// The independently implemented binding handler counts exact canonical keys.
func TestRoleAuthoringEntrypoints(t *testing.T) {
	for _, role := range []DelegateCapability{CapSynthesize, CapInspect} {
		t.Run(string(role), func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			expected, err := RequirementInterface(role)
			if err != nil {
				t.Fatal(err)
			}
			raw := roleTestProvider(t, expected)
			record, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{string(role)}})
			if err != nil {
				t.Fatal(err)
			}
			query := "provider." + checkBindingSpecOperationNames(role)[0]
			work := "provider." + capabilityOperation[role]
			handler := &roleTestInvoker{result: func(selector string, value any) any {
				switch selector {
				case query:
					return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
				case work:
					if role == CapInspect {
						return map[string]any{"targets": []any{}, "exhaustive": true}
					}
					return map[string]any{"openbindings": "0.2.0", "operations": map[string]any{}, "name": "selected-provider"}
				default:
					t.Errorf("unadmitted/decoy operation: %s", selector)
					return nil
				}
			}}
			old := defaultInvokerOverride
			defaultInvokerOverride = invoke.NewOperationInvoker(handler)
			resetNativeTokens()
			t.Cleanup(func() { defaultInvokerOverride = old; resetNativeTokens() })
			call := func() error {
				if role == CapInspect {
					out, err := InspectSource(t.Context(), &openbindings.Source{BindingSpec: "example.work@1", Content: json.RawMessage(`{}`)})
					if err == nil && (out == nil || !out.Exhaustive) {
						t.Fatal("wrong inspection")
					}
					return err
				}
				out, err := SynthesizeInterfaceFromSource(t.Context(), &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: "example.work@1", Content: json.RawMessage(`{}`)}}})
				if err == nil && (out == nil || out.Name != "selected-provider") {
					t.Fatal("wrong synthesis")
				}
				return err
			}
			if err := call(); err != nil {
				t.Fatalf("registered retained provider was not used: %v", err)
			}
			handler.mu.Lock()
			if len(handler.calls) != 2 || handler.calls[0] != query || handler.calls[1] != work {
				t.Errorf("query/work correspondence: %v", handler.calls)
			}
			handler.mu.Unlock()
			if err := r.unregister(record.ID); err != nil {
				t.Fatal(err)
			}
			if err := call(); err == nil {
				t.Fatal("removed provider remained usable")
			}
			handler.mu.Lock()
			defer handler.mu.Unlock()
			if len(handler.calls) != 2 {
				t.Fatal("removed provider was queried or invoked")
			}
		})
	}
}

func TestRoleAuthoringNoImplicitEnrollment(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	expected, _ := RequirementInterface(CapInspect)
	provider, _ := openbindings.ValidateDocument(roleTestProvider(t, expected))
	other, _ := RequirementInterface(CapSynthesize)
	extra, _ := openbindings.ValidateDocument(roleTestProvider(t, other))
	for key, op := range extra.Operations {
		provider.Operations[key] = op
	}
	for key, binding := range extra.Bindings {
		provider.Bindings[key] = binding
	}
	for key, schema := range extra.Schemas {
		provider.Schemas[key] = schema
	}
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{"inspect"}}); err != nil {
		t.Fatal(err)
	}
	handler := &roleTestInvoker{}
	old := defaultInvokerOverride
	defaultInvokerOverride = invoke.NewOperationInvoker(handler)
	resetNativeTokens()
	t.Cleanup(func() { defaultInvokerOverride = old; resetNativeTokens() })
	_, err = SynthesizeInterfaceFromSource(t.Context(), &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: "example.work@1"}}})
	if err == nil {
		t.Fatal("unrequested role was activated")
	}
	if len(handler.calls) != 0 {
		t.Fatal("unrequested role received provider calls")
	}
}

func TestRoleAuthoringRefusesLegacyState(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	seedLegacyMigration(t, r, provider)
	_, _, err := synthesizeViaDelegate(t.Context(), &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: "example.work@1"}}})
	if err == nil || !strings.Contains(err.Error(), "explicit migration") {
		t.Fatalf("legacy state not refused: %v", err)
	}
}

func TestRoleAuthoringNativeFirst(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	for _, role := range []DelegateCapability{CapSynthesize, CapInspect} {
		expected, _ := RequirementInterface(role)
		if _, err := r.register(RoleRegistrationInput{Interface: roleTestProvider(t, expected), Roles: []string{string(role)}, RolePreferences: json.RawMessage(`{"` + string(role) + `":1e400}`)}); err != nil {
			t.Fatal(err)
		}
	}
	handler := &roleTestInvoker{}
	old := defaultInvokerOverride
	defaultInvokerOverride = invoke.NewOperationInvoker(handler, openapi.NewAdapter())
	resetNativeTokens()
	t.Cleanup(func() { defaultInvokerOverride = old; resetNativeTokens() })
	doc := json.RawMessage(`{"openapi":"3.1.0","info":{"title":"Native","version":"1"},"paths":{}}`)
	if _, err := SynthesizeInterfaceFromSource(t.Context(), &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: openapi.BindingSpecOpenAPI31, Content: doc}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectSource(t.Context(), &openbindings.Source{BindingSpec: openapi.BindingSpecOpenAPI31, Content: doc}); err != nil {
		t.Fatal(err)
	}
	if len(handler.calls) != 0 {
		t.Fatal("native-first path queried an external provider")
	}
	// Unreadable new-format configuration is not an empty registry for external work.
	if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), []byte(`{"delegateRegistry":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := inspectViaDelegate(t.Context(), &openbindings.Source{BindingSpec: "example.work@1"})
	if err == nil {
		t.Fatal("corrupt registry silently ignored")
	}
}

func TestRoleAuthoringNoEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("OB_CONFIG_DIR", filepath.Join(t.TempDir(), "absent"))
	selected, err := selectInstalledRole(t.Context(), CapSynthesize, "example.work@1", roleNativeFirst)
	if err != nil || selected != nil {
		t.Fatalf("missing environment is not an empty inventory: %v %v", selected, err)
	}
	ResetDefaultInvoker()
	t.Cleanup(ResetDefaultInvoker)
	runtime := defaultCLIRuntime()
	runtime.Synthesizer = additionalDetectionClaim{runtime.Synthesizer}
	_, err = SynthesizeInterfaceFromSource(t.Context(), &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: "openbindings.test@1", Content: json.RawMessage(`{}`)}}})
	if err != nil {
		t.Fatalf("installed synthesis-only provider was not usable without an environment: %v", err)
	}
}
