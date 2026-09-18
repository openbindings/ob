package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

// invokeProvider is a complete, unbound provider value for the invoke role:
// the role's own accepted interface, carried by value.
func invokeProvider(t *testing.T) json.RawMessage {
	t.Helper()
	iface, err := RequirementInterface(CapInvoke)
	if err != nil {
		t.Fatal(err)
	}
	iface.Name = "facade-provider"
	raw, err := jsonvalue.Marshal(iface)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDelegateFacadeWithoutEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("OB_CONFIG_DIR", t.TempDir())
	roles, err := ListDelegateRoles()
	if err != nil || len(roles.Roles) != 3 {
		t.Fatalf("roles need no environment: %v %v", roles, err)
	}
	list, err := ListDelegates("")
	if err != nil || list.Delegates == nil || len(list.Delegates) != 0 {
		t.Fatalf("missing environment must be an honest empty registry: %+v %v", list, err)
	}
	if _, err := RegisterDelegate(RoleRegistrationInput{Interface: invokeProvider(t), Roles: []string{"invoke"}}); !IsNoEnvironment(err) {
		t.Fatalf("register without environment: %v", err)
	}
	if err := SetDelegatePreference("dlg_x", "invoke", nil); !IsNoEnvironment(err) {
		t.Fatalf("prefer without environment: %v", err)
	}
	if err := UnregisterDelegate("dlg_absent"); err != nil {
		t.Fatalf("absent ID in absent environment must succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("OB_CONFIG_DIR"), EnvConfigFile)); !os.IsNotExist(err) {
		t.Fatal("facade created an environment implicitly")
	}
}

func TestDelegateFacadeLifecycle(t *testing.T) {
	envPath := envConfigTestEnv(t)
	provider := invokeProvider(t)
	before, _ := os.ReadFile(filepath.Join(envPath, EnvConfigFile))

	// Rejections leave storage untouched.
	if _, err := RegisterDelegate(RoleRegistrationInput{Interface: provider, Roles: []string{"synthesize"}}); err == nil {
		t.Fatal("invoke provider admitted for synthesize")
	}
	if _, err := RegisterDelegate(RoleRegistrationInput{Interface: provider, Roles: []string{"invoke"}, RolePreferences: json.RawMessage(`{"synthesize": 1}`)}); err == nil {
		t.Fatal("preference outside requested roles admitted")
	}
	if _, err := RegisterDelegate(RoleRegistrationInput{ID: "dlg_missing", Interface: provider, Roles: []string{"invoke"}}); err == nil {
		t.Fatal("unknown ID replacement admitted")
	}
	after, _ := os.ReadFile(filepath.Join(envPath, EnvConfigFile))
	if !bytes.Equal(before, after) {
		t.Fatal("rejected mutations changed storage")
	}

	first, err := RegisterDelegate(RoleRegistrationInput{Interface: provider, Roles: []string{"invoke"}, RolePreferences: json.RawMessage(`{"invoke": 9007199254740993}`)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RegisterDelegate(RoleRegistrationInput{Interface: provider, Roles: []string{"invoke"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.RolePreferences["invoke"] != "9007199254740993" || len(second.RolePreferences) != 0 {
		t.Fatalf("identity/preference semantics: %+v %+v", first, second)
	}
	status, err := GetEnvironmentStatus()
	if err != nil || status.DelegateCount != 2 {
		t.Fatalf("environment count: %+v %v", status, err)
	}
	for role, want := range map[string]int{"": 2, "invoke": 2, "synthesize": 0, "not-a-role": 0} {
		list, err := ListDelegates(role)
		if err != nil || len(list.Delegates) != want {
			t.Fatalf("list %q: %d records, want %d (%v)", role, len(list.Delegates), want, err)
		}
	}
	zero := json.Number("0")
	if err := SetDelegatePreference(first.ID, "invoke", &zero); err != nil {
		t.Fatal(err)
	}
	list, _ := ListDelegates("invoke")
	if list.Delegates[0].ID != first.ID || list.Delegates[0].RolePreferences["invoke"] != "0" {
		t.Fatalf("explicit zero lost or order changed: %+v", list.Delegates)
	}
	if err := SetDelegatePreference(first.ID, "synthesize", &zero); err == nil {
		t.Fatal("preference for an unenrolled role accepted")
	}
	if err := SetDelegatePreference("dlg_absent", "invoke", nil); err == nil {
		t.Fatal("clearing an absent registration succeeded")
	}
	override := json.Number("-1.25")
	if err := SetDelegateBindingPreference(first.ID, "invoke", "example.test@1", &override); err != nil {
		t.Fatal(err)
	}
	if err := SetDelegateBindingPreference(first.ID, "inspect", "example.test@1", &override); err == nil {
		t.Fatal("override enrolled an unrequested role")
	}
	if err := SetDelegatePreference(first.ID, "invoke", nil); err != nil {
		t.Fatal(err)
	}
	list, _ = ListDelegates("")
	if _, present := list.Delegates[0].RolePreferences["invoke"]; present {
		t.Fatal("null clear did not remove the explicit entry")
	}
	config, _ := LoadEnvConfig(envPath)
	state, err := readRoleState(config)
	if err != nil || state.BindingPreferences[first.ID]["invoke"]["example.test@1"] != "-1.25" {
		t.Fatal("shared clear erased the native override or override not retained")
	}
	replaced, err := RegisterDelegate(RoleRegistrationInput{ID: first.ID, Interface: provider, Roles: []string{"invoke"}, RolePreferences: json.RawMessage(`{}`)})
	if err != nil || replaced.ID != first.ID || len(replaced.RolePreferences) != 0 {
		t.Fatalf("replacement: %+v %v", replaced, err)
	}
	if err := UnregisterDelegate(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterDelegate(first.ID); err != nil {
		t.Fatal("repeated removal must succeed")
	}
	list, _ = ListDelegates("")
	if len(list.Delegates) != 1 || list.Delegates[0].ID != second.ID {
		t.Fatalf("removal touched the other record: %+v", list.Delegates)
	}
	if _, err := RegisterDelegate(RoleRegistrationInput{ID: first.ID, Interface: provider, Roles: []string{"invoke"}}); err == nil {
		t.Fatal("removed ID resurrected")
	}
}

func TestDelegateFacadeStateClassification(t *testing.T) {
	envPath := envConfigTestEnv(t)
	legacy := []byte(`{"delegates":[{"location":"exec:old","operations":[]}]}` + "\n")
	if err := os.WriteFile(filepath.Join(envPath, EnvConfigFile), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListDelegates(""); !IsDelegateRegistryUnavailable(err) || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("legacy rows must be an unavailable registry with guidance, got %v", err)
	}
	if _, err := RegisterDelegate(RoleRegistrationInput{Interface: invokeProvider(t), Roles: []string{"invoke"}}); !IsDelegateRegistryUnavailable(err) {
		t.Fatalf("register over legacy rows: %v", err)
	}
	if err := UnregisterDelegate("dlg_x"); !IsDelegateRegistryUnavailable(err) {
		t.Fatalf("unregister over legacy rows must not report absence: %v", err)
	}
	if _, err := GetEnvironmentStatus(); err == nil || !strings.Contains(err.Error(), "migrat") {
		t.Fatalf("environment status must surface legacy state: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(envPath, EnvConfigFile)); !bytes.Equal(got, legacy) {
		t.Fatal("legacy state was modified by refused operations")
	}
	if err := os.WriteFile(filepath.Join(envPath, EnvConfigFile), []byte(`{"delegateRegistry":{"format":"ob.delegate-registry@1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListDelegates(""); !IsEnvironmentUnreadable(err) {
		t.Fatalf("corrupt registry must be unreadable, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(envPath, EnvConfigFile), []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListDelegates(""); err == nil {
		t.Fatal("unparsable configuration listed as empty")
	}
}

func TestRoleRegistrationInputDecoding(t *testing.T) {
	provider := string(invokeProvider(t))
	for _, tc := range []struct {
		name, body string
		ok         bool
		prefs      string
	}{
		{"fresh omitted preferences", `{"interface": ` + provider + `, "roles": ["invoke"]}`, true, ""},
		{"empty map", `{"interface": ` + provider + `, "roles": ["invoke"], "rolePreferences": {}}`, true, "{}"},
		{"null map", `{"interface": ` + provider + `, "roles": ["invoke"], "rolePreferences": null}`, false, ""},
		{"empty id", `{"id": "", "interface": ` + provider + `, "roles": ["invoke"]}`, false, ""},
		{"null roles", `{"interface": ` + provider + `, "roles": null}`, false, ""},
		{"locator interface", `{"interface": "exec:tool", "roles": ["invoke"]}`, false, ""},
		{"unknown field", `{"interface": ` + provider + `, "roles": ["invoke"], "location": "x"}`, false, ""},
		{"not an object", `[]`, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input RoleRegistrationInput
			err := jsonvalue.Unmarshal([]byte(tc.body), &input)
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v want %v: %v", err == nil, tc.ok, err)
			}
			if tc.ok && string(input.RolePreferences) != tc.prefs {
				t.Fatalf("preferences %q, want %q", input.RolePreferences, tc.prefs)
			}
		})
	}
}

func TestDelegatePreferenceInputDecoding(t *testing.T) {
	for _, tc := range []struct {
		body   string
		ok     bool
		number string
		clear  bool
	}{
		{`{"id":"dlg_1","role":"invoke","preference":1e400}`, true, "1e400", false},
		{`{"id":"dlg_1","role":"invoke","preference":0}`, true, "0", false},
		{`{"id":"dlg_1","role":"invoke","preference":null}`, true, "", true},
		{`{"id":"dlg_1","role":"invoke"}`, false, "", false},
		{`{"id":"dlg_1","role":"invoke","preference":"1"}`, false, "", false},
		{`{"id":"","role":"invoke","preference":1}`, false, "", false},
		{`{"id":"dlg_1","role":"invoke","preference":1,"bindingSpec":"x"}`, false, "", false},
	} {
		var input DelegatePreferenceInput
		err := jsonvalue.Unmarshal([]byte(tc.body), &input)
		if (err == nil) != tc.ok {
			t.Fatalf("%s: ok=%v want %v: %v", tc.body, err == nil, tc.ok, err)
		}
		if !tc.ok {
			continue
		}
		if tc.clear != (input.Preference == nil) || (!tc.clear && string(*input.Preference) != tc.number) {
			t.Fatalf("%s: decoded %+v", tc.body, input)
		}
	}
	var binding DelegateBindingPreferenceInput
	if err := jsonvalue.Unmarshal([]byte(`{"id":"dlg_1","role":"invoke","bindingSpec":"example.test@1","preference":9007199254740993}`), &binding); err != nil || binding.BindingSpec != "example.test@1" || string(*binding.Preference) != "9007199254740993" {
		t.Fatalf("binding preference decoding: %+v %v", binding, err)
	}
	if err := jsonvalue.Unmarshal([]byte(`{"id":"dlg_1","role":"invoke","preference":1}`), &binding); err == nil {
		t.Fatal("missing bindingSpec accepted for the native override")
	}
}

func TestDelegateRoleRequirementProjection(t *testing.T) {
	for _, role := range []string{"invoke", "synthesize", "inspect"} {
		data, err := DelegateRoleRequirementJSON(role)
		if err != nil {
			t.Fatal(err)
		}
		catalogue, _ := defaultRoleCatalogue()
		var want, got any
		_ = json.Unmarshal(catalogue.alternatives[role][0].value, &want)
		_ = json.Unmarshal(data, &got)
		if equal, err := jsonvalue.Equal(want, got); err != nil || !equal {
			t.Fatalf("%s: projection differs from the catalogue alternative", role)
		}
	}
	if _, err := DelegateRoleRequirementJSON("bogus"); err == nil {
		t.Fatal("unknown role projected")
	}
	multi, err := newRoleCatalogue([]DelegateRole{{ID: "A", Description: "two alternatives", AcceptedInterfaces: []json.RawMessage{
		json.RawMessage(`{"openbindings":"0.2.0","operations":{"one":{}}}`), json.RawMessage(`{"openbindings":"0.2.0","operations":{"two":{}}}`),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roleRequirementJSON(multi, "A"); err == nil || !strings.Contains(err.Error(), "2 alternative") {
		t.Fatalf("multi-alternative role must refuse the projection, got %v", err)
	}
	if first, second := mustCatalogue(t), mustCatalogue(t); first != second {
		t.Fatal("built-in catalogue is not cached")
	}
}

func mustCatalogue(t *testing.T) *roleCatalogue {
	t.Helper()
	c, err := defaultRoleCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
