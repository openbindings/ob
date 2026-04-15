// Package app - delegates_list.go contains the CLI command for listing delegates.
package app

import (
	"strings"

	"github.com/openbindings/ob/internal/delegates"
)

// DelegateListParams configures the delegate list command.
type DelegateListParams struct {
	OutputFormat string
	OutputPath   string
}

// DelegateFormatInfo represents a format supported by a delegate.
type DelegateFormatInfo struct {
	Format      string `json:"format"`
	Description string `json:"description,omitempty"`
}

// DelegateListEntry represents a delegate with its formats.
type DelegateListEntry struct {
	Name     string               `json:"name"`
	Location string               `json:"location,omitempty"`
	Formats  []DelegateFormatInfo `json:"formats"`
}

// DelegateListOutput is the output of the delegate list operation.
type DelegateListOutput struct {
	Delegates []DelegateListEntry `json:"delegates"`
	Error     *Error              `json:"error,omitempty"`
}

// Render returns a human-friendly representation.
func (o DelegateListOutput) Render() string {
	s := Styles
	var sb strings.Builder

	if o.Error != nil {
		sb.WriteString(s.Error.Render("Error: "))
		sb.WriteString(o.Error.Message)
		return sb.String()
	}

	if len(o.Delegates) == 0 {
		return s.Dim.Render("No delegates registered")
	}

	sb.WriteString(s.Header.Render("Delegates:"))

	for _, p := range o.Delegates {
		sb.WriteString("\n\n  ")
		sb.WriteString(s.Key.Render(p.Name))
		if p.Location != "" {
			sb.WriteString(s.Dim.Render(" " + p.Location))
		}
		renderDelegateFormats(&sb, p, s)
	}

	return sb.String()
}

func renderDelegateFormats(sb *strings.Builder, p DelegateListEntry, s styles) {
	if len(p.Formats) == 0 {
		sb.WriteString("\n      ")
		sb.WriteString(s.Dim.Render("(no formats)"))
		return
	}
	for _, f := range p.Formats {
		sb.WriteString("\n      ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(f.Format)
		if f.Description != "" {
			sb.WriteString(s.Dim.Render(" - " + f.Description))
		}
	}
}

// DelegateList is the CLI command handler for listing delegates.
func DelegateList(params DelegateListParams) error {
	output := BuildDelegateListOutput(params)
	if output.Error != nil {
		return exitText(1, output.Error.Message, true)
	}
	return OutputResult(output, params.OutputFormat, params.OutputPath)
}

// BuildDelegateListOutput builds the delegate list output.
func BuildDelegateListOutput(params DelegateListParams) DelegateListOutput {
	delCtx := GetDelegateContext()

	discovered, err := delegates.Discover(delegates.DiscoverParams{
		Delegates: delCtx.Delegates,
	})
	if err != nil {
		return DelegateListOutput{
			Error: &Error{Code: "discovery_failed", Message: err.Error()},
		}
	}

	var entries []DelegateListEntry
	for _, p := range discovered {
		entry := DelegateListEntry{
			Name:     p.Name,
			Location: p.Location,
		}

		delegateFormats, err := delegates.ProbeFormats(p.Location, delegates.DefaultProbeTimeout)
		if err == nil {
			for _, f := range delegateFormats {
				entry.Formats = append(entry.Formats, DelegateFormatInfo{Format: f})
			}
		}

		entries = append(entries, entry)
	}

	return DelegateListOutput{Delegates: entries}
}
