package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
	"github.com/openbindings/openbindings-go/synthesize"
)

// Create / inspect delegation. When a source's format is not natively
// supported, ob routes the work to a registered delegate that satisfies the
// matching role, through the SDK's prepared operation dependency. Support and
// workload use the same retained provider and admitted operation correspondence;
// execution never rediscovers the provider's top-level OBI by location. Native
// formats and the no-delegate case fall through to the in-process authoring path.
//
// Automatic routing applies when the format is explicit and non-native. An
// explicit registration constraint (explicitRegistration) selects exactly that
// enrolled registration for the role with no native or alternate fallback.

// explicitRegistrationKey carries an operation-local OB-native selection
// constraint: perform one role through exactly this enrolled registration.
// It is never part of a shared operation input, never derived from x-ob source
// hints, and never a locator or display name.
type explicitRegistrationKey struct{ role DelegateCapability }

// WithExplicitRegistration constrains the named role to one registration ID for
// the calls made with the returned context.
func WithExplicitRegistration(ctx context.Context, role DelegateCapability, registrationID string) context.Context {
	if registrationID == "" {
		return ctx
	}
	return context.WithValue(ctx, explicitRegistrationKey{role: role}, registrationID)
}

func explicitRegistration(ctx context.Context, role DelegateCapability) string {
	id, _ := ctx.Value(explicitRegistrationKey{role: role}).(string)
	return id
}

// selectExplicitRole retains the one selected registration's route for a
// token. Enrollment is filtered before any provider call; a registration that
// is absent, not enrolled, unbound, or unsupported for the token refuses.
func selectExplicitRole(ctx context.Context, role DelegateCapability, token, registrationID string) (*roleSelection, error) {
	if token == "" {
		return nil, fmt.Errorf("an explicit registration requires an explicit binding specification for %s", role)
	}
	selected, err := selectInstalledRolesFrom(ctx, role, []string{token}, roleRanked, registrationID)
	if err != nil {
		return nil, err
	}
	chosen := selected[token]
	if chosen == nil || chosen.Builtin || chosen.Work == nil {
		return nil, fmt.Errorf("registration %s does not support %s for role %s; no built-in or alternate provider is used in explicit mode", registrationID, token, role)
	}
	return chosen, nil
}

// synthesizeViaDelegate routes a single-source synthesizeInterface either to
// the explicitly selected registration or, for a non-native format, to the
// native-first automatic policy. routed reports whether a delegate handled it;
// when false, the caller uses the native synthesizer.
func synthesizeViaDelegate(ctx context.Context, input *synthesize.SynthesizeInput) (iface *openbindings.Interface, routed bool, err error) {
	if input == nil {
		return nil, false, nil
	}
	if id := explicitRegistration(ctx, CapSynthesize); id != "" {
		if len(input.Sources) != 1 {
			return nil, true, errors.New("an explicit registration synthesizes exactly one source per call")
		}
		chosen, selectionErr := selectExplicitRole(ctx, CapSynthesize, input.Sources[0].BindingSpec, id)
		if selectionErr != nil {
			return nil, true, selectionErr
		}
		out, ierr := chosen.unary(ctx, input)
		if ierr != nil {
			return nil, true, fmt.Errorf("delegate %q synthesizeInterface: %w", id, ierr)
		}
		result, cerr := decodeOutput[openbindings.Interface](out)
		if cerr != nil {
			return nil, true, fmt.Errorf("delegate %q returned an invalid interface: %w", id, cerr)
		}
		return result, true, nil
	}
	if len(input.Sources) != 1 {
		return nil, false, nil // multi-source/mixed not routed; native handles or errors
	}
	format := input.Sources[0].BindingSpec
	if format == "" || BuiltinSupportsFormat(format) {
		return nil, false, nil // native
	}
	chosen, selectionErr := selectInstalledRole(ctx, CapSynthesize, format, roleNativeFirst)
	if selectionErr != nil {
		return nil, true, selectionErr
	}
	if chosen == nil || chosen.Builtin {
		return nil, false, nil // no delegate; let the native path report the unsupported format
	}

	out, ierr := chosen.unary(ctx, input)
	if ierr != nil {
		return nil, true, fmt.Errorf("delegate %q synthesizeInterface: %w", chosen.Runtime.candidate.Record.ID, ierr)
	}
	result, cerr := decodeOutput[openbindings.Interface](out)
	if cerr != nil {
		return nil, true, fmt.Errorf("delegate %q returned an invalid interface: %w", chosen.Runtime.candidate.Record.ID, cerr)
	}
	return result, true, nil
}

// inspectViaDelegate routes a non-native inspectSource to an inspect-capable
// delegate. routed reports whether a delegate handled it.
func inspectViaDelegate(ctx context.Context, source *openbindings.Source) (ins *synthesize.SourceInspection, routed bool, err error) {
	if source == nil {
		return nil, false, nil
	}
	if id := explicitRegistration(ctx, CapInspect); id != "" {
		chosen, selectionErr := selectExplicitRole(ctx, CapInspect, source.BindingSpec, id)
		if selectionErr != nil {
			return nil, true, selectionErr
		}
		out, ierr := chosen.unary(ctx, map[string]any{"source": source})
		if ierr != nil {
			return nil, true, fmt.Errorf("delegate %q inspectSource: %w", id, ierr)
		}
		result, cerr := decodeOutput[synthesize.SourceInspection](out)
		if cerr != nil {
			return nil, true, fmt.Errorf("delegate %q returned an invalid inspection: %w", id, cerr)
		}
		return result, true, nil
	}
	if source.BindingSpec == "" || BuiltinSupportsFormat(source.BindingSpec) {
		return nil, false, nil
	}
	chosen, selectionErr := selectInstalledRole(ctx, CapInspect, source.BindingSpec, roleNativeFirst)
	if selectionErr != nil {
		return nil, true, selectionErr
	}
	if chosen == nil || chosen.Builtin {
		return nil, false, nil
	}

	out, ierr := chosen.unary(ctx, map[string]any{"source": source})
	if ierr != nil {
		return nil, true, fmt.Errorf("delegate %q inspectSource: %w", chosen.Runtime.candidate.Record.ID, ierr)
	}
	result, cerr := decodeOutput[synthesize.SourceInspection](out)
	if cerr != nil {
		return nil, true, fmt.Errorf("delegate %q returned an invalid inspection: %w", chosen.Runtime.candidate.Record.ID, cerr)
	}
	return result, true, nil
}

// decodeOutput converts an opaque delegate output value into a typed result
// via a JSON round-trip.
func decodeOutput[T any](v any) (*T, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out T
	if err := jsonvalue.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
