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

	// Compare contract operations against the target via the v1 comparison
	// engine — the same pairing (OBI-T-12 key+alias resolution) and schema
	// verdicts `ob compat` reports, so conform and compat can never disagree.
	// The contract rides the left side (what must be satisfied), the target
	// OBI the right.
	deltas := compareOperationDeltas(
		resolvedComparisonInput{iface: contractIface},
		resolvedComparisonInput{iface: targetIface},
		"subsume",
	)
	deltaByContractOp := map[string]OperationDelta{}
	for _, d := range deltas {
		if d.Left != nil {
			deltaByContractOp[d.Left.Key] = d
		}
	}

	modified := false

	// Sort contract operation keys for deterministic output.
	contractOpKeys := make([]string, 0, len(contractIface.Operations))
	for k := range contractIface.Operations {
		contractOpKeys = append(contractOpKeys, k)
	}
	sort.Strings(contractOpKeys)

	for _, opName := range contractOpKeys {
		contractOp := contractIface.Operations[opName]

		delta, ok := deltaByContractOp[opName]
		if !ok || delta.Status != "paired" {
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

		if !deltaNeedsWork(delta) {
			// Already conformant.
			output.Actions = append(output.Actions, ConformAction{
				Operation: opName,
				Action:    "compatible",
			})
			continue
		}

		// Paired but not affirmatively compatible — offer to replace.
		details := deltaDetails(delta)
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
			// The engine's pairing names the target key that satisfies this
			// contract operation (it differs when matched via alias).
			replaceOperationSchemas(targetIface, delta.Right.Key, opName, contractOp, contractIface)
			modified = true
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
	// The conformed document always rides the report: ConformResult.interface
	// is contract-required, so both lanes carry it (the served operation for
	// its caller, the CLI's -F json machine lane for the exec binding). The
	// CLI additionally persists it to TargetPath above.
	output.Result = targetIface

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

// --- conform's compatibility gate over the v1 comparison engine ---

// slotNeedsWork reports whether a slot verdict fails to affirm compatibility.
// incompatible is real drift; unverified (e.g. regex containment, external
// $ref) and indeterminate (comparison impossible) also count — conform
// reporting "compatible" on a slot it could not verify would be a silent lie.
func slotNeedsWork(sc *SchemaCompatibility) bool {
	return sc != nil && verdictRank(sc.Verdict) >= verdictRank("unverified")
}

// deltaNeedsWork reports whether a paired operation needs a schema replace.
func deltaNeedsWork(d OperationDelta) bool {
	return slotNeedsWork(d.Input) || slotNeedsWork(d.Output)
}

// deltaDetails renders the not-affirmed slots of a paired delta for the
// replace prompt and action details, e.g.
// "input incompatible: schema.required.added /operations/set/input/required".
func deltaDetails(d OperationDelta) string {
	var parts []string
	for _, sc := range []*SchemaCompatibility{d.Input, d.Output} {
		if !slotNeedsWork(sc) {
			continue
		}
		detail := sc.Direction + " " + sc.Verdict
		var reasons []string
		for _, r := range sc.Reasons {
			reasons = append(reasons, r.Kind+" "+r.Pointer)
		}
		if len(reasons) > 0 {
			detail += ": " + strings.Join(reasons, ", ")
		}
		parts = append(parts, detail)
	}
	return strings.Join(parts, "; ")
}
