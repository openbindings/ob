package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

func TestRoleDiagnosticSurfaces(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("OB_CONFIG_DIR", t.TempDir())
	if _, err := app.Init(false); err != nil {
		t.Fatal(err)
	}
	ts := testEnv(t)
	defer ts.Close()
	for _, test := range []struct {
		role, token, path, wantPath string
		available                   bool
	}{
		{"invoke", "openbindings.usage@1", "", "ranked", true},
		{"invoke", "openbindings.usage@1", "native-first", "native-first", true},
		{"synthesize", "openbindings.openapi-3.1@1", "", "native-first", true},
		{"inspect", "openbindings.openapi-3.1@1", "", "native-first", true},
		{"invoke", "example.UNSUPPORTED@1", "", "ranked", false},
	} {
		t.Run(test.role+"/"+test.wantPath+"/"+test.token, func(t *testing.T) {
			input := map[string]any{"role": test.role, "bindingSpec": test.token}
			args := []string{"delegate", "resolve", "--role", test.role, "--binding-spec", test.token, "-F", "json"}
			if test.path != "" {
				input["path"] = test.path
				args = append(args, "--path", test.path)
			}
			root := NewRoot()
			root.SetArgs(args)
			result, ok := root.ExecuteContext(t.Context()).(app.ExitResult)
			if !ok || result.Code != 0 {
				t.Fatalf("CLI: %+v", result)
			}
			var cli map[string]any
			if err := json.Unmarshal([]byte(result.Message), &cli); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(input)
			resp, err := authedPost(ts.URL+"/delegates/resolve", "test-token", string(raw))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("HTTP %d: %v", resp.StatusCode, mustJSON(t, resp))
			}
			httpValue := mustJSON(t, resp)
			want := map[string]any{"role": test.role, "bindingSpec": test.token, "path": test.wantPath, "available": test.available, "builtin": test.available}
			if !reflect.DeepEqual(cli, want) || !reflect.DeepEqual(httpValue, want) {
				t.Fatalf("CLI=%v HTTP=%v want=%v", cli, httpValue, want)
			}
		})
	}
	for _, body := range []string{
		`{}`, `null`, `[]`,
		`{"role":"invoke","bindingSpec":""}`,
		`{"role":"other","bindingSpec":"openbindings.usage@1"}`,
		`{"role":"inspect","bindingSpec":"openbindings.usage@1","path":"ranked"}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","path":"explicit"}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","registrationId":"missing"}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","registrationId":"exec:ob"}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","registrationId":null}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","registrationId":""}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","path":null}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","path":""}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","registrationId":"id","path":"ranked"}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1","unknown":true}`,
		`{"role":"invoke","bindingSpec":"openbindings.usage@1"} {}`,
	} {
		resp, err := authedPost(ts.URL+"/delegates/resolve", "test-token", body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 400 {
			t.Errorf("accepted %s: %d", body, resp.StatusCode)
		}
		response := mustJSON(t, resp)
		if _, ok := response["available"]; ok {
			t.Fatalf("refusal became negative result: %v", response)
		}
	}
	for _, args := range [][]string{
		{"delegate", "resolve"},
		{"delegate", "resolve", "old-operation"},
		{"delegate", "resolve-binding-spec", "openbindings.usage@1"},
		{"delegate", "resolve", "--role", "invoke", "--binding-spec", "openbindings.usage@1", "--registration", ""},
		{"delegate", "resolve", "--role", "invoke", "--binding-spec", "openbindings.usage@1", "--registration", "missing"},
		{"delegate", "resolve", "--role", "inspect", "--binding-spec", "openbindings.usage@1", "--path", "ranked"},
	} {
		root := NewRoot()
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
		if result, ok := err.(app.ExitResult); ok && result.Code == 0 {
			t.Fatalf("invalid args succeeded: %v", args)
		}
	}
	for _, route := range []string{"/delegates/resolve/old-operation", "/delegates/resolve-binding-spec"} {
		resp, err := authedPost(ts.URL+route, "test-token", `{}`)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("retired route active: %s", route)
		}
	}
	// Existing middleware must protect the diagnostic too.
	for _, test := range []struct {
		token, origin string
		status        int
	}{
		{"wrong", "", 401}, {"test-token", "https://untrusted.example", 200},
	} {
		req, _ := http.NewRequest("POST", ts.URL+"/delegates/resolve", strings.NewReader(`{"role":"invoke","bindingSpec":"openbindings.usage@1"}`))
		req.Header.Set("Authorization", "Bearer "+test.token)
		if test.origin != "" {
			req.Header.Set("Origin", test.origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != test.status {
			t.Fatalf("guard status %d, want %d", resp.StatusCode, test.status)
		}
		// CORS is a browser disclosure guard, not bearer authentication.
		if test.origin != "" && resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted browser origin was granted response access")
		}
	}
	// Corrupt or legacy registry state is not unavailable/builtin fallback.
	envPath, err := app.FindEnvPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, contents := range []string{`{"delegateRegistry":null}`, `{"delegates":[{"location":"exec:never-run"}]}`} {
		if err := os.WriteFile(filepath.Join(envPath, app.EnvConfigFile), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		resp, err := authedPost(ts.URL+"/delegates/resolve", "test-token", `{"role":"invoke","bindingSpec":"openbindings.usage@1"}`)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 400 || strings.Contains(string(body), `"available"`) {
			t.Fatalf("state refusal lost: %d %s", resp.StatusCode, body)
		}
	}
}
