package app

import (
	"encoding/json"
	"fmt"
)

// SelfDelegateLocation is the self-delegate's registration identity: the
// opaque location ob resolves to itself, in-process. It is the same "ob"
// identifier recorded in x-ob.delegate.
const SelfDelegateLocation = "ob"

// DelegateRecord is one registered delegate as persisted in the environment
// config. It holds two kinds of data with two owners:
//
//   - the snapshot (Name, Operations, ContentHash, Capabilities, Formats) is
//     the delegate's data, taken when its location was last resolved and
//     replaced in full whenever it is resolved again;
//   - the preferences are the registrar's data, and no refresh touches them.
type DelegateRecord struct {
	Location string `json:"location"`

	// Snapshot — replaced on (re-)registration.
	Name         string                    `json:"name,omitempty"`
	Operations   []string                  `json:"operations"`
	ContentHash  string                    `json:"contentHash,omitempty"`
	Capabilities []DelegateCapability      `json:"capabilities,omitempty"`
	BindingSpecs []DelegateBindingSpecInfo `json:"bindingSpecs,omitempty"`

	// Preferences — the registrar's, survive refresh. Preference is the
	// delegate-level default (absent = the baseline 0); OperationPreferences
	// overrides it per operation identifier; BindingSpecPreferences is ob's extra
	// axis, overriding an operation entry for one binding-source format.
	Preference             *float64                `json:"preference,omitempty"`
	OperationPreferences   map[string]float64      `json:"operationPreferences,omitempty"`
	BindingSpecPreferences []BindingSpecPreference `json:"bindingSpecPreferences,omitempty"`
}

// UnmarshalJSON refuses registry records written under the retired
// format vocabulary loudly — never silent non-matching: a record whose
// snapshot keys predate the binding-spec rename would otherwise load with
// empty claims and quietly stop matching.
func (r *DelegateRecord) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for _, retired := range []string{"formats", "formatPreferences"} {
		if _, has := raw[retired]; has {
			return fmt.Errorf("delegate registry record %s uses the retired %q vocabulary; re-register the delegate (`ob delegate register <location>`) to refresh its snapshot", string(raw["location"]), retired)
		}
	}
	type plain DelegateRecord
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*r = DelegateRecord(p)
	return nil
}

// DelegateBindingSpecInfo is one binding specification a delegate reported handling.
type DelegateBindingSpecInfo struct {
	BindingSpec string `json:"bindingSpec"`
	Description string `json:"description,omitempty"`
}

// BindingSpecPreference is a preference override scoped to (operation,
// binding specification) — ob's granularity beyond the delegate-manager
// contract's per-operation index.
type BindingSpecPreference struct {
	Operation   string  `json:"operation"`
	BindingSpec string  `json:"bindingSpec"`
	Preference  float64 `json:"preference"`
}

// capabilityOperation maps each of ob's format capabilities to the published
// operation it delegates. Capability-scoped ergonomics (the --capability flag)
// are sugar for these operation identifiers.
var capabilityOperation = map[DelegateCapability]string{
	CapInvoke:     "openbindings.binding-invoker.invokeBinding",
	CapSynthesize: "openbindings.interface-synthesizer.synthesizeInterface",
	CapInspect:    "openbindings.source-inspector.inspectSource",
}

// CapabilityOperation returns the published operation an ob capability
// delegates — the target of capability-scoped ergonomics like --capability.
func CapabilityOperation(cap DelegateCapability) (string, bool) {
	op, ok := capabilityOperation[cap]
	return op, ok
}

// effectiveOperationPreference is the preference to rank this delegate by when
// resolving an operation: its per-operation entry when set, else its
// delegate-level value, else the baseline 0.
func (r *DelegateRecord) effectiveOperationPreference(operation string) float64 {
	if p, ok := r.OperationPreferences[operation]; ok {
		return p
	}
	if r.Preference != nil {
		return *r.Preference
	}
	return 0
}

// DelegateContext is the delegate registry loaded from the environment config.
type DelegateContext struct {
	Delegates []DelegateRecord
}

// getDelegateContextFunc is the indirection point for tests so they can
// substitute a fixed registry without depending on whatever .openbindings/
// config exists in the developer's home directory or the global fallback.
var getDelegateContextFunc = defaultGetDelegateContext

// GetDelegateContext loads the environment config and extracts the delegate
// registry. Returns an empty registry if no environment exists.
func GetDelegateContext() DelegateContext {
	return getDelegateContextFunc()
}

func defaultGetDelegateContext() DelegateContext {
	envPath, err := FindEnvPath()
	if err != nil {
		return DelegateContext{}
	}
	config, err := LoadEnvConfig(envPath)
	if err != nil {
		return DelegateContext{}
	}
	return DelegateContext{Delegates: config.Delegates}
}

// findDelegateRecord returns the index of the record registered under
// location, or -1.
func findDelegateRecord(config *EnvConfig, location string) int {
	for i := range config.Delegates {
		if config.Delegates[i].Location == location {
			return i
		}
	}
	return -1
}
