package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/schemaprofile"
)

// ConformInput specifies the interface to satisfy and the target OBI to
// update.
type ConformInput struct {
	// InterfaceLocator is the file path or URL of the interface whose
	// operations the target should satisfy.
	InterfaceLocator string
	// TargetPath is the file path of the target OBI to update.
	TargetPath string
	// ContractInterface/TargetInterface are inline documents (the served
	// operation); they take precedence over the locator/path. When TargetPath is
	// empty, no file is written and the conformed document is returned in
	// ConformOutput.Result instead.
	ContractInterface *openbindings.Interface
	TargetInterface   *openbindings.Interface
	// Yes auto-accepts all scaffolding and replacements.
	Yes bool
	// DryRun shows what would change without modifying the file.
	DryRun bool
}

// ConformAction describes a single change to be made (or that was made).
type ConformAction struct {
	Operation string `json:"operation"`
	Action    string `json:"action"` // "scaffold", "replace", "skip", "compatible"
	Details   string `json:"details,omitempty"`
}

// ConformOutput is the result of a conform operation. On the wire it realizes
// the contract's ConformResult: the conformed document rides the `interface`
// key (set when the target came in as an inline document); the locator/path
// fields are CLI-lane context, omitted on the served operation.
type ConformOutput struct {
	InterfaceName    string                  `json:"interfaceName,omitempty"`
	InterfaceLocator string                  `json:"interfaceLocator,omitempty"`
	TargetPath       string                  `json:"targetPath,omitempty"`
	Actions          []ConformAction         `json:"actions"`
	Modified         bool                    `json:"modified"`
	Result           *openbindings.Interface `json:"interface,omitempty"`
	Error            *Error                  `json:"error,omitempty"`
}

// Render returns a human-friendly representation.
func (o ConformOutput) Render() string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Conform Report"))
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  interface: "))
	sb.WriteString(o.InterfaceName)
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  target:    "))
	sb.WriteString(o.TargetPath)
	sb.WriteString("\n\n")

	if o.Error != nil {
		sb.WriteString(s.Error.Render("  ✗ Error: "))
		sb.WriteString(o.Error.Message)
		return sb.String()
	}

	for _, a := range o.Actions {
		switch a.Action {
		case "scaffold":
			sb.WriteString(s.Success.Render("  + "))
			sb.WriteString(s.Key.Render(a.Operation))
			sb.WriteString(s.Dim.Render(" — scaffolded"))
		case "replace":
			sb.WriteString(s.Warning.Render("  ~ "))
			sb.WriteString(s.Key.Render(a.Operation))
			sb.WriteString(s.Dim.Render(" — replaced"))
			if a.Details != "" {
				sb.WriteString(s.Dim.Render(fmt.Sprintf(" (%s)", a.Details)))
			}
		case "compatible":
			sb.WriteString(s.Success.Render("  ✓ "))
			sb.WriteString(s.Key.Render(a.Operation))
			sb.WriteString(s.Dim.Render(" — in sync"))
		case "skip":
			sb.WriteString(s.Dim.Render("  - "))
			sb.WriteString(s.Key.Render(a.Operation))
			sb.WriteString(s.Dim.Render(" — skipped"))
			if a.Details != "" {
				sb.WriteString(s.Dim.Render(fmt.Sprintf(" (%s)", a.Details)))
			}
		}
		sb.WriteString("\n")
	}

	if o.Modified {
		sb.WriteString("\n")
		sb.WriteString(s.Success.Render(fmt.Sprintf("  Wrote %s", o.TargetPath)))
	} else {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  No changes needed"))
	}

	return sb.String()
}

