package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ConflictsInput represents input for the conflicts command.
type ConflictsInput struct {
	OBIPath string
}

// ObjectConflict describes all field-level conflicts on a single operation or binding.
type ObjectConflict struct {
	Key    string          `json:"key"`
	Type   string          `json:"type"` // "operation" or "binding"
	Fields []FieldConflict `json:"fields"`
}

// ConflictsOutput represents the result of the conflicts command.
type ConflictsOutput struct {
	Conflicts []ObjectConflict `json:"conflicts,omitempty"`
}

// Render returns a human-friendly representation.
func (o ConflictsOutput) Render() string {
	s := Styles
	var sb strings.Builder

	if len(o.Conflicts) == 0 {
		sb.WriteString(s.Success.Render("No conflicts."))
		return sb.String()
	}

	sb.WriteString(s.Header.Render(fmt.Sprintf("%d object(s) with conflicts", len(o.Conflicts))))
	sb.WriteString("\n")

	for _, oc := range o.Conflicts {
		sb.WriteString(fmt.Sprintf("\n  %s %s\n", s.Dim.Render(oc.Type+":"), s.Key.Render(oc.Key)))
		for _, fc := range oc.Fields {
			sb.WriteString(fmt.Sprintf("    %s\n", s.Warning.Render(fc.Field)))
			if len(fc.Base) > 0 {
				sb.WriteString(fmt.Sprintf("      base:   %s\n", s.Dim.Render(compactJSON(fc.Base))))
			}
			if len(fc.Local) > 0 {
				sb.WriteString(fmt.Sprintf("      local:  %s\n", compactJSON(fc.Local)))
			}
			if len(fc.Source) > 0 {
				sb.WriteString(fmt.Sprintf("      source: %s\n", s.Dim.Render(compactJSON(fc.Source))))
			}
		}
	}

	return sb.String()
}

// Conflicts scans an OBI for all field-level merge conflicts between
// local content and what sources would produce.
func Conflicts(input ConflictsInput) (ConflictsOutput, error) {
	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return ConflictsOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	previews := PreviewAllSourceMerges(iface, input.OBIPath)

	var allConflicts []ObjectConflict
	for _, preview := range previews {
		allConflicts = append(allConflicts, preview.Conflicts()...)
	}

	sort.Slice(allConflicts, func(i, j int) bool {
		if allConflicts[i].Type != allConflicts[j].Type {
			return allConflicts[i].Type < allConflicts[j].Type
		}
		return allConflicts[i].Key < allConflicts[j].Key
	})

	return ConflictsOutput{Conflicts: allConflicts}, nil
}

// compactJSON returns a compact string representation of a JSON value.
func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	s := string(b)
	// Truncate very long values for display.
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
