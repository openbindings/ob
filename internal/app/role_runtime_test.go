package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

const roleRuntimeTestSpec = "example.delegate-manager-test@1"

// A real binding runtime below the SDK operation/dependency machinery. It has
// no registry knowledge and records the exact selected selector and input.
type roleTestInvoker struct {
	mu     sync.Mutex
	calls  []string
	inputs []any
	result func(string, any) any
}

func (r *roleTestInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: roleRuntimeTestSpec}}
}
func (r *roleTestInvoker) CheckBindingSpecs(tokens []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(tokens, r.BindingSpecs())
}
func (r *roleTestInvoker) InvokeBinding(ctx context.Context, args *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	call := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		value, err := call.ReadInput(ctx)
		if err != nil {
			call.FireError(invoke.AsInvocationError(err))
			return
		}
		r.mu.Lock()
		r.calls = append(r.calls, args.Selector)
		r.inputs = append(r.inputs, value)
		r.mu.Unlock()
		_ = call.CloseInput()
		result := value
		if r.result != nil {
			result = r.result(args.Selector, value)
		}
		// A binding runtime emits JSON values, not an application-specific Go
		// struct projection. Keep this independent test runtime on that boundary.
		encoded, err := jsonvalue.Marshal(result)
		if err != nil {
			call.FireError(invoke.AsInvocationError(err))
			return
		}
		if err := jsonvalue.Unmarshal(encoded, &result); err != nil {
			call.FireError(invoke.AsInvocationError(err))
			return
		}
		_ = call.EmitOutput(result)
		call.CloseOutput()
	}()
	return call
}