// Conform loads the interface to satisfy, compares it against a target OBI,
// scaffolds missing operations, and optionally replaces drifted ones.
func Conform(input ConformInput, confirm func(op string, action string) bool) ConformOutput {
	output := ConformOutput{
		InterfaceLocator: input.InterfaceLocator,
		TargetPath:       input.TargetPath,
		Actions:          []ConformAction{},
	}

	// Load the interface to satisfy (inline document wins over the locator).
	contractIface := input.ContractInterface
	if contractIface == nil {
		var err error
		contractIface, err = resolveInterface(input.InterfaceLocator)
		if err != nil {
			output.Error = &Error{Code: "resolve_error", Message: fmt.Sprintf("interface: %v", err)}
			return output
		}
	}

	// Load the target OBI (inline document wins over the path).
	targetIface := input.TargetInterface
	if targetIface == nil {
		var err error
		targetIface, err = loadInterfaceFile(input.TargetPath)
		if err != nil {
			output.Error = &Error{Code: "resolve_error", Message: fmt.Sprintf("target: %v", err)}
			return output
		}
	}

	// Display label for the contract being conformed to.
	if contractIface.Name != "" {
		output.InterfaceName = contractIface.Name
	} else {
		output.InterfaceName = input.InterfaceLocator
	}

	// Ensure operations map exists.
	if targetIface.Operations == nil {
		targetIface.Operations = make(map[string]openbindings.Operation)
	}

	// Compare contract operations against the target. Correspondence is by the
	// spec's key+alias resolution (OBI-T-12): the contract is the "target" in
	// compat terms (what we need to fulfill), our OBI is the "candidate".
	reports := compareOps(contractIface, targetIface)

	modified := false

	// Sort contract operation keys for deterministic output.
	contractOpKeys := make([]string, 0, len(contractIface.Operations))
	for k := range contractIface.Operations {
		contractOpKeys = append(contractOpKeys, k)
	}
	sort.Strings(contractOpKeys)

	for _, opName := range contractOpKeys {
		contractOp := contractIface.Operations[opName]

		// Find the matching report.
		var report *OperationReport
		for i := range reports {
			if reports[i].Operation == opName {
				report = &reports[i]
				break
			}
		}

		if report == nil || !report.Matched {
			// Operation not found in target — scaffold it.
			shouldScaffold := input.Yes || confirm(opName, "scaffold")
			if !shouldScaffold {
				output.Actions = append(output.Actions, ConformAction{
					Operation: opName,
					Action:    "skip",
					Details:   "not scaffolded",
				})
				continue
			}

			if !input.DryRun {
				scaffoldOperation(targetIface, opName, contractOp, contractIface)
				modified = true
			}
			output.Actions = append(output.Actions, ConformAction{
				Operation: opName,
				Action:    "scaffold",
			})
			continue
		}

		if report.Compatible {
			// Already conformant.
			output.Actions = append(output.Actions, ConformAction{
				Operation: opName,
				Action:    "compatible",
			})
			continue
		}

		// Matched but incompatible — offer to replace.
		details := strings.Join(report.Details, "; ")
		shouldReplace := input.Yes || confirm(opName, fmt.Sprintf("replace (%s)", details))
		if !shouldReplace {
			output.Actions = append(output.Actions, ConformAction{
				Operation: opName,
				Action:    "skip",
				Details:   details,
			})
			continue
		}

		if !input.DryRun {
			// Find the actual operation key in the target (might differ via alias matching).
			targetOpKey := findTargetOpKey(opName, contractOp, targetIface)
			if targetOpKey != "" {
				replaceOperationSchemas(targetIface, targetOpKey, opName, contractOp, contractIface)
				modified = true
			}
		}
		output.Actions = append(output.Actions, ConformAction{
			Operation: opName,
			Action:    "replace",
			Details:   details,
		})
	}

	// Persist or return the conformed OBI. The CLI writes to TargetPath; the
	// served operation has no path, so the conformed document is returned in
	// Result for the caller (e.g. an agent) to use.
	if modified && !input.DryRun {
		if input.TargetPath != "" {
			if err := WriteInterfaceFile(input.TargetPath, targetIface); err != nil {
				output.Error = &Error{Code: "write_error", Message: err.Error()}
				return output
			}
		}
		output.Modified = true
	}
	if input.TargetInterface != nil {
		output.Result = targetIface
	}

	return output
}

