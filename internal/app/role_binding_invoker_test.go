package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openbindings/ob/internal/frames"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Proves actual work through a prepared role dependency and the unchanged SDK
// AsyncAPI client, not the application's old WebSocket-specific delegate path.
func TestRoleBindingInvokerSDKStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var connections, inputs, artifacts atomic.Int32
	var artifact []byte
	mux := http.NewServeMux()
	mux.HandleFunc("GET /asyncapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		artifacts.Add(1)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(artifact)
	})
	mux.HandleFunc("GET /bindings/invoke", func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		opened := false
		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var frame frames.InputFrame
			if err := jsonvalue.Unmarshal(data, &frame); err != nil {
				t.Error(err)
				return
			}
			var response frames.OutputFrame
			switch frame.Kind {
			case frames.KindOpen:
				if opened || frame.Input.Source.BindingSpec != "example.work@1" {
					t.Error("invalid retained invocation")
					return
				}
				opened = true
				continue
			case frames.KindInput:
				if !opened {
					t.Error("input preceded open")
					return
				}
				inputs.Add(1)
				response = frames.Output(frame.Value)
			case frames.KindClose:
				response = frames.Complete()
			default:
				t.Error("unexpected frame")
				return
			}
			encoded, _ := jsonvalue.Marshal(response)
			if err := conn.Write(r.Context(), websocket.MessageText, encoded); err != nil {
				return
			}
			if response.Terminal() {
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
	expected, _ := RequirementInterface(CapInvoke)
	provider, _, err := openbindings.ValidateDocument(roleTestProvider(t, expected))
	if err != nil {
		t.Fatal(err)
	}
	work := "provider.openbindings.binding-invoker.invokeBinding"
	provider.Sources["stream"] = openbindings.Source{BindingSpec: asyncapi.BindingSpec, Location: server.URL + "/asyncapi.yaml"}
	provider.Bindings[work] = openbindings.BindingEntry{Operation: work, Source: "stream", Selector: "#/operations/invokeBinding"}
	raw, _ := jsonvalue.Marshal(provider)
	record, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{"invoke"}})
	if err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(string, any) any {
		return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
	}}
	selected, err := selectRoleRuntime(ctx, r, CapInvoke, "example.work@1", roleRanked, false, func(roleCandidate) (invoke.ProviderRuntime, invoke.RealizationSelector) {
		engine := invoke.NewOperationInvoker(queries, asyncapi.NewInvoker())
		engine.ContextResolver = func(_ context.Context, details *invoke.ContextRequiredDetails) (map[string]any, error) {
			// Only the synthetic provider's own transport credential. Never
			// read a real context store or supply downstream credentials here.
			if !strings.Contains(details.Target, strings.TrimPrefix(server.URL, "http://")) {
				return nil, errors.New("unrelated credential target")
			}
			return map[string]any{"bearerToken": "test-transport-token"}, nil
		}
		return engine, nil
	})
	if err != nil || selected == nil {
		t.Fatalf("selection: %v", err)
	}
	if selected.Work.ProviderOperationKey != work {
		t.Fatal("work key lost correspondence")
	}
	// Removing the registration must not dispose the retained dependency.
	if err := r.unregister(record.ID); err != nil {
		t.Fatal(err)
	}
	engine := &roleBindingInvoker{spec: "example.work@1", route: selected.Work}
	call := engine.InvokeBinding(ctx, &invoke.BindingInvocationArgs{Source: invoke.InvocationSource{BindingSpec: "example.work@1", Content: json.RawMessage(`{}`)}, Selector: "echo"})
	defer call.Cancel()
	for _, value := range []any{json.Number("9007199254740993"), "second"} {
		if err := call.Write(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	_ = call.Close()
	out := call.Outputs()
	for _, expected := range []any{json.Number("9007199254740993"), "second"} {
		value, err := out.Read(ctx)
		if err != nil {
			t.Fatalf("stream failed: %#v (requests=%d artifacts=%d)", err, connections.Load(), artifacts.Load())
		}
		if equal, err := jsonvalue.Equal(value, expected); err != nil || !equal {
			t.Fatalf("output mismatch: %v %v", value, err)
		}
	}
	if _, err := out.Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal: %v", err)
	}
	if connections.Load() != 1 || inputs.Load() != 2 || artifacts.Load() == 0 {
		t.Fatalf("calls: connections=%d inputs=%d artifacts=%d", connections.Load(), inputs.Load(), artifacts.Load())
	}
	if rows, err := r.candidates("invoke"); err != nil || len(rows) != 0 {
		t.Fatal("new lookup retained removed delegate")
	}
}
