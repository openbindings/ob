package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Real CLI and HTTP boundaries over retained role registrations. A separate
// synthetic provider exposes support over native OpenAPI; no mocked SDK/runtime,
// real configuration, installed executable or production endpoint participates.
func TestRoleDiagnosticProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the real binary and loopback server")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ob")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-race", "-o", bin, "./cmd/ob")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	r, _ := migrationTestRegistry(t)
	var queries, work atomic.Int32
	var mode atomic.Int32
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path != "/check" {
			work.Add(1)
			http.Error(w, "unexpected workload", 500)
			return
		}
		queries.Add(1)
		var input map[string]any
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		tokens, ok := input["bindingSpecs"].([]any)
		if !ok || len(tokens) != 1 || len(input) != 1 {
			t.Errorf("not token-only: %v", input)
			return
		}
		if mode.Load() == 2 {
			fmt.Fprint(w, `[{"supported":"invalid"}]`)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{map[string]any{"bindingSpec": tokens[0], "supported": mode.Load() == 0}})
	}))
	defer providerServer.Close()
	provider, err := RequirementInterface(CapSynthesize)
	if err != nil {
		t.Fatal(err)
	}
	provider.Description = "retained-private-provider-must-not-leak"
	artifact := map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "Support fixture", "version": "1"}, "servers": []any{map[string]any{"url": providerServer.URL}}}
	paths := map[string]any{}
	provider.Bindings = map[string]openbindings.BindingEntry{}
	expectedOperations := provider.Operations
	provider.Operations = map[string]openbindings.Operation{}
	for key, op := range expectedOperations {
		path := "/work"
		if strings.HasSuffix(key, ".checkBindingSpecs") {
			path = "/check"
		}
		paths[path] = map[string]any{"post": map[string]any{"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{}}}}, "responses": map[string]any{"200": map[string]any{"description": "ok", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{}}}}}}}
		// Qualified alias dispatch, not a bare-name shortcut.
		op.Aliases = append(op.Aliases, key)
		provider.Operations["fixture."+key] = op
		provider.Bindings[key] = openbindings.BindingEntry{Operation: "fixture." + key, Source: "api", Selector: "#/paths/~1" + strings.TrimPrefix(path, "/") + "/post", InputTransform: &openbindings.TransformOrRef{Inline: `{"body": $$}`}}
	}
	artifact["paths"] = paths
	artifactJSON, err := jsonvalue.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	provider.Sources = map[string]openbindings.Source{"api": {BindingSpec: openapi.BindingSpecOpenAPI31, Content: artifactJSON}}
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	record, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{"synthesize"}, RolePreferences: json.RawMessage(`{"synthesize":-1e400}`)})
	if err != nil {
		t.Fatal(err)
	}
	// A higher-preference alternate must not be consulted by explicit lookup.
	if _, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{"synthesize"}, RolePreferences: json.RawMessage(`{"synthesize":1e400}`)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	serverCmd := exec.CommandContext(ctx, bin, "start", "--port", fmt.Sprint(port), "--strict-port")
	serverCmd.Env = append(os.Environ(), "OB_START_TOKEN=diagnostic-test-token")
	stderr, err := serverCmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := serverCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverCmd.Process.Kill(); _ = serverCmd.Wait() })
	ready := make(chan string, 1)
	go func() {
		defer close(ready)
		scanner := bufio.NewScanner(stderr)
		pattern := regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`)
		for scanner.Scan() {
			if address := pattern.FindString(scanner.Text()); address != "" {
				select {
				case ready <- address:
				default:
				}
			}
		}
	}()
	var address string
	select {
	case address = <-ready:
		if address == "" {
			t.Fatal("server exited before readiness")
		}
	case <-ctx.Done():
		t.Fatal("server did not become ready")
	}
	for _, transport := range []string{"CLI", "HTTP"} {
		for _, scenario := range []string{"supported", "false", "malformed", "missing", "wrong-role"} {
			t.Run(transport+"/"+scenario, func(t *testing.T) {
				mode.Store(0)
				if scenario == "false" {
					mode.Store(1)
				}
				if scenario == "malformed" {
					mode.Store(2)
				}
				id, role := record.ID, "synthesize"
				if scenario == "missing" {
					id = "https://not-an-id.invalid"
				}
				if scenario == "wrong-role" {
					role = "inspect"
				}
				before := queries.Load()
				var data []byte
				var failed bool
				if transport == "CLI" {
					command := exec.CommandContext(ctx, bin, "delegate", "resolve", "--role", role, "--binding-spec", openapi.BindingSpecOpenAPI31, "--registration", id, "-F", "json")
					var err error
					data, err = command.CombinedOutput()
					failed = err != nil
				} else {
					body, _ := json.Marshal(RoleResolutionInput{Role: DelegateCapability(role), BindingSpec: openapi.BindingSpecOpenAPI31, RegistrationID: id})
					req, _ := http.NewRequestWithContext(ctx, "POST", address+"/delegates/resolve", strings.NewReader(string(body)))
					req.Header.Set("Authorization", "Bearer diagnostic-test-token")
					req.Header.Set("Content-Type", "application/json")
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					var value json.RawMessage
					err = json.NewDecoder(resp.Body).Decode(&value)
					resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					data = value
					failed = resp.StatusCode != 200
				}
				wantFailure := scenario == "malformed" || scenario == "missing" || scenario == "wrong-role"
				if failed != wantFailure {
					t.Fatalf("failure=%v want=%v queries=%d work=%d: %s", failed, wantFailure, queries.Load()-before, work.Load(), data)
				}
				if !failed {
					var result RoleResolutionOutput
					if err := json.Unmarshal(data, &result); err != nil {
						t.Fatal(err)
					}
					if result.Path != "explicit" || result.Builtin || result.Available != (scenario == "supported") || result.Role != CapSynthesize || result.BindingSpec != openapi.BindingSpecOpenAPI31 {
						t.Fatalf("wrong result: %+v", result)
					}
					if result.Available && result.RegistrationID != record.ID || !result.Available && result.RegistrationID != "" {
						t.Fatal("wrong registration")
					}
				}
				wantQueries := int32(1)
				if scenario == "missing" || scenario == "wrong-role" {
					wantQueries = 0
				}
				if queries.Load()-before != wantQueries || work.Load() != 0 {
					t.Fatalf("queries=%d want=%d work=%d", queries.Load()-before, wantQueries, work.Load())
				}
				if strings.Contains(string(data), provider.Description) || strings.Contains(string(data), providerServer.URL) {
					t.Fatalf("provider disclosure: %s", data)
				}
			})
		}
	}
}
