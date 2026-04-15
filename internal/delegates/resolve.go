// Package delegates - resolve.go contains binding format delegate resolution logic.
package delegates

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/execref"
	openbindings "github.com/openbindings/openbindings-go"
)

// Resolved contains the result of delegate resolution.
type Resolved struct {
	Format   string       `json:"format"`
	Delegate string       `json:"delegate"`
	Source   string       `json:"source"`             // environment
	Location string       `json:"location,omitempty"` // how to reach the delegate
	OBI      *ResolvedOBI `json:"-"`                  // delegate's OBI, populated during resolution
}

// ResolvedOBI holds a delegate's OBI obtained during probing.
type ResolvedOBI struct {
	Interface openbindings.Interface
}

// ResolveParams configures delegate resolution.
type ResolveParams struct {
	Format string
	// Delegates is the list of delegate locations from the active environment.
	Delegates []string
	// ExcludeLocations is a list of delegate locations to skip during resolution
	// (e.g., the current binary to prevent self-recursion).
	ExcludeLocations []string
}

// Resolve finds the delegate for a given format by probing delegates.
func Resolve(params ResolveParams) (Resolved, error) {
	format := strings.TrimSpace(params.Format)
	if format == "" {
		return Resolved{}, fmt.Errorf("format is required")
	}

	excluded := make(map[string]struct{}, len(params.ExcludeLocations))
	for _, loc := range params.ExcludeLocations {
		excluded[loc] = struct{}{}
	}

	type match struct {
		info Info
		obi  *openbindings.Interface
	}
	var matches []match
	for _, loc := range params.Delegates {
		loc = strings.TrimSpace(loc)
		if loc == "" {
			continue
		}
		if _, skip := excluded[loc]; skip {
			continue
		}

		cmd := loc
		if IsExecURL(loc) {
			if c, err := execref.RootCommand(loc); err == nil {
				cmd = c
			}
		}

		iface, err := RunCLIOpenBindings(cmd, DefaultProbeTimeout)
		if err != nil {
			continue
		}

		fmts, err := probeFormatsFromInterface(cmd, DefaultProbeTimeout, iface)
		if err != nil {
			continue
		}

		for _, f := range fmts {
			if SupportsFormat(f, format) {
				matches = append(matches, match{
					info: Info{
						Name:     NameFromLocation(loc),
						Location: loc,
						Source:   SourceEnvironment,
					},
					obi: &iface,
				})
				break
			}
		}
	}

	switch len(matches) {
	case 0:
		return Resolved{}, fmt.Errorf("no delegate supports %s", format)
	case 1:
		return Resolved{
			Format:   format,
			Delegate: matches[0].info.Name,
			Source:   matches[0].info.Source,
			Location: matches[0].info.Location,
			OBI:      &ResolvedOBI{Interface: *matches[0].obi},
		}, nil
	default:
		return Resolved{}, fmt.Errorf("multiple delegates support %s; use --delegate to specify which one", format)
	}
}

// SupportsFormat checks if a delegate's format token supports a requested format.
// This handles version matching (e.g., "usage@^2.0.0" supports "usage@2.1.0").
func SupportsFormat(delegateFormat, requestedFormat string) bool {
	return supportsFormatToken(delegateFormat, requestedFormat)
}
