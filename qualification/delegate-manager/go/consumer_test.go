// Independent Go consumer for the Delegate Manager. It never imports an ob
// package: it discovers the candidate's bound CLI OBI (ob --openbindings) and
// served OBI (/.well-known/openbindings) and invokes the five shared
// operations through the SDK's ordinary operation invocation over the Usage
// and OpenAPI bindings, then proves that management led to actual
// role-scoped delegated work on an independently counted provider.
//
// Required environment:
//
//	OB_CANDIDATE_BINARY   absolute path of the candidate ob executable
//	OB_PROVIDER_BINARY    absolute path of the built cmd/provider fixture
package consumer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/formats/usage"
	"github.com/openbindings/openbindings-go/invoke"
	jsonataevaluator "github.com/openbindings/openbindings-go/invoke/jsonata"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

const (
	shared     = "openbindings.delegate-manager."
	listRoles  = shared + "listRoles"
	register   = shared + "registerDelegate"
	list       = shared + "listDelegates"
	prefer     = shared + "setDelegatePreference"
	unregister = shared + "unregisterDelegate"
)

type harness struct {
	t         *testing.T
	binary    string
	workDir   string
	env       []string
	serverURL string
	token     string
	cliOBI    *openbindings.Interface
	servedOBI *openbindings.Interface
	provider  providerFixture
}

type providerFixture struct {
	URL   string `json:"url"`
	OBI   string `json:"obi"`
	Token string `json:"token"`
	value json.RawMessage
}

func requireEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	if _, err := os.Stat(value); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return value
}

// The consumer's own transcription of the published bound-CLI recipe
// (docs/bound-cli-recipe.md): JSON machine lanes for the manager operations
// and the stdin route for the by-value interface document.
func recipe() usage.HookTable {
	ops := []string{"openbindings.ob.listDelegateRoles", "openbindings.ob.registerDelegate", "openbindings.ob.listDelegates", "openbindings.ob.setDelegatePreference", "openbindings.ob.unregisterDelegate", "openbindings.ob.resolveRoleDelegate", "openbindings.ob.describe"}
	return usage.HookTable{DecodeJSON: ops, Routes: map[string]map[string]string{"openbindings.ob.registerDelegate": {"interface": usage.RouteStdinDash}}}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, binary: requireEnv(t, "OB_CANDIDATE_BINARY"), workDir: t.TempDir()}
	// The Usage transport resolves `ob` on this process's PATH and children
	// inherit this process's environment: isolate both here, for every ob the
	// harness or the SDK starts. No user configuration, cache or credential
	// store is reachable.
	t.Setenv("PATH", filepath.Dir(h.binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OB_CONFIG_DIR", filepath.Join(h.workDir, "config"))
	t.Setenv("OB_CACHE_DIR", filepath.Join(h.workDir, "cache"))
	t.Setenv("OB_CREDENTIALS_FILE", filepath.Join(h.workDir, "credentials.json"))
	t.Setenv("OB_NO_UPDATE_CHECK", "1")
	h.env = os.Environ()
	if out, err := h.run("init"); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	// The bound CLI OBI is what a delegate registrar consumes.
	raw, err := h.run("--openbindings")
	if err != nil {
		t.Fatalf("--openbindings: %v", err)
	}
	if err := jsonvalue.Unmarshal([]byte(raw), &h.cliOBI); err != nil {
		t.Fatal(err)
	}
	h.startServer()
	h.startProvider()
	return h
}

func (h *harness) run(args ...string) (string, error) {
	cmd := exec.Command(h.binary, args...)
	cmd.Dir = h.workDir
	cmd.Env = h.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (h *harness) startServer() {
	t := h.t
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	h.token = "consumer-harness-token"
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, h.binary, "start", "--port", fmt.Sprint(port), "--strict-port", "--token", h.token)
	cmd.Dir = h.workDir
	cmd.Env = h.env
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
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
	select {
	case h.serverURL = <-ready:
	case <-time.After(60 * time.Second):
		t.Fatal("ob start did not become ready")
	}
	if h.serverURL == "" {
		t.Fatal("ob start exited before readiness")
	}
	resp, err := http.Get(h.serverURL + "/.well-known/openbindings")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := jsonvalue.Unmarshal(body, &h.servedOBI); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) startProvider() {
	t := h.t
	providerBinary := requireEnv(t, "OB_PROVIDER_BINARY")
	// Expected outcomes come from the interface: the provider is built from
	// the candidate's advertised accepted interfaces, not from ob's source.
	roles := h.cli(t, listRoles, nil).(map[string]any)["roles"].([]any)
	write := func(id string) string {
		for _, raw := range roles {
			role := raw.(map[string]any)
			if role["id"] == id {
				data, _ := json.Marshal(role["acceptedInterfaces"].([]any)[0])
				path := filepath.Join(h.workDir, id+".accepted.json")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				return path
			}
		}
		t.Fatalf("role %s not advertised", id)
		return ""
	}
	obiPath := filepath.Join(h.workDir, "provider.obi.json")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, providerBinary, "--accepted", write("invoke"), "--extra", write("synthesize"), "--obi", obiPath)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); cancel(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(line), &h.provider); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	h.provider.value = value
}

