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

	"nhooyr.io/websocket"

	openbindings "github.com/openbindings/openbindings-go"

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
			open.Input.Source.Format != "thrift@1.0" || open.Input.Ref != "Service/method" {
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
				"asyncapi": {Format: "asyncapi@3.0", Location: ts.URL + "/asyncapi.yaml"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.asyncapi": {Operation: "invokeBinding", Ref: "#/operations/invokeBinding", Source: "asyncapi"},
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
	inv := invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
		Source: openbindings.InvocationSource{Format: "thrift@1.0", Location: "service.thrift"},
		Ref:    "Service/method",
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
				"asyncapi": {Format: "asyncapi@3.0", Location: "http://localhost:1/asyncapi.yaml"},
				"usage":    {Format: "usage@2.0.0", Location: "exec:test-delegate --usage-spec"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.asyncapi": {Operation: "invokeBinding", Ref: "#/operations/invokeBinding", Source: "asyncapi"},
				"invokeBinding.usage":    {Operation: "invokeBinding", Ref: "binding invoke", Source: "usage"},
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
				"asyncapi": {Format: "asyncapi@3.0", Location: "internal/server/asyncapi.yaml"},
				"usage":    {Format: "usage@2.0.0", Location: "exec:test-delegate --usage-spec"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.asyncapi": {Operation: "invokeBinding", Ref: "#/operations/invokeBinding", Source: "asyncapi"},
				"invokeBinding.usage":    {Operation: "invokeBinding", Ref: "binding invoke", Source: "usage"},
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
				"openapi": {Format: "openapi@3.1", Location: "http://localhost:1/openapi.yaml"},
			},
			map[string]openbindings.BindingEntry{
				"invokeBinding.openapi": {Operation: "invokeBinding", Ref: "#/paths/~1bindings~1invoke/post", Source: "openapi"},
			},
		),
	}
	if _, err := DelegateBindingInvoker(resolved); err == nil {
		t.Fatal("expected an error for a delegate without a usable invokeBinding binding")
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
						"usage": {Format: "usage@2.0.0", Content: "cmd \"binding\" { cmd \"invoke\" { } }"},
					},
					Bindings: map[string]openbindings.BindingEntry{
						tc.key + ".usage": {Operation: tc.key, Source: "usage", Ref: "binding invoke"},
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
		"  cmd \"invoke\" opKey=\"invokeBinding\" {\n" +
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
				"usage": {Format: "usage@2.0.0", Content: usageSpec},
			},
			Bindings: map[string]openbindings.BindingEntry{
				"openbindings.ob.invokeBinding.usage": {
					Operation:      "openbindings.ob.invokeBinding",
					Source:         "usage",
					Ref:            "binding invoke",
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
	inv := invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
		Source: openbindings.InvocationSource{Format: "thrift@1.0", Location: "service.thrift"},
		Ref:    "Service/method",
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
	// InvokeOperationInput the transform produced. Its round-tripping proves
	// every link — transform, argv build, exec, stdout parse — held.
	top, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("output = %#v, want an object", v)
	}
	received, ok := top["output"].(map[string]any)["received"].(map[string]any)
	if !ok {
		t.Fatalf("no received payload in %#v", v)
	}
	if received["ref"] != "Service/method" {
		t.Errorf("ref = %v, want Service/method", received["ref"])
	}
	src, _ := received["source"].(map[string]any)
	if src["format"] != "thrift@1.0" || src["location"] != "service.thrift" {
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
