package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go"
)

// MergeAction describes what to do with a single operation during merge.
type MergeAction string

const (
	MergeAdd     MergeAction = "add"     // in source, not in target
	MergeUpdate  MergeAction = "update"  // in both, schemas differ
	MergeUnbind  MergeAction = "remove"  // binding in target, absent from source
	MergeSkip    MergeAction = "skip"    // in sync, no change needed
	MergeUnbound MergeAction = "unbound" // in target, no binding to source
)

// MergeEntry describes a single merge action.
type MergeEntry struct {
	Operation string      `json:"operation"`
	Action    MergeAction `json:"action"`
	Details   []string    `json:"details,omitempty"`
	Applied   bool        `json:"applied"`
}

// MergePromptFunc is called for each actionable merge entry to ask the user
// whether to apply the change. It returns true to apply, false to skip.
// If nil, no interactive prompting occurs (use --all for batch mode).
type MergePromptFunc func(entry MergeEntry) (bool, error)

// MergeInput represents input for the merge command.
type MergeInput struct {
	// For two-arg mode:
	TargetPath    string
	SourceLocator string

	// For --from-sources mode:
	FromSources bool
	OnlySource  string

	// Mode flags:
	All          bool     // apply all changes (batch mode)
	DryRun       bool     // show what would change without writing
	OutPath      string   // write to alternate path instead of target
	Operations   []string // if non-empty, only merge these operations (cherry-pick)
	ExcludeOps   []string // if non-empty, skip these operations

	// PromptFunc is called for each actionable entry when in interactive mode.
	// Set by the cmd layer when TTY is detected and --all is not specified.
	PromptFunc MergePromptFunc
}

// MergeOutput is the result of a merge operation.
type MergeOutput struct {
	Entries  []MergeEntry `json:"entries"`
	Applied  int          `json:"applied"`
	Skipped  int          `json:"skipped"`
	Warnings []string     `json:"warnings,omitempty"`
	DryRun   bool         `json:"dryRun,omitempty"`
}

// Render returns a human-friendly representation.
func (o MergeOutput) Render() string {
	s := Styles
	var sb strings.Builder

	if o.DryRun {
		sb.WriteString(s.Warning.Render("DRY RUN — no changes written"))
		sb.WriteString("\n\n")
	}

	sb.WriteString(s.Header.Render("Merge summary"))
	sb.WriteString("\n")

	// Group by action.
	added, updated, removed, skipped, unbound := groupMergeEntries(o.Entries)

	if len(added) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Added.Render(fmt.Sprintf("  + %d added", len(added))))
		sb.WriteString("\n")
		for _, e := range added {
			marker := "+"
			if !e.Applied {
				marker = "·"
			}
			sb.WriteString(fmt.Sprintf("    %s %s\n", s.Added.Render(marker), e.Operation))
		}
	}

	if len(updated) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Warning.Render(fmt.Sprintf("  ~ %d updated", len(updated))))
		sb.WriteString("\n")
		for _, e := range updated {
			marker := "~"
			if !e.Applied {
				marker = "·"
			}
			sb.WriteString(fmt.Sprintf("    %s %s\n", s.Warning.Render(marker), e.Operation))
			for _, d := range e.Details {
				sb.WriteString(fmt.Sprintf("      %s\n", s.Dim.Render(d)))
			}
		}
	}

	if len(removed) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Removed.Render(fmt.Sprintf("  - %d bindings removed", len(removed))))
		sb.WriteString("\n")
		for _, e := range removed {
			marker := "-"
			if !e.Applied {
				marker = "·"
			}
			sb.WriteString(fmt.Sprintf("    %s %s\n", s.Removed.Render(marker), e.Operation))
		}
	}

	if len(skipped) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Success.Render(fmt.Sprintf("  = %d in sync", len(skipped))))
		sb.WriteString("\n")
	}

	if len(unbound) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render(fmt.Sprintf("  · %d unbound (untouched)", len(unbound))))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %d applied, %d skipped",
		o.Applied, o.Skipped))

	if len(o.Warnings) > 0 {
		sb.WriteString("\n")
		for _, w := range o.Warnings {
			sb.WriteString(fmt.Sprintf("\n  %s %s", s.Warning.Render("!"), w))
		}
	}

	return sb.String()
}

