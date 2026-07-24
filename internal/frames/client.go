package frames

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/coder/websocket"

	openbindings "github.com/openbindings/openbindings-go"
)

// Dialer establishes the WebSocket connection for one invocation. It runs on
// the invocation's goroutine (creation stays inert); a non-nil error becomes
// the handle's pre-side-effect terminal.
type Dialer func(ctx context.Context) (*websocket.Conn, *openbindings.InvocationError)

// Invoke drives one binding-invoker frame stream as a caller-facing
// Invocation handle: caller writes become `input` frames, `output` frames
// become handle outputs, a service `input_closed` closes the handle's input
// side, and the terminal frame (or its absence at transport closure, rule 5)
// terminates the handle. One connection carries exactly one invocation.
func Invoke(ctx context.Context, dial Dialer, input *BindingInvocationInput) openbindings.Invocation[any, any] {
	impl := openbindings.NewInvocationImpl[any, any](ctx)

	go func() {
		conn, ierr := dial(ctx)
		if ierr != nil {
			impl.FireError(ierr)
			return
		}

		// Terminal teardown: whatever ends the invocation (terminal frame,
		// caller Cancel, ctx cancellation) closes the socket, which unblocks
		// both pumps.
		go func() {
			<-impl.Done()
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		go writePump(ctx, conn, impl, input)
		readPump(ctx, conn, impl)
	}()

	return impl
}

// writePump sends the open frame, then forwards caller writes as input
// frames and the caller's input closure as the close frame. Send failures
// are not terminal here: the read side owns terminal reporting (a broken
// transport surfaces as ERR_TRANSPORT_CLOSED from readPump).
func writePump(ctx context.Context, conn *websocket.Conn, impl *openbindings.InvocationImpl[any, any], input *BindingInvocationInput) {
	if writeFrame(ctx, conn, Open(input)) != nil {
		return
	}
	for {
		v, err := impl.ReadInput(ctx)
		if errors.Is(err, io.EOF) {
			// Input side closed — by the caller's Close or by the service's
			// input_closed. The close frame is idempotent caller intent
			// ("no more input frames") and valid in both cases.
			_ = writeFrame(ctx, conn, Close())
			return
		}
		if err != nil {
			return // invocation already terminal
		}
		if writeFrame(ctx, conn, Input(v)) != nil {
			return
		}
	}
}

// readPump consumes output frames until the terminal frame, synthesizing
// ERR_TRANSPORT_CLOSED when the transport closes without one (rule 5) and
// ERR_PROTOCOL when a frame fails strict decoding (rule 7, consumer side).
func readPump(ctx context.Context, conn *websocket.Conn, impl *openbindings.InvocationImpl[any, any]) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			// FireError is a no-op once terminal, so closures caused by our
			// own teardown (after complete/error/Cancel) don't overwrite.
			impl.FireError(&openbindings.InvocationError{
				Code:    openbindings.ErrCodeTransportClosed,
				Message: "transport closed before a terminal frame: " + err.Error(),
			})
			return
		}
		var frame OutputFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			impl.FireError(&openbindings.InvocationError{
				Code:    openbindings.ErrCodeProtocol,
				Message: "invalid output frame: " + err.Error(),
			})
			return
		}
		switch frame.Kind {
		case KindOutput:
			if impl.EmitOutput(frame.Value) != nil {
				return // invocation terminated while the emit was parked
			}
		case KindInputClosed:
			_ = impl.CloseInput()
		case KindComplete:
			impl.CloseOutput()
			return
		case KindError:
			impl.FireError(frame.Error.InvocationError())
			return
		}
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, frame InputFrame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
