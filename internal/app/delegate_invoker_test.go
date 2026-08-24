package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coder/websocket"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/invoke"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/frames"
)

// delegateOBI builds the minimal delegate interface advertising invokeBinding
// through the given sources/bindings.
func delegateOBI(sources map[string]openbindings.Source, bindings map[string]openbindings.BindingEntry) *delegates.ResolvedOBI {
	return &delegates.ResolvedOBI{Interface: openbindings.Interface{
		OpenBindings: "0.2.0",
		Operations:   map[string]openbindings.Operation{"invokeBinding": {}},
		Sources:      sources,
		Bindings:     bindings,
	}}
}

const delegateAsyncDoc = `asyncapi: "3.0.0"
info:
  title: delegate
  version: 0.1.0
servers:
  local:
    host: %s
    protocol: ws
channels:
  bindingsInvoke:
    address: /bindings/invoke
operations:
  invokeBinding:
    action: send
    channel:
      $ref: "#/channels/bindingsInvoke"
`

// TestDelegateFrameInvoker_UnaryRoundTrip exercises the full delegate frame
// path: endpoint resolution from the delegate's AsyncAPI document, the dial,
// and a unary invocation as frames.
func TestDelegateFrameInvoker_UnaryRoundTrip(t *testing.T) {
	var ts *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /asyncapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprintf(w, delegateAsyncDoc, strings.TrimPrefix(ts.URL, "http://"))
	})
	mux.HandleFunc("GET /bindings/invoke", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		ctx := r.Context()
		readFrame := func() frames.InputFrame {
			_, data, rerr := conn.Read(ctx)
			if rerr != nil {
				t.Errorf("server read: %v", rerr)
				return frames.InputFrame{}
			}
			var f frames.InputFrame
			if uerr := json.Unmarshal(data, &f); uerr != nil {
				t.Errorf("server decode: %v", uerr)
			}
			return f
		}
		writeFrame := func(f frames.OutputFrame) {
			data, _ := json.Marshal(f)
			_ = conn.Write(ctx, websocket.MessageText, data)
		}

		open := readFrame()
		if open.Kind != frames.KindOpen || open.Input == nil ||
			open.Input.Source.BindingSpec != "thrift@1.0" || open.Input.Selector != "Service/method" {
			t.Errorf("unexpected open frame: %#v", open)
		}
		input := readFrame()
		if input.Kind != frames.KindInput {
			t.Errorf("expected input frame, got %#v", input)
		}
		writeFrame(frames.InputClosed())
		writeFrame(frames.Output(map[string]any{"echo": input.Value}))
		writeFrame(frames.Complete())
	})
	ts = httptest.NewServer(mux)
	defer ts.Close()

	resolved := delegates.Resolved{
		Format:   "thrift@1.0",
		Delegate: "test-delegate",
		Location: ts.URL,
		OBI: delegateOBI(
			map[string]openbindings.Source{
				"asyncapi": {BindingSpec: "openbindings.asyncapi@1", Location: ts.URL + "/asyncapi.yaml"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.asyncapi": {Operation: "invokeBinding", Selector: "#/operations/invokeBinding", Source: "asyncapi"},
			},
		),
	}

	invoker, err := DelegateBindingInvoker(resolved)
	if err != nil {
		t.Fatalf("DelegateBindingInvoker: %v", err)
	}
	if _, ok := invoker.(*delegateFrameInvoker); !ok {
		t.Fatalf("expected the frame invoker, got %T", invoker)
	}

	ctx := t.Context()
	inv := invoker.InvokeBinding(ctx, &invoke.BindingInvocationArgs{
		Source:   invoke.InvocationSource{BindingSpec: "thrift@1.0", Location: "service.thrift"},
		Selector: "Service/method",
	})
	if werr := inv.Write(ctx, "ping"); werr != nil {
		t.Fatalf("write: %v", werr)
	}
	if cerr := inv.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	out := inv.Outputs()
	v, rerr := out.Read(ctx)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if echo, ok := v.(map[string]any); !ok || echo["echo"] != "ping" {
		t.Errorf("output = %#v, want {echo: ping}", v)
	}
	if _, rerr := out.Read(ctx); !errors.Is(rerr, io.EOF) {
		t.Fatalf("expected EOF, got %v", rerr)
	}
}

