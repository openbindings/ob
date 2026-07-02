package app

import (
	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
)

// Routing is ob's application policy layered on the delegate registry: given a
// (capability, format) task, select ONE delegate to route to. Candidates come
// from the registration-time records — no live probing — and the selected
// external's interface is resolved lazily, verified against its snapshot pin.

// delegateCandidate is a delegate considered for a (capability, format) task:
// the self-delegate or a registered external, in registry-record shape.
type delegateCandidate struct {
	record  DelegateRecord
	builtin bool
	// iface is the delegate's resolved-and-pin-verified OBI, populated by
	// resolveInterface for externals. Nil for the self-delegate, which runs
	// in-process rather than over a transport.
	iface *openbindings.Interface
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
	for _, fp := range c.record.FormatPreferences {
		if fp.Operation == operation && delegates.SupportsFormat(fp.Format, format) {
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

func (c *delegateCandidate) handles(format string) bool {
	for _, f := range c.record.Formats {
		if delegates.SupportsFormat(f.Format, format) {
			return true
		}
	}
	return false
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
func selectDelegate(cap DelegateCapability, format string) *delegateCandidate {
	return selectDelegateFrom(gatherDelegates(), cap, format)
}

// selectDelegateFrom picks the delegate that should handle (capability, format)
// per OBI-T-09 applied to delegates: a candidate must provide the capability
// AND handle the format (ob's narrowing); among those, higher effective
// preference wins, and ties go to the builtin self-delegate first, then to
// registration order. Returns nil when no candidate qualifies.
func selectDelegateFrom(candidates []delegateCandidate, cap DelegateCapability, format string) *delegateCandidate {
	var best *delegateCandidate
	var bestPref float64
	for i := range candidates {
		c := &candidates[i]
		if !c.provides(cap) || !c.handles(format) {
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
	return best
}
