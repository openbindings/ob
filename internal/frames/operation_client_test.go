package frames

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func frameValue(t *testing.T, frame any) any {
	t.Helper()
	data, err := jsonvalue.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := jsonvalue.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOperationClientFrames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	operation := invoke.NewInvocationImpl[any, any](ctx)
	caller := InvokeOperation(ctx, operation, &BindingInvocationInput{Source: InvokeSource{BindingSpec: "example.test@1", Content: json.RawMessage(`{}`)}, Selector: "echo"})
	defer caller.Cancel()
	if err := caller.Write(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if err := caller.Write(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	_ = caller.Close()
	for _, expected := range []string{KindOpen, KindInput, KindInput, KindClose} {
		value, err := operation.ReadInput(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if value.(map[string]any)["kind"] != expected {
			t.Fatal("frame order changed")
		}
	}
	go func() {
		_ = operation.EmitOutput(frameValue(t, Output("first")))
		_ = operation.EmitOutput(frameValue(t, Output("second")))
		_ = operation.EmitOutput(frameValue(t, Complete()))
	}()
	out := caller.Outputs()
	for _, expected := range []string{"first", "second"} {
		value, err := out.Read(ctx)
		if err != nil || value != expected {
			t.Fatalf("output: %v %v", value, err)
		}
	}
	if _, err := out.Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal: %v", err)
	}
	select {
	case <-operation.Done():
	case <-ctx.Done():
		t.Fatal("operation was not torn down")
	}
}

func TestOperationClientTerminals(t *testing.T) {
	for _, name := range []string{"missing-terminal", "invalid-frame", "context-required", "input-closed", "cancel"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			operation := invoke.NewInvocationImpl[any, any](ctx)
			caller := InvokeOperation(ctx, operation, &BindingInvocationInput{Source: InvokeSource{BindingSpec: "example.test@1", Content: json.RawMessage(`{}`)}, Selector: "echo"})
			defer caller.Cancel()
			if _, err := operation.ReadInput(ctx); err != nil {
				t.Fatal(err)
			}
			code := invoke.ErrCodeTransportClosed
			switch name {
			case "missing-terminal":
				operation.CloseOutput()
			case "invalid-frame":
				code = invoke.ErrCodeFrameProtocol
				_ = operation.EmitOutput(map[string]any{"kind": "complete", "unknown": true})
			case "context-required":
				code = invoke.ErrCodeContextRequired
				_ = operation.EmitOutput(frameValue(t, Error(invoke.NewContextRequiredError(&invoke.ContextRequiredDetails{Target: "https://needed.invalid", Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{Type: "auth.bearer"}}}}}))))
			case "input-closed":
				_ = operation.EmitOutput(frameValue(t, InputClosed()))
				select {
				case <-caller.InputClosed():
				case <-ctx.Done():
					t.Fatal("input closure not forwarded")
				}
				_ = operation.EmitOutput(frameValue(t, Complete()))
				if _, err := caller.Outputs().Read(ctx); !errors.Is(err, io.EOF) {
					t.Fatal(err)
				}
				return
			case "cancel":
				code = invoke.ErrCodeCancelled
				caller.Cancel()
			}
			_, err := caller.Outputs().Read(ctx)
			var failure *invoke.InvocationError
			if !errors.As(err, &failure) || failure.Code != code {
				t.Fatalf("terminal=%v want %s", err, code)
			}
			select {
			case <-operation.Done():
			case <-ctx.Done():
				t.Fatal("terminal leaked operation")
			}
		})
	}
}

func TestOperationClientEncodingFailure(t *testing.T) {
	for _, phase := range []string{"open", "input"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			operation := invoke.NewInvocationImpl[any, any](ctx)
			input := &BindingInvocationInput{Source: InvokeSource{BindingSpec: "example.test@1", Content: json.RawMessage(`{}`)}, Selector: "echo"}
			if phase == "open" {
				input.Source.Content = json.RawMessage(`{`)
			}
			caller := InvokeOperation(ctx, operation, input)
			defer caller.Cancel()
			if phase == "input" {
				if _, err := operation.ReadInput(ctx); err != nil {
					t.Fatal(err)
				}
				var failure *invoke.InvocationError
				if err := caller.Write(ctx, make(chan int)); !errors.As(err, &failure) || failure.Code != invoke.ErrCodeTypeMismatch {
					t.Fatalf("unsupported input must be rejected at admission: %v", err)
				}
				// A rejected value does not terminate a usable stream.
				if err := caller.Write(ctx, "valid"); err != nil {
					t.Fatal(err)
				}
				value, err := operation.ReadInput(ctx)
				if err != nil || value.(map[string]any)["value"] != "valid" {
					t.Fatalf("valid input after rejection: %v, %v", value, err)
				}
				caller.Cancel()
				select {
				case <-operation.Done():
				case <-ctx.Done():
					t.Fatal("cancellation leaked operation")
				}
				return
			}
			_, err := caller.Outputs().Read(ctx)
			var failure *invoke.InvocationError
			if !errors.As(err, &failure) || failure.Code != invoke.ErrCodeFrameProtocol {
				t.Fatalf("encoding failure must terminate promptly, got %v", err)
			}
			select {
			case <-operation.Done():
			case <-ctx.Done():
				t.Fatal("encoding failure leaked operation")
			}
		})
	}
}

func TestOperationClientUnderlyingInputClosure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	operation := invoke.NewInvocationImpl[any, any](ctx)
	caller := InvokeOperation(ctx, operation, &BindingInvocationInput{Source: InvokeSource{BindingSpec: "example.test@1", Content: json.RawMessage(`{}`)}, Selector: "echo"})
	defer caller.Cancel()
	if _, err := operation.ReadInput(ctx); err != nil {
		t.Fatal(err)
	}
	_ = operation.CloseInput()
	select {
	case <-caller.InputClosed():
	case <-ctx.Done():
		t.Fatal("underlying input closure not forwarded")
	}
	// Input rejection is not the outcome: the provider still completes normally.
	_ = operation.EmitOutput(frameValue(t, Complete()))
	if _, err := caller.Outputs().Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("input closure replaced output verdict: %v", err)
	}
}