func TestDelegateBindingInvoker_PrefersFramesOverCLI(t *testing.T) {
	resolved := delegates.Resolved{
		Format:   "thrift@1.0",
		Delegate: "test-delegate",
		OBI: delegateOBI(
			map[string]openbindings.Source{
				"asyncapi": {BindingSpec: "openbindings.asyncapi@1", Location: "http://localhost:1/asyncapi.yaml"},
				"usage":    {BindingSpec: "openbindings.usage@1", Location: "exec:test-delegate --usage-spec"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.asyncapi": {Operation: "invokeBinding", Selector: "#/operations/invokeBinding", Source: "asyncapi"},
				"invokeBinding.usage":    {Operation: "invokeBinding", Selector: "binding invoke", Source: "usage"},
			},
		),
	}
	invoker, err := DelegateBindingInvoker(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := invoker.(*delegateFrameInvoker); !ok {
		t.Errorf("expected the frame invoker to win, got %T", invoker)
	}
}

func TestDelegateBindingInvoker_CLIWhenAsyncAPIUnreachable(t *testing.T) {
	// A relative asyncapi location (a CLI delegate's local file) is not a
	// reachable frame endpoint; the usage binding carries the invocation.
	resolved := delegates.Resolved{
		Format:   "thrift@1.0",
		Delegate: "test-delegate",
		OBI: delegateOBI(
			map[string]openbindings.Source{
				"asyncapi": {BindingSpec: "openbindings.asyncapi@1", Location: "internal/server/asyncapi.yaml"},
				"usage":    {BindingSpec: "openbindings.usage@1", Location: "exec:test-delegate --usage-spec"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.asyncapi": {Operation: "invokeBinding", Selector: "#/operations/invokeBinding", Source: "asyncapi"},
				"invokeBinding.usage":    {Operation: "invokeBinding", Selector: "binding invoke", Source: "usage"},
			},
		),
	}
	invoker, err := DelegateBindingInvoker(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := invoker.(*delegateCLIInvoker); !ok {
		t.Errorf("expected the CLI invoker, got %T", invoker)
	}
}

func TestDelegateBindingInvoker_NoUsableBinding(t *testing.T) {
	resolved := delegates.Resolved{
		Format:   "thrift@1.0",
		Delegate: "test-delegate",
		OBI: delegateOBI(
			map[string]openbindings.Source{
				"openapi": {BindingSpec: "openbindings.openapi@1", Location: "http://localhost:1/openapi.yaml"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.openapi": {Operation: "invokeBinding", Selector: "#/paths/~1bindings~1invoke/post", Source: "openapi"},
			},
		),
	}
	if _, err := DelegateBindingInvoker(resolved); err == nil {
		t.Fatal("expected an error for a delegate without a usable invokeBinding binding")
	}
}

func TestDelegateBindingInvoker_RequiresExactTransportIdentifiers(t *testing.T) {
	cases := []struct {
		name       string
		bindingSpec string
		location   string
		wantRetired bool
	}{
		{name: "asyncapi future revision", bindingSpec: "openbindings.asyncapi@10", location: "https://example.test/asyncapi.yaml"},
		{name: "bare asyncapi", bindingSpec: "asyncapi", location: "https://example.test/asyncapi.yaml"},
		{name: "usage prefix form", bindingSpec: "usage@1", location: "exec:example"},
		{name: "bare usage", bindingSpec: "usage", location: "exec:example"},
		{name: "retired wrapper", bindingSpec: "openbindings.usage@0.1.0", location: "exec:example", wantRetired: true},
		{name: "retired wrapper adjacent", bindingSpec: "openbindings.usage@0.1.0.future", location: "exec:example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := delegates.Resolved{
				Delegate: "exact-test",
				Location: "exec:exact-test",
				OBI: &delegates.ResolvedOBI{Interface: openbindings.Interface{
					Operations: map[string]openbindings.Operation{
						"invokeBinding": {Aliases: []string{"openbindings.binding-invoker.invokeBinding"}},
					},
					Sources: map[string]openbindings.Source{"transport": {BindingSpec: tc.bindingSpec, Location: tc.location}},
					Bindings: map[string]openbindings.BindingEntry{
						"invokeBinding.transport": {Operation: "invokeBinding", Source: "transport"},
					},
				}},
			}
			_, err := DelegateBindingInvoker(resolved)
			if err == nil {
				t.Fatalf("prefix-adjacent transport token %q was silently accepted", tc.bindingSpec)
			}
			if gotRetired := strings.Contains(err.Error(), "retired") && strings.Contains(err.Error(), "re-register"); gotRetired != tc.wantRetired {
				t.Errorf("error = %q, retired migration branch = %v, want %v", err, gotRetired, tc.wantRetired)
			}
		})
	}
}

func TestResolveFrameEndpoint(t *testing.T) {
	var ts *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /asyncapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, delegateAsyncDoc, "example.com:20290")
	})
	ts = httptest.NewServer(mux)
	defer ts.Close()

	got, err := resolveFrameEndpoint(context.Background(), ts.URL+"/asyncapi.yaml", "#/operations/invokeBinding")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws://example.com:20290/bindings/invoke"; got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}

	if _, err := resolveFrameEndpoint(context.Background(), ts.URL+"/asyncapi.yaml", "#/operations/nope"); err == nil {
		t.Error("expected an error for an unknown operation ref")
	}
}

