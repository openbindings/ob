package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go"
)

// toStringSet builds a set from a slice, or returns nil if the slice is empty.
func toStringSet(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

// --- List ---

// OperationListOutput is the listOperations wire output: a bare array of
// entries per the contract's output schema (never null — an empty interface
// lists as []).
type OperationListOutput []OperationEntry

// OperationEntry is a single operation in the list: the operation as stored in
// the interface, tagged with its key and the bindings that realize it.
type OperationEntry struct {
	Key       string                 `json:"key"`
	Operation openbindings.Operation `json:"operation"`
	Bindings  []string               `json:"bindings,omitempty"`
}

// Render returns a human-friendly representation.
func (o OperationListOutput) Render() string {
	s := Styles
	if len(o) == 0 {
		return s.Dim.Render("No operations defined")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render(fmt.Sprintf("Operations (%d)", len(o))))
	sb.WriteString("\n")
	for _, op := range o {
		sb.WriteString("\n  ")
		sb.WriteString(s.Key.Render(op.Key))
		if len(op.Operation.Tags) > 0 {
			sb.WriteString(s.Dim.Render("  ["))
			sb.WriteString(strings.Join(op.Operation.Tags, ", "))
			sb.WriteString(s.Dim.Render("]"))
		}
		if IsSourceOwned(op.Operation.LosslessFields) {
			sb.WriteString(s.Dim.Render("  source-owned"))
		}
		if len(op.Bindings) > 0 {
			sb.WriteString(s.Dim.Render(fmt.Sprintf("  %d binding(s)", len(op.Bindings))))
		}
		if op.Operation.Description != "" {
			sb.WriteString("\n    ")
			sb.WriteString(s.Dim.Render(op.Operation.Description))
		}
	}
	return sb.String()
}

// OperationList lists all operations in an OBI file.
func OperationList(obiPath string, tagFilter string) (OperationListOutput, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return nil, fmt.Errorf("resolve OBI: %w", err)
	}

	// Collect the binding keys that realize each operation.
	bindingsByOp := map[string][]string{}
	for bkey, b := range iface.Bindings {
		bindingsByOp[b.Operation] = append(bindingsByOp[b.Operation], bkey)
	}
	for _, keys := range bindingsByOp {
		sort.Strings(keys)
	}

	entries := OperationListOutput{}
	for key, op := range iface.Operations {
		// Apply tag filter.
		if tagFilter != "" && !containsTag(op.Tags, tagFilter) {
			continue
		}
		entries = append(entries, OperationEntry{
			Key:       key,
			Operation: op,
			Bindings:  bindingsByOp[key],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	return entries, nil
}

// containsTag checks if a tag list contains the given tag.
func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// --- Rename ---

// OperationRenameOutput represents the result of renaming an operation.
type OperationRenameOutput struct {
	OldKey          string `json:"oldKey"`
	NewKey          string `json:"newKey"`
	BindingsUpdated int    `json:"bindingsUpdated"`
}

// Render returns a human-friendly representation.
func (o OperationRenameOutput) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Renamed operation"))
	sb.WriteString("\n\n")
	sb.WriteString(s.Dim.Render("  "))
	sb.WriteString(s.Removed.Render(o.OldKey))
	sb.WriteString(s.Dim.Render(" → "))
	sb.WriteString(s.Added.Render(o.NewKey))
	if o.BindingsUpdated > 0 {
		sb.WriteString(fmt.Sprintf("\n\n  %d binding(s) updated", o.BindingsUpdated))
	}
	return sb.String()
}

// OperationRename renames an operation and updates all references throughout the OBI.
func OperationRename(obiPath, oldKey, newKey string) (OperationRenameOutput, error) {
	if oldKey == newKey {
		return OperationRenameOutput{}, fmt.Errorf("old and new keys are the same")
	}

	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationRenameOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	// Verify old key exists.
	op, exists := iface.Operations[oldKey]
	if !exists {
		return OperationRenameOutput{}, fmt.Errorf("operation %q not found", oldKey)
	}

	// Verify the new key is free in the flat key+alias namespace (OBI-D-04) —
	// not just as an operations-map key. It must not collide with another
	// operation's alias either, which would produce an invalid document.
	if owner := operationNameOwner(iface, newKey); owner != "" {
		if owner == newKey {
			return OperationRenameOutput{}, fmt.Errorf("operation %q already exists", newKey)
		}
		return OperationRenameOutput{}, fmt.Errorf("name %q is already an alias of operation %q", newKey, owner)
	}

	// Move the operation.
	iface.Operations[newKey] = op
	delete(iface.Operations, oldKey)

	// Update bindings: operation field and binding keys.
	bindingsUpdated := 0
	if iface.Bindings != nil {
		// Collect renames to avoid mutating the map during iteration.
		type rename struct {
			oldBK string
			newBK string
			entry openbindings.BindingEntry
		}
		var renames []rename
		for bk, be := range iface.Bindings {
			if be.Operation == oldKey {
				be.Operation = newKey
				bindingsUpdated++
				newBK := renameBindingKey(bk, oldKey, newKey)
				renames = append(renames, rename{oldBK: bk, newBK: newBK, entry: be})
			}
		}
		for _, r := range renames {
			delete(iface.Bindings, r.oldBK)
			iface.Bindings[r.newBK] = r.entry
		}
	}

	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationRenameOutput{}, fmt.Errorf("write OBI: %w", err)
	}

	return OperationRenameOutput{
		OldKey:          oldKey,
		NewKey:          newKey,
		BindingsUpdated: bindingsUpdated,
	}, nil
}

// renameBindingKey replaces the operation portion of a binding key.
// Convention: binding keys are "operation.source". If the key starts with
// oldOp + ".", replace that prefix. Otherwise return the key unchanged.
func renameBindingKey(bindingKey, oldOp, newOp string) string {
	prefix := oldOp + "."
	if strings.HasPrefix(bindingKey, prefix) {
		return newOp + "." + strings.TrimPrefix(bindingKey, prefix)
	}
	return bindingKey
}

// --- Remove ---

// OperationRemoveOutput represents the result of removing operations.
type OperationRemoveOutput struct {
	Removed         []string `json:"removed"`
	BindingsRemoved int      `json:"bindingsRemoved"`
}

// Render returns a human-friendly representation.
func (o OperationRemoveOutput) Render() string {
	s := Styles
	var sb strings.Builder
	if len(o.Removed) == 1 {
		sb.WriteString(s.Header.Render("Removed operation"))
		sb.WriteString(" ")
		sb.WriteString(s.Key.Render(o.Removed[0]))
	} else {
		sb.WriteString(s.Header.Render(fmt.Sprintf("Removed %d operations", len(o.Removed))))
		for _, key := range o.Removed {
			sb.WriteString("\n  ")
			sb.WriteString(s.Removed.Render(key))
		}
	}
	if o.BindingsRemoved > 0 {
		sb.WriteString(fmt.Sprintf("\n\n  %d binding(s) removed", o.BindingsRemoved))
	}
	return sb.String()
}

// OperationRemove removes one or more operations and their associated bindings from an OBI.
func OperationRemove(obiPath string, keys []string) (OperationRemoveOutput, error) {
	if len(keys) == 0 {
		return OperationRemoveOutput{}, fmt.Errorf("no operation keys specified")
	}

	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationRemoveOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	// Remove the operations that are present; an absent key is a tolerant no-op.
	var removed []string
	for _, key := range keys {
		if _, exists := iface.Operations[key]; exists {
			delete(iface.Operations, key)
			removed = append(removed, key)
		}
	}
	removeSet := toStringSet(removed)

	// Remove associated bindings.
	bindingsRemoved := 0
	for bk, be := range iface.Bindings {
		if _, ok := removeSet[be.Operation]; ok {
			delete(iface.Bindings, bk)
			bindingsRemoved++
		}
	}

	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationRemoveOutput{}, fmt.Errorf("write OBI: %w", err)
	}

	sort.Strings(removed)
	return OperationRemoveOutput{
		Removed:         removed,
		BindingsRemoved: bindingsRemoved,
	}, nil
}

