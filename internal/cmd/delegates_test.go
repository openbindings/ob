package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// runOB executes the real Cobra tree in-process and returns the exit result.
func runDelegateCLI(t *testing.T, stdin string, args ...string) app.ExitResult {
	t.Helper()
	root := NewRoot()
	root.SetArgs(args)
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.ExecuteContext(t.Context())
	if err == nil {
		return app.ExitResult{}
	}
	result, ok := err.(app.ExitResult)
	if !ok {
		return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}
	return result
}

func mustExit(t *testing.T, result app.ExitResult, code int, contains string) {
	t.Helper()
	if result.Code != code || !strings.Contains(result.Message, contains) {
		t.Fatalf("exit %d %q, want %d containing %q", result.Code, result.Message, code, contains)
	}
}

func delegateTestEnv(t *testing.T) (envPath string, providerFile string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("OB_CONFIG_DIR", t.TempDir())
	if _, err := app.Init(false); err != nil {
		t.Fatal(err)
	}
	envPath, err := app.FindEnvPath()
	if err != nil {
		t.Fatal(err)
	}
	iface, err := app.RequirementInterface(app.CapInvoke)
	if err != nil {
		t.Fatal(err)
	}
	iface.Name = "cli-provider"
	raw, err := jsonvalue.Marshal(iface)
	if err != nil {
		t.Fatal(err)
	}
	providerFile = filepath.Join(t.TempDir(), "provider.obi.json")
	if err := os.WriteFile(providerFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return envPath, providerFile
}

func TestDelegateCLISurface(t *testing.T) {
	envPath, providerFile := delegateTestEnv(t)
	configFile := filepath.Join(envPath, app.EnvConfigFile)

	roles := runDelegateCLI(t, "", "delegate", "roles", "-F", "json")
	var catalogue map[string][]map[string]any
	if roles.Code != 0 || json.Unmarshal([]byte(roles.Message), &catalogue) != nil || len(catalogue["roles"]) != 3 {
		t.Fatalf("roles: %+v", roles)
	}

	// Retired and malformed usages fail with guidance and no state change.
	before, _ := os.ReadFile(configFile)
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", "exec:tool", "--role", "invoke"), 2, "location")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile), 2, "role")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "invoke", "--id", ""), 2, "--id")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "invoke", "--role", "invoke"), 2, "repeated")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "invoke", "--preference", "invoke=abc"), 2, "exact JSON number")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "invoke", "--preference", "invoke=1", "--preference", "invoke=2"), 2, "twice")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "invoke", "--preference", "invoke=1", "--clear-preferences"), 2, "exclusive")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "synthesize"), 1, "no complete compatible alternative")
	mustExit(t, runDelegateCLI(t, `{"openbindings":"0.2.0","operations":{}} trailing`, "delegate", "register", "-", "--role", "invoke"), 2, "trailing")
	mustExit(t, runDelegateCLI(t, `"exec:tool"`, "delegate", "register", "-", "--role", "invoke"), 2, "JSON object")
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", "dlg_x", "1", "--role", "invoke", "--operation", "op"), 2, "--operation is retired")
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", "dlg_x", "1", "--role", "invoke", "--capability", "invoke"), 2, "--capability is retired")
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", "exec:tool", "1", "--role", "invoke"), 2, "location")
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", "dlg_x", "1"), 2, "role")
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", "dlg_x", "--role", "invoke"), 2, "--clear")
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", "dlg_x", "1", "--clear", "--role", "invoke"), 2, "takes no preference")
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", "dlg_x", "0x1", "--role", "invoke"), 2, "exact JSON number")
	mustExit(t, runDelegateCLI(t, "", "delegate", "unregister", "exec:tool"), 2, "location")
	mustExit(t, runDelegateCLI(t, "", "delegate", "list", "--role", ""), 2, "--role")
	mustExit(t, runDelegateCLI(t, "", "delegate", "requirements", "bogus"), 2, "unknown delegate role")
	if after, _ := os.ReadFile(configFile); !bytes.Equal(before, after) {
		t.Fatal("refused commands changed the environment")
	}

	// Fresh enrollment from a file, with exact preferences and explicit zero.
	registered := runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "invoke", "--preference", "invoke=9007199254740993", "-F", "json")
	if registered.Code != 0 {
		t.Fatalf("register: %+v", registered)
	}
	var record map[string]any
	if err := jsonvalue.Unmarshal([]byte(registered.Message), &record); err != nil {
		t.Fatal(err)
	}
	id, _ := record["id"].(string)
	prefs, _ := record["rolePreferences"].(map[string]any)
	if id == "" || prefs["invoke"] != json.Number("9007199254740993") {
		t.Fatalf("registration output: %s", registered.Message)
	}
	if iface, _ := record["interface"].(map[string]any); iface["name"] != "cli-provider" {
		t.Fatalf("interface not retained by value: %v", record["interface"])
	}
	// The same document through stdin is a second registration.
	doc, _ := os.ReadFile(providerFile)
	second := runDelegateCLI(t, string(doc), "delegate", "register", "-", "--role", "invoke", "--clear-preferences", "-F", "json")
	var secondRecord map[string]any
	if second.Code != 0 || jsonvalue.Unmarshal([]byte(second.Message), &secondRecord) != nil || secondRecord["id"] == id {
		t.Fatalf("stdin registration: %+v", second)
	}
	list := runDelegateCLI(t, "", "delegate", "list", "--role", "invoke", "-F", "json")
	var listed map[string][]map[string]any
	if list.Code != 0 || json.Unmarshal([]byte(list.Message), &listed) != nil || len(listed["delegates"]) != 2 || listed["delegates"][0]["id"] != id {
		t.Fatalf("list: %+v", list)
	}
	if unknown := runDelegateCLI(t, "", "delegate", "list", "--role", "unknown", "-F", "json"); unknown.Code != 0 || !strings.Contains(unknown.Message, `"delegates": []`) {
		t.Fatalf("unknown role filter must be an empty complete result: %+v", unknown)
	}
	human := runDelegateCLI(t, "", "delegate", "list")
	if human.Code != 0 || !strings.Contains(human.Message, id) || strings.Contains(human.Message, `"operations"`) {
		t.Fatalf("human list must summarize, not dump: %+v", human)
	}
	// Preferences: set zero, native override, clear; each returns null on the machine lane.
	for _, args := range [][]string{
		{"delegate", "prefer", id, "0", "--role", "invoke", "-F", "json"},
		{"delegate", "prefer", id, "1e400", "--role", "invoke", "--binding-spec", "example.test@1", "-F", "json"},
	} {
		if result := runDelegateCLI(t, "", args...); result.Code != 0 || strings.TrimSpace(result.Message) != "null" {
			t.Fatalf("%v: %+v", args, result)
		}
	}
	list = runDelegateCLI(t, "", "delegate", "list", "-F", "json")
	_ = json.Unmarshal([]byte(list.Message), &listed)
	if listed["delegates"][0]["rolePreferences"].(map[string]any)["invoke"] != float64(0) {
		t.Fatalf("explicit zero not retained: %v", listed["delegates"][0]["rolePreferences"])
	}
	mustExit(t, runDelegateCLI(t, "", "delegate", "prefer", id, "1", "--role", "inspect"), 1, "not enrolled")
	if result := runDelegateCLI(t, "", "delegate", "prefer", id, "--clear", "--role", "invoke"); result.Code != 0 || !strings.Contains(result.Message, "Cleared") {
		t.Fatalf("clear: %+v", result)
	}
	config, _ := app.LoadEnvConfig(envPath)
	if !strings.Contains(string(config.DelegateRegistry), `"example.test@1":1e400`) {
		t.Fatalf("native override lost after shared clear: %s", config.DelegateRegistry)
	}
	// Replacement keeps the ID; removal is idempotent; requirements project the catalogue.
	replaced := runDelegateCLI(t, "", "delegate", "register", providerFile, "--id", id, "--role", "invoke", "-F", "json")
	var replacedRecord map[string]any
	if replaced.Code != 0 || jsonvalue.Unmarshal([]byte(replaced.Message), &replacedRecord) != nil || replacedRecord["id"] != id {
		t.Fatalf("replacement: %+v", replaced)
	}
	if result := runDelegateCLI(t, "", "delegate", "unregister", id, "-F", "json"); result.Code != 0 || strings.TrimSpace(result.Message) != "null" {
		t.Fatalf("unregister: %+v", result)
	}
	if result := runDelegateCLI(t, "", "delegate", "rm", id); result.Code != 0 || !strings.Contains(result.Message, "absent") {
		t.Fatalf("repeat unregister: %+v", result)
	}
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--id", id, "--role", "invoke"), 1, "unknown registration ID")
	requirements := runDelegateCLI(t, "", "delegate", "requirements", "invoke")
	if requirements.Code != 0 {
		t.Fatalf("requirements: %+v", requirements)
	}
	env := runDelegateCLI(t, "", "environment", "-F", "json")
	if env.Code != 0 || !strings.Contains(env.Message, `"delegateCount": 1`) {
		t.Fatalf("environment count: %+v", env)
	}
}

