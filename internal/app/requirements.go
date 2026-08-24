package app

import (
	"embed"
	"encoding/json"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
)

// requirementsFS holds vendored copies of the published interfaces from which
// ob derives its delegate capability requirements. They are pinned to this ob
// release. A capability requirement selects only the operations ob actually
// consumes; correspondence is per-operation, so an invoke-only delegate is not
// forced to implement prepareBinding and a strict synthesizer is not forced to
// implement the independent coverage operation.
//
//go:embed requirements/*.json
var requirementsFS embed.FS

// DelegateCapability is one format-handling capability a delegate can provide.
// Each maps to the published interface the delegate must satisfy.
type DelegateCapability string

const (
	CapInvoke     DelegateCapability = "invoke"     // openbindings.binding-invoker
	CapSynthesize DelegateCapability = "synthesize" // openbindings.interface-synthesizer
	CapInspect    DelegateCapability = "inspect"    // openbindings.source-inspector
)

// DelegateCapabilities is the ordered set of delegatable capabilities.
var DelegateCapabilities = []DelegateCapability{CapInvoke, CapSynthesize, CapInspect}

// requirementFiles maps each capability to its embedded requirement interface.
var requirementFiles = map[DelegateCapability]string{
	CapInvoke:     "requirements/binding-invoker.json",
	CapSynthesize: "requirements/interface-synthesizer.json",
	CapInspect:    "requirements/source-inspector.json",
}

// capabilityRequirementOperations is the minimum published operation set ob
// uses for each capability. listBindingSpecs remains the advisory display
// surface; checkBindingSpecs is the authoritative selection warrant.
var capabilityRequirementOperations = map[DelegateCapability][]string{
	CapInvoke: {
		"openbindings.binding-invoker.listBindingSpecs",
		"openbindings.binding-invoker.checkBindingSpecs",
		"openbindings.binding-invoker.invokeBinding",
	},
	CapSynthesize: {
		"openbindings.interface-synthesizer.listBindingSpecs",
		"openbindings.interface-synthesizer.checkBindingSpecs",
		"openbindings.interface-synthesizer.synthesizeInterface",
	},
	CapInspect: {
		"openbindings.source-inspector.listBindingSpecs",
		"openbindings.interface-synthesizer.checkBindingSpecs",
		"openbindings.source-inspector.inspectSource",
	},
}

func publishedRequirementInterface(cap DelegateCapability) (*openbindings.Interface, error) {
	name, ok := requirementFiles[cap]
	if !ok {
		return nil, fmt.Errorf("unknown delegate capability %q", cap)
	}
	data, err := requirementsFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var iface openbindings.Interface
	if err := json.Unmarshal(data, &iface); err != nil {
		return nil, fmt.Errorf("parse published interface for %q: %w", cap, err)
	}
	return &iface, nil
}

// RequirementInterface returns the minimal interface a delegate must satisfy
// for the given ob capability. Its operations and schemas are copied from the
// canonical published interface; only unrelated operations are omitted.
func RequirementInterface(cap DelegateCapability) (*openbindings.Interface, error) {
	iface, err := publishedRequirementInterface(cap)
	if err != nil {
		return nil, err
	}
	// Source-inspector has no support-query operation of its own. ob's inspect
	// routing therefore consumes the interface-synthesizer query alongside the
	// source-inspector operation; reference authoring implementations expose
	// both from the same exact supported set.
	if cap == CapInspect {
		const checkKey = "openbindings.interface-synthesizer.checkBindingSpecs"
		synth, synthErr := publishedRequirementInterface(CapSynthesize)
		if synthErr != nil {
			return nil, synthErr
		}
		iface.Operations[checkKey] = synth.Operations[checkKey]
		iface.Schemas["BindingSpecVerdict"] = synth.Schemas["BindingSpecVerdict"]
	}
	required, ok := capabilityRequirementOperations[cap]
	if !ok {
		return nil, fmt.Errorf("unknown delegate capability %q", cap)
	}
	operations := make(map[string]openbindings.Operation, len(required))
	for _, key := range required {
		op, exists := iface.Operations[key]
		if !exists {
			return nil, fmt.Errorf("published interface for %q is missing required operation %q", cap, key)
		}
		operations[key] = op
	}
	iface.Operations = operations
	iface.Name += " — ob " + string(cap) + " requirement"
	iface.Description = "The operation subset of the published interface that ob consumes for its " + string(cap) + " delegate capability. Correspondence is evaluated per operation; operations outside this subset are independent capabilities."
	return iface, nil
}

// RequirementInterfaceJSON returns the derived capability requirement for
// printing or serving. It is deterministic and contains the canonical
// operation/schema definitions from the vendored published interface.
func RequirementInterfaceJSON(cap DelegateCapability) ([]byte, error) {
	iface, err := RequirementInterface(cap)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(iface, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode requirement interface for %q: %w", cap, err)
	}
	return append(data, '\n'), nil
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
// the requirement interface declares (each paired and affirmatively
// compatible). This runs the v1 comparison engine — the same pairing
// (OBI-T-12 key+alias resolution) and schema verdicts `ob compat` reports.
func satisfiesInterface(candidate, requirement *openbindings.Interface) bool {
	deltas := compareOperationDeltas(
		resolvedComparisonInput{iface: requirement},
		resolvedComparisonInput{iface: candidate},
		"subsume",
	)
	satisfied := false
	for _, d := range deltas {
		if d.Left == nil {
			continue // only_right: candidate operations beyond the requirement are fine
		}
		if d.Status != "paired" || deltaNeedsWork(d) {
			return false
		}
		satisfied = true
	}
	return satisfied
}
