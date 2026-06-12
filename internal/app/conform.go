package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// ConformInput specifies the interface to satisfy and the target OBI to
// update.
type ConformInput struct {
	// InterfaceLocator is the file path or URL of the interface whose
	// operations the target should satisfy.
	InterfaceLocator string
	// TargetPath is the file path of the target OBI to update.
	TargetPath string
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

// ConformOutput is the result of a conform operation.
type ConformOutput struct {
	Interface        string          `json:"interface"`
	InterfaceLocator string          `json:"interfaceLocator"`
	TargetPath       string          `json:"targetPath"`
	Actions          []ConformAction `json:"actions"`
	Modified         bool            `json:"modified"`
	Error            *Error          `json:"error,omitempty"`
}

// Render returns a human-friendly representation.
func (o ConformOutput) Render() string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Conform Report"))
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  interface: "))
	sb.WriteString(o.Interface)
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
	}

	// Load the interface to satisfy.
	contractIface, err := resolveInterface(input.InterfaceLocator)
	if err != nil {
		output.Error = &Error{Code: "resolve_error", Message: fmt.Sprintf("interface: %v", err)}
		return output
	}

	// Load the target OBI.
	targetIface, err := loadInterfaceFile(input.TargetPath)
	if err != nil {
		output.Error = &Error{Code: "resolve_error", Message: fmt.Sprintf("target: %v", err)}
		return output
	}

	// Display label for the contract being conformed to.
	if contractIface.Name != "" {
		output.Interface = contractIface.Name
	} else {
		output.Interface = input.InterfaceLocator
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

	// Write the updated OBI if modified.
	if modified && !input.DryRun {
		if err := WriteInterfaceFile(input.TargetPath, targetIface); err != nil {
			output.Error = &Error{Code: "write_error", Message: err.Error()}
			return output
		}
		output.Modified = true
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