// TestDelegateBindingInvoker_MatchesKeyOrAlias covers the OBI-T-12 matching
// rules: a delegate may carry invokeBinding under its own namespaced key with
// the published alias (ob's bound CLI OBI does), or under any key aliased to
// the binding-invoker interface — not just the bare short-name.
func TestDelegateBindingInvoker_MatchesKeyOrAlias(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"full contract key with alias", "openbindings.ob.invokeBinding"},
		{"custom key with alias", "acme.tool.callBinding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := delegates.Resolved{
				Format:   "thrift@1.0",
				Delegate: "exec:acme",
				Location: "exec:acme",
				OBI: &delegates.ResolvedOBI{Interface: openbindings.Interface{
					OpenBindings: "0.2.0",
					Operations: map[string]openbindings.Operation{
						tc.key: {Aliases: []string{"openbindings.binding-invoker.invokeBinding"}},
					},
					Sources: map[string]openbindings.Source{
						"usage": {BindingSpec: "openbindings.usage@1", Content: openbindings.TextContent("bin \"acme\"\ncmd \"binding\" subcommand_required=#true { cmd \"invoke\" { flag \"--input <json>\" } }")},
					},
					Bindings: map[string]openbindings.BindingEntry{
						tc.key + ".usage": {Operation: tc.key, Source: "usage", Selector: "binding invoke"},
					},
				}},
			}
			invoker, err := DelegateBindingInvoker(resolved)
			if err != nil {
				t.Fatalf("DelegateBindingInvoker: %v", err)
			}
			if _, ok := invoker.(*delegateCLIInvoker); !ok {
				t.Fatalf("expected the CLI invoker, got %T", invoker)
			}
		})
	}
}

// TestDelegateBindingInvoker_NoInvokeOperation: an OBI that does not carry
// the invoke operation at all (by key or alias) errors before binding
// selection, with a message naming the missing operation.
func TestDelegateBindingInvoker_NoInvokeOperation(t *testing.T) {
	resolved := delegates.Resolved{
		Delegate: "exec:acme",
		OBI: &delegates.ResolvedOBI{Interface: openbindings.Interface{
			OpenBindings: "0.2.0",
			Operations:   map[string]openbindings.Operation{"somethingElse": {}},
		}},
	}
	_, err := DelegateBindingInvoker(resolved)
	if err == nil {
		t.Fatal("expected an error for a delegate without an invokeBinding operation")
	}
	if !strings.Contains(err.Error(), "invokeBinding") {
		t.Errorf("error should name the missing operation, got: %v", err)
	}
}

