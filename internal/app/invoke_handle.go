package app

import (
	"context"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
)

// InvokeBindingHandle returns the raw cardinality-agnostic Invocation handle
// for a binding invocation, routing by source format: the builtin invoker, or
// a resolved delegate (see DelegateBindingInvoker). This is the entrypoint of
// `ob serve`'s frame endpoint — the frame stream is this handle, serialized.
//
// Builtin invocations get a store-backed preflight (prepareBinding): when the
// invoker reports its requirements statically and ob's context store satisfies
// them under the key derived from the challenge's target, the stored context
// merges beneath the per-call context (per-call wins). Anything still missing
// surfaces as a terminal CONTEXT_REQUIRED for the remote caller — the consumer
// runtime owns reactive resolution (binding-invoker rule 9); a frame server
// never prompts.
func InvokeBindingHandle(ctx context.Context, input InvokeOperationInput) openbindings.Invocation[any, any] {
	if input.Source.Format == "" {
		return openbindings.NewErroredInvocation[any, any](&Error{
			Code: openbindings.ErrCodeValidationFailed, Message: "source.format is required",
		})
	}
	if input.Ref == "" {
		return openbindings.NewErroredInvocation[any, any](&Error{
			Code: openbindings.ErrCodeValidationFailed, Message: "ref is required",
		})
	}

	bindCtx := input.Context
	if input.Source.Binary != "" {
		bindCtx = withBinaryMetadata(bindCtx, input.Source.Binary)
	}
	args := &openbindings.BindingInvocationArgs{
		Source: openbindings.InvocationSource{
			Format:   input.Source.Format,
			Location: input.Source.Location,
			Content:  input.Source.Content,
		},
		Ref:       input.Ref,
		Context:   bindCtx,
		Interface: input.Interface,
	}

	if BuiltinSupportsFormat(input.Source.Format) {
		invoker := DefaultInvoker()
		args.Context = withStoredContext(ctx, invoker, args)
		return invoker.InvokeBinding(ctx, args)
	}

	delegateInvoker, err := resolveDelegateInvoker(input.Source.Format)
	if err != nil {
		return openbindings.NewErroredInvocation[any, any](&Error{
			Code:    openbindings.ErrCodeBindingNotFound,
			Message: err.Error(),
		})
	}
	return delegateInvoker.InvokeBinding(ctx, args)
}

// resolveDelegateInvoker selects an invoke-capable delegate that handles the
// format via the unified delegate selection (OBI-T-09: capability + format,
// preference, self-first ties), then wraps it as a BindingInvoker. The
// self-delegate is excluded here by construction — it has no iface (it runs
// natively, and native handling was already tried before falling through to a
// delegate). The chosen delegate's frame/CLI transport is still carried by
// DelegateBindingInvoker (the frame-transport collapse is the tracked "B"
// follow-up; see delegate-model-design.md §11b).
func resolveDelegateInvoker(format string) (openbindings.BindingInvoker, error) {
	chosen := selectDelegate(CapInvoke, format)
	if chosen == nil || chosen.iface == nil {
		return nil, fmt.Errorf("no invoker or delegate handles format %q", format)
	}
	return DelegateBindingInvoker(delegates.Resolved{
		Format:   format,
		Delegate: chosen.name,
		Location: chosen.location,
		OBI:      &delegates.ResolvedOBI{Interface: *chosen.iface},
	})
}

// withStoredContext runs the side-effect-free preflight and merges stored
// context beneath the per-call context when the merge satisfies the reported
// requirements; otherwise the per-call context passes through untouched and
// the binding's own CONTEXT_REQUIRED challenge reaches the caller.
func withStoredContext(ctx context.Context, invoker *openbindings.OperationInvoker, args *openbindings.BindingInvocationArgs) map[string]any {
	details, err := invoker.PrepareBinding(ctx, args)
	if err != nil || details == nil {
		return args.Context
	}
	stored, _ := NewCLIContextStore().Get(ctx, openbindings.NormalizeEndpoint(details.Target))
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
	if !openbindings.ContextSatisfies(merged, details) {
		return args.Context
	}
	return merged
}

// PrepareBinding is the prepareBinding operation: a side-effect-free
// preflight reporting the context a binding would require, or nil when the
// requirements cannot be determined statically. Formats without a builtin
// preparer — including formats handled by delegates — report nil, the
// always-satisfiable conformant answer; invokeBinding's reactive
// CONTEXT_REQUIRED challenge remains authoritative.
func PrepareBinding(ctx context.Context, input InvokeOperationInput) (*openbindings.ContextRequiredDetails, error) {
	if input.Source.Format == "" {
		return nil, fmt.Errorf("source.format is required")
	}
	if input.Ref == "" {
		return nil, fmt.Errorf("ref is required")
	}
	if !BuiltinSupportsFormat(input.Source.Format) {
		return nil, nil
	}
	return DefaultInvoker().PrepareBinding(ctx, &openbindings.BindingInvocationArgs{
		Source: openbindings.InvocationSource{
			Format:   input.Source.Format,
			Location: input.Source.Location,
			Content:  input.Source.Content,
		},
		Ref:     input.Ref,
		Context: input.Context,
	})
}