// scaffoldOperation adds a new operation to the target OBI, keyed by the
// contract's operation name (so it resolves to the contract by key) and
// copying the contract's schemas.
func scaffoldOperation(target *openbindings.Interface, opName string, contractOp openbindings.Operation, contractIface *openbindings.Interface) {
	newOp := openbindings.Operation{
		Description: contractOp.Description,
		Idempotent:  contractOp.Idempotent,
	}

	// Copy input schema, resolving $refs from the contract into the target.
	if contractOp.Input != nil {
		newOp.Input = copySchema(contractOp.Input, contractIface, target)
	}

	// Copy output schema.
	if contractOp.Output != nil {
		newOp.Output = copySchema(contractOp.Output, contractIface, target)
	}

	target.Operations[opName] = newOp
}

// replaceOperationSchemas updates an existing operation's input/output schemas
// to match the contract. When the target operation's key differs from the
// contract operation name, it carries that name as an alias so the
// correspondence is declared per the spec (key+alias namespace, OBI-T-12).
func replaceOperationSchemas(target *openbindings.Interface, opKey string, contractOpName string, contractOp openbindings.Operation, contractIface *openbindings.Interface) {
	op := target.Operations[opKey]

	// Replace schemas.
	if contractOp.Input != nil {
		op.Input = copySchema(contractOp.Input, contractIface, target)
	} else {
		op.Input = nil
	}
	if contractOp.Output != nil {
		op.Output = copySchema(contractOp.Output, contractIface, target)
	} else {
		op.Output = nil
	}

	// Declare correspondence via an alias when the key doesn't already match.
	if opKey != contractOpName {
		hasAlias := false
		for _, a := range op.Aliases {
			if a == contractOpName {
				hasAlias = true
				break
			}
		}
		if !hasAlias {
			op.Aliases = append(op.Aliases, contractOpName)
		}
	}

	target.Operations[opKey] = op
}

// findTargetOpKey finds the key in the target's operations map that
// corresponds to the given contract operation, by the spec's key+alias
// resolution (OBI-T-12).
func findTargetOpKey(contractOpName string, contractOp openbindings.Operation, target *openbindings.Interface) string {
	// Direct key match.
	if _, ok := target.Operations[contractOpName]; ok {
		return contractOpName
	}

	// The contract operation's aliases against target keys.
	for _, alias := range contractOp.Aliases {
		if _, ok := target.Operations[alias]; ok {
			return alias
		}
	}

	// Target operations carrying the contract name as an alias.
	for k, op := range target.Operations {
		for _, alias := range op.Aliases {
			if alias == contractOpName {
				return k
			}
		}
	}

	return contractOpName // fallback
}

// copySchema copies a JSON Schema from the contract interface to the target,
// including any $ref'd schemas from the contract's schemas pool.
func copySchema(schema openbindings.JSONSchema, contractIface, target *openbindings.Interface) openbindings.JSONSchema {
	if schema == nil {
		return nil
	}

	// If the schema is a $ref to a contract schema, copy the referenced schema
	// into the target's schemas pool and return the same $ref.
	if ref, ok := schema["$ref"].(string); ok {
		if strings.HasPrefix(ref, "#/schemas/") {
			schemaName := strings.TrimPrefix(ref, "#/schemas/")
			if contractSchema, ok := contractIface.Schemas[schemaName]; ok {
				if target.Schemas == nil {
					target.Schemas = make(map[string]openbindings.JSONSchema)
				}
				if _, exists := target.Schemas[schemaName]; !exists {
					// Deep copy the schema.
					target.Schemas[schemaName] = deepCopySchema(contractSchema)
					// Recursively copy any nested $refs.
					copyNestedRefs(target.Schemas[schemaName], contractIface, target)
				}
			}
		}
		return openbindings.JSONSchema{"$ref": ref}
	}

	// For inline schemas, deep copy and handle nested $refs.
	copied := deepCopySchema(schema)
	copyNestedRefs(copied, contractIface, target)
	return copied
}

