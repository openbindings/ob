package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go"
)

// --- List ---

// OperationListOutput represents the result of listing operations.
type OperationListOutput struct {
	Operations []OperationEntry `json:"operations"`
}

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
	if len(o.Operations) == 0 {
		return s.Dim.Render("No operations defined")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render(fmt.Sprintf("Operations (%d)", len(o.Operations))))
	sb.WriteString("\n")
	for _, op := range o.Operations {
		sb.WriteString("\n  ")
		sb.WriteString(s.Key.Render(op.Key))
		if len(op.Operation.Tags) > 0 {
			sb.WriteString(s.Dim.Render("  ["))
			sb.WriteString(strings.Join(op.Operation.Tags, ", "))
			sb.WriteString(s.Dim.Render("]"))
		}
		if HasXOB(op.Operation.LosslessFields) {
			sb.WriteString(s.Dim.Render("  managed"))
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
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationListOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	// Collect the binding keys that realize each operation.
	bindingsByOp := map[string][]string{}
	for bkey, b := range iface.Bindings {
		bindingsByOp[b.Operation] = append(bindingsByOp[b.Operation], bkey)
	}
	for _, keys := range bindingsByOp {
		sort.Strings(keys)
	}

	var entries []OperationEntry
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

	return OperationListOutput{Operations: entries}, nil
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

	// Verify new key doesn't already exist.
	if _, exists := iface.Operations[newKey]; exists {
		return OperationRenameOutput{}, fmt.Errorf("operation %q already exists", newKey)
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

	// Verify all keys exist before removing any.
	for _, key := range keys {
		if _, exists := iface.Operations[key]; !exists {
			return OperationRemoveOutput{}, fmt.Errorf("operation %q not found", key)
		}
	}

	removeSet := toStringSet(keys)

	// Remove operations.
	for _, key := range keys {
		delete(iface.Operations, key)
	}

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

	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)
	return OperationRemoveOutput{
		Removed:         sorted,
		BindingsRemoved: bindingsRemoved,
	}, nil
}

// --- Add ---

// OperationAddInput represents the input for adding an operation.
type OperationAddInput struct {
	OBIPath     string
	Key         string
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
	if _, exists := iface.Operations[input.Key]; exists {
		return OperationAddOutput{}, fmt.Errorf("operation %q already exists", input.Key)
	}

	op := openbindings.Operation{
		Description: input.Description,
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
