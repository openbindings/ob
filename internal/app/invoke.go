package app

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// InvokeSource represents the binding source for invocation.
type InvokeSource struct {
	Format   string `json:"format"`
	Location string `json:"location,omitempty"`
	Content  any    `json:"content,omitempty"`
	Binary   string `json:"binary,omitempty"` // Optional: binary name hint for CLI invocation
}

// InvokeOperationInput is the input for invokeBinding.
type InvokeOperationInput struct {
	Source    InvokeSource            `json:"source"`
	Ref       string                  `json:"ref"`
	Input     any                     `json:"input,omitempty"`
	Context   map[string]any          `json:"context,omitempty"`
	Interface *openbindings.Interface `json:"interface,omitempty"`
}

// InvokeOperationOutput is the output of invokeBinding.
type InvokeOperationOutput struct {
	Output     any    `json:"output,omitempty"`
	Status     int    `json:"status,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      *Error `json:"error,omitempty"`
	BindingKey string `json:"bindingKey,omitempty"`
}

// DefaultBindingForOp finds the most-preferred, non-deprecated binding for a given operation.
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

// withBinaryMetadata returns a context with the binary hint set under metadata.
// It clones the input context (and the metadata sub-map) so existing callers
// don't observe a mutation.
func withBinaryMetadata(ctx map[string]any, binary string) map[string]any {
	out := make(map[string]any, len(ctx)+1)
	for k, v := range ctx {
		out[k] = v
	}
	var meta map[string]any
	if existing, ok := out["metadata"].(map[string]any); ok {
		meta = make(map[string]any, len(existing)+1)
		for k, v := range existing {
			meta[k] = v
		}
	} else {
		meta = make(map[string]any, 1)
	}
	meta["binary"] = binary
	out["metadata"] = meta
	return out
}

// resolvedBinding holds the resolved components for an OBI operation invocation.
type resolvedBinding struct {
	bindingKey string
	binding    *openbindings.BindingEntry
	source     openbindings.Source
	input      any
}

// resolveBindingAndSource resolves a binding, source, and input transform
// from an OBI interface. Context resolution is handled by the invoker and
// per-format invokers via the ContextStore.
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
func resolveSourceLocation(source openbindings.Source, obiDir string) openbindings.InvocationSource {
	es := openbindings.InvocationSource{Format: source.Format}
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

// InvocationOutput is the app-layer event shape: one output value, or a
// terminal error. The SDK's invocation handle yields bare output values and a
// separate terminal error; ob's internal consumers (serve, mcpbridge,
// operation, TUI) work over a channel of these, so this type bridges the two.
// Status is best-effort (derived from an HTTP error's Details when present);
// DurationMs is filled by the caller.
type InvocationOutput struct {
	Output     any                           `json:"output,omitempty"`
	Error      *openbindings.InvocationError `json:"error,omitempty"`
	Status     int                           `json:"status,omitempty"`
	DurationMs int64                         `json:"durationMs,omitempty"`
}

// maxBindingContextRounds caps CONTEXT_REQUIRED resolve-and-retry rounds for
// the app-level binding paths, mirroring the SDK operation layer's cap.
const maxBindingContextRounds = 3

// driveBinding invokes a binding (via the supplied invoke function), writes
// the single input (when non-nil), closes the input side, and streams the
// handle's outputs (and any terminal error) onto a channel of app-layer
// InvocationOutput.
//
// ob's app layer drives the BINDING layer directly (it resolves the binding,
// source, and transforms itself), so it owns the CONTEXT_REQUIRED negotiation
// the SDK's operation layer would otherwise provide: a challenge raised before
// any output is resolved through the configured resolver and the binding is
// re-invoked with the merged context, replaying the input. Once the binding
// shows observable progress, challenges surface to the caller instead.
//
// Write errors are not reported here — the output read loop owns terminal
// reporting (matching the SDK's own pattern). The channel closes when the
// invocation ends.
func driveBinding(
	ctx context.Context,
	invoke func(context.Context, map[string]any) openbindings.Invocation[any, any],
	contextData map[string]any,
	input any,
	resolver openbindings.ContextResolver,
) <-chan InvocationOutput {
	ch := make(chan InvocationOutput, 16)
	go func() {
		defer close(ch)

		for round := 0; ; round++ {
			call := invoke(ctx, contextData)
			if input != nil {
				_ = call.Write(ctx, input)
			}
			_ = call.Close()

			out := call.Outputs()
			emitted := false
			for {
				v, err := out.Read(ctx)
				if errors.Is(err, io.EOF) {
					return
				}
				if err != nil {
					ie := openbindings.AsInvocationError(err)
					// Resolve-and-retry: only before any observable progress,
					// only with a resolver, and only a bounded number of times.
					if details := openbindings.ContextRequiredFrom(ie); details != nil &&
						!emitted && resolver != nil && round < maxBindingContextRounds {
						resolved, rerr := resolver(ctx, details)
						if rerr == nil && len(resolved) > 0 {
							merged := make(map[string]any, len(contextData)+len(resolved))
							for k, val := range contextData {
								merged[k] = val
							}
							for k, val := range resolved {
								merged[k] = val
							}
							contextData = merged
							break // next round re-invokes with merged context
						}
					}
					// Select on ctx so an abandoned consumer (e.g. a WS client
					// that disconnected) cannot strand this goroutine on a full,
					// unread channel. Cancelling ctx tears the binding down via
					// the SDK's terminal model; we exit rather than block.
					select {
					case ch <- InvocationOutput{Error: ie, Status: statusFromError(ie)}:
					case <-ctx.Done():
					}
					return
				}
				emitted = true
				select {
				case ch <- InvocationOutput{Output: v}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch
}

// statusFromError extracts an HTTP status from a terminal error's Details
// (HTTP-based format invokers carry {"status": N}); returns 0 when absent.
func statusFromError(err *openbindings.InvocationError) int {
	if err == nil {
		return 0
	}
	d, ok := err.Details.(map[string]any)
	if !ok {
		return 0
	}
	switch s := d["status"].(type) {
	case int:
		return s
	case float64:
		return int(s)
	}
	return 0
}

// InvokeOBIOperation invokes an operation from an OBI file and returns a
// stream of events. Every operation is a stream — unary calls produce one
// event. Input/output transforms are applied as declared in the binding entry.
//
// If the resolved format has a builtin streaming invoker, it is used.
// Otherwise, the unary invocation path is used and its result is wrapped as
// a single InvocationOutput.
//
// Exactly one of opKey or bindingKey must be non-empty:
//   - opKey: selects the most-preferred binding for that operation.
//   - bindingKey: looks up the binding directly (operation is read from the entry).
func InvokeOBIOperation(ctx context.Context, obiPath string, opKey string, bindingKey string, input any) (<-chan InvocationOutput, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI %q: %w", obiPath, err)
	}
	return invokeOnInterface(ctx, iface, opKey, bindingKey, input, filepath.Dir(obiPath))
}

// invokeOnInterface invokes an operation (or a specific binding) on an
// already-resolved interface, resolving relative source locations against
// obiDir. It is the core shared by file-backed invocation (InvokeOBIOperation)
// and delegate invocation: ob operation-invokes a delegate's operation against
// the delegate's own resolved OBI through this same path.
func invokeOnInterface(ctx context.Context, iface *openbindings.Interface, opKey, bindingKey string, input any, obiDir string) (<-chan InvocationOutput, error) {
	resolved, err := resolveBindingAndSource(iface, opKey, bindingKey, input)
	if err != nil {
		return nil, err
	}

	es := resolveSourceLocation(resolved.source, obiDir)

	lowLevel := InvokeOperationInput{
		Source:    InvokeSource{Format: es.Format, Location: es.Location, Content: es.Content},
		Ref:       resolved.binding.Ref,
		Input:     resolved.input,
		Interface: iface,
	}

	// Try the streaming path (builtin drivers only).
	if BuiltinSupportsFormat(es.Format) {
		src, sErr := SubscribeOperationWithContext(ctx, lowLevel)
		if sErr == nil {
			return transformEventStream(src, iface, resolved), nil
		}
	}

	// Fall back to unary invocation (supports delegates).
	result := InvokeOperationWithContext(ctx, lowLevel)
	result.BindingKey = resolved.bindingKey

	if resolved.binding.OutputTransform != nil && result.Error == nil {
		transformed, tErr := ApplyTransform(iface.Transforms, resolved.binding.OutputTransform, result.Output)
		if tErr != nil {
			result.Error = &Error{Code: "output_transform_error", Message: fmt.Sprintf("output transform failed: %v", tErr)}
		} else {
			result.Output = transformed
		}
	}

	ch := make(chan InvocationOutput, 1)
	if result.Error != nil {
		ch <- InvocationOutput{Error: &openbindings.InvocationError{Code: result.Error.Code, Message: result.Error.Message}}
	} else {
		ch <- InvocationOutput{Output: result.Output}
	}
	close(ch)
	return ch, nil
}

// PrepareOperation is the operation-level preflight: it resolves an operation
// (or a specific binding) on an interface to a concrete binding, then reports
// the context that binding would require before invocation, without invoking it
// or causing side effects. It is the by-reference counterpart to PrepareBinding.
// Returns nil when requirements cannot be determined without invoking (the
// always-satisfiable answer). Context resolution is not performed here; any
// supplied callerContext narrows the reported requirements to what is still
// unsatisfied, exactly as for PrepareBinding.
func PrepareOperation(ctx context.Context, obiPath string, opKey string, bindingKey string, callerContext map[string]any) (*openbindings.ContextRequiredDetails, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI %q: %w", obiPath, err)
	}

	resolved, err := resolveBindingAndSource(iface, opKey, bindingKey, nil)
	if err != nil {
		return nil, err
	}

	es := resolveSourceLocation(resolved.source, filepath.Dir(obiPath))

	return PrepareBinding(ctx, InvokeOperationInput{
		Source:    InvokeSource{Format: es.Format, Location: es.Location, Content: es.Content},
		Ref:       resolved.binding.Ref,
		Context:   callerContext,
		Interface: iface,
	})
}

// PrepareOperationOutput wraps a prepareOperation result for CLI rendering.
// A nil Details means no requirements could be determined without invoking.
type PrepareOperationOutput struct {
	Details *openbindings.ContextRequiredDetails `json:"details"`
}

// Render returns a human-friendly representation.
func (o PrepareOperationOutput) Render() string {
	s := Styles
	if o.Details == nil {
		return s.Dim.Render("No context requirements (none determinable without invoking)")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Context required"))
	sb.WriteString("\n  ")
	sb.WriteString(s.Dim.Render("target: "))
	sb.WriteString(s.Key.Render(o.Details.Target))
	for i, alt := range o.Details.Alternatives {
		sb.WriteString("\n\n  ")
		sb.WriteString(s.Dim.Render(fmt.Sprintf("alternative %d (all required):", i+1)))
		for _, req := range alt.Requirements {
			sb.WriteString("\n    - ")
			sb.WriteString(s.Key.Render(req.Type))
			if req.Description != "" {
				sb.WriteString(s.Dim.Render(" — " + req.Description))
			}
		}
	}
	return sb.String()
}

// transformEventStream applies the binding's outputTransform to each event.
// Returns the source channel directly if no transform is configured.
func transformEventStream(src <-chan InvocationOutput, iface *openbindings.Interface, resolved *resolvedBinding) <-chan InvocationOutput {
	if resolved.binding.OutputTransform == nil {
		return src
	}
	out := make(chan InvocationOutput)
	go func() {
		defer close(out)
		for ev := range src {
			if ev.Error != nil || ev.Output == nil {
				out <- ev
				continue
			}
			transformed, err := ApplyTransform(iface.Transforms, resolved.binding.OutputTransform, ev.Output)
			if err != nil {
				out <- InvocationOutput{Error: &openbindings.InvocationError{
					Code:    "output_transform_error",
					Message: fmt.Sprintf("output transform failed: %v", err),
				}}
				continue
			}
			out <- InvocationOutput{Output: transformed}
		}
	}()
	return out
}

// SubscribeOBIOperationDirect opens a streaming subscription using
// pre-resolved binding components. Used by the TUI which already has the
// interface, binding, and source loaded.
func SubscribeOBIOperationDirect(ctx context.Context, binding *openbindings.BindingEntry, source openbindings.Source, obiDir string) (<-chan InvocationOutput, error) {
	es := resolveSourceLocation(source, obiDir)
	invoker := DefaultInvoker()
	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source:  es,
			Ref:     binding.Ref,
			Context: ctxData,
		})
	}
	return driveBinding(ctx, invoke, nil, nil, invoker.ContextResolver), nil
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

// InvokeOperationWithContext invokes an operation with cancellation support.
// Pass a cancellable context to allow aborting long-running operations.
func InvokeOperationWithContext(ctx context.Context, input InvokeOperationInput) InvokeOperationOutput {
	start := time.Now()

	// Validate input
	if input.Source.Format == "" {
		return InvokeOperationOutput{
			Error: &Error{
				Code:    "invalid_input",
				Message: "source.format is required",
			},
		}
	}
	if input.Ref == "" {
		return InvokeOperationOutput{
			Error: &Error{
				Code:    "invalid_input",
				Message: "ref is required",
			},
		}
	}

	// Unified delegate selection (capability + format, preference, self-first
	// ties). Native formats select the self-delegate (iface nil → in-process);
	// non-native formats select an external delegate when one is registered.
	chosen := selectDelegate(CapInvoke, input.Source.Format)

	var output InvokeOperationOutput
	if chosen == nil || chosen.iface == nil {
		// Self-delegate or nothing: invoke in-process when ob supports the
		// format natively, else there is nowhere to route.
		if BuiltinSupportsFormat(input.Source.Format) {
			output = invokeViaBuiltin(ctx, input)
		} else {
			return InvokeOperationOutput{
				Error: &Error{
					Code:    "delegate_resolution_failed",
					Message: fmt.Sprintf("no invoker or delegate handles format %q", input.Source.Format),
				},
			}
		}
	} else {
		output = invokeViaExternalDelegate(ctx, delegates.Resolved{
			Format:   input.Source.Format,
			Delegate: chosen.name,
			Location: chosen.location,
			OBI:      &delegates.ResolvedOBI{Interface: *chosen.iface},
		}, input)
	}

	output.DurationMs = time.Since(start).Milliseconds()
	return output
}

// SubscribeOperationWithContext opens a streaming subscription with cancellation
// support. Mirrors InvokeOperationWithContext but returns a channel of events
// instead of a single output. External delegates are not supported (streaming
// across process boundaries requires a transport protocol; use builtin drivers).
func SubscribeOperationWithContext(ctx context.Context, input InvokeOperationInput) (<-chan InvocationOutput, error) {
	if input.Source.Format == "" {
		return nil, fmt.Errorf("source.format is required")
	}
	if input.Ref == "" {
		return nil, fmt.Errorf("ref is required")
	}

	if !BuiltinSupportsFormat(input.Source.Format) {
		return nil, fmt.Errorf("streaming not supported for format %q (no builtin invoker)", input.Source.Format)
	}

	invoker := DefaultInvoker()
	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source: openbindings.InvocationSource{
				Format:   input.Source.Format,
				Location: input.Source.Location,
				Content:  input.Source.Content,
			},
			Ref:       input.Ref,
			Context:   ctxData,
			Interface: input.Interface,
		})
	}
	return driveBinding(ctx, invoke, input.Context, input.Input, invoker.ContextResolver), nil
}

// invokeViaBuiltin invokes an operation using the built-in OperationInvoker.
func invokeViaBuiltin(ctx context.Context, input InvokeOperationInput) InvokeOperationOutput {
	bindCtx := input.Context
	if input.Source.Binary != "" {
		bindCtx = withBinaryMetadata(bindCtx, input.Source.Binary)
	}

	invoker := DefaultInvoker()
	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source: openbindings.InvocationSource{
				Format:   input.Source.Format,
				Location: input.Source.Location,
				Content:  input.Source.Content,
			},
			Ref:       input.Ref,
			Context:   ctxData,
			Interface: input.Interface,
		})
	}

	return reduceUnaryInvocation(driveBinding(ctx, invoke, bindCtx, input.Input, invoker.ContextResolver))
}

// reduceUnaryInvocation collapses an invocation event stream to the unary
// output shape: the last output wins; a terminal error takes precedence.
func reduceUnaryInvocation(events <-chan InvocationOutput) InvokeOperationOutput {
	var last *InvocationOutput
	for ev := range events {
		ev := ev
		last = &ev
	}
	if last == nil {
		return InvokeOperationOutput{}
	}
	if last.Error != nil {
		status := last.Status
		if status == 0 {
			status = 1
		}
		return InvokeOperationOutput{
			Status:     status,
			DurationMs: last.DurationMs,
			Error:      last.Error,
		}
	}
	return InvokeOperationOutput{
		Output:     last.Output,
		Status:     last.Status,
		DurationMs: last.DurationMs,
	}
}

// invokeViaExternalDelegate invokes an operation via an external delegate's
// invokeBinding capability (the binding-invoker frame protocol over WebSocket,
// or the delegate's CLI realization; see DelegateBindingInvoker), driving the
// unary shape through the invocation handle. CONTEXT_REQUIRED challenges from
// the delegate (or the downstream binding behind it) resolve through the
// configured resolver, exactly as for in-process invokers.
func invokeViaExternalDelegate(ctx context.Context, resolved delegates.Resolved, input InvokeOperationInput) InvokeOperationOutput {
	delegateInvoker, err := DelegateBindingInvoker(resolved)
	if err != nil {
		return InvokeOperationOutput{
			Error: &Error{Code: "delegate_error", Message: err.Error()},
		}
	}

	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return delegateInvoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source: openbindings.InvocationSource{
				Format:   input.Source.Format,
				Location: input.Source.Location,
				Content:  input.Source.Content,
			},
			Ref:     input.Ref,
			Context: ctxData,
		})
	}
	return reduceUnaryInvocation(driveBinding(ctx, invoke, input.Context, input.Input, DefaultInvoker().ContextResolver))
}