// deepCopySchema creates a deep copy of a JSON Schema via JSON round-trip.
func deepCopySchema(schema openbindings.JSONSchema) openbindings.JSONSchema {
	b, err := json.Marshal(schema)
	if err != nil {
		return schema
	}
	var copy openbindings.JSONSchema
	if err := json.Unmarshal(b, &copy); err != nil {
		return schema
	}
	return copy
}

// copyNestedRefs walks a schema and copies any $ref'd schemas from the contract.
func copyNestedRefs(schema openbindings.JSONSchema, contractIface, target *openbindings.Interface) {
	for _, v := range schema {
		switch val := v.(type) {
		case map[string]any:
			if ref, ok := val["$ref"].(string); ok && strings.HasPrefix(ref, "#/schemas/") {
				schemaName := strings.TrimPrefix(ref, "#/schemas/")
				if contractSchema, ok := contractIface.Schemas[schemaName]; ok {
					if target.Schemas == nil {
						target.Schemas = make(map[string]openbindings.JSONSchema)
					}
					if _, exists := target.Schemas[schemaName]; !exists {
						target.Schemas[schemaName] = deepCopySchema(contractSchema)
						copyNestedRefs(target.Schemas[schemaName], contractIface, target)
					}
				}
			}
			copyNestedRefs(val, contractIface, target)
		case []any:
			for _, item := range val {
				if m, ok := item.(map[string]any); ok {
					copyNestedRefs(m, contractIface, target)
				}
			}
		}
	}
}

// --- operation-level compatibility engine ---
//
// conform decides scaffold/replace/compatible per contract operation by
// checking the target's matched operation slot-by-slot. This engine implements
// the spec's slot semantics (compatible / incompatible / unspecified) over
// normalized schemas; the full report convention lives in comparison.go.

// SlotStatus represents the compatibility status of a single schema slot
// per the spec: compatible, incompatible, or unspecified.
type SlotStatus string

const (
	SlotCompatible   SlotStatus = "compatible"
	SlotIncompatible SlotStatus = "incompatible"
	SlotUnspecified  SlotStatus = "unspecified"
)

// OperationReport reports compatibility for a single operation, including
// per-slot status as required by the spec.
type OperationReport struct {
	Operation string `json:"operation"`

	// Matched is true if a matching operation exists in the candidate.
	Matched bool `json:"matched"`

	// Per-slot status: compatible, incompatible, or unspecified.
	Input  SlotStatus `json:"input,omitempty"`
	Output SlotStatus `json:"output,omitempty"`

	// Details provides human-readable context for incompatible or error slots.
	// Distinguishes schema incompatibility from normalization/analysis failures.
	Details []string `json:"details,omitempty"`

	// Compatible is true when the operation fully passes all applicable checks.
	Compatible bool `json:"compatible"`
}

// compareOps checks each target operation against the candidate per the spec.
func compareOps(target, candidate *openbindings.Interface) []OperationReport {
	// Sort operation keys for deterministic output.
	opKeys := make([]string, 0, len(target.Operations))
	for k := range target.Operations {
		opKeys = append(opKeys, k)
	}
	sort.Strings(opKeys)

	tgtRoot := buildNormalizerRoot(target)
	candRoot := buildNormalizerRoot(candidate)

	var reports []OperationReport
	for _, opName := range opKeys {
		tgtOp := target.Operations[opName]

		// Operation matching per spec (OBI-T-12): key/alias resolution.
		candOp, matched := matchOperation(opName, tgtOp, candidate)

		if !matched {
			reports = append(reports, OperationReport{
				Operation:  opName,
				Matched:    false,
				Compatible: false,
			})
			continue
		}

		report := buildOperationReport(opName, tgtOp, candOp, tgtRoot, candRoot)
		reports = append(reports, report)
	}

	return reports
}

