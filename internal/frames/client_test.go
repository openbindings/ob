package frames

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	openbindings "github.com/openbindings/openbindings-go"
)

// frameServer runs a scripted frame-protocol service for client tests: it
// accepts the upgrade, decodes input frames onto a channel, and hands the
// connection to the script.
func frameServer(t *testing.T, script func(ctx context.Context, conn *websocket.Conn, in <-chan InputFrame)) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		ctx := r.Context()
		in := make(chan InputFrame, 16)
		go func() {
			defer close(in)
			for {
				_, data, rerr := conn.Read(ctx)
				if rerr != nil {
					return
				}
				var f InputFrame
				if json.Unmarshal(data, &f) == nil {
					in <- f
				}
			}
		}()
		script(ctx, conn, in)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func testDialer(ts *httptest.Server) Dialer {
	return func(ctx context.Context) (*websocket.Conn, *openbindings.InvocationError) {
		wsURL := strings.Replace(ts.URL, "http://", "ws://", 1)
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			return nil, openbindings.NewInvocationError(openbindings.ErrCodeConnectFailed)
		}
		return conn, nil
	}
}

func mustWrite(t *testing.T, ctx context.Context, conn *websocket.Conn, frame OutputFrame) {
	t.Helper()
	data, err := json.Marshal(frame)
	if err != nil {
		t.Errorf("marshal output frame: %v", err)
		return
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Errorf("write output frame: %v", err)
	}
}

func testInput() *BindingInvocationInput {
	return &BindingInvocationInput{
		Source: InvokeSource{BindingSpec: "openbindings.openapi@1", Location: "https://x/openapi.yaml"},
		Ref:    "#/paths/~1t/get",
	}
}

func TestInvoke_UnaryRoundTrip(t *testing.T) {
	// Scripted unary service: open, one input, close -> input_closed, one
	// output, complete.
	ts := frameServer(t, func(ctx context.Context, conn *websocket.Conn, in <-chan InputFrame) {
		open := <-in
		if open.Kind != KindOpen || open.Input.Ref != "#/paths/~1t/get" {
			t.Errorf("expected open frame, got %#v", open)
		}
		input := <-in
		if input.Kind != KindInput {
			t.Errorf("expected input frame, got %#v", input)
		}
		mustWrite(t, ctx, conn, InputClosed())
		mustWrite(t, ctx, conn, Output(map[string]any{"echo": input.Value}))
		mustWrite(t, ctx, conn, Complete())
	})

	ctx := t.Context()
	inv := Invoke(ctx, testDialer(ts), testInput())
	if err := inv.Write(ctx, "ping"); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := inv.Outputs()
	v, err := out.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if echo, ok := v.(map[string]any); !ok || echo["echo"] != "ping" {
		t.Errorf("output = %#v, want {echo: ping}", v)
	}
	if _, err := out.Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}

	// The service's input_closed must have closed the handle's input side.
	select {
	case <-inv.InputClosed():
	default:
		t.Error("expected the handle's input side to be closed after input_closed")
	}
}

func TestInvoke_TransportClosedSynthesized(t *testing.T) {
	// Rule 5: the transport closing without a terminal frame synthesizes a
	// terminal ERR_TRANSPORT_CLOSED on the consumer side — after buffered
	// outputs drain.
	ts := frameServer(t, func(ctx context.Context, conn *websocket.Conn, in <-chan InputFrame) {
		<-in // open
		mustWrite(t, ctx, conn, Output("partial"))
		_ = conn.CloseNow() // vanish without a terminal frame
	})

	ctx := t.Context()
	inv := Invoke(ctx, testDialer(ts), testInput())

	out := inv.Outputs()
	v, err := out.Read(ctx)
	if err != nil {
		t.Fatalf("expected the partial output first, got %v", err)
	}
	if v != "partial" {
		t.Errorf("output = %#v, want partial", v)
	}

	_, err = out.Read(ctx)
	ie := openbindings.AsInvocationError(err)
	if ie == nil || ie.Code != openbindings.ErrCodeTransportClosed {
		t.Fatalf("expected ERR_TRANSPORT_CLOSED, got %v", err)
	}
}

func TestInvoke_ErrorFrameDataPassThrough(t *testing.T) {
	// A terminal error frame (CONTEXT_REQUIRED with data) surfaces as the
	// handle's terminal error with the typed data recoverable.
	details := &openbindings.ContextRequiredDetails{
		Target: "api.example.com",
		Alternatives: []openbindings.ContextAlternative{
			{Requirements: []openbindings.ContextRequirement{{Type: "auth.bearer"}}},
		},
	}
	ts := frameServer(t, func(ctx context.Context, conn *websocket.Conn, in <-chan InputFrame) {
		<-in // open
		mustWrite(t, ctx, conn, Error(openbindings.NewContextRequiredError(details)))
	})

	ctx := t.Context()
	inv := Invoke(ctx, testDialer(ts), testInput())

	_, err := inv.Outputs().Read(ctx)
	ie := openbindings.AsInvocationError(err)
	if ie == nil || ie.Code != openbindings.ErrCodeContextRequired {
		t.Fatalf("expected CONTEXT_REQUIRED, got %v", err)
	}
	got := openbindings.ContextRequiredFrom(ie)
	if got == nil || got.Target != "api.example.com" {
		t.Fatalf("expected typed details after the wire, got %#v", got)
	}
}

func TestInvoke_MalformedOutputFrameIsProtocolError(t *testing.T) {
	// Rule 7, consumer side: an output frame with unknown properties is a
	// protocol violation.
	ts := frameServer(t, func(ctx context.Context, conn *websocket.Conn, in <-chan InputFrame) {
		<-in // open
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"event","output":1}`))
	})

	ctx := t.Context()
	inv := Invoke(ctx, testDialer(ts), testInput())

	_, err := inv.Outputs().Read(ctx)
	ie := openbindings.AsInvocationError(err)
	if ie == nil || ie.Code != openbindings.ErrCodeFrameProtocol {
		t.Fatalf("expected ERR_FRAME_PROTOCOL, got %v", err)
	}
}

func TestInvoke_DialFailureIsTerminal(t *testing.T) {
	ctx := t.Context()
	dial := func(ctx context.Context) (*websocket.Conn, *openbindings.InvocationError) {
		return nil, openbindings.NewInvocationError(openbindings.ErrCodeConnectFailed)
	}
	inv := Invoke(ctx, dial, testInput())
	_, err := inv.Outputs().Read(ctx)
	ie := openbindings.AsInvocationError(err)
	if ie == nil || ie.Code != openbindings.ErrCodeConnectFailed {
		t.Fatalf("expected ERR_CONNECT_FAILED, got %v", err)
	}
}

func TestInvoke_CallerCloseSendsCloseFrame(t *testing.T) {
	// The caller's Close crosses as the close frame; the service completes a
	// client-streaming call on it.
	ts := frameServer(t, func(ctx context.Context, conn *websocket.Conn, in <-chan InputFrame) {
		<-in // open
		var count int
		for f := range in {
			if f.Kind == KindClose {
				mustWrite(t, ctx, conn, Output(count))
				mustWrite(t, ctx, conn, Complete())
				return
			}
			if f.Kind == KindInput {
				count++
			}
		}
	})

	ctx := t.Context()
	inv := Invoke(ctx, testDialer(ts), testInput())
	for i := 0; i < 3; i++ {
		if err := inv.Write(ctx, i); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := inv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	out := inv.Outputs()
	v, err := out.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if v != float64(3) {
		t.Errorf("output = %#v, want 3", v)
	}
	if _, err := out.Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}