// Merge performs a merge operation.
func Merge(input MergeInput) (MergeOutput, error) {
	// Load the target OBI.
	target, err := loadInterfaceFile(input.TargetPath)
	if err != nil {
		return MergeOutput{}, fmt.Errorf("load target: %w", err)
	}

	// Get the source OBI.
	var source *openbindings.Interface
	var warnings []string

	if input.FromSources {
		source, warnings, err = buildSourceFromBindings(target, input)
		if err != nil {
			return MergeOutput{}, err
		}
	} else {
		source, err = resolveInterface(input.SourceLocator)
		if err != nil {
			return MergeOutput{}, fmt.Errorf("load source: %w", err)
		}
	}

	// Compute what needs to happen.
	entries := computeMergeEntries(target, source)

	// If --op is set, filter to only those operations.
	if len(input.Operations) > 0 {
		allowSet := make(map[string]bool, len(input.Operations))
		for _, op := range input.Operations {
			allowSet[op] = true
		}
		var filtered []MergeEntry
		for _, e := range entries {
			if allowSet[e.Operation] {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// If --exclude-op is set, remove those operations.
	if len(input.ExcludeOps) > 0 {
		excludeSet := make(map[string]bool, len(input.ExcludeOps))
		for _, op := range input.ExcludeOps {
			excludeSet[op] = true
		}
		var filtered []MergeEntry
		for _, e := range entries {
			if !excludeSet[e.Operation] {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// Determine which entries to apply.
	applied := 0
	skipped := 0
	for i := range entries {
		e := &entries[i]
		switch e.Action {
		case MergeSkip, MergeUnbound:
			skipped++
		case MergeAdd, MergeUpdate, MergeUnbind:
			if input.All {
				// Batch mode: apply everything.
				e.Applied = true
				applied++
			} else if input.PromptFunc != nil && !input.DryRun {
				// Interactive mode: ask the user.
				accept, err := input.PromptFunc(*e)
				if err != nil {
					return MergeOutput{}, fmt.Errorf("prompt: %w", err)
				}
				if accept {
					e.Applied = true
					applied++
				} else {
					skipped++
				}
			} else {
				// No --all, no interactive prompt: skip.
				skipped++
			}
		}
	}

	// Apply the merge if not dry-run.
	if !input.DryRun && applied > 0 {
		applyMerge(target, source, entries)

		outPath := input.TargetPath
		if input.OutPath != "" {
			outPath = input.OutPath
		}

		if err := WriteInterfaceFile(outPath, target); err != nil {
			return MergeOutput{}, fmt.Errorf("write: %w", err)
		}
	}

	sort.Strings(warnings)

	return MergeOutput{
		Entries:  entries,
		Applied:  applied,
		Skipped:  skipped,
		Warnings: warnings,
		DryRun:   input.DryRun,
	}, nil
}

// buildSourceFromBindings derives a source OBI from the target's binding sources.
func buildSourceFromBindings(target *openbindings.Interface, input MergeInput) (*openbindings.Interface, []string, error) {
	obiDir := filepath.Dir(input.TargetPath)

	derived, err := deriveFromAllSources(target, obiDir, input.OnlySource)
	if err != nil {
		return nil, nil, err
	}

	return derived.Assembled, derived.Warnings, nil
}

// computeMergeEntries determines what needs to happen for each operation.
func computeMergeEntries(target, source *openbindings.Interface) []MergeEntry {
	var entries []MergeEntry

	// Build set of bound operations (operations that have a binding in source).
	sourceBoundOps := make(map[string]bool)
	for _, b := range source.Bindings {
		sourceBoundOps[b.Operation] = true
	}

	// Build set of operations bound in target.
	targetBoundOps := make(map[string]string) // operation → source key
	for _, b := range target.Bindings {
		if targetBoundOps[b.Operation] == "" {
			targetBoundOps[b.Operation] = b.Source
		}
	}

	// Build normalizer roots.
	targetRoot := buildNormalizerRoot(target)
	sourceRoot := buildNormalizerRoot(source)

	// Collect all operation keys.
	allOps := make(map[string]bool)
	for k := range target.Operations {
		allOps[k] = true
	}
	for k := range source.Operations {
		allOps[k] = true
	}

	sortedKeys := make([]string, 0, len(allOps))
	for k := range allOps {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		targetOp, inTarget := target.Operations[key]
		sourceOp, inSource := source.Operations[key]

		switch {
		case inTarget && inSource:
			// Both have it — check if schemas differ.
			details := diffOperation(targetOp, sourceOp, targetRoot, sourceRoot)
			if len(details) > 0 {
				entries = append(entries, MergeEntry{
					Operation: key,
					Action:    MergeUpdate,
					Details:   details,
				})
			} else {
				entries = append(entries, MergeEntry{
					Operation: key,
					Action:    MergeSkip,
				})
			}

		case !inTarget && inSource:
			// Only in source — add.
			entries = append(entries, MergeEntry{
				Operation: key,
				Action:    MergeAdd,
			})

		case inTarget && !inSource:
			// Only in target. If it has a binding to the source, it was removed.
			if _, bound := targetBoundOps[key]; bound {
				entries = append(entries, MergeEntry{
					Operation: key,
					Action:    MergeUnbind,
				})
			} else {
				entries = append(entries, MergeEntry{
					Operation: key,
					Action:    MergeUnbound,
				})
			}
		}
	}

	return entries
}

// applyMerge applies the merge entries to the target interface.
func applyMerge(target, source *openbindings.Interface, entries []MergeEntry) {
	for _, e := range entries {
		if !e.Applied {
			continue
		}

		switch e.Action {
		case MergeAdd:
			// Add operation from source.
			if source.Operations != nil {
				if op, ok := source.Operations[e.Operation]; ok {
					if target.Operations == nil {
						target.Operations = map[string]openbindings.Operation{}
					}
					target.Operations[e.Operation] = op

					// Migrate $ref targets from source schemas (D9).
					migrateSchemaRefs(target, source, op)
				}
			}

			// Add corresponding bindings and their source artifacts.
			if target.Bindings == nil {
				target.Bindings = map[string]openbindings.BindingEntry{}
			}
			for k, b := range source.Bindings {
				if b.Operation == e.Operation {
					target.Bindings[k] = b
					migrateBindingSource(target, source, b.Source)
					migrateBindingTransforms(target, source, b)
				}
			}

		case MergeUpdate:
			// Update operation schemas from source, preserving user-authored fields (D9).
			if sourceOp, ok := source.Operations[e.Operation]; ok {
				targetOp := target.Operations[e.Operation]

				// Replace schema slots.
				targetOp.Input = sourceOp.Input
				targetOp.Output = sourceOp.Output

				// Preserve: Description, Aliases, Satisfies, Deprecated, etc.
				// These are user-authored fields.

				target.Operations[e.Operation] = targetOp

				// Migrate $ref targets from source schemas (D9).
				migrateSchemaRefs(target, source, sourceOp)

				// Ensure binding source artifacts and transforms are present in target.
				for _, b := range source.Bindings {
					if b.Operation == e.Operation {
						migrateBindingSource(target, source, b.Source)
						migrateBindingTransforms(target, source, b)
					}
				}
			}

		case MergeUnbind:
			// Remove binding entries for this operation, but keep the operation.
			for k, b := range target.Bindings {
				if b.Operation == e.Operation {
					delete(target.Bindings, k)
				}
			}
		}
	}
}

// migrateBindingSources copies the source artifact and any referenced transforms for a
// binding from source into target. Existing entries in target are not overwritten.
func migrateBindingSource(target, source *openbindings.Interface, sourceKey string) {
	if sourceKey == "" {
		return
	}
	// Copy source artifact.
	if srcDef, ok := source.Sources[sourceKey]; ok {
		if target.Sources == nil {
			target.Sources = map[string]openbindings.Source{}
		}
		if _, exists := target.Sources[sourceKey]; !exists {
			target.Sources[sourceKey] = srcDef
		}
	}
}

// migrateBindingTransforms copies any named transforms referenced by a binding's
// inputTransform/outputTransform $ref into the target. Existing entries are not overwritten.
func migrateBindingTransforms(target, source *openbindings.Interface, b openbindings.BindingEntry) {
	if len(source.Transforms) == 0 {
		return
	}
	refs := []string{}
	if b.InputTransform != nil && b.InputTransform.IsRef() {
		refs = append(refs, b.InputTransform.Ref)
	}
	if b.OutputTransform != nil && b.OutputTransform.IsRef() {
		refs = append(refs, b.OutputTransform.Ref)
	}
	for _, ref := range refs {
		key := strings.TrimPrefix(ref, "#/transforms/")
		if key == ref {
			continue // not a local transform ref
		}
		if t, ok := source.Transforms[key]; ok {
			if target.Transforms == nil {
				target.Transforms = map[string]openbindings.Transform{}
			}
			if _, exists := target.Transforms[key]; !exists {
				target.Transforms[key] = t
			}
		}
	}
}

// migrateSchemaRefs walks an operation's schema slots and copies any
// $ref-referenced schemas from source into target's schemas pool.
// If a schema with the same key already exists in target and is equivalent,
// it's left as-is. If it differs, the source version wins (since it's
// what the operation was derived against).
func migrateSchemaRefs(target, source *openbindings.Interface, op openbindings.Operation) {
	if len(source.Schemas) == 0 {
		return
	}

	// Collect all $ref keys from the operation's schema slots.
	refs := collectRefs(op.Input)
	refs = append(refs, collectRefs(op.Output)...)

	if len(refs) == 0 {
		return
	}

	// Ensure target has a schemas pool.
	if target.Schemas == nil {
		target.Schemas = map[string]openbindings.JSONSchema{}
	}

	// Copy referenced schemas.
	for _, ref := range refs {
		if schema, ok := source.Schemas[ref]; ok {
			target.Schemas[ref] = schema
		}
	}
}

// collectRefs extracts schema keys referenced by $ref in a schema.
// It looks for $ref values of the form "#/schemas/Foo" and returns ["Foo"].
// It walks nested schemas (properties, items, allOf, etc.) recursively.
func collectRefs(schema map[string]any) []string {
	if schema == nil {
		return nil
	}

	var refs []string

	// Check for direct $ref.
	if ref, ok := schema["$ref"].(string); ok {
		if strings.HasPrefix(ref, "#/schemas/") {
			refs = append(refs, strings.TrimPrefix(ref, "#/schemas/"))
		}
	}

	// Walk properties.
	if props, ok := schema["properties"].(map[string]any); ok {
		for _, v := range props {
			if propSchema, ok := v.(map[string]any); ok {
				refs = append(refs, collectRefs(propSchema)...)
			}
		}
	}

	// Walk items.
	if items, ok := schema["items"].(map[string]any); ok {
		refs = append(refs, collectRefs(items)...)
	}

	// Walk allOf/anyOf/oneOf.
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if arr, ok := schema[keyword].([]any); ok {
			for _, item := range arr {
				if itemSchema, ok := item.(map[string]any); ok {
					refs = append(refs, collectRefs(itemSchema)...)
				}
			}
		}
	}

	// Walk additionalProperties.
	if addProps, ok := schema["additionalProperties"].(map[string]any); ok {
		refs = append(refs, collectRefs(addProps)...)
	}

	return refs
}

// groupMergeEntries groups entries by action type.
func groupMergeEntries(entries []MergeEntry) (added, updated, removed, skipped, unbound []MergeEntry) {
	for _, e := range entries {
		switch e.Action {
		case MergeAdd:
			added = append(added, e)
		case MergeUpdate:
			updated = append(updated, e)
		case MergeUnbind:
			removed = append(removed, e)
		case MergeSkip:
			skipped = append(skipped, e)
		case MergeUnbound:
			unbound = append(unbound, e)
		}
	}
	return
}
