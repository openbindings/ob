package app

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openbindings/openbindings-go/compare"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

const registryTestInterface = `{"openbindings":"0.2.0","operations":{"example.read":{"input":{"type":"string"},"output":{"type":"string"}}}}`

func testRoleCatalogue(t *testing.T) *roleCatalogue {
	t.Helper()
	roles := []DelegateRole{}
	for _, id := range []string{"A", "B", "C"} {
		roles = append(roles, DelegateRole{ID: id, Description: "Test responsibility; enrollment is not activation.", AcceptedInterfaces: []json.RawMessage{json.RawMessage(registryTestInterface)}})
	}
	c, err := newRoleCatalogue(roles)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testRoleRegistry(t *testing.T) *roleRegistry {
	t.Helper()
	return &roleRegistry{path: envConfigTestEnv(t), catalogue: testRoleCatalogue(t)}
}

func testRegistration(roles ...string) RoleRegistrationInput {
	return RoleRegistrationInput{Interface: json.RawMessage(registryTestInterface), Roles: roles}
}

func requireRecords(t *testing.T, r *roleRegistry, role string, count int) []DelegateRegistration {
	t.Helper()
	rows, err := r.list(role)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != count {
		t.Fatalf("records=%d, want %d", len(rows), count)
	}
	return rows
}

func TestRoleCatalogue(t *testing.T) {
	empty, err := newRoleCatalogue(nil)
	if err != nil {
		t.Fatal(err)
	}
	if roles, err := empty.list(); err != nil || roles == nil || len(roles) != 0 {
		t.Fatal("empty catalogue is not []")
	}
	c, err := defaultRoleCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	roles, err := c.list()
	if err != nil || len(roles) != 3 {
		t.Fatalf("OB roles: %v, %v", roles, err)
	}
	for _, role := range roles {
		expected, err := RequirementInterface(DelegateCapability(role.ID))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := jsonvalue.Marshal(expected)
		admitted, err := c.admit(raw, []string{role.ID})
		if err != nil {
			t.Fatalf("%s: %v: %+v", role.ID, err, compare.CheckInterfaceCompatibility(expected, expected))
		}
		if len(admitted[role.ID].Operations) != len(expected.Operations) {
			t.Fatal("role consumption drift")
		}
		for key := range expected.Operations {
			if admitted[role.ID].Operations[key] != key {
				t.Fatalf("missing exact operation %s", key)
			}
		}
	}
	snapshot, _ := c.list()
	roles[0].ID = "mutated"
	roles[0].AcceptedInterfaces[0][0] = '!'
	again, _ := c.list()
	a, _ := jsonvalue.Marshal(snapshot)
	b, _ := jsonvalue.Marshal(again)
	if !bytes.Equal(a, b) {
		t.Fatal("catalogue exposed mutable state")
	}
	for _, invalid := range [][]DelegateRole{
		{{ID: "A", Description: "test"}},
		{{ID: "A", Description: "test", AcceptedInterfaces: []json.RawMessage{json.RawMessage(`{"openbindings":"0.2.0","operations":{}}`)}}},
		{{ID: "A", Description: "test", AcceptedInterfaces: []json.RawMessage{json.RawMessage(registryTestInterface)}}, {ID: "A", Description: "duplicate", AcceptedInterfaces: []json.RawMessage{json.RawMessage(registryTestInterface)}}},
	} {
		if _, err := newRoleCatalogue(invalid); err == nil {
			t.Fatal("invalid catalogue accepted")
		}
	}
}

func TestRoleRegistryLifecycle(t *testing.T) {
	r := testRoleRegistry(t)
	input := testRegistration("A", "B")
	input.RolePreferences = json.RawMessage(`{"A":0,"B":-1.25}`)
	a, err := r.register(input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.register(input)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("identical documents collapsed into one registration")
	}
	rows := requireRecords(t, r, "A", 2)
	if len(rows[0].Roles) != 2 || rows[0].RolePreferences["A"] != "0" || rows[0].RolePreferences["B"] != "-1.25" {
		t.Fatal("filter projected away full record/zero")
	}
	replace := testRegistration("A", "C")
	replace.ID = a.ID
	if _, err := r.register(replace); err != nil {
		t.Fatal(err)
	}
	rows = requireRecords(t, r, "", 2)
	if rows[0].ID != a.ID || rows[1].ID != b.ID || len(rows[0].RolePreferences) != 1 || rows[0].RolePreferences["A"] != "0" {
		t.Fatal("replacement changed order or preference inheritance")
	}
	replace.Roles = []string{"A", "B"}
	if _, err := r.register(replace); err != nil {
		t.Fatal(err)
	}
	if _, restored := requireRecords(t, r, "", 2)[0].RolePreferences["B"]; restored {
		t.Fatal("removed preference resurrected")
	}
	replace.RolePreferences = json.RawMessage(`{}`)
	if _, err := r.register(replace); err != nil {
		t.Fatal(err)
	}
	if len(requireRecords(t, r, "", 2)[0].RolePreferences) != 0 {
		t.Fatal("empty supplied map did not clear")
	}
	for _, value := range []json.Number{"-0", "-2.75", "9007199254740993", "1e10000"} {
		if err := r.prefer(a.ID, "A", &value); err != nil {
			t.Fatal(err)
		}
		if got := requireRecords(t, r, "", 2)[0].RolePreferences["A"]; got != value {
			t.Fatalf("number rounded: %s != %s", got, value)
		}
	}
	if err := r.prefer(a.ID, "A", nil); err != nil {
		t.Fatal(err)
	}
	if _, explicit := requireRecords(t, r, "", 2)[0].RolePreferences["A"]; explicit {
		t.Fatal("clear retained explicit entry")
	}
	if err := r.prefer(a.ID, "C", nil); err == nil {
		t.Fatal("unenrolled preference accepted")
	}
	if err := r.prefer("unknown", "A", nil); err == nil {
		t.Fatal("unknown preference accepted")
	}
	requireRecords(t, r, "unknown", 0)
	// Caller/result mutation cannot alter the saved record.
	input.Interface[0] = '!'
	a.Interface[0] = '!'
	rows[0].Roles[0] = "wrong"
	if requireRecords(t, r, "A", 2)[0].Roles[0] != "A" {
		t.Fatal("caller mutation escaped")
	}
	if err := r.unregister(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.unregister(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.register(replace); err == nil {
		t.Fatal("removed ID resurrected by replacement")
	}
	fresh := &roleRegistry{path: r.path, catalogue: testRoleCatalogue(t)}
	c, err := fresh.register(testRegistration("A"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == a.ID || c.ID == b.ID {
		t.Fatal("removed ID reused after restart")
	}
	rows = requireRecords(t, r, "", 2)
	if rows[0].ID != b.ID || len(rows[1].RolePreferences) != 0 {
		t.Fatal("removal affected unrelated record or fresh preferences")
	}
}

func TestRoleRegistryRejectedReplacement(t *testing.T) {
	r := testRoleRegistry(t)
	a, err := r.register(testRegistration("A"))
	if err != nil {
		t.Fatal(err)
	}
	zero := json.Number("0")
	if err := r.prefer(a.ID, "A", &zero); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
	cases := []RoleRegistrationInput{
		{ID: a.ID, Interface: json.RawMessage(`null`), Roles: []string{"A"}},
		{ID: a.ID, Interface: json.RawMessage(registryTestInterface), Roles: []string{}},
		{ID: a.ID, Interface: json.RawMessage(registryTestInterface), Roles: []string{"A", "A"}},
		{ID: a.ID, Interface: json.RawMessage(registryTestInterface), Roles: []string{"unknown"}},
	}
	for _, preferences := range []string{`null`, `{"B":0}`, `{"A":null}`, `{"A":"1"}`, `{"A":1e10001}`} {
		request := testRegistration("A")
		request.ID = a.ID
		request.RolePreferences = json.RawMessage(preferences)
		cases = append(cases, request)
	}
	for i, request := range cases {
		if _, err := r.register(request); err == nil {
			t.Fatalf("invalid replacement %d accepted", i)
		}
		after, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
		if !bytes.Equal(before, after) {
			t.Fatalf("rejection %d mutated state", i)
		}
	}
}

func TestRoleRegistryWithdrawnRoles(t *testing.T) {
	r := testRoleRegistry(t)
	a, err := r.register(testRegistration("A", "B"))
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := newRoleCatalogue(nil)
	changed := &roleRegistry{path: r.path, catalogue: empty}
	requireRecords(t, changed, "B", 1)
	n := json.Number("2")
	if err := changed.prefer(a.ID, "B", &n); err != nil {
		t.Fatal(err)
	}
	if _, err := changed.register(testRegistration("B")); err == nil {
		t.Fatal("withdrawn catalogue reactivated record")
	}
	if _, err := changed.candidates("B"); err == nil {
		t.Fatal("withdrawn catalogue eligible for runtime use")
	}
	if err := changed.unregister(a.ID); err != nil {
		t.Fatal(err)
	}
	requireRecords(t, r, "", 0)
}

func TestRoleRegistryValueRetention(t *testing.T) {
	r := testRoleRegistry(t)
	// These are instance values, not comparable schemas. Retain their exact
	// carrier, including lone UTF-16 surrogates and numbers beyond binary64.
	raw := strings.TrimSuffix(registryTestInterface, "}") + `,"x-sentinel":{"big":9007199254740993,"huge":1e400,"tiny":1e-400,"null":null,"object":{},"array":[],"unicode":"\ud800","emoji":"\ud83d\ude00"}}`
	input := testRegistration("A")
	input.Interface = json.RawMessage(raw)
	if _, err := r.register(input); err != nil {
		t.Fatal(err)
	}
	stored := requireRecords(t, r, "", 1)[0].Interface
	var expected, actual any
	if err := jsonvalue.Unmarshal([]byte(raw), &expected); err != nil {
		t.Fatal(err)
	}
	if err := jsonvalue.Unmarshal(stored, &actual); err != nil {
		t.Fatal(err)
	}
	if equal, err := jsonvalue.Equal(expected, actual); err != nil || !equal {
		t.Fatalf("retained values changed: %v", err)
	}
	config, err := LoadEnvConfig(r.path)
	if err != nil {
		t.Fatal(err)
	}
	config.Extra = map[string]json.RawMessage{"unrelated": json.RawMessage(`{"n":9007199254740993,"s":"\ud800"}`)}
	if err := SaveEnvConfig(r.path, config); err != nil {
		t.Fatal(err)
	}
	if err := r.prefer(requireRecords(t, r, "", 1)[0].ID, "A", nil); err != nil {
		t.Fatal(err)
	}
	config, err = LoadEnvConfig(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(config.Extra["unrelated"], []byte("9007199254740993")) || !bytes.Contains(config.Extra["unrelated"], []byte(`\ud800`)) {
		t.Fatal("unrelated field pruned/rounded")
	}
}

func TestRoleRegistryIssuance(t *testing.T) {
	r := testRoleRegistry(t)
	if _, err := r.register(testRegistration("A")); err != nil {
		t.Fatal(err)
	}
	_, err := mutateEnvConfig(r.path, func(c *EnvConfig) (bool, error) {
		s, err := readRoleState(c)
		if err != nil {
			return false, err
		}
		s.Next = "18446744073709551615"
		return true, retainRoleState(c, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
	if _, err := r.register(testRegistration("A")); err == nil {
		t.Fatal("issuance exhausted without refusal")
	}
	after, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
	if !bytes.Equal(before, after) {
		t.Fatal("exhaustion mutated state")
	}
	_, err = mutateEnvConfig(r.path, func(c *EnvConfig) (bool, error) {
		s, err := readRoleState(c)
		if err != nil {
			return false, err
		}
		s.Revision = math.MaxUint64
		data, err := jsonvalue.Marshal(s)
		c.DelegateRegistry = data
		return true, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.unregister(requireRecords(t, r, "", 1)[0].ID); err == nil {
		t.Fatal("revision exhaustion was ignored")
	}
}

func TestRoleRegistryLimits(t *testing.T) {
	r := testRoleRegistry(t)
	request := testRegistration("A")
	request.Interface = json.RawMessage(strings.TrimSuffix(registryTestInterface, "}") + `,"description":"` + strings.Repeat("x", maxDelegateInterfaceBytes) + `"}`)
	if _, err := r.register(request); err == nil {
		t.Fatal("oversized OBI admitted")
	}
	requireRecords(t, r, "", 0)
	// Construct a full, valid persisted inventory without quadratic setup.
	_, err := mutateEnvConfig(r.path, func(c *EnvConfig) (bool, error) {
		s, err := newRoleState(r.catalogue.identity)
		if err != nil {
			return false, err
		}
		for i := 0; i < maxDelegateRecords; i++ {
			s.Records = append(s.Records, DelegateRegistration{ID: strings.Repeat("x", i+1), Interface: json.RawMessage(registryTestInterface), Roles: []string{"A"}, RolePreferences: map[string]json.Number{}})
		}
		return true, retainRoleState(c, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireRecords(t, r, "", maxDelegateRecords)
	if _, err := r.register(testRegistration("A")); err == nil {
		t.Fatal("record capacity silently exceeded")
	}
}

func TestRoleRegistryCatalogue(t *testing.T) {
	r := testRoleRegistry(t)
	if _, err := r.register(testRegistration("A")); err != nil {
		t.Fatal(err)
	}
	roles, _ := r.catalogue.list()
	roles[0].Description += " changed policy"
	other, err := newRoleCatalogue(roles)
	if err != nil {
		t.Fatal(err)
	}
	stale := &roleRegistry{path: r.path, catalogue: other}
	if _, err := stale.register(testRegistration("A")); err == nil {
		t.Fatal("mixed catalogue accepted mutation")
	}
	if _, err := stale.candidates("A"); err == nil {
		t.Fatal("mixed catalogue produced runtime candidates")
	}
	if len(requireRecords(t, stale, "", 1)) != 1 {
		t.Fatal("administrative inspection unavailable")
	}
}

func TestRoleRegistryCandidateSnapshot(t *testing.T) {
	r := testRoleRegistry(t)
	a, err := r.register(testRegistration("A"))
	if err != nil {
		t.Fatal(err)
	}
	if candidates, err := r.candidates("B"); err != nil || len(candidates) != 0 {
		t.Fatal("unrequested compatible role inferred")
	}
	before, err := r.candidates("A")
	if err != nil || len(before) != 1 {
		t.Fatal(err)
	}
	if before[0].Admission.Operations["example.read"] != "example.read" {
		t.Fatal("exact admission correspondence lost")
	}
	if err := r.unregister(a.ID); err != nil {
		t.Fatal(err)
	}
	after, err := r.candidates("A")
	if err != nil || len(after) != 0 {
		t.Fatal("new lookup retained removed registration")
	}
	if before[0].Record.ID != a.ID || string(before[0].Record.Interface) != registryTestInterface {
		t.Fatal("in-flight snapshot mutated")
	}
}

func TestRoleRegistryMalformedState(t *testing.T) {
	r := testRoleRegistry(t)
	if _, err := r.register(testRegistration("A")); err != nil {
		t.Fatal(err)
	}
	config, err := LoadEnvConfig(r.path)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(config.DelegateRegistry)
	for _, raw := range []string{
		`null`, `{}`, strings.Replace(valid, roleRegistryFormat, "future@2", 1),
		strings.Replace(valid, `"rolePreferences":{}`, `"rolePreferences":{"A":"2"}`, 1),
		strings.Replace(valid, `"rolePreferences":{}`, `"rolePreferences":{"A":null}`, 1),
	} {
		config.DelegateRegistry = json.RawMessage(raw)
		if _, err := readRoleState(config); err == nil {
			t.Fatalf("invalid state accepted: %s", raw[:min(len(raw), 50)])
		}
	}
	missing := &roleRegistry{path: filepath.Join(t.TempDir(), "absent"), catalogue: r.catalogue}
	if rows, err := missing.list(""); err != nil || rows == nil || len(rows) != 0 {
		t.Fatal("absent environment not empty")
	}
	path := filepath.Join(r.path, EnvConfigFile)
	if err := os.WriteFile(path, []byte(`{"delegateRegistry":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.list(""); err == nil {
		t.Fatal("corrupt registry silently empty")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.list(""); err == nil {
		t.Fatal("unreadable registry silently empty")
	}
}

func TestRoleRegistryRecoveryIssuance(t *testing.T) {
	r := testRoleRegistry(t)
	a, err := r.register(testRegistration("A"))
	if err != nil {
		t.Fatal(err)
	}
	backup, err := LoadEnvConfig(r.path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.register(testRegistration("A"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = mutateEnvConfig(r.path, func(c *EnvConfig) (bool, error) {
		restored, err := readRoleState(backup)
		if err != nil {
			return false, err
		}
		if err := rotateRoleIssuance(restored); err != nil {
			return false, err
		}
		return true, retainRoleState(c, restored)
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.register(testRegistration("A"))
	if err != nil {
		t.Fatal(err)
	}
	rows := requireRecords(t, r, "", 2)
	if rows[0].ID != a.ID || c.ID == b.ID || c.ID == a.ID {
		t.Fatal("restored timeline reused identity")
	}
}

func TestRoleRegistryByteLimit(t *testing.T) {
	r := testRoleRegistry(t)
	input := testRegistration("A")
	input.Interface = json.RawMessage(strings.TrimSuffix(registryTestInterface, "}") + `,"description":"` + strings.Repeat("x", 850000) + `"}`)
	for i := 0; i < 4; i++ {
		if _, err := r.register(input); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
	if _, err := r.register(input); err == nil {
		t.Fatal("oversized full-list state accepted")
	}
	after, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
	if !bytes.Equal(before, after) {
		t.Fatal("capacity failure partially wrote")
	}
	requireRecords(t, r, "", 4)
}

func TestRoleRegistryAdmissionDoesNotDiscoverProviders(t *testing.T) {
	r := testRoleRegistry(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	request := testRegistration("A")
	request.Interface = json.RawMessage(strings.TrimSuffix(registryTestInterface, "}") + `,"sources":{"remote":{"bindingSpec":"example.test@1","location":"` + server.URL + `"}},"bindings":{"implementation":{"operation":"example.read","source":"remote","selector":"anything"}},"dependencies":{"needed":{"operation":"example.read"}}}`)
	record, err := r.register(request)
	if err != nil {
		t.Fatal(err)
	}
	listed := requireRecords(t, r, "", 1)
	if _, err := r.candidates("A"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatal("admission fetched an artifact or invoked the provider")
	}
	if !bytes.Equal(record.Interface, listed[0].Interface) || !bytes.Contains(listed[0].Interface, []byte(`"dependencies"`)) {
		t.Fatal("dependency/source structure changed")
	}
}

func TestRoleCatalogueLimits(t *testing.T) {
	roles, _ := testRoleCatalogue(t).list()
	role := roles[0]
	role.AcceptedInterfaces = make([]json.RawMessage, maxRoleAlternatives)
	for i := range role.AcceptedInterfaces {
		role.AcceptedInterfaces[i] = json.RawMessage(registryTestInterface)
	}
	if _, err := newRoleCatalogue([]DelegateRole{role}); err != nil {
		t.Fatal(err)
	}
	role.AcceptedInterfaces = append(role.AcceptedInterfaces, json.RawMessage(registryTestInterface))
	if _, err := newRoleCatalogue([]DelegateRole{role}); err == nil {
		t.Fatal("alternative limit ignored")
	}
	roles = nil
	for i := 0; i < maxDelegateRoles; i++ {
		roles = append(roles, DelegateRole{ID: strings.Repeat("x", i+1), Description: "test", AcceptedInterfaces: []json.RawMessage{json.RawMessage(registryTestInterface)}})
	}
	if _, err := newRoleCatalogue(roles); err != nil {
		t.Fatal(err)
	}
	roles = append(roles, DelegateRole{ID: "overflow", Description: "test", AcceptedInterfaces: []json.RawMessage{json.RawMessage(registryTestInterface)}})
	if _, err := newRoleCatalogue(roles); err == nil {
		t.Fatal("role limit ignored")
	}
}
