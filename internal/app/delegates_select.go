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
	// preference is the delegate-level preference (higher = more preferred;
	// 0 = the baseline, which absent also maps to). It is the default for every
	// offering, overridable per (capability[, format]) by perOffering.
	preference float64
	// perOffering overrides the delegate-level preference for a specific
	// capability (and optionally format), mirroring binding-vs-source preference.
	perOffering []OfferingPreference
}

// OfferingPreference overrides a delegate's preference for one offering — a
// capability, optionally scoped to a format. Higher = more preferred.
type OfferingPreference struct {
	Capability DelegateCapability `json:"capability"`
	Format     string             `json:"format,omitempty"`
	Preference float64            `json:"preference"`
}

// effectivePreference is the preference to rank this candidate by for a
// (capability, format) task: the most specific matching per-offering override
// (capability+format beats capability-only), else the delegate-level value,
// else the baseline 0.
func (c *delegateCandidate) effectivePreference(cap DelegateCapability, format string) float64 {
	pref := c.preference
	matchedSpecific := false
	matched := false
	for _, o := range c.perOffering {
		if o.Capability != cap {
			continue
		}
		specific := o.Format != ""
		if specific && !delegates.SupportsFormat(o.Format, format) {
			continue
		}
		if !matched || (specific && !matchedSpecific) {
			pref = o.Preference
			matched = true
			matchedSpecific = specific
		}
	}
	return pref
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
	delCtx := GetDelegateContext()
	for _, loc := range delCtx.Delegates {
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
		if pref, ok := delCtx.Preferences[loc]; ok {
			c.preference = pref.Preference
			c.perOffering = pref.PerOffering
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

// ranksAbove reports whether candidate a should outrank the current best b by
// delegate-level preference (used where there is no capability context, e.g.
// resolveDelegate): higher preference first; on equal preference the builtin
// self-delegate wins; otherwise keep the earlier candidate.
func ranksAbove(a, b *delegateCandidate) bool {
	if a.preference != b.preference {
		return a.preference > b.preference
	}
	if a.builtin != b.builtin {
		return a.builtin
	}
	return false
}