func roleTestProvider(t *testing.T, expected *openbindings.Interface) json.RawMessage {
	t.Helper()
	provider := *expected
	provider.Operations = map[string]openbindings.Operation{}
	provider.Sources = map[string]openbindings.Source{"test": {BindingSpec: roleRuntimeTestSpec, Content: json.RawMessage(`{"test":true}`)}}
	provider.Bindings = map[string]openbindings.BindingEntry{}
	for key, op := range expected.Operations {
		canonical := "provider." + key
		op.Aliases = []string{key}
		provider.Operations[canonical] = op
		provider.Bindings[canonical] = openbindings.BindingEntry{Operation: canonical, Source: "test", Selector: canonical}
		// Deliberately insert a bare-name decoy with the same shape. Only the
		// expected qualified alias admits the real provider operation.
		bare := key[strings.LastIndex(key, ".")+1:]
		op.Aliases = nil
		provider.Operations[bare] = op
		provider.Bindings[bare] = openbindings.BindingEntry{Operation: bare, Source: "test", Selector: "DECOY." + bare}
	}
	raw, err := jsonvalue.Marshal(&provider)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRoleRuntimeComposition(t *testing.T) {
	for _, capability := range DelegateCapabilities {
		t.Run(string(capability), func(t *testing.T) {
			catalogue, err := defaultRoleCatalogue()
			if err != nil {
				t.Fatal(err)
			}
			expected, err := RequirementInterface(capability)
			if err != nil {
				t.Fatal(err)
			}
			r := &roleRegistry{path: envConfigTestEnv(t), catalogue: catalogue}
			record, err := r.register(RoleRegistrationInput{Interface: roleTestProvider(t, expected), Roles: []string{string(capability)}})
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := r.candidates(string(capability))
			if err != nil || len(candidates) != 1 {
				t.Fatalf("candidates: %v %v", candidates, err)
			}
			binding := &roleTestInvoker{result: func(selector string, value any) any {
				return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
			}}
			runtime, err := newRoleRuntime(catalogue, candidates[0], invoke.NewOperationInvoker(binding), nil)
			if err != nil {
				t.Fatal(err)
			}
			consumer := runtime.session.Consumer().InterfaceSnapshot()
			if len(consumer.Dependencies) != len(expected.Operations) {
				t.Fatal("missing actual consumption dependencies")
			}
			roles, _ := catalogue.list()
			for _, role := range roles {
				for _, raw := range role.AcceptedInterfaces {
					iface, _ := openbindings.ValidateDocument(raw)
					if len(iface.Dependencies) != 0 {
						t.Fatal("consumer mutated advertised expectation")
					}
				}
			}
			for key := range expected.Operations {
				route, err := runtime.route(t.Context(), key)
				if err != nil {
					t.Fatal(err)
				}
				if route.ProviderOperationKey != "provider."+key {
					t.Fatalf("wrong admitted key: %+v", route)
				}
			}
			key := checkBindingSpecOperationNames(capability)[0]
			out, err := runtime.unary(t.Context(), key, map[string]any{"bindingSpecs": []string{"example.work@1"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeRoleSupport(out, []string{"example.work@1"}); err != nil {
				t.Fatal(err)
			}
			binding.mu.Lock()
			if len(binding.calls) != 1 || binding.calls[0] != "provider."+key {
				t.Errorf("unexpected calls: %v", binding.calls)
			}
			binding.mu.Unlock()
			if _, err := runtime.route(t.Context(), "checkBindingSpecs"); err == nil {
				t.Fatal("bare decoy accepted outside admitted dependency")
			}
			for _, other := range DelegateCapabilities {
				if other == capability {
					continue
				}
				rows, err := r.candidates(string(other))
				if err != nil || len(rows) != 0 {
					t.Fatal("registration leaked into an unenrolled role")
				}
			}
			if record.ID != runtime.candidate.Record.ID {
				t.Fatal("provider registration identity lost")
			}
		})
	}
}

func TestRoleRuntimeLifecycle(t *testing.T) {
	r := testRoleRegistry(t)
	expected, _ := openbindings.ValidateDocument([]byte(registryTestInterface))
	input := RoleRegistrationInput{Interface: roleTestProvider(t, expected), Roles: []string{"A"}}
	a, err := r.register(input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.register(input)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := r.candidates("A")
	if err != nil {
		t.Fatal(err)
	}
	binding := &roleTestInvoker{}
	one, err := newRoleRuntime(r.catalogue, rows[0], invoke.NewOperationInvoker(binding), nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := newRoleRuntime(r.catalogue, rows[1], invoke.NewOperationInvoker(binding), nil)
	if err != nil {
		t.Fatal(err)
	}
	if one.provider.Key() == two.provider.Key() {
		t.Fatal("identical documents collapsed provider identities")
	}
	retained, err := one.route(t.Context(), "example.read")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.unregister(a.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = r.candidates("A")
	if err != nil || len(rows) != 1 || rows[0].Record.ID != b.ID {
		t.Fatal("new lookup retained removed registration")
	}
	call := retained.Invoke(t.Context())
	defer call.Cancel()
	if err := call.Write(t.Context(), "retained route works"); err != nil {
		t.Fatal(err)
	}
	_ = call.Close()
	value, err := invoke.Single(t.Context(), call.Outputs())
	if err != nil || value != "retained route works" {
		t.Fatalf("retained route disposed: %v %v", value, err)
	}
	if err := r.prefer(b.ID, "A", func() *json.Number { n := json.Number("9007199254740993"); return &n }()); err != nil {
		t.Fatal(err)
	}
	newRows, err := r.candidates("A")
	if err != nil {
		t.Fatal(err)
	}
	three, err := newRoleRuntime(r.catalogue, newRows[0], invoke.NewOperationInvoker(binding), nil)
	if err != nil {
		t.Fatal(err)
	}
	if three.provider.Key() == two.provider.Key() {
		t.Fatal("configuration revision was not captured")
	}
	if two.candidate.preference("") != "0" || three.candidate.preference("") != "9007199254740993" {
		t.Fatal("retained preference mutated or rounded")
	}
}

func TestRoleRuntimeOwnsCandidateSnapshot(t *testing.T) {
	r := testRoleRegistry(t)
	expected, _ := openbindings.ValidateDocument([]byte(registryTestInterface))
	_, err := r.register(RoleRegistrationInput{
		Interface: roleTestProvider(t, expected), Roles: []string{"A"},
		RolePreferences: json.RawMessage(`{"A":7}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := r.candidates("A")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newRoleRuntime(r.catalogue, rows[0], invoke.NewOperationInvoker(&roleTestInvoker{}), nil)
	if err != nil {
		t.Fatal(err)
	}
	admitted := runtime.candidate.Admission.Operations["example.read"]
	rows[0].Admission.Operations["example.read"] = "not-admitted"
	rows[0].Record.RolePreferences["A"] = "999"
	rows[0].Record.Roles[0] = "different-role"
	rows[0].Record.Interface[0] = '!'
	if runtime.candidate.Admission.Operations["example.read"] != admitted || runtime.candidate.preference("") != "7" || runtime.candidate.Record.Roles[0] != "A" || runtime.candidate.Record.Interface[0] != '{' {
		t.Fatal("caller mutation changed a retained runtime snapshot")
	}
	if _, err := runtime.route(t.Context(), "example.read"); err != nil {
		t.Fatalf("caller mutation changed retained correspondence: %v", err)
	}
}

func TestRoleRuntimeSupportRefusals(t *testing.T) {
	for _, value := range []any{nil, map[string]any{}, []any{}, []any{map[string]any{"bindingSpec": "x"}}, []any{map[string]any{"bindingSpec": "x", "supported": "false"}}, []any{map[string]any{"bindingSpec": "y", "supported": false}}} {
		if _, err := decodeRoleSupport(value, []string{"x"}); err == nil {
			t.Fatalf("malformed verdict accepted: %#v", value)
		}
	}
	verdicts, err := decodeRoleSupport([]any{map[string]any{"bindingSpec": "x", "supported": false}}, []string{"x"})
	if err != nil || verdicts[0].Supported {
		t.Fatal("authoritative false not preserved")
	}
	r := testRoleRegistry(t)
	_, err = r.register(testRegistration("A")) // admitted but deliberately unbound
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := r.candidates("A")
	runtime, err := newRoleRuntime(r.catalogue, rows[0], invoke.NewOperationInvoker(&roleTestInvoker{}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.route(t.Context(), "example.read"); err == nil {
		t.Fatal("unbound provider treated as executable")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := runtime.route(cancelled, "example.read"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
}

func TestRoleRuntimeSelection(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	expected, _ := RequirementInterface(CapInvoke)
	provider := roleTestProvider(t, expected)
	ids := []string{}
	handlers := map[string]*roleTestInvoker{}
	for i, token := range []string{"example.work@1", "example.work@2", "example.work@1", "example.work@3", "example.work@1"} {
		record, err := r.register(RoleRegistrationInput{Interface: provider, Roles: []string{"invoke"}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, record.ID)
		n := json.Number([]string{"9007199254740992", "1e400", "9007199254740993", "-1e400", "9007199254740993"}[i])
		if err := r.prefer(record.ID, "invoke", &n); err != nil {
			t.Fatal(err)
		}
		handlers[record.ID] = &roleTestInvoker{result: func(_ string, value any) any {
			input, _ := decodeOutput[struct {
				BindingSpecs []string `json:"bindingSpecs"`
			}](value)
			return openbindings.CheckBindingSpecs(input.BindingSpecs, []openbindings.BindingSpecInfo{{BindingSpec: token}})
		}}
	}
	factory := func(candidate roleCandidate) (invoke.ProviderRuntime, invoke.RealizationSelector) {
		return invoke.NewOperationInvoker(handlers[candidate.Record.ID]), nil
	}
	choose := func(path roleRoutingPath, native bool) *roleSelection {
		t.Helper()
		selection, err := selectRoleRuntime(t.Context(), r, CapInvoke, "example.work@1", path, native, factory)
		if err != nil {
			t.Fatal(err)
		}
		return selection
	}
	selected := choose(roleRanked, false)
	if selected == nil || selected.Runtime.candidate.Record.ID != ids[2] {
		t.Fatal("exact order/tie or authoritative support broken")
	}
	for _, h := range handlers {
		h.mu.Lock()
		if len(h.calls) != 1 || h.calls[0] != "provider.openbindings.binding-invoker.checkBindingSpecs" {
			t.Errorf("work or wrong query leaked: %v", h.calls)
		}
		h.mu.Unlock()
	}
	if !choose(roleNativeFirst, true).Builtin {
		t.Fatal("native-first policy changed")
	}
	for _, h := range handlers {
		h.mu.Lock()
		if len(h.calls) != 1 {
			t.Error("native-first queried external")
		}
		h.mu.Unlock()
	}
	if choose(roleRanked, true).Builtin {
		t.Fatal("ranked high-preference external lost to native")
	}
	negative := json.Number("-1e400")
	if err := r.preferBindingSpec(ids[2], "invoke", "example.work@1", &negative); err != nil {
		t.Fatal(err)
	}
	if choose(roleRanked, false).Runtime.candidate.Record.ID != ids[4] {
		t.Fatal("native override not applied")
	}
	if err := r.prefer(ids[2], "invoke", nil); err != nil {
		t.Fatal(err)
	}
	if choose(roleRanked, false).Runtime.candidate.Record.ID != ids[4] {
		t.Fatal("shared clear erased native override")
	}
	if err := r.preferBindingSpec(ids[2], "inspect", "example.work@1", &negative); err == nil {
		t.Fatal("native preference enrolled role")
	}
	// Valid false is ordinary ineligibility; malformed assessment is loud.
	handlers[ids[0]].result = func(string, any) any { return []any{map[string]any{"bindingSpec": "example.work@1"}} }
	if _, err := selectRoleRuntime(t.Context(), r, CapInvoke, "example.work@1", roleRanked, false, factory); err == nil {
		t.Fatal("malformed support silently fell through")
	}
}

func TestRoleRuntimeUnboundEligibility(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	if _, err := r.register(RoleRegistrationInput{Interface: provider, Roles: []string{"invoke"}}); err != nil {
		t.Fatal(err)
	}
	handler := &roleTestInvoker{}
	selection, err := selectRoleRuntime(t.Context(), r, CapInvoke, "example.work@1", roleRanked, false, func(roleCandidate) (invoke.ProviderRuntime, invoke.RealizationSelector) {
		return invoke.NewOperationInvoker(handler), nil
	})
	if err != nil || selection != nil || len(handler.calls) != 0 {
		t.Fatal("unbound record was invoked/probed")
	}
}
