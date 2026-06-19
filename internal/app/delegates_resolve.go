package app

import (
	"fmt"
	"strings"
)

// DelegateResolveResult is returned by DelegateResolve (ResolveDelegateResult in
// the contract): which delegate handles a format, and the capabilities it
// offers for it.
type DelegateResolveResult struct {
	Format       string               `json:"format"`
	Name         string               `json:"name,omitempty"`
	Location     string               `json:"location,omitempty"`
	Builtin      bool                 `json:"builtin,omitempty"`
	Capabilities []DelegateCapability `json:"capabilities,omitempty"`
}

// Render returns a human-readable summary.
func (r DelegateResolveResult) Render() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Format: %s\n", r.Format)
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

// DelegateResolve reports which delegate handles a given format and the
// capabilities it offers for it, via the unified delegate selection (the
// self-delegate is delegate 0). This is the diagnostic view of routing: the
// highest-ranked delegate that handles the format wins.
func DelegateResolve(format string) (*DelegateResolveResult, error) {
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
		if best == nil || ranksAbove(c, best) {
			best = c
		}
	}
	if best == nil {
		return nil, exitText(1, fmt.Sprintf("no delegate handles format %q", format), true)
	}

	return &DelegateResolveResult{
		Format:       format,
		Name:         best.name,
		Location:     best.location,
		Builtin:      best.builtin,
		Capabilities: best.capabilities,
	}, nil
}
