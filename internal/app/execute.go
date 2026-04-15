package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/execref"
)

// ExecuteSource represents the binding source for execution.
type ExecuteSource struct {
	Format   string `json:"format"`
	Location string `json:"location,omitempty"`
	Content  any    `json:"content,omitempty"`
	Binary   string `json:"binary,omitempty"` // Optional: binary name hint for CLI execution
}

// ExecuteOperationInput is the input for executeBinding.
type ExecuteOperationInput struct {
	Source    ExecuteSource                  `json:"source"`
	Ref       string                         `json:"ref"`
	Input     any                            `json:"input,omitempty"`
	Context   map[string]any                 `json:"context,omitempty"`
	Options   *openbindings.ExecutionOptions `json:"options,omitempty"`
	Interface *openbindings.Interface         `json:"interface,omitempty"`
}

// ExecuteOperationOutput is the output of executeBinding.
type ExecuteOperationOutput struct {
	Output     any    `json:"output,omitempty"`
	Status     int    `json:"status,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      *Error `json:"error,omitempty"`
	BindingKey string `json:"bindingKey,omitempty"`
}

// DefaultBindingForOp finds the highest-priority, non-deprecated binding for a given operation.
// Returns the binding key and entry, or ("", nil) if no binding matches.
func DefaultBindingForOp(opKey string, iface *openbindings.Interface) (string, *openbindings.BindingEntry) {
	key, entry, err := openbindings.DefaultBindingSelector(iface, opKey)
	if err != nil {
		return "", nil
	}
	return key, entry
}

// bindingByKey looks up a binding by its key.
// Returns nil if the key does not exist.
func bindingByKey(bindingKey string, iface *openbindings.Interface) *openbindings.BindingEntry {
	if iface == nil {
		return nil
	}
	b, ok := iface.Bindings[bindingKey]
	if !ok {
		return nil
	}
	return &b
}

// resolvedBinding holds the resolved components for an OBI operation execution.
type resolvedBinding struct {
	bindingKey string
	binding    *openbindings.BindingEntry
	source     openbindings.Source
	input      any
}

// resolveBindingAndSource resolves a binding, source, and input transform
// from an OBI interface. Context resolution is handled by the executor and
// executors via the ContextStore.
func resolveBindingAndSource(iface *openbindings.Interface, opKey, bindingKey string, input any) (*resolvedBinding, error) {
	if opKey != "" && bindingKey != "" {
		return nil, fmt.Errorf("operation key and binding key are mutually exclusive")
	}

	var binding *openbindings.BindingEntry
	var resolvedKey string
	if bindingKey != "" {
		binding = bindingByKey(bindingKey, iface)
		if binding == nil {
			return nil, fmt.Errorf("binding %q not found", bindingKey)
		}
		resolvedKey = bindingKey
		opKey = binding.Operation
		if _, ok := iface.Operations[opKey]; !ok {
			return nil, fmt.Errorf("operation %q (referenced by binding %q) not found", opKey, bindingKey)
		}
	} else {
		if _, ok := iface.Operations[opKey]; !ok {
			return nil, fmt.Errorf("operation %q not found", opKey)
		}
		resolvedKey, binding = DefaultBindingForOp(opKey, iface)
		if binding == nil {
			return nil, fmt.Errorf("no binding for operation %q", opKey)
		}
	}

	source, ok := iface.Sources[binding.Source]
	if !ok {
		return nil, fmt.Errorf("binding source %q not found", binding.Source)
	}

	execInput := input
	if binding.InputTransform != nil {
		transformed, tErr := ApplyTransform(iface.Transforms, binding.InputTransform, input)
		if tErr != nil {
			return nil, fmt.Errorf("input transform failed: %w", tErr)
		}
		execInput = transformed
	}

	return &resolvedBinding{
		bindingKey: resolvedKey,
		binding:    binding,
		source:     source,
		input:      execInput,
	}, nil
}

// resolveSourceLocation resolves a source location relative to the OBI directory.
// exec: refs, URIs, absolute paths, and host:port addresses pass through unchanged;
// relative file paths are joined with obiDir.
func resolveSourceLocation(source openbindings.Source, obiDir string) openbindings.BindingExecutionSource {
	es := openbindings.BindingExecutionSource{Format: source.Format}
	if source.Location != "" {
		loc := source.Location
		if !execref.IsExec(loc) && !strings.Contains(loc, "://") && !filepath.IsAbs(loc) && !isHostPort(loc) && obiDir != "" {
			loc = filepath.Join(obiDir, loc)
		}
		es.Location = loc
	} else if source.Content != nil {
		es.Content = source.Content
	}
	return es
}

// isHostPort returns true if s looks like a host:port network address.
func isHostPort(s string) bool {
	_, _, err := net.SplitHostPort(s)
	return err == nil
}

// StreamEvent is an app-layer alias for openbindings.StreamEvent.
type StreamEvent = openbindings.StreamEvent

// ExecuteOBIOperation executes an operation from an OBI file and returns a
// stream of events. Every operation is a stream — unary calls produce one
// event. Input/output transforms are applied as declared in the binding entry.
//
// If the resolved format has a builtin streaming executor, it is used.
// Otherwise, the unary execution path is used and its result is wrapped as
// a single StreamEvent.
//
// Exactly one of opKey or bindingKey must be non-empty:
//   - opKey: selects the highest-priority binding for that operation.
//   - bindingKey: looks up the binding directly (operation is read from the entry).
func ExecuteOBIOperation(ctx context.Context, obiPath string, opKey string, bindingKey string, input any) (<-chan StreamEvent, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI %q: %w", obiPath, err)
	}

	resolved, err := resolveBindingAndSource(iface, opKey, bindingKey, input)
	if err != nil {
		return nil, err
	}

	es := resolveSourceLocation(resolved.source, filepath.Dir(obiPath))

	lowLevel := ExecuteOperationInput{
		Source:    ExecuteSource{Format: es.Format, Location: es.Location, Content: es.Content},
		Ref:       resolved.binding.Ref,
		Input:     resolved.input,
		Interface: iface,
	}

	// Try the streaming path (builtin executors only).
	if BuiltinSupportsFormat(es.Format) {
		src, sErr := SubscribeOperationWithContext(ctx, lowLevel)
		if sErr == nil {
			return transformEventStream(src, iface, resolved), nil
		}
	}

	// Fall back to unary execution (supports delegates).
	result := ExecuteOperationWithContext(ctx, lowLevel)
	result.BindingKey = resolved.bindingKey

	if resolved.binding.OutputTransform != nil && result.Error == nil {
		transformed, tErr := ApplyTransform(iface.Transforms, resolved.binding.OutputTransform, result.Output)
		if tErr != nil {
			result.Error = &Error{Code: "output_transform_error", Message: fmt.Sprintf("output transform failed: %v", tErr)}
		} else {
			result.Output = transformed
		}
	}

	ch := make(chan StreamEvent, 1)
	if result.Error != nil {
		ch <- StreamEvent{Error: &openbindings.ExecuteError{Code: result.Error.Code, Message: result.Error.Message}}
	} else {
		ch <- StreamEvent{Data: result.Output}
	}
	close(ch)
	return ch, nil
}

// transformEventStream applies the binding's outputTransform to each event.
// Returns the source channel directly if no transform is configured.
func transformEventStream(src <-chan StreamEvent, iface *openbindings.Interface, resolved *resolvedBinding) <-chan StreamEvent {
	if resolved.binding.OutputTransform == nil {
		return src
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		for ev := range src {
			if ev.Error != nil || ev.Data == nil {
				out <- ev
				continue
			}
			transformed, err := ApplyTransform(iface.Transforms, resolved.binding.OutputTransform, ev.Data)
			if err != nil {
				out <- StreamEvent{Error: &openbindings.ExecuteError{
					Code:    "output_transform_error",
					Message: fmt.Sprintf("output transform failed: %v", err),
				}}
				continue
			}
			out <- StreamEvent{Data: transformed}
		}
	}()
	return out
}

// SubscribeOBIOperationDirect opens a streaming subscription using
// pre-resolved binding components. Used by the TUI which already has the
// interface, binding, and source loaded.
func SubscribeOBIOperationDirect(ctx context.Context, binding *openbindings.BindingEntry, source openbindings.Source, obiDir string) (<-chan StreamEvent, error) {
	es := resolveSourceLocation(source, obiDir)
	return DefaultExecutor().ExecuteBinding(ctx, &openbindings.BindingExecutionInput{
		Source: es,
		Ref:    binding.Ref,
	})
}

var (
	selfPath     string
	selfPathOnce sync.Once
)

// isSelf checks if a delegate location refers to the current binary.
// Used for recursion prevention and in-process optimization.
func isSelf(location string) bool {
	if !execref.IsExec(location) {
		return false
	}
	cmd, err := execref.RootCommand(location)
	if err != nil {
		return false
	}

	selfPathOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		selfPath, _ = filepath.EvalSymlinks(exe)
	})
	if selfPath == "" {
		return false
	}

	resolved, err := exec.LookPath(cmd)
	if err != nil {
		return false
	}
	resolved, _ = filepath.EvalSymlinks(resolved)
	return resolved == selfPath
}

// ExecuteOperationWithContext executes an operation with cancellation support.
// Pass a cancellable context to allow aborting long-running operations.
func ExecuteOperationWithContext(ctx context.Context, input ExecuteOperationInput) ExecuteOperationOutput {
	start := time.Now()

	// Validate input
	if input.Source.Format == "" {
		return ExecuteOperationOutput{
			Error: &Error{
				Code:    "invalid_input",
				Message: "source.format is required",
			},
		}
	}
	if input.Ref == "" {
		return ExecuteOperationOutput{
			Error: &Error{
				Code:    "invalid_input",
				Message: "ref is required",
			},
		}
	}

	delCtx := GetDelegateContext()

	var excludeLocs []string
	for _, loc := range delCtx.Delegates {
		if isSelf(loc) {
			excludeLocs = append(excludeLocs, loc)
		}
	}

	resolved, err := delegates.Resolve(delegates.ResolveParams{
		Format:             input.Source.Format,
		Delegates: delCtx.Delegates,
		ExcludeLocations:   excludeLocs,
	})

	var output ExecuteOperationOutput
	if err != nil {
		// No external delegate found — try in-process execution.
		// This handles the case where ob itself supports the format natively.
		if BuiltinSupportsFormat(input.Source.Format) {
			output = executeViaBuiltin(ctx, input)
		} else {
			return ExecuteOperationOutput{
				Error: &Error{
					Code:    "delegate_resolution_failed",
					Message: err.Error(),
				},
			}
		}
	} else {
		output = executeViaExternalDelegate(ctx, resolved, input)
	}

	output.DurationMs = time.Since(start).Milliseconds()
	return output
}

// SubscribeOperationWithContext opens a streaming subscription with cancellation
// support. Mirrors ExecuteOperationWithContext but returns a channel of events
// instead of a single output. External delegates are not supported (streaming
// across process boundaries requires a transport protocol; use builtin executors).
func SubscribeOperationWithContext(ctx context.Context, input ExecuteOperationInput) (<-chan StreamEvent, error) {
	if input.Source.Format == "" {
		return nil, fmt.Errorf("source.format is required")
	}
	if input.Ref == "" {
		return nil, fmt.Errorf("ref is required")
	}

	if !BuiltinSupportsFormat(input.Source.Format) {
		return nil, fmt.Errorf("streaming not supported for format %q (no builtin executor)", input.Source.Format)
	}

	return DefaultExecutor().ExecuteBinding(ctx, &openbindings.BindingExecutionInput{
		Source: openbindings.BindingExecutionSource{
			Format:   input.Source.Format,
			Location: input.Source.Location,
			Content:  input.Source.Content,
		},
		Ref:       input.Ref,
		Input:     input.Input,
		Context:   input.Context,
		Options:   input.Options,
		Interface: input.Interface,
	})
}

// executeViaBuiltin executes an operation using the built-in OperationExecutor.
func executeViaBuiltin(ctx context.Context, input ExecuteOperationInput) ExecuteOperationOutput {
	opts := input.Options
	if input.Source.Binary != "" {
		if opts == nil {
			opts = &openbindings.ExecutionOptions{}
		}
		if opts.Metadata == nil {
			opts.Metadata = map[string]any{}
		}
		opts.Metadata["binary"] = input.Source.Binary
	}

	ch, err := DefaultExecutor().ExecuteBinding(ctx, &openbindings.BindingExecutionInput{
		Source: openbindings.BindingExecutionSource{
			Format:   input.Source.Format,
			Location: input.Source.Location,
			Content:  input.Source.Content,
		},
		Ref:       input.Ref,
		Input:     input.Input,
		Context:   input.Context,
		Options:   opts,
		Interface: input.Interface,
	})
	if err != nil {
		return ExecuteOperationOutput{
			Error: &Error{
				Code:    "execution_failed",
				Message: err.Error(),
			},
		}
	}

	var last *openbindings.StreamEvent
	for ev := range ch {
		ev := ev
		last = &ev
	}
	if last == nil {
		return ExecuteOperationOutput{}
	}
	if last.Error != nil {
		status := last.Status
		if status == 0 {
			status = 1
		}
		return ExecuteOperationOutput{
			Status:     status,
			DurationMs: last.DurationMs,
			Error:      last.Error,
		}
	}
	return ExecuteOperationOutput{
		Output:     last.Data,
		Status:     last.Status,
		DurationMs: last.DurationMs,
	}
}

// executeViaExternalDelegate executes an operation via an external delegate.
// Uses the delegate's OBI to find its executeBinding binding and invokes it
// through the normal binding execution system.
func executeViaExternalDelegate(ctx context.Context, resolved delegates.Resolved, input ExecuteOperationInput) ExecuteOperationOutput {
	loc := resolved.Location
	if loc == "" {
		return ExecuteOperationOutput{
			Error: &Error{
				Code:    "invalid_delegate",
				Message: fmt.Sprintf("delegate %q has no location", resolved.Delegate),
			},
		}
	}

	if resolved.OBI == nil {
		return executeViaCLILegacy(ctx, loc, input)
	}

	iface := &resolved.OBI.Interface

	bindingKey, binding := DefaultBindingForOp("executeBinding", iface)
	if binding == nil {
		return executeViaCLILegacy(ctx, loc, input)
	}

	sourceName := binding.Source
	source, ok := iface.Sources[sourceName]
	if !ok {
		return ExecuteOperationOutput{
			Error: &Error{
				Code:    "delegate_error",
				Message: fmt.Sprintf("delegate %q: binding source %q not found", resolved.Delegate, sourceName),
			},
		}
	}

	var inputPayload any = input
	if binding.InputTransform != nil {
		transformed, tErr := ApplyTransform(iface.Transforms, binding.InputTransform, input)
		if tErr != nil {
			return ExecuteOperationOutput{
				Error: &Error{
					Code:    "transform_error",
					Message: fmt.Sprintf("delegate %q: input transform for %q failed: %v", resolved.Delegate, bindingKey, tErr),
				},
			}
		}
		inputPayload = transformed
	}

	execInput := ExecuteOperationInput{
		Source: ExecuteSource{
			Format:   source.Format,
			Location: source.Location,
		},
		Ref:     binding.Ref,
		Input:   inputPayload,
		Context: input.Context,
		Options: input.Options,
	}

	es := resolveSourceLocation(source, "")
	execInput.Source.Location = es.Location
	if es.Content != nil {
		execInput.Source.Content = es.Content
	}

	return executeViaBuiltin(ctx, execInput)
}

// executeViaCLILegacy is the fallback for delegates that don't have an OBI
// with an executeBinding binding. Uses the legacy execute --as-delegate protocol.
func executeViaCLILegacy(ctx context.Context, delegatePath string, input ExecuteOperationInput) ExecuteOperationOutput {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return ExecuteOperationOutput{
			Error: &Error{
				Code:    "json_marshal_error",
				Message: fmt.Sprintf("failed to marshal input: %v", err),
			},
		}
	}

	cmd := exec.CommandContext(ctx, delegatePath, "execute", "--as-delegate")
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	if ctx.Err() != nil {
		return ExecuteOperationOutput{
			Error: &Error{
				Code:    "cancelled",
				Message: "operation cancelled",
			},
		}
	}

	var output ExecuteOperationOutput
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), &output); jsonErr != nil {
			output.Output = stdout.String()
		}
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			output.Status = exitErr.ExitCode()
		} else {
			output.Status = 1
		}
		if output.Error == nil && stderr.Len() > 0 {
			output.Error = &Error{
				Code:    "execution_failed",
				Message: strings.TrimSpace(stderr.String()),
			}
		}
	}

	return output
}