// --- Add ---

// OperationAddInput represents the input for adding an operation.
type OperationAddInput struct {
	OBIPath     string
	Key         string
	Aliases     []string
	Description string
	Tags        []string
	Input       map[string]any
	Output      map[string]any
	Idempotent  *bool
}

// OperationAddOutput represents the result of adding an operation.
type OperationAddOutput struct {
	Key string `json:"key"`
}

// Render returns a human-friendly representation.
func (o OperationAddOutput) Render() string {
	return Styles.Header.Render("Added operation") + " " + Styles.Key.Render(o.Key)
}

// OperationAdd adds a new operation to an OBI file.
func OperationAdd(input OperationAddInput) (OperationAddOutput, error) {
	if input.Key == "" {
		return OperationAddOutput{}, fmt.Errorf("operation key is required")
	}

	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return OperationAddOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	if iface.Operations == nil {
		iface.Operations = map[string]openbindings.Operation{}
	}
	// The key must be free in the flat key+alias namespace (OBI-D-04), not just
	// as an operations-map key.
	if owner := operationNameOwner(iface, input.Key); owner != "" {
		if owner == input.Key {
			return OperationAddOutput{}, fmt.Errorf("operation %q already exists", input.Key)
		}
		return OperationAddOutput{}, fmt.Errorf("operation key %q is already an alias of operation %q", input.Key, owner)
	}
	// Validate aliases against the flat namespace and each other.
	seen := map[string]bool{input.Key: true}
	for _, a := range input.Aliases {
		if seen[a] {
			return OperationAddOutput{}, fmt.Errorf("duplicate alias %q", a)
		}
		if owner := operationNameOwner(iface, a); owner != "" {
			return OperationAddOutput{}, fmt.Errorf("alias %q is already in use by operation %q", a, owner)
		}
		seen[a] = true
	}

	op := openbindings.Operation{
		Description: input.Description,
		Aliases:     input.Aliases,
		Tags:        input.Tags,
		Idempotent:  input.Idempotent,
		Input:       input.Input,
		Output:      input.Output,
	}

	iface.Operations[input.Key] = op

	if err := WriteInterfaceFile(input.OBIPath, iface); err != nil {
		return OperationAddOutput{}, fmt.Errorf("write OBI: %w", err)
	}

	return OperationAddOutput{Key: input.Key}, nil
}