func (h *harness) counters() map[string]any {
	resp, err := http.Get(h.provider.URL + "/counters")
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// newEngine composes an ordinary SDK operation invoker with the SDK's Core
// transform evaluator; the bindings' adaptation transforms are part of the
// published OBIs and evaluate on the consumer's side.
func newEngine(t *testing.T, invokers ...invoke.BindingInvoker) *invoke.OperationInvoker {
	t.Helper()
	evaluator, err := jsonataevaluator.New(jsonataevaluator.Options{})
	if err != nil {
		t.Fatal(err)
	}
	engine := invoke.NewOperationInvoker(invokers...)
	engine.TransformEvaluator = evaluator
	return engine
}

// cli invokes a shared operation through the bound CLI OBI's Usage binding.
func (h *harness) cli(t *testing.T, operation string, input any) any {
	t.Helper()
	inv := usage.NewInvoker()
	inv.AuthorizeExec = func(argv []string) bool { return len(argv) > 0 && (argv[0] == "ob" || argv[0] == h.binary) }
	engine := newEngine(t, inv)
	engine.OutputDecoder, engine.ResultClassifier, engine.FieldRouter = recipe().Hooks()
	// Every CLI realization is unary: one input message (null for an
	// input-less operation) carries the binding's adaptation transform.
	return invokeUnary(t, engine, h.cliOBI, operation, input, true)
}

// httpOp invokes a shared operation through the served OBI's OpenAPI binding.
func (h *harness) httpOp(t *testing.T, operation string, input any) any {
	t.Helper()
	engine := newEngine(t, openapi.NewInvoker())
	return invokeUnary(t, engine, h.servedOBI, operation, input, input != nil, invoke.WithContext(map[string]any{
		"bearerToken":   h.token,
		"configuration": map[string]any{"security": map[string]any{"index": 1}},
	}))
}

func invokeUnary(t *testing.T, engine *invoke.OperationInvoker, iface *openbindings.Interface, operation string, input any, writeInput bool, options ...invoke.InvokeOption) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	call := invoke.Invoke(ctx, engine, iface, invoke.NewOperationSignature[any, any](operation), options...)
	defer call.Cancel()
	if writeInput {
		if err := call.Write(ctx, input); err != nil {
			t.Fatalf("%s write: %v", operation, err)
		}
	}
	if err := call.Close(); err != nil {
		t.Fatalf("%s close: %v", operation, err)
	}
	out, err := call.Outputs().Read(ctx)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		if failure := invoke.AsInvocationError(err); failure != nil {
			data, _ := json.Marshal(failure.Data)
			t.Fatalf("%s: %s (%s)", operation, failure.Code, data)
		}
		t.Fatalf("%s: %v", operation, err)
	}
	return out
}

func invokeExpectError(t *testing.T, engine *invoke.OperationInvoker, iface *openbindings.Interface, operation string, input any, options ...invoke.InvokeOption) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	call := invoke.Invoke(ctx, engine, iface, invoke.NewOperationSignature[any, any](operation), options...)
	defer call.Cancel()
	if input != nil {
		if err := call.Write(ctx, input); err != nil {
			return err
		}
	}
	if err := call.Close(); err != nil {
		return err
	}
	_, err := call.Outputs().Read(ctx)
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("%s: expected a failure", operation)
	}
	return err
}

