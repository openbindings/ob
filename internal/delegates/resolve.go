// Package delegates - resolve.go: the resolved-delegate carrier and
// format-token support matching.
package delegates

import (
	openbindings "github.com/openbindings/openbindings-go"
)

// Resolved identifies a delegate chosen for a task, with its resolved OBI.
type Resolved struct {
	Format   string       `json:"format"`
	Delegate string       `json:"delegate"`
	Location string       `json:"location,omitempty"` // how to reach the delegate
	OBI      *ResolvedOBI `json:"-"`                  // delegate's OBI, resolved and pin-verified
}

// ResolvedOBI holds a delegate's OBI obtained during resolution.
type ResolvedOBI struct {
	Interface openbindings.Interface
}

// SupportsFormat checks if a delegate's format token supports a requested format.
// This handles version matching (e.g., "usage@^2.0.0" supports "usage@2.1.0").
func SupportsFormat(delegateFormat, requestedFormat string) bool {
	return supportsFormatToken(delegateFormat, requestedFormat)
}