// TestDelegateCLIInvoker_ExecChainRoundTrip drives the invoke capability's
// exec path as one live chain: DelegateBindingInvoker resolves the op by
// full key + alias, delegateCLIInvoker applies the binding's machine-lane
// inputTransform (verbatim what boundgen emits — pinned against the real
// bound OBI by TestGenerateBoundCLI_AttachesWireInputTransforms), the usage
// invoker builds argv from the embedded spec, and a real subprocess receives
// `binding invoke --input <json>` and answers on stdout.
func TestDelegateCLIInvoker_ExecChainRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cliPath := filepath.Join(dir, "fixture-cli")
	script := `#!/bin/sh
if [ "$1" = "binding" ] && [ "$2" = "invoke" ] && [ "$3" = "--input" ]; then
  printf '{"output":{"received":%s}}\n' "$4"
  exit 0
fi
echo "unexpected argv: $*" >&2
exit 1
`
	if err := os.WriteFile(cliPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	usageSpec := "min_usage_version \"2.0.0\"\nname \"fixture\"\nbin \"" + cliPath + "\"\n" +
		"cmd \"binding\" subcommand_required=#true {\n" +
		"  cmd \"invoke\" {\n" +
		"    flag \"--input <json>\"\n  }\n}\n"
	resolved := delegates.Resolved{
		Format:   "thrift@1.0",
		Delegate: "exec:fixture",
		Location: "exec:" + cliPath,
		OBI: &delegates.ResolvedOBI{Interface: openbindings.Interface{
			OpenBindings: "0.2.0",
			Operations: map[string]openbindings.Operation{
				"openbindings.ob.invokeBinding": {Aliases: []string{"openbindings.binding-invoker.invokeBinding"}},
			},
			Sources: map[string]openbindings.Source{
				"usage": {BindingSpec: "openbindings.usage@1", Content: openbindings.TextContent(usageSpec)},
			},
			Bindings: map[string]openbindings.BindingEntry{
				"openbindings.ob.invokeBinding.usage": {
					Operation:      "openbindings.ob.invokeBinding",
					Source:         "usage",
					Selector:       "binding invoke",
					InputTransform: &openbindings.TransformOrRef{Inline: `{ "input": $string($$) }`},
				},
			},
		}},
	}

	invoker, err := DelegateBindingInvoker(resolved)
	if err != nil {
		t.Fatalf("DelegateBindingInvoker: %v", err)
	}
	if _, ok := invoker.(*delegateCLIInvoker); !ok {
		t.Fatalf("expected the CLI invoker, got %T", invoker)
	}

	ctx := t.Context()
	inv := invoker.InvokeBinding(ctx, &invoke.BindingInvocationArgs{
		Source:   invoke.InvocationSource{BindingSpec: "thrift@1.0", Location: "service.thrift"},
		Selector: "Service/method",
	})
	if werr := inv.Write(ctx, map[string]any{"limit": 10}); werr != nil {
		t.Fatalf("write: %v", werr)
	}
	if cerr := inv.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	out := inv.Outputs()
	v, rerr := out.Read(ctx)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}

	// The fixture echoes the --input payload it received: the JSON-serialized
	// InvocationInput the transform produced. Its round-tripping proves
	// every link — transform, argv build, exec, stdout parse — held.
	top, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("output = %#v, want an object", v)
	}
	received, ok := top["output"].(map[string]any)["received"].(map[string]any)
	if !ok {
		t.Fatalf("no received payload in %#v", v)
	}
	if received["selector"] != "Service/method" {
		t.Errorf("ref = %v, want Service/method", received["selector"])
	}
	src, _ := received["source"].(map[string]any)
	if src["bindingSpec"] != "thrift@1.0" || src["location"] != "service.thrift" {
		t.Errorf("source = %#v, want thrift@1.0 / service.thrift", src)
	}
	input, _ := received["input"].(map[string]any)
	if input["limit"] != float64(10) {
		t.Errorf("input = %#v, want {limit: 10}", input)
	}

	if _, rerr := out.Read(ctx); !errors.Is(rerr, io.EOF) {
		t.Fatalf("expected EOF, got %v", rerr)
	}
}

