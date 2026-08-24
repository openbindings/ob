package app

import (
	"context"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"

	"github.com/openbindings/ob/internal/delegates"
)

// Routing is ob's application policy layered on the delegate registry: given a
// (capability, format) task, select ONE delegate to route to. Candidates come
// from the registration-time records, but binding-spec support is checked
// authoritatively against every capability candidate at selection time. The
// selected external's interface is resolved and verified against its snapshot
// pin before its support query is invoked.

// delegateCandidate is a delegate considered for a (capability, format) task:
// the self-delegate or a registered external, in registry-record shape.
type delegateCandidate struct {
	record  DelegateRecord
	builtin bool
	// iface is the delegate's resolved-and-pin-verified OBI, populated by
	// resolveInterface for externals. Nil for the self-delegate, which runs
	// in-process rather than over a transport.
	iface *openbindings.Interface
	// supportCheck is a test seam. Production candidates leave it nil and use
	// the delegate's live checkBindingSpecs operation.
	supportCheck func(context.Context, DelegateCapability, []string) ([]openbindings.BindingSpecVerdict, error)
}

func (c *delegateCandidate) name() string     { return c.record.Name }
func (c *delegateCandidate) location() string { return c.record.Location }

// resolveInterface resolves the candidate's interface for use, verifying it
// against the registration snapshot's content digest (match and invoke the
// same document). The result is cached on the candidate.
func (c *delegateCandidate) resolveInterface() (*openbindings.Interface, error) {
	if c.builtin {
		return nil, nil // the self-delegate runs in-process
	}
	if c.iface != nil {
		return c.iface, nil
	}
	iface, err := resolvePinnedDelegateInterface(c.record)
	if err != nil {
		return nil, err
	}
	c.iface = iface
	return iface, nil
}

// effectivePreference is the preference to rank this candidate by for a
// (capability, format) task: the most specific matching entry wins — the
// (operation, format) entry, else the operation entry, else the
// delegate-level value, else the baseline 0. The capability names the
// operation ob delegates for it (capabilityOperation).
func (c *delegateCandidate) effectivePreference(cap DelegateCapability, format string) float64 {
	operation := capabilityOperation[cap]
	for _, fp := range c.record.BindingSpecPreferences {
		if fp.Operation == operation && delegates.SupportsFormat(fp.BindingSpec, format) {
			return fp.Preference
		}
	}
	return c.record.effectiveOperationPreference(operation)
}

func (c *delegateCandidate) provides(cap DelegateCapability) bool {
	for _, have := range c.record.Capabilities {
		if have == cap {
			return true
		}
	}
	return false
}

func (c *delegateCandidate) checkBindingSpecs(ctx context.Context, cap DelegateCapability, bindingSpecs []string) ([]openbindings.BindingSpecVerdict, error) {
	if c.supportCheck != nil {
		return c.supportCheck(ctx, cap, bindingSpecs)
	}
	if c.builtin {
		return CheckBindingSpecs(bindingSpecs), nil
	}
	iface, err := c.resolveInterface()
	if err != nil {
		return nil, fmt.Errorf("resolve delegate %q: %w", c.name(), err)
	}
	names := checkBindingSpecOperationNames(cap)
	opKey, ok := delegateOpKey(iface, names...)
	if !ok {
		return nil, fmt.Errorf("delegate %q was registered without checkBindingSpecs; re-register it (`ob delegate register %s`) to refresh its pinned interface", c.name(), c.location())
	}

	// The published support-query result is structured JSON. Usage sources
	// need the same explicit machine-lane decoder ob uses for a delegate's
	// invokeBinding operation; other formats fall through to their defaults.
	call := invoke.Invoke(ctx, delegateExecInvoker(c.name()), iface, invoke.NewOperationSignature[any, any](opKey))
	if err := call.Write(ctx, map[string]any{"bindingSpecs": bindingSpecs}); err != nil {
		call.Cancel()
		return nil, fmt.Errorf("delegate %q checkBindingSpecs: %w", c.name(), err)
	}
	_ = call.Close()
	out, err := invoke.Single(ctx, call.Outputs())
	if err != nil {
		return nil, fmt.Errorf("delegate %q checkBindingSpecs: %w", c.name(), err)
	}
	verdicts, err := decodeOutput[[]openbindings.BindingSpecVerdict](out)
	if err != nil {
		return nil, fmt.Errorf("delegate %q returned invalid checkBindingSpecs verdicts: %w", c.name(), err)
	}
	if err := validateBindingSpecVerdicts(bindingSpecs, *verdicts); err != nil {
		return nil, fmt.Errorf("delegate %q returned invalid checkBindingSpecs verdicts: %w", c.name(), err)
	}
	return *verdicts, nil
}

