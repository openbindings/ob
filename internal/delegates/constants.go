// Package delegates - constants.go centralizes delegate-related constants.
package delegates

import (
	"time"

	"github.com/openbindings/openbindings-go/httpdiscovery"
)

// URL schemes and prefixes.
const (
	// ExecScheme is the prefix for executable command references.
	ExecScheme = "exec:"

	// HTTPScheme is the HTTP URL prefix.
	HTTPScheme = "http://"

	// HTTPSScheme is the HTTPS URL prefix.
	HTTPSScheme = "https://"
)

// WellKnownPath re-exports the HTTP Discovery path for backward compatibility.
const WellKnownPath = httpdiscovery.WellKnownPath

// Standard operation names from the OpenBindings binding invoker interface.
const (
	// OpListBindingSpecs is the binding-invoker contract's listBindingSpecs
	// operation key; a delegate's spec-listing operation corresponds to it
	// by key or alias (OBI-T-12).
	OpListBindingSpecs = "openbindings.binding-invoker.listBindingSpecs"
)

// Timeouts for network and probe operations.
const (
	// DefaultProbeTimeout is the default timeout for probing delegates.
	DefaultProbeTimeout = 2 * time.Second
)
