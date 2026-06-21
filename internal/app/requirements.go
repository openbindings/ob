package app

import (
	"embed"
	"encoding/json"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
)

// requirementsFS holds vendored copies of the published interfaces a delegate
// must satisfy to provide a capability. They are pinned to this ob release and
// power two things: compat-based capability detection at registration, and the
// `ob delegate requirements` / serve exposure that lets a prospective delegate
// author check their software against the exact contract.
//
//go:embed requirements/*.json
var requirementsFS embed.FS

// DelegateCapability is one format-handling capability a delegate can provide.
// Each maps to the published interface the delegate must satisfy.
type DelegateCapability string

const (
	CapInvoke  DelegateCapability = "invoke"  // openbindings.binding-invoker
	CapSynthesize  DelegateCapability = "synthesize"  // openbindings.interface-synthesizer
	CapInspect DelegateCapability = "inspect" // openbindings.source-inspector
)

// DelegateCapabilities is the ordered set of delegatable capabilities.
var DelegateCapabilities = []DelegateCapability{CapInvoke, CapSynthesize, CapInspect}

// requirementFiles maps each capability to its embedded requirement interface.
var requirementFiles = map[DelegateCapability]string{
	CapInvoke:  "requirements/binding-invoker.json",
	CapSynthesize:  "requirements/interface-synthesizer.json",
	CapInspect: "requirements/source-inspector.json",
}

// RequirementInterfaceJSON returns the raw embedded JSON for a capability's
// requirement interface, for emitting or serving it verbatim.
func RequirementInterfaceJSON(cap DelegateCapability) ([]byte, error) {
	name, ok := requirementFiles[cap]
	if !ok {
		return nil, fmt.Errorf("unknown delegate capability %q", cap)
	}
	return requirementsFS.ReadFile(name)
}

// RequirementInterface returns the parsed interface a delegate must satisfy to
// provide the given capability.
func RequirementInterface(cap DelegateCapability) (*openbindings.Interface, error) {
	data, err := RequirementInterfaceJSON(cap)
	if err != nil {
		return nil, err
	}
	var iface openbindings.Interface
	if err := json.Unmarshal(data, &iface); err != nil {
		return nil, fmt.Errorf("parse requirement interface for %q: %w", cap, err)
	}
	return &iface, nil
}

// delegateCapabilities returns the capabilities a delegate OBI provides, decided
// by conformance (compat) against each requirement interface — not by operation
// name matching. A delegate provides whichever subset it satisfies.
func delegateCapabilities(delegate *openbindings.Interface) []DelegateCapability {
	var caps []DelegateCapability
	for _, cap := range DelegateCapabilities {
		req, err := RequirementInterface(cap)
		if err != nil {
			continue
		}
		if satisfiesInterface(delegate, req) {
			caps = append(caps, cap)
		}
	}
	return caps
}

// satisfiesInterface reports whether the candidate conforms to every operation
// the requirement interface declares (each matched and compatible). This is the
// same key+alias resolution and schema check `ob compat` uses (OBI-T-12).
func satisfiesInterface(candidate, requirement *openbindings.Interface) bool {
	reports := compareOps(requirement, candidate)
	if len(reports) == 0 {
		return false
	}
	for _, r := range reports {
		if !r.Matched || !r.Compatible {
			return false
		}
	}
	return true
}
