package frames

import (
	"context"
	"errors"
	"io"

	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// InvokeOperation consumes the Binding Invoker interface through an already
// selected abstract invocation. The SDK owns its protocol/realization; this
// function owns only the published application's frame grammar. It never
// resolves another provider or starts another invocation after failure.
func InvokeOperation(ctx context.Context, operation invoke.Invocation[any, any], input *BindingInvocationInput) invoke.Invocation[any, any] {
	caller := invoke.NewInvocationImpl[any, any](ctx)
	go func() { <-caller.Done(); operation.Cancel() }()
	go func() {
		select {
		case <-operation.InputClosed():
			_ = caller.CloseInput()
		case <-caller.Done():
		}
	}()
	go func() {
		write := func(frame InputFrame) error {
			data, err := jsonvalue.Marshal(frame)
			if err != nil {
				caller.FireError(&invoke.InvocationError{Code: invoke.ErrCodeFrameProtocol})
				return err
			}
			var value any
			if err := jsonvalue.Unmarshal(data, &value); err != nil {
				caller.FireError(&invoke.InvocationError{Code: invoke.ErrCodeFrameProtocol})
				return err
			}
			// Local encoding failure is ours to report. Write rejection is
			// only fast-fail; the underlying output terminal remains the
			// authoritative outcome, including completion racing a write.
			return operation.Write(ctx, value)
		}
		if err := write(Open(input)); err != nil {
			return
		}
		for {
			value, err := caller.ReadInput(ctx)
			if errors.Is(err, io.EOF) {
				_ = write(Close())
				_ = operation.Close()
				return
			}
			if err != nil {
				return
			}
			if err := write(Input(value)); err != nil {
				return
			}
		}
	}()
	go func() {
		out := operation.Outputs()
		for {
			value, err := out.Read(ctx)
			if errors.Is(err, io.EOF) {
				caller.FireError(&invoke.InvocationError{Code: invoke.ErrCodeTransportClosed})
				return
			}
			if err != nil {
				caller.FireError(invoke.AsInvocationError(err))
				return
			}
			data, err := jsonvalue.Marshal(value)
			var frame OutputFrame
			if err != nil || jsonvalue.Unmarshal(data, &frame) != nil {
				caller.FireError(&invoke.InvocationError{Code: invoke.ErrCodeFrameProtocol})
				return
			}
			switch frame.Kind {
			case KindOutput:
				if caller.EmitOutput(frame.Value) != nil {
					return
				}
			case KindInputClosed:
				_ = caller.CloseInput()
			case KindComplete:
				caller.CloseOutput()
				return
			case KindError:
				caller.FireError(frame.Error.InvocationError())
				return
			}
		}
	}()
	return caller
}
