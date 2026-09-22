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
			value, err := inputFrameValue(frame)
			if err != nil {
				caller.FireError(&invoke.InvocationError{Code: invoke.ErrCodeFrameProtocol})
				return err
			}
			// Admission can reject an invalid host value (for example in the
			// open context) without terminating the underlying stream. This
			// frame cannot be forwarded, so waiting for its terminal would hang.
			err = operation.Write(ctx, value)
			if err != nil && invoke.AsInvocationError(err).Code == invoke.ErrCodeTypeMismatch {
				caller.FireError(invoke.NewInvocationError(invoke.ErrCodeFrameProtocol))
			}
			// Closure/terminal write rejection remains only fast-fail: the
			// underlying output terminal owns completion racing a write.
			return err
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
		defer out.Stop()
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
			frame, err := outputFrameFromValue(value)
			if err != nil {
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

// Only source content is serialized JSON; frame envelopes and invocation values
// remain ordinary logical objects until a transport actually needs wire bytes.
func inputFrameValue(frame InputFrame) (map[string]any, error) {
	fields := map[string]any{"kind": frame.Kind}
	switch frame.Kind {
	case KindOpen:
		fields["input"] = nil
		if frame.Input == nil {
			return fields, nil
		}
		input := frame.Input
		source := map[string]any{"bindingSpec": input.Source.BindingSpec}
		if input.Source.Location != "" {
			source["location"] = input.Source.Location
		}
		if len(input.Source.Content) != 0 {
			var content any
			if err := jsonvalue.Unmarshal(input.Source.Content, &content); err != nil {
				return nil, err
			}
			source["content"] = content
		}
		if input.Source.Description != "" {
			source["description"] = input.Source.Description
		}
		payload := map[string]any{"source": source, "selector": input.Selector}
		if len(input.Context) != 0 {
			payload["context"] = input.Context
		}
		fields["input"] = payload
	case KindInput:
		fields["value"] = frame.Value
	case KindClose:
	default:
		return nil, protocolErrorf("unknown input frame kind %q", frame.Kind)
	}
	return fields, nil
}
