package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// clearSourceOwnership converts a source-owned object into a hand-authored
// one by dropping the x-ob base snapshot — the field that IS ownership —
// and nothing else. Author-set metadata (codegenName) is untouched by
// construction, and any future OpBindingXOB field survives by default: add
// handling here only for fields that are provenance. When nothing remains,
// setOpBindingXOB removes the x-ob key entirely (a bare {} would misreport
// the object as source-owned).
func clearSourceOwnership(lf *openbindings.LosslessFields) error {
	xob, err := getOpBindingXOB(*lf)
	if err != nil {
		// Malformed x-ob cannot be surgically edited; shed it wholesale so a
		// corrupt marker cannot keep an object source-owned after detach.
		if lf.Extensions != nil {
			delete(lf.Extensions, xobKey)
		}
		return nil
	}
	xob.Base = nil
	return setOpBindingXOB(lf, xob)
}

// --- Detach ---

// OperationDetachOutput represents the result of detaching an operation.
type OperationDetachOutput struct {
	Key string `json:"key"`
}

// Render returns a human-friendly representation.
func (o OperationDetachOutput) Render() string {
	return Styles.Header.Render("Detached operation") + " " + Styles.Key.Render(o.Key) +
		"\n  " + Styles.Dim.Render("now hand-authored; 'ob source pull' will no longer overwrite it")
}

// OperationDetach converts a source-owned operation into a hand-authored one
// (strips its x-ob provenance) so future pulls leave its schema untouched.
func OperationDetach(obiPath, op string) (OperationDetachOutput, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationDetachOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	key, operation, found := openbindings.ResolveOperation(iface, op)
	if !found {
		return OperationDetachOutput{}, fmt.Errorf("operation %q not found", op)
	}
	if !IsSourceOwned(operation.LosslessFields) {
		return OperationDetachOutput{}, fmt.Errorf("operation %q is already hand-authored (not source-owned)", key)
	}
	if err := clearSourceOwnership(&operation.LosslessFields); err != nil {
		return OperationDetachOutput{}, fmt.Errorf("clear source ownership: %w", err)
	}
	iface.Operations[key] = operation
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationDetachOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationDetachOutput{Key: key}, nil
}

// --- Set ---

// OperationSetInput holds the fields to change on an existing operation. Only
// non-nil fields are applied.
type OperationSetInput struct {
	OBIPath     string
	Op          string
	Description *string
	Idempotent  *bool
	Deprecated  *bool
	Input       openbindings.JSONSchema
	Output      openbindings.JSONSchema
	AddTags     []string
	RemoveTags  []string
	Own         bool // detach a source-owned operation before editing
}

// OperationSetOutput represents the result of editing an operation.
type OperationSetOutput struct {
	Key string `json:"key"`
}

// Render returns a human-friendly representation.
func (o OperationSetOutput) Render() string {
	return Styles.Header.Render("Updated operation") + " " + Styles.Key.Render(o.Key)
}

