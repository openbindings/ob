package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// ConformInput specifies the role interface to conform to and the target OBI to update.
type ConformInput struct {
	// RoleLocator is the file path or URL of the role interface.
	RoleLocator string
	// RoleKey is the key to use in the target's roles map.
	// If empty, derived from the role interface's name.
	RoleKey string
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
	RoleKey    string          `json:"roleKey"`
	RoleLocator string         `json:"roleLocator"`
	TargetPath string          `json:"targetPath"`
	Actions    []ConformAction `json:"actions"`
	Modified   bool            `json:"modified"`
	Error      *Error          `json:"error,omitempty"`
}

// Render returns a human-friendly representation.
func (o ConformOutput) Render() string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Conform Report"))
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  role:   "))
	sb.WriteString(o.RoleKey)
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  target: "))
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

// ConformToRole loads a role interface, compares it against a target OBI,
// scaffolds missing operations, and optionally replaces drifted ones.
func ConformToRole(input ConformInput, confirm func(op string, action string) bool) ConformOutput {
	output := ConformOutput{
		RoleLocator: input.RoleLocator,
		TargetPath:  input.TargetPath,
	}

	// Load the role interface.
	roleIface, err := resolveInterface(input.RoleLocator)
	if err != nil {
		output.Error = &Error{Code: "resolve_error", Message: fmt.Sprintf("role: %v", err)}
		return output
	}

	// Load the target OBI.
	targetIface, err := loadInterfaceFile(input.TargetPath)
	if err != nil {
		output.Error = &Error{Code: "resolve_error", Message: fmt.Sprintf("target: %v", err)}
		return output
	}

	// Derive role key.
	roleKey := input.RoleKey
	if roleKey == "" {
		if roleIface.Name != "" {
			// Use lowercase, dot-separated name.
			roleKey = strings.ToLower(strings.ReplaceAll(roleIface.Name, " ", "."))
		} else {
			roleKey = "role"
		}
	}
	output.RoleKey = roleKey

	// Ensure roles map exists.
	if targetIface.Roles == nil {
		targetIface.Roles = make(map[string]string)
	}

	// Add or update the role reference.
	targetIface.Roles[roleKey] = input.RoleLocator

	// Ensure operations map exists.
	if targetIface.Operations == nil {
		targetIface.Operations = make(map[string]openbindings.Operation)
	}

	// Compare role operations against target.
	// The role is the "target" in compat terms (what we need to satisfy),
	// and our OBI is the "candidate".
	reports := compareOps(input.RoleLocator, roleIface, targetIface)

	modified := false

	// Sort role operation keys for deterministic output.
	roleOpKeys := make([]string, 0, len(roleIface.Operations))
	for k := range roleIface.Operations {
		roleOpKeys = append(roleOpKeys, k)
	}
	sort.Strings(roleOpKeys)

	for _, opName := range roleOpKeys {
		roleOp := roleIface.Operations[opName]

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
				scaffoldOperation(targetIface, opName, roleOp, roleKey, roleIface)
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
			// Find the actual operation key in the target (might differ via satisfies/alias matching).
			targetOpKey := findTargetOpKey(opName, roleOp, input.RoleLocator, targetIface)
			if targetOpKey != "" {
				replaceOperationSchemas(targetIface, targetOpKey, opName, roleOp, roleKey, roleIface)
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

// scaffoldOperation adds a new operation to the target OBI, copying schemas
// from the role interface and adding a satisfies declaration.
func scaffoldOperation(target *openbindings.Interface, opName string, roleOp openbindings.Operation, roleKey string, roleIface *openbindings.Interface) {
	newOp := openbindings.Operation{
		Description: roleOp.Description,
		Idempotent:  roleOp.Idempotent,
		Satisfies: []openbindings.Satisfies{
			{Role: roleKey, Operation: opName},
		},
	}

	// Copy input schema, resolving $refs from the role into the target.
	if roleOp.Input != nil {
		newOp.Input = copySchema(roleOp.Input, roleIface, target)
	}

	// Copy output schema.
	if roleOp.Output != nil {
		newOp.Output = copySchema(roleOp.Output, roleIface, target)
	}

	target.Operations[opName] = newOp
}

// replaceOperationSchemas updates an existing operation's input/output schemas
// to match the role interface, and ensures a satisfies declaration exists.
func replaceOperationSchemas(target *openbindings.Interface, opKey string, roleOpName string, roleOp openbindings.Operation, roleKey string, roleIface *openbindings.Interface) {
	op := target.Operations[opKey]

	// Replace schemas.
	if roleOp.Input != nil {
		op.Input = copySchema(roleOp.Input, roleIface, target)
	} else {
		op.Input = nil
	}
	if roleOp.Output != nil {
		op.Output = copySchema(roleOp.Output, roleIface, target)
	} else {
		op.Output = nil
	}

	// Ensure satisfies declaration exists for this role.
	hasSatisfies := false
	for _, sat := range op.Satisfies {
		if sat.Role == roleKey {
			hasSatisfies = true
			break
		}
	}
	if !hasSatisfies {
		op.Satisfies = append(op.Satisfies, openbindings.Satisfies{
			Role: roleKey, Operation: roleOpName,
		})
	}

	target.Operations[opKey] = op
}

// findTargetOpKey finds the key in the target's operations map that matches
// the given role operation (via satisfies, key, or alias matching).
func findTargetOpKey(roleOpName string, roleOp openbindings.Operation, roleLocator string, target *openbindings.Interface) string {
	// Direct key match.
	if _, ok := target.Operations[roleOpName]; ok {
		return roleOpName
	}

	// Check satisfies declarations.
	if target.Roles != nil {
		targetRoleKeys := make(map[string]bool)
		for key, loc := range target.Roles {
			if loc == roleLocator {
				targetRoleKeys[key] = true
			}
		}
		if len(targetRoleKeys) > 0 {
			for k, op := range target.Operations {
				for _, sat := range op.Satisfies {
					if targetRoleKeys[sat.Role] && sat.Operation == roleOpName {
						return k
					}
				}
			}
		}
	}

	// Check aliases.
	for _, alias := range roleOp.Aliases {
		if _, ok := target.Operations[alias]; ok {
			return alias
		}
	}
	for k, op := range target.Operations {
		for _, alias := range op.Aliases {
			if alias == roleOpName {
				return k
			}
		}
	}

	return roleOpName // fallback
}

// copySchema copies a JSON Schema from the role interface to the target,
// including any $ref'd schemas from the role's schemas pool.
func copySchema(schema openbindings.JSONSchema, roleIface, target *openbindings.Interface) openbindings.JSONSchema {
	if schema == nil {
		return nil
	}

	// If the schema is a $ref to a role schema, copy the referenced schema
	// into the target's schemas pool and return the same $ref.
	if ref, ok := schema["$ref"].(string); ok {
		if strings.HasPrefix(ref, "#/schemas/") {
			schemaName := strings.TrimPrefix(ref, "#/schemas/")
			if roleSchema, ok := roleIface.Schemas[schemaName]; ok {
				if target.Schemas == nil {
					target.Schemas = make(map[string]openbindings.JSONSchema)
				}
				if _, exists := target.Schemas[schemaName]; !exists {
					// Deep copy the schema.
					target.Schemas[schemaName] = deepCopySchema(roleSchema)
					// Recursively copy any nested $refs.
					copyNestedRefs(target.Schemas[schemaName], roleIface, target)
				}
			}
		}
		return openbindings.JSONSchema{"$ref": ref}
	}

	// For inline schemas, deep copy and handle nested $refs.
	copied := deepCopySchema(schema)
	copyNestedRefs(copied, roleIface, target)
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

// copyNestedRefs walks a schema and copies any $ref'd schemas from the role.
func copyNestedRefs(schema openbindings.JSONSchema, roleIface, target *openbindings.Interface) {
	for _, v := range schema {
		switch val := v.(type) {
		case map[string]any:
			if ref, ok := val["$ref"].(string); ok && strings.HasPrefix(ref, "#/schemas/") {
				schemaName := strings.TrimPrefix(ref, "#/schemas/")
				if roleSchema, ok := roleIface.Schemas[schemaName]; ok {
					if target.Schemas == nil {
						target.Schemas = make(map[string]openbindings.JSONSchema)
					}
					if _, exists := target.Schemas[schemaName]; !exists {
						target.Schemas[schemaName] = deepCopySchema(roleSchema)
						copyNestedRefs(target.Schemas[schemaName], roleIface, target)
					}
				}
			}
			copyNestedRefs(val, roleIface, target)
		case []any:
			for _, item := range val {
				if m, ok := item.(map[string]any); ok {
					copyNestedRefs(m, roleIface, target)
				}
			}
		}
	}
}
