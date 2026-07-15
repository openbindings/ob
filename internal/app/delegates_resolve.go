package app

import (
	"fmt"
	"strings"
)

// ResolveDelegateForBindingSpecResult is resolveDelegateForBindingSpec's output: which
// delegate ob's routing would select for a format token, and the capabilities
// it offers for it. This is ob's format-narrowing diagnostic, layered on top
// of the operation-keyed resolveDelegate.
type ResolveDelegateForBindingSpecResult struct {
	BindingSpec  string               `json:"bindingSpec"`
	Name         string               `json:"name,omitempty"`
	Location     string               `json:"location,omitempty"`
	Builtin      bool                 `json:"builtin,omitempty"`
	Capabilities []DelegateCapability `json:"capabilities,omitempty"`
}

// Render returns a human-readable summary.
func (r ResolveDelegateForBindingSpecResult) Render() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "BindingSpec: %s\n", r.BindingSpec)
	fmt.Fprintf(&sb, "Delegate: %s", r.Name)
	if r.Builtin {
		sb.WriteString(" (builtin)")
	} else if r.Location != "" {
		fmt.Fprintf(&sb, "\nLocation: %s", r.Location)
	}
	if len(r.Capabilities) > 0 {
		caps := make([]string, len(r.Capabilities))
		for i, c := range r.Capabilities {
			caps[i] = string(c)
		}
		fmt.Fprintf(&sb, "\nCapabilities: %s", strings.Join(caps, ", "))
	}
	return sb.String()
}

// ResolveDelegateForFormat reports which delegate ob's routing would select
// for a format: the highest-ranked candidate (by delegate-level preference;
// ties favor the self-delegate, then registration order) that handles it.
func ResolveDelegateForBindingSpec(format string) (*ResolveDelegateForBindingSpecResult, error) {
	if strings.TrimSpace(format) == "" {
		return nil, usageExit("delegate resolve <format>")
	}

	candidates := gatherDelegates()
	var best *delegateCandidate
	for i := range candidates {
		c := &candidates[i]
		if !c.handles(format) {
			continue
		}
		if best == nil || delegateLevelPreference(c) > delegateLevelPreference(best) {
			best = c
		}
	}
	if best == nil {
		return nil, exitText(1, fmt.Sprintf("no delegate handles format %q", format), true)
	}

	return &ResolveDelegateForBindingSpecResult{
		BindingSpec:  format,
		Name:         best.name(),
		Location:     best.location(),
		Builtin:      best.builtin,
		Capabilities: best.record.Capabilities,
	}, nil
}

// delegateLevelPreference is the candidate's delegate-level preference (the
// baseline 0 when unset) — the ranking used where there is no operation
// context.
func delegateLevelPreference(c *delegateCandidate) float64 {
	if c.record.Preference != nil {
		return *c.record.Preference
	}
	return 0
}
