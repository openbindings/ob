package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Generic SDK prerequisite: declining a consumer hook must retain the native
// document-declared JSON decode. No manager, frame client, registry, generated
// OB artifact or production provider participates in this reproduction.
func TestRoleAsyncAPIDecliningHookPrerequisite(t *testing.T) {
	for _, hook := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-hook", true: "declining-hook"}[hook], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				_, _, err = conn.Read(r.Context())
				if err != nil {
					return
				}
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"n":9007199254740993}`))
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			doc := fmt.Sprintf(`asyncapi: 3.0.0
info:
  title: Independent decode control
  version: '1'
servers:
  local:
    host: %s
    protocol: ws
channels:
  exchange:
    address: /echo
    messages:
      value:
        contentType: application/json
        payload: {}
operations:
  exchange:
    action: receive
    channel:
      $ref: '#/channels/exchange'
    messages:
      - $ref: '#/channels/exchange/messages/value'
    reply:
      channel:
        $ref: '#/channels/exchange'
      messages:
        - $ref: '#/channels/exchange/messages/value'
`, strings.TrimPrefix(server.URL, "http://"))
			engine := invoke.NewOperationInvoker(asyncapi.NewInvoker())
			var hooks atomic.Int32
			if hook {
				engine.OutputDecoder = func(invoke.InvokeSite, invoke.RawResult) (any, error) { hooks.Add(1); return nil, invoke.ErrUseDefault }
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			call := engine.InvokeBinding(ctx, &invoke.BindingInvocationArgs{Source: invoke.InvocationSource{BindingSpec: asyncapi.BindingSpec, Content: openbindings.TextContent(doc)}, Selector: "#/operations/exchange", Context: map[string]any{"configuration": map[string]any{"websocketMessageType": "text"}}})
			defer call.Cancel()
			if err := call.Write(ctx, map[string]any{"request": true}); err != nil {
				t.Fatal(err)
			}
			value, err := call.Outputs().Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if hook && hooks.Load() != 1 {
				t.Fatal("hook not exercised")
			}
			if equal, err := jsonvalue.Equal(value, map[string]any{"n": json.Number("9007199254740993")}); err != nil || !equal {
				t.Fatalf("declared JSON changed with hook=%v: got %T %#v (%v)", hook, value, value, err)
			}
		})
	}
}