func exact(t *testing.T, value any) string {
	t.Helper()
	raw, err := jsonvalue.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestConsumerLifecycleAndDelegation(t *testing.T) {
	h := newHarness(t)
	var providerValue any
	if err := jsonvalue.Unmarshal(h.provider.value, &providerValue); err != nil {
		t.Fatal(err)
	}
	for _, surface := range []struct {
		name string
		call func(t *testing.T, operation string, input any) any
	}{{"usage", h.cli}, {"openapi", h.httpOp}} {
		t.Run(surface.name, func(t *testing.T) {
			call := surface.call
			roles := call(t, listRoles, nil).(map[string]any)["roles"].([]any)
			ids := map[string]bool{}
			for _, raw := range roles {
				role := raw.(map[string]any)
				ids[role["id"].(string)] = true
				if role["description"] == "" || len(role["acceptedInterfaces"].([]any)) == 0 {
					t.Fatalf("incomplete role: %v", role)
				}
			}
			if !ids["invoke"] || !ids["synthesize"] || !ids["inspect"] || len(ids) != 3 {
				t.Fatalf("roles: %v", ids)
			}
			// Enroll only for invoke, with exact preferences; the provider's
			// synthesize capability is present but must never be enrolled.
			var prefs any
			_ = jsonvalue.Unmarshal([]byte(`{"invoke": 9007199254740993}`), &prefs)
			record := call(t, register, map[string]any{"interface": providerValue, "roles": []any{"invoke"}, "rolePreferences": prefs}).(map[string]any)
			id, _ := record["id"].(string)
			if id == "" {
				t.Fatalf("no id: %v", record)
			}
			if exact(t, record["rolePreferences"]) != `{"invoke":9007199254740993}` {
				t.Fatalf("preference not exact: %s", exact(t, record["rolePreferences"]))
			}
			if exact(t, record["roles"]) != `["invoke"]` {
				t.Fatalf("roles changed: %s", exact(t, record["roles"]))
			}
			if equal, err := jsonvalue.Equal(record["interface"], providerValue); err != nil || !equal {
				t.Fatal("retained interface differs from the supplied value")
			}
			second := call(t, register, map[string]any{"interface": providerValue, "roles": []any{"invoke"}}).(map[string]any)
			if second["id"] == id || exact(t, second["rolePreferences"]) != `{}` {
				t.Fatalf("second enrollment: %v", second)
			}
			listed := call(t, list, map[string]any{"role": "invoke"}).(map[string]any)["delegates"].([]any)
			if len(listed) != 2 || listed[0].(map[string]any)["id"] != id {
				t.Fatalf("filtered list: %d entries", len(listed))
			}
			if len(call(t, list, map[string]any{"role": "synthesize"}).(map[string]any)["delegates"].([]any)) != 0 {
				t.Fatal("unrequested capability was enrolled")
			}
			// Preference fidelity: explicit zero, negative fraction, null clear.
			for _, value := range []string{"0", "-1.25", "1e400"} {
				var number any
				_ = jsonvalue.Unmarshal([]byte(value), &number)
				if out := call(t, prefer, map[string]any{"id": id, "role": "invoke", "preference": number}); out != nil {
					t.Fatalf("preference must return null, got %v", out)
				}
				got := call(t, list, map[string]any{"role": "invoke"}).(map[string]any)["delegates"].([]any)[0].(map[string]any)["rolePreferences"]
				if exact(t, got) != `{"invoke":`+value+`}` {
					t.Fatalf("preference %s not retained: %s", value, exact(t, got))
				}
			}
			if out := call(t, prefer, map[string]any{"id": id, "role": "invoke", "preference": nil}); out != nil {
				t.Fatalf("clear must return null, got %v", out)
			}
			got := call(t, list, nil).(map[string]any)["delegates"].([]any)[0].(map[string]any)["rolePreferences"]
			if exact(t, got) != `{}` {
				t.Fatalf("null did not clear: %s", exact(t, got))
			}
			// Replacement keeps the ID and applies the complete map; removal is idempotent.
			replaced := call(t, register, map[string]any{"id": id, "interface": providerValue, "roles": []any{"invoke"}, "rolePreferences": map[string]any{}}).(map[string]any)
			if replaced["id"] != id {
				t.Fatalf("replacement changed id: %v", replaced)
			}
			for i := 0; i < 2; i++ {
				if out := call(t, unregister, map[string]any{"id": second["id"]}); out != nil {
					t.Fatalf("unregister must return null, got %v", out)
				}
			}
			if len(call(t, list, nil).(map[string]any)["delegates"].([]any)) != 1 {
				t.Fatal("removal touched the wrong record")
			}
			// Delegated work: the invoke role now routes to this provider for
			// its exact token, with a support query first and streaming work.
			before := h.counters()
			h.streamThroughCandidate(t)
			after := h.counters()
			if after["work"].(float64) != before["work"].(float64)+1 || after["support"].(float64) < before["support"].(float64)+1 {
				t.Fatalf("delegated work not observed: before %v after %v", before, after)
			}
			if after["extra"].(float64) != 0 || after["decoyCalls"].(float64) != 0 || after["credentialBearing"].(float64) != 0 {
				t.Fatalf("unrequested capability, decoy or credential reached the provider: %v", after)
			}
			if out := call(t, unregister, map[string]any{"id": id}); out != nil {
				t.Fatal("final removal must return null")
			}
			if len(call(t, list, nil).(map[string]any)["delegates"].([]any)) != 0 {
				t.Fatal("registry not empty after removal")
			}
			// A fresh lookup after removal refuses: nothing supports the token.
			if resolved := call(t, "openbindings.ob.resolveRoleDelegate", map[string]any{"role": "invoke", "bindingSpec": h.provider.Token}).(map[string]any); resolved["available"] != false {
				t.Fatalf("removed registration still routed: %v", resolved)
			}
		})
	}
}

// streamThroughCandidate drives the candidate's served invokeBinding frame
// stream (its genuine AsyncAPI binding) for the provider's token and proves
// the values echo back exactly through the delegated provider.
func (h *harness) streamThroughCandidate(t *testing.T) {
	t.Helper()
	engine := newEngine(t, asyncapi.NewInvoker())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	call := invoke.Invoke(ctx, engine, h.servedOBI, invoke.NewOperationSignature[any, any]("openbindings.ob.invokeBinding"),
		invoke.WithContext(map[string]any{"bearerToken": h.token, "configuration": map[string]any{"websocketMessageType": "text"}}))
	defer call.Cancel()
	open := map[string]any{"kind": "open", "input": map[string]any{
		"source":   map[string]any{"bindingSpec": h.provider.Token, "location": "https://work.example.invalid/service"},
		"selector": "echo",
		"context":  map[string]any{"caller": "consumer-harness"},
	}}
	if err := call.Write(ctx, open); err != nil {
		t.Fatalf("open: %v", err)
	}
	var big any
	_ = jsonvalue.Unmarshal([]byte(`{"n": 9007199254740993, "e": 1e400, "s": "ünïcode", "empty": {}, "list": [null, []]}`), &big)
	if err := call.Write(ctx, map[string]any{"kind": "input", "value": big}); err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := call.Write(ctx, map[string]any{"kind": "close"}); err != nil {
		t.Fatalf("close frame: %v", err)
	}
	if err := call.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	var frames []any
	for {
		frame, err := call.Outputs().Read(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream: %v (frames so far %v)", err, frames)
		}
		frames = append(frames, frame)
	}
	if len(frames) < 2 {
		t.Fatalf("expected an output and a terminal frame, got %v", frames)
	}
	output, _ := frames[0].(map[string]any)
	if output["kind"] != "output" {
		t.Fatalf("first frame is not output: %v", frames)
	}
	if exact(t, output["value"]) != exact(t, big) {
		t.Fatalf("streamed value changed: %s vs %s", exact(t, output["value"]), exact(t, big))
	}
	if last, _ := frames[len(frames)-1].(map[string]any); last["kind"] != "complete" {
		t.Fatalf("terminal frame: %v", frames)
	}
}

func TestConsumerRefusalsLeaveStateUntouched(t *testing.T) {
	h := newHarness(t)
	var providerValue any
	_ = jsonvalue.Unmarshal(h.provider.value, &providerValue)
	engine := newEngine(t, openapi.NewInvoker())
	options := invoke.WithContext(map[string]any{"bearerToken": h.token, "configuration": map[string]any{"security": map[string]any{"index": 1}}})
	if err := invokeExpectError(t, engine, h.servedOBI, register, map[string]any{"interface": providerValue, "roles": []any{"inspect"}}, options); err == nil {
		t.Fatal("inspect enrollment of an invoke provider accepted")
	}
	if err := invokeExpectError(t, engine, h.servedOBI, register, map[string]any{"interface": providerValue, "roles": []any{"invoke"}, "rolePreferences": map[string]any{"inspect": 1}}, options); err == nil {
		t.Fatal("preference outside roles accepted")
	}
	if err := invokeExpectError(t, engine, h.servedOBI, prefer, map[string]any{"id": "dlg_absent", "role": "invoke", "preference": nil}, options); err == nil {
		t.Fatal("preference on an absent registration accepted")
	}
	if listed := h.httpOp(t, list, nil).(map[string]any)["delegates"].([]any); len(listed) != 0 {
		t.Fatalf("refusals mutated the registry: %v", listed)
	}
	if out := h.httpOp(t, unregister, map[string]any{"id": "dlg_never_registered"}); out != nil {
		t.Fatal("absent removal must return null")
	}
	counters := h.counters()
	if counters["support"].(float64) != 0 || counters["work"].(float64) != 0 || counters["extra"].(float64) != 0 {
		t.Fatalf("admission contacted the provider: %v", counters)
	}
	if strings.Contains(exact(t, h.httpOp(t, "openbindings.ob.resolveRoleDelegate", map[string]any{"role": "invoke", "bindingSpec": h.provider.Token})), "sensitive-provider-document-marker") {
		t.Fatal("diagnostic disclosed the provider document")
	}
}
