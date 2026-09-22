package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openbindings/ob/internal/frames"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/bindingsupport"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

const roleFrameTestSpec = "example.delegate-frame-test@1"

// Independent frame-speaking binding implementation. It knows no registry,
// provider election, or production selector; tests observe what reaches it.
type roleFrameTestInvoker struct {
	mu            sync.Mutex
	selectors     []string
	opened        []frames.BindingInvocationInput
	terminal      *invoke.InvocationError
	waitForCancel bool
	stopped       chan struct{}
}

func (h *roleFrameTestInvoker) BindingSpecs() []bindingsupport.BindingSpecInfo {
	return []bindingsupport.BindingSpecInfo{{BindingSpec: roleFrameTestSpec}}
}
func (h *roleFrameTestInvoker) CheckBindingSpecs(tokens []string) []bindingsupport.BindingSpecVerdict {
	return bindingsupport.CheckBindingSpecs(tokens, h.BindingSpecs())
}
func (h *roleFrameTestInvoker) InvokeBinding(ctx context.Context, args *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	call := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		ctx, stop := invoke.DoneContext(ctx, call.Done())
		defer stop()
		if h.stopped != nil {
			defer close(h.stopped)
		}
		h.mu.Lock()
		h.selectors = append(h.selectors, args.Selector)
		h.mu.Unlock()
		emit := func(frame frames.OutputFrame) bool {
			raw, err := jsonvalue.Marshal(frame)
			var value any
			if err != nil || jsonvalue.Unmarshal(raw, &value) != nil {
				call.FireError(invoke.NewInvocationError(invoke.ErrCodeFrameProtocol))
				return false
			}
			return call.EmitOutput(value) == nil
		}
		for {
			value, err := call.ReadInput(ctx)
			if err != nil {
				return
			}
			raw, err := jsonvalue.Marshal(value)
			var frame frames.InputFrame
			if err != nil || jsonvalue.Unmarshal(raw, &frame) != nil {
				call.FireError(invoke.NewInvocationError(invoke.ErrCodeFrameProtocol))
				return
			}
			switch frame.Kind {
			case frames.KindOpen:
				h.mu.Lock()
				h.opened = append(h.opened, *frame.Input)
				h.mu.Unlock()
				if h.terminal != nil {
					emit(frames.Error(h.terminal))
					call.CloseOutput()
					return
				}
			case frames.KindInput:
				if !emit(frames.Output(frame.Value)) {
					return
				}
				if h.waitForCancel {
					<-ctx.Done()
					return
				}
			case frames.KindClose:
				emit(frames.Complete())
				call.CloseOutput()
				return
			}
		}
	}()
	return call
}

func roleFrameProvider(t *testing.T) json.RawMessage {
	t.Helper()
	return roleFrameProviderWithSelector(t, "qualified-work")
}