// TestOpInvoke_ExternalDelegateDisplacesElections is the dispatch-
// unification harness case: `op invoke` routes a usage op to a PREFERRED
// EXTERNAL delegate, and standing-election displacement fires. Ob's
// standing internal-table elections for the op cannot cross the delegate
// boundary, so the invocation proceeds with a loud attributed warning and
// the delegate answers.
func TestOpInvoke_ExternalDelegateDisplacesElections(t *testing.T) {
	dir := t.TempDir()
	cliPath := filepath.Join(dir, "ext-delegate")
	delegateOBIFile := filepath.Join(dir, "delegate.obi.json")

	// The external delegate's own OBI: invokeBinding bound to a usage source
	// whose bin is the fixture; `binding invoke` takes --input.
	usageSpec := "min_usage_version \"2.0.0\"\nname \"ext\"\nbin \"" + cliPath + "\"\n" +
		"cmd \"binding\" subcommand_required=#true {\n  cmd \"invoke\" {\n    flag \"--input <json>\"\n  }\n}\n" +
		"cmd \"binding-specs\" subcommand_required=#true {\n  cmd \"check\" {\n    flag \"--input <json>\"\n  }\n}\n"
	delegateIface := openbindings.Interface{
		OpenBindings: "0.2.0",
		Operations: map[string]openbindings.Operation{
			"invokeBinding":     {Aliases: []string{"openbindings.binding-invoker.invokeBinding"}},
			"checkBindingSpecs": {Aliases: []string{"openbindings.binding-invoker.checkBindingSpecs"}},
		},
		Sources: map[string]openbindings.Source{"usage": {BindingSpec: "openbindings.usage@1", Content: openbindings.TextContent(usageSpec)}},
		Bindings: map[string]openbindings.BindingEntry{
			"invokeBinding.usage": {
				Operation:      "invokeBinding",
				Source:         "usage",
				Selector:       "binding invoke",
				InputTransform: &openbindings.TransformOrRef{Inline: `{ "input": $string($$) }`},
			},
			"checkBindingSpecs.usage": {
				Operation:      "checkBindingSpecs",
				Source:         "usage",
				Selector:       "binding-specs check",
				InputTransform: &openbindings.TransformOrRef{Inline: `{ "input": $string($$) }`},
			},
		},
	}
	obiBytes, err := json.Marshal(delegateIface)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(delegateOBIFile, obiBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--openbindings\" ]; then cat " + delegateOBIFile + "; exit 0; fi\n" +
		"if [ \"$1\" = \"binding-specs\" ] && [ \"$2\" = \"check\" ]; then printf '[{\"bindingSpec\":\"openbindings.usage@1\",\"supported\":true}]\\n'; exit 0; fi\n" +
		"if [ \"$1\" = \"binding\" ] && [ \"$2\" = \"invoke\" ]; then printf '{\"delegated\":true}\\n'; exit 0; fi\n" +
		"echo \"unexpected: $*\" >&2; exit 1\n"
	if err := os.WriteFile(cliPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// The invoked OBI declares one of ob's OWN bound ops (validateInterface,
	// which carries the ok-exit {0,1} standing election) over a usage source,
	// so displacement is observable against ob's internal table.
	invokedIface := openbindings.Interface{
		OpenBindings: "0.2.0",
		Operations:   map[string]openbindings.Operation{"openbindings.ob.validateInterface": {}},
		Sources:      map[string]openbindings.Source{"usage": {BindingSpec: "openbindings.usage@1", Location: "exec:" + cliPath}},
		Bindings: map[string]openbindings.BindingEntry{
			"openbindings.ob.validateInterface.usage": {Operation: "openbindings.ob.validateInterface", Source: "usage", Selector: "validate"},
		},
	}
	invokedFile := filepath.Join(dir, "invoked.obi.json")
	ib, err := json.Marshal(invokedIface)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invokedFile, ib, 0o644); err != nil {
		t.Fatal(err)
	}

	// Register the external delegate and prefer it for the invoke operation
	// so it outranks the self-delegate for the usage format.
	pref := 100.0
	getDelegateContextFunc = func() DelegateContext {
		return DelegateContext{Delegates: []DelegateRecord{{
			Location:             "exec:" + cliPath,
			Name:                 "ext",
			Capabilities:         []DelegateCapability{CapInvoke},
			BindingSpecs:         []DelegateBindingSpecInfo{{BindingSpec: "openbindings.usage@1"}},
			OperationPreferences: map[string]float64{"openbindings.binding-invoker.invokeBinding": pref},
		}}}
	}
	t.Cleanup(func() { getDelegateContextFunc = defaultGetDelegateContext })

	ctx := t.Context()

	// The delegate answers and the displacement warning fires.
	run, err := InvokeOBIOperationConfigured(ctx, invokedFile, "openbindings.ob.validateInterface", "", nil, nil)
	if err != nil {
		t.Fatalf("configured invoke: %v", err)
	}
	if run.DisplacedWarning == "" {
		t.Error("expected a displacement warning for a standing election that cannot reach the delegate")
	} else if !strings.Contains(run.DisplacedWarning, "ext") {
		t.Errorf("warning must name the delegate: %q", run.DisplacedWarning)
	}
	got := reduceUnaryInvocation(run.Events)
	if got.Error != nil {
		t.Fatalf("delegate invocation errored: %s", got.Error.Message)
	}
	if m, _ := got.Output.(map[string]any); m["delegated"] != true {
		t.Errorf("delegate did not answer the hop: %#v", got.Output)
	}
}