// operationNameOwner returns the canonical key of the operation that already
// claims `name` in the flat key+alias namespace (OBI-T-12 / OBI-D-04), or ""
// if the name is free.
func operationNameOwner(iface *openbindings.Interface, name string) string {
	if key, _, found := openbindings.ResolveOperation(iface, name); found {
		return key
	}
	return ""
}

// --- Alias ---

// OperationAliasOutput represents the result of adding or removing aliases.
type OperationAliasOutput struct {
	Key     string   `json:"key"`     // the operation's canonical key
	Action  string   `json:"action"`  // "added" or "removed"
	Changed []string `json:"changed"` // the aliases added or removed
	Aliases []string `json:"aliases"` // the operation's aliases after the change
}

// Render returns a human-friendly representation.
func (o OperationAliasOutput) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render(fmt.Sprintf("%s %d alias(es)", titleCase(o.Action), len(o.Changed))))
	sb.WriteString(" on ")
	sb.WriteString(s.Key.Render(o.Key))
	for _, a := range o.Changed {
		sb.WriteString("\n  ")
		if o.Action == "removed" {
			sb.WriteString(s.Removed.Render(a))
		} else {
			sb.WriteString(s.Added.Render(a))
		}
	}
	return sb.String()
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// OperationAliasAdd declares satisfaction aliases on an operation (OBI-T-12).
// The operation may be referenced by its key or any existing identifier.
func OperationAliasAdd(obiPath, op string, aliases []string) (OperationAliasOutput, error) {
	if len(aliases) == 0 {
		return OperationAliasOutput{}, fmt.Errorf("no aliases specified")
	}
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationAliasOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	key, operation, found := openbindings.ResolveOperation(iface, op)
	if !found {
		return OperationAliasOutput{}, fmt.Errorf("operation %q not found", op)
	}

	have := map[string]bool{}
	for _, a := range operation.Aliases {
		have[a] = true
	}
	seen := map[string]bool{}
	for _, a := range aliases {
		if a == key {
			return OperationAliasOutput{}, fmt.Errorf("alias %q is the operation's own key", a)
		}
		if have[a] {
			return OperationAliasOutput{}, fmt.Errorf("operation %q already has alias %q", key, a)
		}
		if seen[a] {
			return OperationAliasOutput{}, fmt.Errorf("duplicate alias %q", a)
		}
		// A collision is only legal if the name already belongs to this very
		// operation (it can't — we checked key and existing aliases above).
		if owner := operationNameOwner(iface, a); owner != "" && owner != key {
			return OperationAliasOutput{}, fmt.Errorf("alias %q is already in use by operation %q", a, owner)
		}
		seen[a] = true
	}

	operation.Aliases = append(operation.Aliases, aliases...)
	iface.Operations[key] = operation

	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationAliasOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationAliasOutput{Key: key, Action: "added", Changed: aliases, Aliases: operation.Aliases}, nil
}