// roleFrameProviderWithSelector builds an invoke provider whose frame work
// binding uses the given selector. Two providers built this way are equally
// admissible (correspondence is on operation keys, not selectors); their work
// selectors differ so a test can tell which retained document actually ran.
func roleFrameProviderWithSelector(t *testing.T, workSelector string) json.RawMessage {
	t.Helper()
	expected, err := RequirementInterface(CapInvoke)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := openbindings.ValidateDocument(roleTestProvider(t, expected))
	if err != nil {
		t.Fatal(err)
	}
	provider.Sources["frames"] = openbindings.Source{BindingSpec: roleFrameTestSpec, Content: json.RawMessage(`{}`)}
	key := "provider.openbindings.binding-invoker.invokeBinding"
	provider.Bindings[key] = openbindings.BindingEntry{Operation: key, Source: "frames", Selector: workSelector}
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestRoleFrameEntrypointReplacementDoesNotReplaceRetainedWork proves R06/R08:
// a registration replaced with a different-but-admissible provider between
// support selection and workload must not change the retained work. The
// replacement (a different work selector) happens inside the support-query
// callback, i.e. after the runtime snapshot and prepared route but before any
// output. The retained invocation must still run the originally selected work
// (selector "qualified-work"), and a fresh invocation after it must observe the
// replacement (selector "replaced-work") without a second support query for the
// first. A provider re-fetch or reselect at or after use would run
// "replaced-work" for the retained call and fail this test.
func TestRoleFrameEntrypointReplacementDoesNotReplaceRetainedWork(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	record, err := r.register(RoleRegistrationInput{Interface: roleFrameProviderWithSelector(t, "qualified-work"), Roles: []string{"invoke"}})
	if err != nil {
		t.Fatal(err)
	}
	replaced := false
	queries := &roleTestInvoker{result: func(selector string, value any) any {
		if !replaced {
			// Replace the retained document mid-flight, once, after selection.
			if _, err := r.register(RoleRegistrationInput{ID: record.ID, Interface: roleFrameProviderWithSelector(t, "replaced-work"), Roles: []string{"invoke"}}); err != nil {
				t.Error(err)
			}
			replaced = true
		}
		return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
	}}
	work := &roleFrameTestInvoker{}
	installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	call := InvokeBindingHandle(ctx, roleFrameInput())
	defer call.Cancel()
	if err := call.Write(ctx, json.Number("1")); err != nil {
		t.Fatal(err)
	}
	if err := call.Close(); err != nil {
		t.Fatal(err)
	}
	out := call.Outputs()
	if _, err := out.Read(ctx); err != nil {
		t.Fatalf("retained work did not run: %v", err)
	}
	for {
		if _, err := out.Read(ctx); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("terminal: %v", err)
		}
	}
	work.mu.Lock()
	if len(work.selectors) != 1 || work.selectors[0] != "qualified-work" {
		t.Fatalf("retained work replaced by a concurrent registration: %v", work.selectors)
	}
	work.mu.Unlock()

	// A fresh invocation now sees the replacement: its work runs the new
	// selector, proving the registry did change and the first call held its
	// own snapshot rather than never observing the replacement.
	next := &roleFrameTestInvoker{}
	installRoleFrameRuntime(t, invoke.NewOperationInvoker(&roleTestInvoker{result: func(string, any) any {
		return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
	}}, next))
	ncall := InvokeBindingHandle(ctx, roleFrameInput())
	defer ncall.Cancel()
	if err := ncall.Write(ctx, json.Number("1")); err != nil {
		t.Fatal(err)
	}
	_ = ncall.Close()
	no := ncall.Outputs()
	if _, err := no.Read(ctx); err != nil {
		t.Fatalf("replacement not usable by a fresh call: %v", err)
	}
	for {
		if _, err := no.Read(ctx); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("fresh terminal: %v", err)
		}
	}
	next.mu.Lock()
	if len(next.selectors) != 1 || next.selectors[0] != "replaced-work" {
		t.Fatalf("fresh call did not observe the replacement: %v", next.selectors)
	}
	next.mu.Unlock()
}

func installRoleFrameRuntime(t *testing.T, engine *invoke.OperationInvoker) {
	t.Helper()
	old := defaultInvokerOverride
	defaultInvokerOverride = engine
	resetNativeTokens()
	t.Cleanup(func() { defaultInvokerOverride = old; resetNativeTokens() })
}

func roleFrameInput() InvocationInput {
	return InvocationInput{Source: InvokeSource{BindingSpec: "example.work@1", Location: "https://work.example.invalid/service"}, Selector: "echo", Context: map[string]any{"caller": "explicit-only"}}
}

