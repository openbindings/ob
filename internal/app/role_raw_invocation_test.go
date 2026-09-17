package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func TestRoleRawInvocationRetainsProvider(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	record, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}})
	if err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(selector string, value any) any {
		if selector != "provider.openbindings.binding-invoker.checkBindingSpecs" {
			t.Errorf("query used a decoy: %s", selector)
		}
		if equal, err := jsonvalue.Equal(value, map[string]any{"bindingSpecs": []string{"example.work@1"}}); err != nil || !equal {
			t.Errorf("query received caller work/context: %v", value)
		}
		if err := r.unregister(record.ID); err != nil {
			t.Error(err)
		}
		return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
	}}
	work := &roleFrameTestInvoker{}
	installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	input := roleFrameInput()
	input.Input = map[string]any{"n": json.Number("1e400")}
	result := InvokeOperationWithContext(ctx, input)
	if result.Error != nil {
		t.Fatalf("retained registration not used: %+v", result.Error)
	}
	if equal, err := jsonvalue.Equal(result.Output, input.Input); err != nil || !equal {
		t.Fatalf("exact input changed: %v", result.Output)
	}
	if next := InvokeOperationWithContext(ctx, input); next.Error == nil {
		t.Fatal("removed registration remained available to a fresh call")
	}
	queries.mu.Lock()
	defer queries.mu.Unlock()
	work.mu.Lock()
	defer work.mu.Unlock()
	if len(queries.calls) != 1 || len(work.selectors) != 1 || work.selectors[0] != "qualified-work" || len(work.opened) != 1 {
		t.Fatalf("query-to-work affinity: %v %v", queries.calls, work.selectors)
	}
	if equal, err := jsonvalue.Equal(work.opened[0].Context, input.Context); err != nil || !equal {
		t.Fatal("unexpected downstream context")
	}
}

func TestRoleRawInvocationNativeRanking(t *testing.T) {
	for _, name := range []string{"no-environment", "native-tie", "preferred-external"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("OB_CONFIG_DIR", filepath.Join(t.TempDir(), "absent"))
			if name != "no-environment" {
				r, _ := migrationTestRegistry(t)
				preference := json.RawMessage(`{"invoke":0}`)
				if name == "preferred-external" {
					preference = json.RawMessage(`{"invoke":1e400}`)
				}
				if _, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}, RolePreferences: preference}); err != nil {
					t.Fatal(err)
				}
			}
			queries := &roleTestInvoker{result: func(string, any) any {
				return []any{map[string]any{"bindingSpec": openapi.BindingSpecOpenAPI31, "supported": true}}
			}}
			work := &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"native":true}`))
			}))
			defer server.Close()
			doc := json.RawMessage(`{"openapi":"3.1.0","info":{"title":"Native ranking","version":"1"},"servers":[{"url":"` + server.URL + `"}],"paths":{"/echo":{"post":{"requestBody":{"content":{"application/json":{"schema":{"type":"string"}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			bindingInput := map[string]any{"body": "delegate-echo"}
			result := InvokeOperationWithContext(ctx, InvocationInput{Source: InvokeSource{BindingSpec: openapi.BindingSpecOpenAPI31, Content: doc}, Selector: "#/paths/~1echo/post", Input: bindingInput})
			if result.Error != nil {
				t.Fatalf("invocation: %+v", result.Error)
			}
			var want any = map[string]any{"native": true}
			wantRequests, wantWork, wantQueries := int32(1), 0, 1
			if name == "no-environment" {
				wantQueries = 0
			}
			if name == "preferred-external" {
				want, wantRequests, wantWork = bindingInput, 0, 1
			}
			if equal, err := jsonvalue.Equal(result.Output, want); err != nil || !equal {
				t.Fatalf("wrong winner: %v want %v", result.Output, want)
			}
			queries.mu.Lock()
			defer queries.mu.Unlock()
			work.mu.Lock()
			defer work.mu.Unlock()
			if requests.Load() != wantRequests || len(work.selectors) != wantWork || len(queries.calls) != wantQueries {
				t.Fatalf("routing counters: native=%d external=%d queries=%d", requests.Load(), len(work.selectors), len(queries.calls))
			}
		})
	}
}

func TestRoleRawInvocationRejectsInvalidRegistry(t *testing.T) {
	for _, mode := range []string{"legacy", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			r, provider := migrationTestRegistry(t)
			want := "explicit migration"
			if mode == "legacy" {
				seedLegacyMigration(t, r, provider)
			} else {
				if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), []byte(`{"delegateRegistry":null}`), 0600); err != nil {
					t.Fatal(err)
				}
				want = "registry"
			}
			result := InvokeOperationWithContext(t.Context(), roleFrameInput())
			if result.Error == nil || !strings.Contains(strings.ToLower(result.Error.Message), want) {
				t.Fatalf("invalid state treated as empty: %+v", result.Error)
			}
		})
	}
}

func TestRoleRawInvocationRefusesMaliciousTarget(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	for range 2 {
		if _, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}}); err != nil {
			t.Fatal(err)
		}
	}
	queries := &roleTestInvoker{result: func(string, any) any {
		return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
	}}
	work := &roleFrameTestInvoker{terminal: invoke.NewContextRequiredError(&invoke.ContextRequiredDetails{Target: "https://unrelated.example.invalid", Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{Type: "auth.bearer"}}}}})}
	engine := invoke.NewOperationInvoker(queries, work)
	var resolutions atomic.Int32
	engine.ContextResolver = func(context.Context, *invoke.ContextRequiredDetails) (map[string]any, error) {
		resolutions.Add(1)
		return map[string]any{"bearerToken": "must-not-leak"}, nil
	}
	installRoleFrameRuntime(t, engine)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	input := roleFrameInput()
	input.Input = "caller-input"
	result := InvokeOperationWithContext(ctx, input)
	if result.Error == nil || result.Error.Code != errCodeDelegateTargetRefused {
		t.Fatalf("wrong target refusal: %+v", result.Error)
	}
	work.mu.Lock()
	defer work.mu.Unlock()
	if resolutions.Load() != 0 || len(work.selectors) != 1 {
		t.Fatalf("unrelated target resolved or work failed over: %d %v", resolutions.Load(), work.selectors)
	}
}