// matchOperation finds the candidate operation corresponding to a target
// operation by the spec's key+alias resolution (OBI-T-12): the key and aliases
// form one flat namespace, and a name matches if it equals the candidate's key
// or appears in its aliases (in either direction). Correspondence to a shared
// contract is declared purely by aliases (spec OBI-T-12).
func matchOperation(name string, tgtOp openbindings.Operation, candidate *openbindings.Interface) (openbindings.Operation, bool) {
	// Direct key match.
	if op, ok := candidate.Operations[name]; ok {
		return op, true
	}

	// Check target aliases against candidate keys.
	for _, alias := range tgtOp.Aliases {
		if op, ok := candidate.Operations[alias]; ok {
			return op, true
		}
	}

	// Check candidate aliases against target key.
	for _, candOp := range candidate.Operations {
		for _, alias := range candOp.Aliases {
			if alias == name {
				return candOp, true
			}
		}
	}

	return openbindings.Operation{}, false
}

// buildOperationReport compares schemas for a matched operation pair.
func buildOperationReport(
	name string,
	tgtOp, candOp openbindings.Operation,
	tgtRoot, candRoot map[string]any,
) OperationReport {
	report := OperationReport{
		Operation: name,
		Matched:   true,
	}

	tgtNorm := &schemaprofile.Normalizer{Root: tgtRoot}
	candNorm := &schemaprofile.Normalizer{Root: candRoot}

	var details []string

	var detail string
	report.Input, detail = slotCompat("input", tgtOp.Input, candOp.Input, tgtNorm, candNorm, true)
	if detail != "" {
		details = append(details, detail)
	}
	report.Output, detail = slotCompat("output", tgtOp.Output, candOp.Output, tgtNorm, candNorm, false)
	if detail != "" {
		details = append(details, detail)
	}

	report.Details = details

	report.Compatible =
		report.Input != SlotIncompatible &&
			report.Output != SlotIncompatible

	return report
}

// slotCompat evaluates a single schema slot (input, output, or payload).
// Returns the status and, for incompatible slots, a human-readable detail
// that distinguishes normalization failures from genuine schema mismatches.
func slotCompat(
	slotName string,
	tgtSchema, candSchema map[string]any,
	tgtNorm, candNorm *schemaprofile.Normalizer,
	isInput bool,
) (SlotStatus, string) {
	// Unspecified if either side omits the schema.
	if tgtSchema == nil || candSchema == nil {
		return SlotUnspecified, ""
	}

	// Normalize both schemas (resolves $ref, strips annotations, flattens allOf).
	tgtNormalized, err := tgtNorm.Normalize(tgtSchema)
	if err != nil {
		return SlotIncompatible, fmt.Sprintf("%s: target schema could not be normalized: %v", slotName, err)
	}
	candNormalized, err := candNorm.Normalize(candSchema)
	if err != nil {
		return SlotIncompatible, fmt.Sprintf("%s: candidate schema could not be normalized: %v", slotName, err)
	}

	// Compare with a fresh normalizer (schemas are already normalized,
	// $refs resolved — empty root is fine).
	n := &schemaprofile.Normalizer{Root: map[string]any{}}
	var ok bool
	var reason string
	if isInput {
		ok, reason, err = n.InputCompatible(tgtNormalized, candNormalized)
	} else {
		ok, reason, err = n.OutputCompatible(tgtNormalized, candNormalized)
	}
	if err != nil {
		return SlotIncompatible, fmt.Sprintf("%s: compatibility check error: %v", slotName, err)
	}
	if !ok {
		if reason != "" {
			return SlotIncompatible, fmt.Sprintf("%s: incompatible: %s", slotName, reason)
		}
		return SlotIncompatible, fmt.Sprintf("%s: incompatible", slotName)
	}
	return SlotCompatible, ""
}
