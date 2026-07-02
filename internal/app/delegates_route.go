package app

import (
	"context"
	"encoding/json"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
)

// Create / inspect delegation. When a source's format is not natively
// supported, ob routes the work to a registered delegate that satisfies the
// matching interface, by operation-invoking the delegate's synthesizeInterface /
// inspectSource against the delegate's own OBI (the same invokeOnInterface core
// the invoke path uses). Native formats and the no-delegate case fall through
// to the in-process synthesizer/inspector unchanged.
//
// First cut: routes when the format is explicit and non-native. Auto-detecting
// a delegate-only format (extending DetectSourceCandidates to try delegate
// synthesizers, via the existing DelegateClaim framework) is a tracked refinement.

// abstract operation names for the delegatable capabilities, namespaced and bare.
var (
	synthesizeOpNames = []string{"openbindings.interface-synthesizer.synthesizeInterface", "synthesizeInterface"}
	inspectOpNames    = []string{"openbindings.source-inspector.inspectSource", "inspectSource"}
	invokeOpNames     = []string{"openbindings.binding-invoker.invokeBinding", "invokeBinding"}
)

// delegateOpKey finds the key in a delegate's OBI for an abstract operation,
// matching by key or alias (OBI-T-12).
func delegateOpKey(iface *openbindings.Interface, names ...string) (string, bool) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for key, op := range iface.Operations {
		if want[key] {
			return key, true
		}
		for _, a := range op.Aliases {
			if want[a] {
				return key, true
			}
		}
	}
	return "", false
}

// synthesizeViaDelegate routes a single-source, non-native synthesizeInterface to a
// synthesize-capable delegate. routed reports whether a delegate handled it; when
// false, the caller uses the native synthesizer.
func synthesizeViaDelegate(ctx context.Context, input *openbindings.SynthesizeInput) (iface *openbindings.Interface, routed bool, err error) {
	if input == nil || len(input.Sources) != 1 {
		return nil, false, nil // multi-source/mixed not routed; native handles or errors
	}
	format := input.Sources[0].Format
	if format == "" || BuiltinSupportsFormat(format) {
		return nil, false, nil // native
	}
	chosen := selectDelegate(CapSynthesize, format)
	if chosen == nil || chosen.builtin {
		return nil, false, nil // no delegate; let the native path report the unsupported format
	}
	delegateIface, rerr := chosen.resolveInterface()
	if rerr != nil {
		return nil, true, rerr // selected but unusable (unreachable or pin mismatch): surface it
	}
	opKey, ok := delegateOpKey(delegateIface, synthesizeOpNames...)
	if !ok {
		return nil, false, nil
	}

	out, ierr := invokeDelegateUnary(ctx, chosen, opKey, input)
	if ierr != nil {
		return nil, true, fmt.Errorf("delegate %q synthesizeInterface: %w", chosen.name(), ierr)
	}
	result, cerr := decodeOutput[openbindings.Interface](out)
	if cerr != nil {
		return nil, true, fmt.Errorf("delegate %q returned an invalid interface: %w", chosen.name(), cerr)
	}
	return result, true, nil
}

// inspectViaDelegate routes a non-native inspectSource to an inspect-capable
// delegate. routed reports whether a delegate handled it.
func inspectViaDelegate(ctx context.Context, source *openbindings.Source) (ins *openbindings.SourceInspection, routed bool, err error) {
	if source == nil || source.Format == "" || BuiltinSupportsFormat(source.Format) {
		return nil, false, nil
	}
	chosen := selectDelegate(CapInspect, source.Format)
	if chosen == nil || chosen.builtin {
		return nil, false, nil
	}
	delegateIface, rerr := chosen.resolveInterface()
	if rerr != nil {
		return nil, true, rerr // selected but unusable (unreachable or pin mismatch): surface it
	}
	opKey, ok := delegateOpKey(delegateIface, inspectOpNames...)
	if !ok {
		return nil, false, nil
	}

	out, ierr := invokeDelegateUnary(ctx, chosen, opKey, source)
	if ierr != nil {
		return nil, true, fmt.Errorf("delegate %q inspectSource: %w", chosen.name(), ierr)
	}
	result, cerr := decodeOutput[openbindings.SourceInspection](out)
	if cerr != nil {
		return nil, true, fmt.Errorf("delegate %q returned an invalid inspection: %w", chosen.name(), cerr)
	}
	return result, true, nil
}

// invokeDelegateUnary operation-invokes a delegate's operation against its OBI
// and reduces the (unary) result to a single output value.
func invokeDelegateUnary(ctx context.Context, chosen *delegateCandidate, opKey string, input any) (any, error) {
	ch, _, err := invokeOnInterface(ctx, chosen.iface, opKey, "", input, "")
	if err != nil {
		return nil, err
	}
	out := reduceUnaryInvocation(ch)
	if out.Error != nil {
		return nil, fmt.Errorf("%s", out.Error.Message)
	}
	return out.Output, nil
}

// decodeOutput converts an opaque delegate output value into a typed result
// via a JSON round-trip.
func decodeOutput[T any](v any) (*T, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
