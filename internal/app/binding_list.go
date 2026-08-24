package app

import (
	"fmt"
	"sort"
	"strings"
)

// BindingListEntry is one row of a binding listing: a binding from the
// interface's bindings map, with its key. Transform presence is a flag rather
// than the expression, since a listing is for orientation.
type BindingListEntry struct {
	Key             string   `json:"key"`
	Operation       string   `json:"operation"`
	Source          string   `json:"source"`
	Selector        string   `json:"selector,omitempty"`
	Preference      *float64 `json:"preference,omitempty"`
	Deprecated      bool     `json:"deprecated,omitempty"`
	InputTransform  bool     `json:"inputTransform,omitempty"`
	OutputTransform bool     `json:"outputTransform,omitempty"`
}

// BindingListOutput is the result of listing an OBI's bindings.
type BindingListOutput []BindingListEntry

// BindingList lists the bindings declared on an OBI, optionally filtered to a
// single operation. It closes the "bindings are writable but unviewable" gap:
// what `operation bind`/`unbind` produced is now readable without opening the
// raw JSON.
func BindingList(obiPath, opFilter string) (BindingListOutput, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI: %w", err)
	}
	out := BindingListOutput{}
	for key, b := range iface.Bindings {
		if opFilter != "" && b.Operation != opFilter {
			continue
		}
		out = append(out, BindingListEntry{
			Key:             key,
			Operation:       b.Operation,
			Source:          b.Source,
			Selector:        b.Selector,
			Preference:      b.Preference,
			Deprecated:      b.Deprecated,
			InputTransform:  b.InputTransform != nil,
			OutputTransform: b.OutputTransform != nil,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Render produces a human-readable binding listing.
func (o BindingListOutput) Render() string {
	s := Styles
	if len(o) == 0 {
		return s.Dim.Render("  (no bindings)")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Bindings:"))
	sb.WriteString("\n")
	for _, b := range o {
		sb.WriteString("  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Key.Render(b.Key))
		sb.WriteString(s.Dim.Render(fmt.Sprintf("  %s → %s", b.Operation, b.Source)))
		if b.Selector != "" {
			sb.WriteString(s.Dim.Render(" " + b.Selector))
		}
		var tags []string
		if b.Deprecated {
			tags = append(tags, "deprecated")
		}
		if b.Preference != nil {
			tags = append(tags, fmt.Sprintf("preference %g", *b.Preference))
		}
		if b.InputTransform {
			tags = append(tags, "inputTransform")
		}
		if b.OutputTransform {
			tags = append(tags, "outputTransform")
		}
		if len(tags) > 0 {
			sb.WriteString(s.Dim.Render(" [" + strings.Join(tags, ", ") + "]"))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}