func checkBindingSpecOperationNames(cap DelegateCapability) []string {
	switch cap {
	case CapInvoke:
		return []string{"openbindings.binding-invoker.checkBindingSpecs", "checkBindingSpecs"}
	case CapSynthesize, CapInspect:
		return []string{"openbindings.interface-synthesizer.checkBindingSpecs", "checkBindingSpecs"}
	default:
		return []string{"checkBindingSpecs"}
	}
}

func validateBindingSpecVerdicts(bindingSpecs []string, verdicts []openbindings.BindingSpecVerdict) error {
	expected := openbindings.CheckBindingSpecs(bindingSpecs, nil)
	if len(verdicts) != len(expected) {
		return fmt.Errorf("got %d verdicts, want %d", len(verdicts), len(expected))
	}
	for i := range expected {
		if verdicts[i].BindingSpec != expected[i].BindingSpec {
			return fmt.Errorf("verdict %d names %q, want exact token %q", i, verdicts[i].BindingSpec, expected[i].BindingSpec)
		}
	}
	return nil
}

func verdictSupportSet(verdicts []openbindings.BindingSpecVerdict) map[string]bool {
	supported := make(map[string]bool, len(verdicts))
	for _, verdict := range verdicts {
		if verdict.Supported {
			supported[verdict.BindingSpec] = true
		}
	}
	return supported
}

// gatherDelegates builds the routing candidate set from the registry records:
// the in-process self-delegate first, then each registered delegate in
// registration order. Snapshots serve matching; interfaces resolve at use.
func gatherDelegates() []delegateCandidate {
	candidates := []delegateCandidate{{record: selfDelegateRecord(), builtin: true}}
	for _, rec := range GetDelegateContext().Delegates {
		if isSelf(rec.Location) {
			continue // folded into the self-delegate
		}
		candidates = append(candidates, delegateCandidate{record: rec})
	}
	return candidates
}

// selectDelegate picks the delegate that should handle (capability, format)
// across the registry, or nil when none qualifies.
func selectDelegate(ctx context.Context, cap DelegateCapability, format string) (*delegateCandidate, error) {
	return selectDelegateFrom(ctx, gatherDelegates(), cap, format)
}

// selectDelegateFrom picks the delegate that should handle (capability, format)
// per OBI-T-09 applied to delegates: a candidate must provide the capability
// AND handle the format (ob's narrowing); among those, higher effective
// preference wins, and ties go to the builtin self-delegate first, then to
// registration order. Returns nil when no candidate qualifies.
func selectDelegateFrom(ctx context.Context, candidates []delegateCandidate, cap DelegateCapability, format string) (*delegateCandidate, error) {
	var best *delegateCandidate
	var bestPref float64
	for i := range candidates {
		c := &candidates[i]
		if !c.provides(cap) {
			continue
		}
		verdicts, err := c.checkBindingSpecs(ctx, cap, []string{format})
		if err != nil {
			return nil, err
		}
		if !verdictSupportSet(verdicts)[format] {
			continue
		}
		pref := c.effectivePreference(cap, format)
		switch {
		case best == nil:
		case pref > bestPref:
		case pref == bestPref && c.builtin && !best.builtin:
			// equal preference → builtin self-delegate wins the tie
		default:
			continue // not better; keep earlier (stable registration order)
		}
		best, bestPref = c, pref
	}
	return best, nil
}

// availableBindingSpecs checks every unique candidate token in one call per
// delegate, producing the support union used by operation binding selection.
func availableBindingSpecs(ctx context.Context, candidates []delegateCandidate, cap DelegateCapability, bindingSpecs []string) (map[string]bool, error) {
	requestedVerdicts := openbindings.CheckBindingSpecs(bindingSpecs, nil)
	requested := make([]string, len(requestedVerdicts))
	for i, verdict := range requestedVerdicts {
		requested[i] = verdict.BindingSpec
	}

	available := make(map[string]bool, len(requested))
	for i := range candidates {
		candidate := &candidates[i]
		if !candidate.provides(cap) {
			continue
		}
		verdicts, err := candidate.checkBindingSpecs(ctx, cap, requested)
		if err != nil {
			return nil, err
		}
		for bindingSpec := range verdictSupportSet(verdicts) {
			available[bindingSpec] = true
		}
	}
	return available, nil
}
