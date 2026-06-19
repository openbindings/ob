package app

import (
	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
)

// delegateCandidate is a delegate considered for a (capability, format) task —
// the self-delegate or a registered external, with the capabilities and formats
// it provides and its effective preference.
type delegateCandidate struct {
	name         string
	location     string
	builtin      bool
	capabilities []DelegateCapability
	formats      []DelegateFormatInfo
	// iface is the delegate's resolved OBI, used to operation-invoke it. Nil for
	// the self-delegate (which runs in-process, not over a transport).
	iface *openbindings.Interface
	// preference is the effective preference for this candidate (higher = more
	// preferred; absent = baseline 0). Phase 5 fills per-delegate/per-offering
	// values; until then every candidate sits at the baseline.
	preference float64
}

// selfDelegateCandidate is ob's own native handling as a routing candidate:
// all three capabilities, native formats, no transport (iface nil → in-process).
func selfDelegateCandidate() delegateCandidate {
	var formats []DelegateFormatInfo
	for _, tok := range getNativeTokens() {
		formats = append(formats, DelegateFormatInfo{Format: tok})
	}
	return delegateCandidate{
		name:         "ob",
		builtin:      true,
		capabilities: []DelegateCapability{CapInvoke, CapCreate, CapInspect},
		formats:      formats,
	}
}

// gatherDelegates builds the candidate set for routing: the in-process
// self-delegate (delegate 0) plus each reachable registered delegate,
// introspected for its capabilities and formats. Unreachable delegates are
// skipped — they cannot serve a task. Introspection is live per call for now;
// the config-record cache (with stored preference) is the optimization that
// follows.
func gatherDelegates() []delegateCandidate {
	candidates := []delegateCandidate{selfDelegateCandidate()}
	for _, loc := range GetDelegateContext().Delegates {
		if isSelf(loc) {
			continue // folded into the self-delegate
		}
		iface, err := resolveDelegateInterface(loc)
		if err != nil {
			continue // unreachable
		}
		c := delegateCandidate{
			name:         delegates.NameFromLocation(loc),
			location:     loc,
			iface:        iface,
			capabilities: delegateCapabilities(iface),
		}
		if iface.Name != "" {
			c.name = iface.Name
		}
		if fmts, ferr := delegates.ProbeFormats(loc, delegates.DefaultProbeTimeout); ferr == nil {
			for _, f := range fmts {
				c.formats = append(c.formats, DelegateFormatInfo{Format: f})
			}
		}
		candidates = append(candidates, c)
	}
	return candidates
}

// selectDelegate picks the delegate that should handle (capability, format)
// across the gathered candidate set, or nil when none qualifies.
func selectDelegate(cap DelegateCapability, format string) *delegateCandidate {
	return selectDelegateFrom(gatherDelegates(), cap, format)
}

func (c *delegateCandidate) provides(cap DelegateCapability) bool {
	for _, have := range c.capabilities {
		if have == cap {
			return true
		}
	}
	return false
}

func (c *delegateCandidate) handles(format string) bool {
	for _, f := range c.formats {
		if delegates.SupportsFormat(f.Format, format) {
			return true
		}
	}
	return false
}

// selectDelegateFrom picks the delegate that should handle (capability, format)
// per OBI-T-09 applied to delegates: a candidate must provide the capability
// AND handle the format; among those, higher effective preference wins, and
// ties go to the builtin self-delegate first, then to input (registration)
// order. Returns nil when no candidate qualifies.
func selectDelegateFrom(candidates []delegateCandidate, cap DelegateCapability, format string) *delegateCandidate {
	var best *delegateCandidate
	for i := range candidates {
		c := &candidates[i]
		if !c.provides(cap) || !c.handles(format) {
			continue
		}
		if best == nil || ranksAbove(c, best) {
			best = c
		}
	}
	return best
}

// ranksAbove reports whether candidate a should outrank the current best b:
// higher preference first; on equal preference the builtin self-delegate wins;
// otherwise keep the earlier candidate (stable registration order).
func ranksAbove(a, b *delegateCandidate) bool {
	if a.preference != b.preference {
		return a.preference > b.preference
	}
	if a.builtin != b.builtin {
		return a.builtin
	}
	return false
}