// OperationSet edits an existing operation's fields. A source-owned operation
// can only be edited with Own=true (which detaches it first); otherwise the
// edit would be silently overwritten by the next pull.
func OperationSet(input OperationSetInput) (OperationSetOutput, error) {
	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return OperationSetOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	key, op, found := openbindings.ResolveOperation(iface, input.Op)
	if !found {
		return OperationSetOutput{}, fmt.Errorf("operation %q not found", input.Op)
	}

	if IsSourceOwned(op.LosslessFields) {
		if !input.Own {
			return OperationSetOutput{}, fmt.Errorf(
				"operation %q is source-owned; an edit would be overwritten by the next 'ob source pull'.\n"+
					"Pass --own to take ownership (detach) before editing, or edit the source instead", key)
		}
		if err := clearSourceOwnership(&op.LosslessFields); err != nil {
			return OperationSetOutput{}, fmt.Errorf("clear source ownership: %w", err)
		}
	}

	if input.Description != nil {
		op.Description = *input.Description
	}
	if input.Idempotent != nil {
		op.Idempotent = input.Idempotent
	}
	if input.Deprecated != nil {
		op.Deprecated = *input.Deprecated
	}
	if input.Input != nil {
		op.Input = input.Input
	}
	if input.Output != nil {
		op.Output = input.Output
	}
	if len(input.AddTags) > 0 || len(input.RemoveTags) > 0 {
		op.Tags = applyTagChanges(op.Tags, input.AddTags, input.RemoveTags)
	}

	iface.Operations[key] = op
	if err := WriteInterfaceFile(input.OBIPath, iface); err != nil {
		return OperationSetOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationSetOutput{Key: key}, nil
}

// applyTagChanges adds and removes tags, preserving order and de-duplicating.
func applyTagChanges(tags, add, remove []string) []string {
	rm := toStringSet(remove)
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		if _, drop := rm[t]; drop || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	for _, t := range add {
		if _, drop := rm[t]; drop || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// --- Bind / Unbind ---

// OperationBindInput holds input for attaching a source selector to an operation.
type OperationBindInput struct {
	OBIPath         string
	Op              string
	Source          string
	Selector        string
	Preference      *float64
	TransformStub   bool
	InputTransform  string
	OutputTransform string
	Force           bool // overwrite an existing binding for this (operation, source)
}

// OperationBindOutput represents the result of binding an operation.
type OperationBindOutput struct {
	BindingKey   string   `json:"bindingKey"`
	Operation    string   `json:"operation"`
	Source       string   `json:"source"`
	Selector     string   `json:"selector"`
	ShapeMatched bool     `json:"shapeMatched"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Render returns a human-friendly representation.
func (o OperationBindOutput) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Bound operation"))
	sb.WriteString("\n  ")
	sb.WriteString(s.Key.Render(o.Operation))
	sb.WriteString(s.Dim.Render(" → "))
	sb.WriteString(o.Source)
	sb.WriteString(s.Dim.Render(" · "))
	sb.WriteString(o.Selector)
	for _, w := range o.Warnings {
		sb.WriteString("\n  ")
		sb.WriteString(s.Warning.Render("warning: " + w))
	}
	return sb.String()
}

// OperationBind attaches a source selector to an existing operation (the
// contract-keyed wire-up). The operation's key is preserved; the source's wire
// identifier lives in the binding's Selector. On a shape mismatch it warns and, with
// TransformStub, scaffolds identity transforms for the author to complete.
func OperationBind(input OperationBindInput) (OperationBindOutput, error) {
	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return OperationBindOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	if iface.Bindings == nil {
		iface.Bindings = map[string]openbindings.BindingEntry{}
	}

	key, op, found := openbindings.ResolveOperation(iface, input.Op)
	if !found {
		return OperationBindOutput{}, fmt.Errorf("operation %q not found; create it first with 'ob operation add'", input.Op)
	}
	src, ok := iface.Sources[input.Source]
	if !ok {
		return OperationBindOutput{}, fmt.Errorf("source %q is not registered; add it with 'ob source add'", input.Source)
	}

	bindingKey := key + "." + input.Source
	if _, exists := iface.Bindings[bindingKey]; exists && !input.Force {
		return OperationBindOutput{}, fmt.Errorf("operation %q already has a binding to source %q; pass --force to re-point it", key, input.Source)
	}

	out := OperationBindOutput{BindingKey: bindingKey, Operation: key, Source: input.Source, Selector: input.Selector, ShapeMatched: true}

	// Best-effort: derive the source's targets to validate the selector and compare
	// shapes. Derivation failure is advisory, not fatal.
	var selIn, selOut any
	selFound := false
	if derived, derr := DeriveFromSource(src, input.Source, filepath.Dir(input.OBIPath)); derr == nil {
		for _, b := range derived.Bindings {
			if b.Selector != input.Selector {
				continue
			}
			selFound = true
			if dop, ok := derived.Operations[b.Operation]; ok {
				selIn, selOut = dop.Input, dop.Output
			}
			break
		}
		if !selFound {
			out.Warnings = append(out.Warnings, fmt.Sprintf("selector %q not found among source %q targets", input.Selector, input.Source))
		}
	} else {
		out.Warnings = append(out.Warnings, fmt.Sprintf("could not derive source %q to validate selector: %v", input.Source, derr))
	}

	be := openbindings.BindingEntry{Operation: key, Source: input.Source, Selector: input.Selector, Preference: input.Preference}

	// Explicit transforms take precedence; otherwise shape-check and optionally stub.
	if input.InputTransform != "" {
		be.InputTransform = &openbindings.TransformOrRef{Inline: input.InputTransform}
	}
	if input.OutputTransform != "" {
		be.OutputTransform = &openbindings.TransformOrRef{Inline: input.OutputTransform}
	}
	if selFound {
		inMatch := schemaValueEqual(op.Input, selIn)
		outMatch := schemaValueEqual(op.Output, selOut)
		if !inMatch || !outMatch {
			out.ShapeMatched = false
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"operation %q and selector %q shapes differ; a transform is needed to bridge them", key, input.Selector))
			if input.TransformStub {
				if !inMatch && be.InputTransform == nil {
					be.InputTransform = &openbindings.TransformOrRef{Inline: "$"}
				}
				if !outMatch && be.OutputTransform == nil {
					be.OutputTransform = &openbindings.TransformOrRef{Inline: "$"}
				}
				out.Warnings = append(out.Warnings, "scaffolded identity transform stub(s) ($); edit the inline JSONata to bridge the shapes")
			}
		}
	}

	iface.Bindings[bindingKey] = be
	if err := WriteInterfaceFile(input.OBIPath, iface); err != nil {
		return OperationBindOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return out, nil
}

// schemaValueEqual reports whether two JSON Schemas are structurally identical by
// canonical JSON encoding. nil and empty are treated as equal.
func schemaValueEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	sa, sb := string(ab), string(bb)
	if sa == "null" {
		sa = ""
	}
	if sb == "null" {
		sb = ""
	}
	return sa == sb
}

// OperationUnbindOutput represents the result of removing a binding.
type OperationUnbindOutput struct {
	BindingKey string `json:"bindingKey"`
}

// Render returns a human-friendly representation.
func (o OperationUnbindOutput) Render() string {
	return Styles.Header.Render("Removed binding") + " " + Styles.Removed.Render(o.BindingKey)
}

// OperationUnbind removes a binding from an operation without removing the
// operation itself.
func OperationUnbind(obiPath, op, source string) (OperationUnbindOutput, error) {
	if op == "" || source == "" {
		return OperationUnbindOutput{}, fmt.Errorf("operation and source are required when binding key is absent")
	}
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationUnbindOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	key, _, found := openbindings.ResolveOperation(iface, op)
	if !found {
		return OperationUnbindOutput{}, fmt.Errorf("operation %q not found", op)
	}
	bindingKey := key + "." + source
	if _, exists := iface.Bindings[bindingKey]; !exists {
		return OperationUnbindOutput{}, fmt.Errorf("operation %q has no binding to source %q", key, source)
	}
	delete(iface.Bindings, bindingKey)
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationUnbindOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationUnbindOutput{BindingKey: bindingKey}, nil
}

// OperationUnbindBinding removes the exact binding key without imposing the
// conventional <operation>.<source> spelling on an OBI document.
func OperationUnbindBinding(obiPath, bindingKey string) (OperationUnbindOutput, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationUnbindOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	if bindingKey == "" {
		return OperationUnbindOutput{}, fmt.Errorf("binding key is required")
	}
	if _, exists := iface.Bindings[bindingKey]; !exists {
		return OperationUnbindOutput{}, fmt.Errorf("binding %q not found", bindingKey)
	}
	delete(iface.Bindings, bindingKey)
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationUnbindOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationUnbindOutput{BindingKey: bindingKey}, nil
}
