package app

import (
	"context"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/invoke"

	"github.com/openbindings/ob/internal/delegates"
)

// InvokeBindingHandle returns the raw cardinality-agnostic Invocation handle
// for a binding invocation, routing by source format: the builtin invoker, or
// a resolved delegate (see DelegateBindingInvoker). This is the entrypoint of
// `ob start`'s frame endpoint — the frame stream is this handle, serialized.
//
// Builtin invocations get a store-backed preflight (prepareBinding): when the
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

// resolveDelegateInvoker selects an invoke-capable delegate that handles the
// format via the unified delegate selection (OBI-T-09's semantics applied to
// delegates — ob's narrowing, not a spec rule: capability + format,
// preference, self-first ties), then wraps it as a BindingInvoker. The
// self-delegate is excluded here by construction — it has no iface (it runs
// natively, and native handling was already tried before falling through to a
// delegate). The chosen delegate's frame/CLI transport is still carried by
// DelegateBindingInvoker (the frame-transport collapse remains a tracked
// follow-up; its owner is the serve-pass handoff note in
// ob-pj/wire-conformance.md).
func resolveDelegateInvoker(ctx context.Context, format string) (invoke.BindingInvoker, error) {
	chosen, selectionErr := selectDelegate(ctx, CapInvoke, format)
	if selectionErr != nil {
		return nil, selectionErr
	}
	if chosen == nil || chosen.builtin {
		return nil, fmt.Errorf("no invoker or delegate handles format %q", format)
	}
	iface, err := chosen.resolveInterface()
	if err != nil {
		return nil, err
	}
	return DelegateBindingInvoker(delegates.Resolved{
		Format:   format,
		Delegate: chosen.name(),
		Location: chosen.location(),
		OBI:      &delegates.ResolvedOBI{Interface: *iface},
	})
}

// withStoredContext runs the side-effect-free preflight and adds only the
// challenge-scoped subset of stored context beneath the explicitly supplied
// per-call context. The caller's context is not store-derived ambient
// authority, so it remains intact and wins on collision. If stored plus
// per-call context cannot satisfy the reported requirements, only the
// per-call context passes through and the binding's own CONTEXT_REQUIRED
// challenge reaches the caller.
func withStoredContext(ctx context.Context, invoker *invoke.OperationInvoker, args *invoke.BindingInvocationArgs) map[string]any {
	details, err := invoker.PrepareBinding(ctx, args)
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

// PrepareBinding is the prepareBinding operation: a side-effect-free
// preflight reporting the context a binding would require, or nil when the
// requirements cannot be determined statically. Formats without a builtin
// preparer — including formats handled by delegates — report nil, the
// always-satisfiable conformant answer; invokeBinding's reactive
// CONTEXT_REQUIRED challenge remains authoritative.
func PrepareBinding(ctx context.Context, input InvocationInput) (*invoke.ContextRequiredDetails, error) {
	if input.Source.BindingSpec == "" {
		return nil, fmt.Errorf("source.bindingSpec is required")
	}
	if input.Selector == "" {
		return nil, fmt.Errorf("selector is required")
	}
	if !BuiltinSupportsFormat(input.Source.BindingSpec) {
		return nil, nil
	}
	return DefaultInvoker().PrepareBinding(ctx, &invoke.BindingInvocationArgs{
		Source: invoke.InvocationSource{
			BindingSpec: input.Source.BindingSpec,
			Location:    input.Source.Location,
			Content:     input.Source.Content,
		},
		Selector: input.Selector,
		Context:  input.Context,
	})
}
