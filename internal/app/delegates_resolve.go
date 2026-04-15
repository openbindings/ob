package app

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/delegates"
)

// DelegateResolveResult is returned by DelegateResolve.
type DelegateResolveResult struct {
	Format   string `json:"format"`
	Delegate string `json:"delegate"`
	Location string `json:"location,omitempty"`
}

// Render returns a human-readable summary.
func (r DelegateResolveResult) Render() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Format: %s\n", r.Format)
	fmt.Fprintf(&sb, "Delegate: %s", r.Delegate)
	if r.Location != "" {
		fmt.Fprintf(&sb, "\nLocation: %s", r.Location)
	}
	return sb.String()
}

// DelegateResolve resolves which delegate handles a given format.
func DelegateResolve(format string) (*DelegateResolveResult, error) {
	if strings.TrimSpace(format) == "" {
		return nil, usageExit("delegate resolve <format>")
	}

	delCtx := GetDelegateContext()

	resolved, err := delegates.Resolve(delegates.ResolveParams{
		Format:    format,
		Delegates: delCtx.Delegates,
	})
	if err != nil {
		return nil, exitText(1, err.Error(), true)
	}

	return &DelegateResolveResult{
		Format:   resolved.Format,
		Delegate: resolved.Delegate,
		Location: resolved.Location,
	}, nil
}