// OperationAliasRemove withdraws satisfaction aliases from an operation.
func OperationAliasRemove(obiPath, op string, aliases []string) (OperationAliasOutput, error) {
	if len(aliases) == 0 {
		return OperationAliasOutput{}, fmt.Errorf("no aliases specified")
	}
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationAliasOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	key, operation, found := openbindings.ResolveOperation(iface, op)
	if !found {
		return OperationAliasOutput{}, fmt.Errorf("operation %q not found", op)
	}

	remove := toStringSet(aliases)
	present := map[string]bool{}
	for _, a := range operation.Aliases {
		present[a] = true
	}
	for _, a := range aliases {
		if !present[a] {
			return OperationAliasOutput{}, fmt.Errorf("operation %q has no alias %q", key, a)
		}
	}

	var kept []string
	for _, a := range operation.Aliases {
		if _, drop := remove[a]; !drop {
			kept = append(kept, a)
		}
	}
	operation.Aliases = kept
	iface.Operations[key] = operation

	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationAliasOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationAliasOutput{Key: key, Action: "removed", Changed: aliases, Aliases: kept}, nil
}

// OperationAliasListEntry is one operation and the interface ops it satisfies.
type OperationAliasListEntry struct {
	Key     string   `json:"key"`
	Aliases []string `json:"aliases"`
}

// OperationAliasListOutput is the satisfaction map — the listOperationAliases
// wire output: a bare array of entries per the contract's output schema
// (never null).
type OperationAliasListOutput []OperationAliasListEntry

// Render returns a human-friendly representation.
func (o OperationAliasListOutput) Render() string {
	s := Styles
	if len(o) == 0 {
		return s.Dim.Render("No satisfaction aliases")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Satisfies"))
	for _, e := range o {
		sb.WriteString("\n\n  ")
		sb.WriteString(s.Key.Render(e.Key))
		if len(e.Aliases) == 0 {
			sb.WriteString(s.Dim.Render("  (no aliases)"))
			continue
		}
		for _, a := range e.Aliases {
			sb.WriteString("\n    ")
			sb.WriteString(s.Dim.Render("→ "))
			sb.WriteString(a)
		}
	}
	return sb.String()
}

// OperationAliasList renders the satisfaction map — each operation and the
// interface operations it satisfies via aliases. When op is non-empty it is
// scoped to that single operation (shown even if it has no aliases).
func OperationAliasList(obiPath, op string) (OperationAliasListOutput, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return nil, fmt.Errorf("resolve OBI: %w", err)
	}

	entries := OperationAliasListOutput{}
	if op != "" {
		key, operation, found := openbindings.ResolveOperation(iface, op)
		if !found {
			return nil, fmt.Errorf("operation %q not found", op)
		}
		aliases := operation.Aliases
		if aliases == nil {
			aliases = []string{} // contract requires an array, never null
		}
		entries = append(entries, OperationAliasListEntry{Key: key, Aliases: aliases})
	} else {
		for key, operation := range iface.Operations {
			if len(operation.Aliases) == 0 {
				continue
			}
			entries = append(entries, OperationAliasListEntry{Key: key, Aliases: operation.Aliases})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	}

	return entries, nil
}