func TestRoleFrameEntrypointRetainsSelectedProvider(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	record, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}})
	if err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(selector string, value any) any {
		if selector != "provider.openbindings.binding-invoker.checkBindingSpecs" {
			t.Errorf("decoy query: %s", selector)
		}
		if equal, err := jsonvalue.Equal(value, map[string]any{"bindingSpecs": []string{"example.work@1"}}); err != nil || !equal {
			t.Errorf("query leaked work/context: %v", value)
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
	call := InvokeBindingHandle(ctx, roleFrameInput())
	defer call.Cancel()
	values := []any{json.Number("9007199254740993"), nil, "last"}
	for _, value := range values {
		if err := call.Write(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := call.Close(); err != nil {
		t.Fatal(err)
	}
	out := call.Outputs()
	for _, want := range values {
		value, err := out.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if equal, err := jsonvalue.Equal(want, value); err != nil || !equal {
			t.Fatalf("value changed: %v %v", want, value)
		}
	}
	if _, err := out.Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal: %v", err)
	}
	work.mu.Lock()
	if len(work.selectors) != 1 || work.selectors[0] != "qualified-work" || len(work.opened) != 1 || work.opened[0].Context["caller"] != "explicit-only" {
		t.Errorf("wrong retained work: %v %v", work.selectors, work.opened)
	}
	work.mu.Unlock()
	next := InvokeBindingHandle(ctx, roleFrameInput())
	defer next.Cancel()
	if _, err := next.Outputs().Read(ctx); err == nil || errors.Is(err, io.EOF) {
		t.Fatal("removed registration still usable")
	}
	queries.mu.Lock()
	defer queries.mu.Unlock()
	if len(queries.calls) != 1 {
		t.Fatalf("reselected provider: %v", queries.calls)
	}
}

func TestRoleFrameEntrypointNoImplicitEnrollment(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	provider, _ := openbindings.ValidateDocument(roleFrameProvider(t))
	expected, _ := RequirementInterface(CapInspect)
	extra, _ := openbindings.ValidateDocument(roleTestProvider(t, expected))
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
	queries, work := &roleTestInvoker{}, &roleFrameTestInvoker{}
	installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work))
	call := InvokeBindingHandle(t.Context(), roleFrameInput())
	defer call.Cancel()
	if _, err := call.Outputs().Read(t.Context()); err == nil || errors.Is(err, io.EOF) {
		t.Fatal("unenrolled provider used")
	}
	if len(queries.calls) != 0 || len(work.selectors) != 0 {
		t.Fatal("unrequested role received query/work")
	}
}

func TestRoleFrameEntrypointRefusesInvalidRegistry(t *testing.T) {
	for _, kind := range []string{"legacy", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			r, provider := migrationTestRegistry(t)
			want := "explicit migration"
			if kind == "legacy" {
				seedLegacyMigration(t, r, provider)
			} else {
				if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), []byte(`{"delegateRegistry":null}`), 0600); err != nil {
					t.Fatal(err)
				}
				want = "registry"
			}
			call := InvokeBindingHandle(t.Context(), roleFrameInput())
			defer call.Cancel()
			_, err := call.Outputs().Read(t.Context())
			if err == nil {
				t.Fatal("invalid registry treated as empty")
			}
			encoded, _ := jsonvalue.Marshal(invoke.AsInvocationError(err))
			if !strings.Contains(strings.ToLower(string(encoded)), want) {
				t.Fatalf("missing actionable refusal: %s", encoded)
			}
		})
	}
}

