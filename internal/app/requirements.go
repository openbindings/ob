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
// forced to implement preflightBinding and a strict synthesizer is not forced to
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

// capabilityOperation names each role's primary consumed operation: the
// workload OB dispatches after a positive support verdict. Legacy per-operation
// preferences map onto a role only through this table.
var capabilityOperation = map[DelegateCapability]string{
	CapInvoke:     "openbindings.binding-invoker.invokeBinding",
	CapSynthesize: "openbindings.interface-synthesizer.synthesizeInterface",
	CapInspect:    "openbindings.source-inspector.inspectSource",
}

// DelegateRoleRequirementJSON is the native convenience behind
// `ob delegate requirements <role>` and GET /delegate-requirements/{role}: the
// one accepted alternative of an advertised role, pretty-printed. A role with
// several alternatives has no single requirement and is refused rather than
// silently projected to its first entry; `listRoles` is the complete surface.
func DelegateRoleRequirementJSON(role string) ([]byte, error) {
	catalogue, err := defaultRoleCatalogue()
	if err != nil {
		return nil, err
	}
	return roleRequirementJSON(catalogue, role)
}

func roleRequirementJSON(catalogue *roleCatalogue, role string) ([]byte, error) {
	alternatives, known := catalogue.alternatives[role]
	if !known {
		return nil, fmt.Errorf("unknown delegate role %q", role)
	}
	if len(alternatives) != 1 {
		return nil, fmt.Errorf("role %q accepts %d alternative interfaces; use 'ob delegate roles' for the complete catalogue", role, len(alternatives))
	}
	var value any
	if err := json.Unmarshal(alternatives[0].value, &value); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode requirement interface for %q: %w", role, err)
	}
	return append(data, '\n'), nil
}
