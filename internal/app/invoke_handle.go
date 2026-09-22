package app

import (
	"context"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/invoke"
)

// InvokeBindingHandle returns the raw cardinality-agnostic Invocation handle
// for a binding invocation, routing by source format: the builtin invoker, or
// a retained invoke-role provider. This is the entrypoint of
// `ob start`'s frame endpoint — the frame stream is this handle, serialized.
//
// Builtin invocations get a store-backed preflight (preflightBinding): when the
// invoker reports its requirements statically and ob's context store satisfies
// them under the key derived from the challenge's target, the stored context
// merges beneath the per-call context (per-call wins). Anything still missing
// surfaces as a terminal CONTEXT_REQUIRED for the remote caller — the consumer
// runtime owns reactive resolution (binding-invoker rule 9); a frame server
// never prompts.
func InvokeBindingHandle(ctx context.Context, input InvocationInput) invoke.Invocation[any, any] {
	if input.Source.BindingSpec == "" {
		return invoke.NewErroredInvocation[any, any](invoke.NewInvocationError(invoke.ErrCodeValidationFailed))
	}
	if input.Selector == "" {
		return invoke.NewErroredInvocation[any, any](invoke.NewInvocationError(invoke.ErrCodeValidationFailed))
	}

	bindCtx := input.Context
	if input.Source.Binary != "" {
		bindCtx = withBinaryMetadata(bindCtx, input.Source.Binary)
	}
	args := &invoke.BindingInvocationArgs{
		Source: invoke.InvocationSource{
			BindingSpec: input.Source.BindingSpec,
			Location:    input.Source.Location,
			Content:     input.Source.Content,
		},
		Selector:  input.Selector,
		Context:   bindCtx,
		Interface: input.Interface,
	}

	if BuiltinSupportsFormat(input.Source.BindingSpec) {
		invoker := DefaultInvoker()
		args.Context = withStoredContext(ctx, invoker, args)
		return invoker.InvokeBinding(ctx, args)
	}

	delegateInvoker, err := resolveDelegateInvoker(ctx, input.Source.BindingSpec)
	if err != nil {
		return invoke.NewErroredInvocation[any, any](invoke.NewInvocationErrorWithData(
			invoke.ErrCodeBindingNotFound,
			map[string]any{"message": err.Error()},
		))
	}
	return delegateInvoker.InvokeBinding(ctx, args)
}

// OperationHandleInput identifies an operation (or one of its bindings) on an
// inline interface. It is the application form of the operation-invoker open
// payload used by ob start's frame endpoint.
type OperationHandleInput struct {
	Interface *openbindings.Interface
	Operation string
	Binding   string
	Context   map[string]any
	// Local evidence only; never serialized into the invocation contract.
	Diagnostics *invoke.DiagnosticCollector
}

// InvokeOperationHandle returns the cardinality-agnostic operation-layer
// handle for an inline interface. Creation is inert; callers drive it with
// Write/Close and consume Outputs exactly as they do InvokeBindingHandle.
func InvokeOperationHandle(ctx context.Context, input OperationHandleInput) invoke.Invocation[any, any] {
	if input.Interface == nil {
		return invoke.NewErroredInvocation[any, any](&invoke.InvocationError{
			Code: invoke.ErrCodeValidationFailed,
		})
	}
	if (input.Operation == "") == (input.Binding == "") {
		return invoke.NewErroredInvocation[any, any](&invoke.InvocationError{
			Code: invoke.ErrCodeValidationFailed,
		})
	}

	operation := input.Operation
	var opts []invoke.InvokeOption
	if input.Diagnostics != nil {
		opts = append(opts, invoke.WithDiagnosticCollector(input.Diagnostics))
	}
	if len(input.Context) > 0 {
		opts = append(opts, invoke.WithContext(input.Context))
	}
	if input.Binding != "" {
		binding, ok := input.Interface.Bindings[input.Binding]
		if !ok {
			return invoke.NewErroredInvocation[any, any](&invoke.InvocationError{
				Code: invoke.ErrCodeBindingNotFound,
			})
		}
		operation = binding.Operation
		opts = append(opts, invoke.WithBindingKey(input.Binding))
	}

	sig := invoke.NewOperationSignature[any, any](operation)
	return invoke.Invoke(ctx, DefaultInvoker(), input.Interface, sig, opts...)
}