func TestDelegateCLILegacyStateRefusal(t *testing.T) {
	envPath, providerFile := delegateTestEnv(t)
	legacy := []byte(`{"delegates":[{"location":"exec:old","operations":[]}]}` + "\n")
	if err := os.WriteFile(filepath.Join(envPath, app.EnvConfigFile), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	mustExit(t, runDelegateCLI(t, "", "delegate", "list"), 1, "migrate")
	mustExit(t, runDelegateCLI(t, "", "delegate", "register", providerFile, "--role", "invoke"), 1, "migrat")
	mustExit(t, runDelegateCLI(t, "", "delegate", "unregister", "dlg_x"), 1, "migrat")
	mustExit(t, runDelegateCLI(t, "", "environment"), 1, "migrat")
	if got, _ := os.ReadFile(filepath.Join(envPath, app.EnvConfigFile)); !bytes.Equal(got, legacy) {
		t.Fatal("legacy state changed")
	}
}

func TestDelegateHTTPSurface(t *testing.T) {
	envPath, providerFile := delegateTestEnv(t)
	ts := testEnv(t)
	defer ts.Close()
	doc, _ := os.ReadFile(providerFile)

	resp, err := authedGet(ts.URL+"/delegates/roles", "test-token")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("roles: %v %v", resp, err)
	}
	if roles := mustJSON(t, resp); len(roles["roles"].([]any)) != 3 {
		t.Fatalf("roles: %v", roles)
	}
	// Malformed input is 400 invalid_request and never mutates.
	before, _ := os.ReadFile(filepath.Join(envPath, app.EnvConfigFile))
	for _, body := range []string{
		`{"interface": ` + string(doc) + `, "roles": ["invoke"], "rolePreferences": null}`,
		`{"interface": ` + string(doc) + `, "roles": ["invoke"], "id": ""}`,
		`{"interface": "exec:tool", "roles": ["invoke"]}`,
		`{"interface": ` + string(doc) + `, "roles": ["invoke"], "location": "x"}`,
		`{"interface": ` + string(doc) + `, "roles": ["invoke"]} {}`,
	} {
		resp, err := authedPost(ts.URL+"/delegates/register", "test-token", body)
		if err != nil {
			t.Fatal(err)
		}
		if got := mustJSON(t, resp); resp.StatusCode != 400 || got["code"] != "invalid_request" {
			t.Fatalf("%s: %d %v", body[:40], resp.StatusCode, got)
		}
	}
	resp, _ = authedPost(ts.URL+"/delegates/register", "test-token", `{"interface": `+string(doc)+`, "roles": ["synthesize"]}`)
	if got := mustJSON(t, resp); resp.StatusCode != 400 || got["code"] != "registration_failed" {
		t.Fatalf("domain rejection: %d %v", resp.StatusCode, got)
	}
	if after, _ := os.ReadFile(filepath.Join(envPath, app.EnvConfigFile)); !bytes.Equal(before, after) {
		t.Fatal("rejected requests mutated the environment")
	}

	resp, _ = authedPost(ts.URL+"/delegates/register", "test-token", `{"interface": `+string(doc)+`, "roles": ["invoke"], "rolePreferences": {"invoke": 9007199254740993}}`)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var record map[string]any
	if resp.StatusCode != 200 || jsonvalue.Unmarshal(raw, &record) != nil || record["id"] == "" {
		t.Fatalf("register: %d %s", resp.StatusCode, raw)
	}
	id := record["id"].(string)
	if !strings.Contains(string(raw), `9007199254740993`) {
		t.Fatalf("preference rounded on the wire: %s", raw)
	}
	// Query handling: exact filter, empty/duplicate/unknown refused.
	for query, want := range map[string]int{"?role=invoke": 200, "": 200, "?role=synthesize": 200, "?role=": 400, "?role=a&role=b": 400, "?other=1": 400} {
		resp, err := authedGet(ts.URL+"/delegates"+query, "test-token")
		if err != nil || resp.StatusCode != want {
			t.Fatalf("GET /delegates%s: %v %v, want %d", query, resp.StatusCode, err, want)
		}
		if want == 200 {
			got := mustJSON(t, resp)
			count := len(got["delegates"].([]any))
			if (query == "?role=synthesize") != (count == 0) {
				t.Fatalf("GET /delegates%s: %v", query, got)
			}
		} else {
			resp.Body.Close()
		}
	}
	// Mixed surfaces: prefer over HTTP, observe through the CLI, then back.
	for body, want := range map[string]int{
		`{"id":"` + id + `","role":"invoke","preference":0}`:                     200,
		`{"id":"` + id + `","role":"invoke"}`:                                    400,
		`{"id":"` + id + `","role":"invoke","preference":"1"}`:                   400,
		`{"id":"` + id + `","role":"inspect","preference":1}`:                    400,
		`{"id":"dlg_absent","role":"invoke","preference":null}`:                  400,
		`{"id":"` + id + `","role":"invoke","preference":null,"bindingSpec":""}`: 400,
	} {
		resp, _ := authedPost(ts.URL+"/delegates/preference", "test-token", body)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %s", body, resp.StatusCode, raw)
		}
		if want == 200 && strings.TrimSpace(string(raw)) != "null" {
			t.Fatalf("preference success must be null: %s", raw)
		}
	}
	cli := runDelegateCLI(t, "", "delegate", "list", "-F", "json")
	var listed map[string][]map[string]any
	if cli.Code != 0 || json.Unmarshal([]byte(cli.Message), &listed) != nil || listed["delegates"][0]["rolePreferences"].(map[string]any)["invoke"] != float64(0) {
		t.Fatalf("CLI did not observe the HTTP mutation: %+v", cli)
	}
	resp, _ = authedPost(ts.URL+"/delegates/binding-preference", "test-token", `{"id":"`+id+`","role":"invoke","bindingSpec":"example.test@1","preference":-1.25}`)
	if resp.StatusCode != 200 {
		t.Fatalf("binding preference: %v", mustJSON(t, resp))
	}
	resp.Body.Close()
	resp, _ = authedPost(ts.URL+"/delegates/binding-preference", "test-token", `{"id":"`+id+`","role":"inspect","bindingSpec":"example.test@1","preference":1}`)
	if got := mustJSON(t, resp); resp.StatusCode != 400 || got["code"] != "preference_failed" {
		t.Fatalf("override enrolled a role: %d %v", resp.StatusCode, got)
	}
	if result := runDelegateCLI(t, "", "delegate", "prefer", id, "--clear", "--role", "invoke"); result.Code != 0 {
		t.Fatalf("CLI clear: %+v", result)
	}
	resp, _ = authedGet(ts.URL+"/delegates?role=invoke", "test-token")
	got := mustJSON(t, resp)
	if prefs := got["delegates"].([]any)[0].(map[string]any)["rolePreferences"].(map[string]any); len(prefs) != 0 {
		t.Fatalf("HTTP did not observe the CLI clear: %v", prefs)
	}
	for i := 0; i < 2; i++ {
		resp, _ := authedPost(ts.URL+"/delegates/unregister", "test-token", `{"id":"`+id+`"}`)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || strings.TrimSpace(string(raw)) != "null" {
			t.Fatalf("unregister %d: %d %s", i, resp.StatusCode, raw)
		}
	}
	resp, _ = authedPost(ts.URL+"/delegates/unregister", "test-token", `{"id":""}`)
	if resp.StatusCode != 400 {
		t.Fatalf("empty id: %v", mustJSON(t, resp))
	}
	resp.Body.Close()
	// Transport guards remain on the added paths.
	for _, path := range []string{"/delegates/roles", "/delegates", "/delegates/binding-preference"} {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if path == "/delegates/binding-preference" {
			req, _ = http.NewRequest("POST", ts.URL+path, strings.NewReader(`{}`))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 401 {
			t.Fatalf("%s without token: %v %v", path, resp, err)
		}
		resp.Body.Close()
	}
	req, _ := http.NewRequest("POST", ts.URL+"/delegates/register", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "text/plain")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 415 {
		t.Fatalf("media type guard: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// Legacy state is 409 registry_unavailable on every route.
	if err := os.WriteFile(filepath.Join(envPath, app.EnvConfigFile), []byte(`{"delegates":[{"location":"exec:old"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct{ method, path, body string }{
		{"GET", "/delegates", ""},
		{"POST", "/delegates/register", `{"interface": ` + string(doc) + `, "roles": ["invoke"]}`},
		{"POST", "/delegates/preference", `{"id":"dlg_x","role":"invoke","preference":null}`},
		{"POST", "/delegates/unregister", `{"id":"dlg_x"}`},
	} {
		var resp *http.Response
		if call.method == "GET" {
			resp, _ = authedGet(ts.URL+call.path, "test-token")
		} else {
			resp, _ = authedPost(ts.URL+call.path, "test-token", call.body)
		}
		if got := mustJSON(t, resp); resp.StatusCode != 409 || got["code"] != "registry_unavailable" {
			t.Fatalf("%s %s over legacy state: %d %v", call.method, call.path, resp.StatusCode, got)
		}
	}
}
