package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
