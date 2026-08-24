package app

import (
	"fmt"
	"strings"

	"github.com/openbindings/openbindings-go/synthesize"
)

// RenderSourceInspection returns a human-friendly summary of a source
// inspection: the bindable targets discovered in a source and whether the
// reported list is exhaustive.
func RenderSourceInspection(format, location string, ins *synthesize.SourceInspection) string {
	s := Styles
	if ins == nil || len(ins.Targets) == 0 {
		return s.Dim.Render("No bindable targets found.")
	}

	scope := "exhaustive"
	if !ins.Exhaustive {
		scope = "partial"
	}

	var sb strings.Builder
	sb.WriteString(s.Header.Render(fmt.Sprintf("%s [%s]: %d bindable target(s) (%s)", location, format, len(ins.Targets), scope)))

	for _, t := range ins.Targets {
		sb.WriteString("\n\n  ")
		if t.OperationKey != "" {
			// A suggested key headlines the entry, with the selector beneath it.
			sb.WriteString(s.Key.Render(t.OperationKey))
			sb.WriteString("\n    ")
			sb.WriteString(s.Dim.Render(t.Selector))
		} else {
			// No suggested key: the selector is the entry.
			sb.WriteString(s.Key.Render(t.Selector))
		}
		if t.Operation != nil && t.Operation.Description != "" {
			sb.WriteString("\n    ")
			sb.WriteString(t.Operation.Description)
		}
	}
	return sb.String()
}
