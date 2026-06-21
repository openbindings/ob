package app

import (
	"fmt"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// GenerateBoundCLI builds the bound CLI realization of ob's interface: the
// unbound contract's operations (keys, aliases, schemas — authoritative for
// operation identity) with usage-transport bindings attached by short-name from
// usage.kdl (authoritative for the CLI wire ref). This is what
// `ob --openbindings` emits; regenerating it from the contract keeps it
// conformant instead of drifting as a hand-maintained file.
//
// contractPath and usagePath are read for generation; usageFormat is the
// usage format token (e.g. "usage@2.13.1"); storedUsageLocation is the source
// location recorded in the output (relative to the output file's directory).
func GenerateBoundCLI(contractPath, usagePath, usageFormat, storedUsageLocation string) (*openbindings.Interface, error) {
	contract, err := loadInterfaceFile(contractPath)
	if err != nil {
		return nil, fmt.Errorf("load contract: %w", err)
	}

	// Derive the CLI's bindable targets from usage.kdl. Derived bindings are
	// keyed by the usage opKey, i.e. the operation short-name.
	derived, err := DeriveFromSource(openbindings.Source{Format: usageFormat, Location: usagePath}, "usage", "")
	if err != nil {
		return nil, fmt.Errorf("derive usage: %w", err)
	}
	refByShort := make(map[string]string, len(derived.Bindings))
	for _, b := range derived.Bindings {
		refByShort[b.Operation] = b.Ref
	}

	bound := &openbindings.Interface{
		OpenBindings: contract.OpenBindings,
		Name:         contract.Name,
		Version:      contract.Version,
		Description:  contract.Description,
		Schemas:      contract.Schemas,
		Operations:   make(map[string]openbindings.Operation, len(contract.Operations)),
		Sources: map[string]openbindings.Source{
			"usage": {Format: usageFormat, Location: storedUsageLocation},
		},
		Bindings: map[string]openbindings.BindingEntry{},
	}

	var unbound []string
	for key, op := range contract.Operations {
		bound.Operations[key] = op
		short := key[strings.LastIndex(key, ".")+1:]
		ref, ok := refByShort[short]
		if !ok {
			unbound = append(unbound, key)
			continue
		}
		bound.Bindings[key+".usage"] = openbindings.BindingEntry{
			Operation: key,
			Source:    "usage",
			Ref:       ref,
		}
	}
	// Operations with no CLI command (e.g. nothing in usage.kdl) stay unbound;
	// that's expected, not an error.
	_ = unbound

	return bound, nil
}