// resolveDelegateInvoker retains the invoke-role provider and exact admitted
// workload route selected alongside its authoritative support query. Native
// handling has already been tried by the frame entrypoint. The Binding Invoker
// interface supplies frame semantics; the SDK owns the selected realization.
// No locator refetch or second selection occurs when work starts.
func resolveDelegateInvoker(ctx context.Context, format string) (invoke.BindingInvoker, error) {
	chosen, selectionErr := selectInstalledRole(ctx, CapInvoke, format, roleNativeFirst)
	if selectionErr != nil {
		return nil, selectionErr
	}
	if chosen == nil || chosen.Builtin {
		return nil, fmt.Errorf("no invoker or delegate handles format %q", format)
	}
	return &roleBindingInvoker{spec: format, route: chosen.Work}, nil
}

// withStoredContext runs the advisory preflight (which never dispatches the
// requested operation) and adds only the challenge-scoped subset of stored
// context beneath the explicitly supplied per-call context. The caller's context is not store-derived ambient
// authority, so it remains intact and wins on collision. If stored plus
// per-call context cannot satisfy the reported requirements, only the
// per-call context passes through and the binding's own CONTEXT_REQUIRED
// challenge reaches the caller.
func withStoredContext(ctx context.Context, invoker *invoke.OperationInvoker, args *invoke.BindingInvocationArgs) map[string]any {
	details, err := invoker.PreflightBinding(ctx, args)
	if err != nil || details == nil {
		return args.Context
	}
	stored, _ := NewCLIContextStore().Get(ctx, invoke.NormalizeEndpoint(details.Target))
	if len(stored) == 0 {
		return args.Context
	}
	merged := make(map[string]any, len(stored)+len(args.Context))
	for k, v := range stored {
		merged[k] = v
	}
	for k, v := range args.Context {
		merged[k] = v
	}
	if !invoke.ContextSatisfies(merged, details) {
		return args.Context
	}
	// Least privilege applies to reusable stored context. Preserve explicit
	// invocation context after reducing the stored entry to the selected
	// challenge alternative.
	scoped := invoke.ScopeContext(stored, details)
	out := make(map[string]any, len(scoped)+len(args.Context))
	for k, v := range scoped {
		out[k] = v
	}
	for k, v := range args.Context {
		out[k] = v
	}
	return out
}

// PreflightBinding is the preflightBinding operation: it tells the binding
// that an invocation of this selection may follow and reports the context
// requirements it can already identify, or nil when it knows of none. The
// answer is advisory and never dispatches the requested operation; the
// binding may do the answer-work its binding specification names. Formats
// without a builtin preflighter — including formats handled by delegates —
// report nil, the always-conformant answer; invokeBinding's live
// CONTEXT_REQUIRED challenge remains authoritative.
func PreflightBinding(ctx context.Context, input InvocationInput) (*invoke.ContextRequiredDetails, error) {
	if input.Source.BindingSpec == "" {
		return nil, fmt.Errorf("source.bindingSpec is required")
	}
	if input.Selector == "" {
		return nil, fmt.Errorf("selector is required")
	}
	if !BuiltinSupportsFormat(input.Source.BindingSpec) {
		return nil, nil
	}
	return DefaultInvoker().PreflightBinding(ctx, &invoke.BindingInvocationArgs{
		Source: invoke.InvocationSource{
			BindingSpec: input.Source.BindingSpec,
			Location:    input.Source.Location,
			Content:     input.Source.Content,
		},
		Selector: input.Selector,
		Context:  input.Context,
	})
}