func TestRoleFrameEntrypointContextAndCancellation(t *testing.T) {
	for _, mode := range []string{"challenge", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			for range 2 {
				if _, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}}); err != nil {
					t.Fatal(err)
				}
			}
			queries := &roleTestInvoker{result: func(string, any) any {
				return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
			}}
			work := &roleFrameTestInvoker{stopped: make(chan struct{}), waitForCancel: mode == "cancel"}
			if mode == "challenge" {
				work.terminal = invoke.NewContextRequiredError(&invoke.ContextRequiredDetails{Target: "https://unrelated.example.invalid", Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{Type: "auth.bearer"}}}}})
			}
			engine := invoke.NewOperationInvoker(queries, work)
			var resolutions atomic.Int32
			engine.ContextResolver = func(context.Context, *invoke.ContextRequiredDetails) (map[string]any, error) {
				resolutions.Add(1)
				return map[string]any{"bearerToken": "must-not-leak"}, nil
			}
			installRoleFrameRuntime(t, engine)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			call := InvokeBindingHandle(ctx, roleFrameInput())
			defer call.Cancel()
			if mode == "challenge" {
				_, err := call.Outputs().Read(ctx)
				if err == nil || invoke.AsInvocationError(err).Code != invoke.ErrCodeContextRequired {
					t.Fatalf("lost challenge: %v", err)
				}
			} else {
				if err := call.Write(ctx, "before-cancel"); err != nil {
					t.Fatal(err)
				}
				if value, err := call.Outputs().Read(ctx); err != nil || value != "before-cancel" {
					t.Fatalf("output: %v %v", value, err)
				}
				call.Cancel()
			}
			select {
			case <-work.stopped:
			case <-ctx.Done():
				t.Fatal("provider did not terminate")
			}
			work.mu.Lock()
			defer work.mu.Unlock()
			if len(work.selectors) != 1 || resolutions.Load() != 0 {
				t.Fatalf("work retried or resolved caller challenge: %v %d", work.selectors, resolutions.Load())
			}
		})
	}
}

func TestRoleFrameEntrypointNativeFirst(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-environment", true: "high-preference-provider"}[initialized], func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("OB_CONFIG_DIR", filepath.Join(t.TempDir(), "absent"))
			if initialized {
				r, _ := migrationTestRegistry(t)
				if _, err := r.register(RoleRegistrationInput{Interface: roleFrameProvider(t), Roles: []string{"invoke"}, RolePreferences: json.RawMessage(`{"invoke":1e400}`)}); err != nil {
					t.Fatal(err)
				}
			}
			queries, work := &roleTestInvoker{}, &roleFrameTestInvoker{}
			installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, openapi.NewAdapter()))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"native":true}`))
			}))
			defer server.Close()
			document := json.RawMessage(`{"openapi":"3.1.0","info":{"title":"Native","version":"1"},"servers":[{"url":"` + server.URL + `"}],"paths":{"/echo":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}}}}}`)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			call := InvokeBindingHandle(ctx, InvocationInput{Source: InvokeSource{BindingSpec: openapi.BindingSpecOpenAPI31, Content: document}, Selector: "#/paths/~1echo/get"})
			defer call.Cancel()
			_ = call.Close()
			value, err := invoke.Single(ctx, call.Outputs())
			if err != nil {
				t.Fatal(err)
			}
			if equal, err := jsonvalue.Equal(value, map[string]any{"native": true}); err != nil || !equal {
				t.Fatalf("native output: %v %v", value, err)
			}
			if requests.Load() != 1 || len(queries.calls) != 0 || len(work.selectors) != 0 {
				t.Fatal("native-first policy queried or invoked external provider")
			}
		})
	}
}

// A full production entrypoint, retained registration and SDK AsyncAPI hop.
// The query fixture has no routing knowledge; the peer implements only frames.
func TestRoleFrameEntrypointRealSDKStream(t *testing.T) {
	var artifact []byte
	var requests, documents atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /asyncapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		documents.Add(1)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(artifact)
	})
	mux.HandleFunc("GET /bindings/invoke", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer provider-transport-only" {
			t.Error("wrong provider transport credential")
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		opened := false
		for {
			_, raw, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var frame frames.InputFrame
			if err := jsonvalue.Unmarshal(raw, &frame); err != nil {
				t.Error(err)
				return
			}
			var output frames.OutputFrame
			switch frame.Kind {
			case frames.KindOpen:
				if opened || frame.Input.Source.BindingSpec != "example.work@1" || frame.Input.Selector != "echo" {
					t.Error("wrong open frame")
					return
				}
				if equal, err := jsonvalue.Equal(frame.Input.Context, map[string]any{"caller": "explicit-only"}); err != nil || !equal {
					t.Error("transport credential leaked into downstream context")
					return
				}
				opened = true
				continue
			case frames.KindInput:
				if !opened {
					t.Error("input before open")
					return
				}
				output = frames.Output(frame.Value)
			case frames.KindClose:
				output = frames.Complete()
			default:
				t.Error("wrong frame")
				return
			}
			raw, err = jsonvalue.Marshal(output)
			if err != nil {
				t.Error(err)
				return
			}
			if err := conn.Write(r.Context(), websocket.MessageText, raw); err != nil {
				return
			}
			if output.Terminal() {
				return
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	var err error
	artifact, err = GenerateServeAsyncAPI("../../ob.obi.json", strings.TrimPrefix(server.URL, "http://"), "ws")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := migrationTestRegistry(t)
	provider, err := openbindings.ValidateDocument(roleFrameProvider(t))
	if err != nil {
		t.Fatal(err)
	}
	provider.Sources["frames"] = openbindings.Source{BindingSpec: asyncapi.BindingSpec, Location: server.URL + "/asyncapi.yaml"}
	key := "provider.openbindings.binding-invoker.invokeBinding"
	provider.Bindings[key] = openbindings.BindingEntry{Operation: key, Source: "frames", Selector: "#/operations/invokeBinding"}
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{"invoke"}}); err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(selector string, value any) any {
		if selector != "provider.openbindings.binding-invoker.checkBindingSpecs" {
			t.Errorf("decoy query: %s", selector)
		}
		return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
	}}
	engine := invoke.NewOperationInvoker(queries, asyncapi.NewInvoker())
	engine.ContextResolver = func(_ context.Context, details *invoke.ContextRequiredDetails) (map[string]any, error) {
		if invoke.NormalizeEndpoint(details.Target) != invoke.NormalizeEndpoint(server.URL) {
			return nil, errors.New("unrelated target")
		}
		return map[string]any{"bearerToken": "provider-transport-only"}, nil
	}
	installRoleFrameRuntime(t, engine)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	call := InvokeBindingHandle(ctx, roleFrameInput())
	defer call.Cancel()
	values := []any{json.Number("9007199254740993"), json.Number("1e400"), json.Number("1e-400"), nil}
	out := call.Outputs()
	for _, value := range values {
		inputBytes, _ := jsonvalue.Marshal(frames.Input(value))
		var inputValue any
		if err := jsonvalue.Unmarshal(inputBytes, &inputValue); err != nil {
			t.Fatal(err)
		}
		if err := openbindings.ValidateOperationInput(inputValue, provider, key); err != nil {
			t.Fatalf("input validation for %v: %v", value, err)
		}
		outputBytes, _ := jsonvalue.Marshal(frames.Output(value))
		var outputValue any
		if err := jsonvalue.Unmarshal(outputBytes, &outputValue); err != nil {
			t.Fatal(err)
		}
		if err := openbindings.ValidateOperationOutput(outputValue, provider, key); err != nil {
			t.Fatalf("output validation for %v: %v", value, err)
		}
		if err := call.Write(ctx, value); err != nil {
			t.Fatal(err)
		}
		got, err := out.Read(ctx)
		if err != nil {
			t.Fatalf("reading expected %v: %v (requests=%d artifacts=%d)", value, err, requests.Load(), documents.Load())
		}
		if equal, err := jsonvalue.Equal(got, value); err != nil || !equal {
			t.Fatalf("value changed: %v %v", value, got)
		}
	}
	if err := call.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal: %v", err)
	}
	if requests.Load() != 1 || documents.Load() == 0 {
		t.Fatalf("transport calls: %d artifacts: %d", requests.Load(), documents.Load())
	}
}
