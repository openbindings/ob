// Package app - delegates_list.go contains the CLI command for listing delegates.
package app

import (
	"strings"
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

// DelegateListEntry represents a delegate and what it provides (DelegateSummary
// in the contract).
type DelegateListEntry struct {
	Name         string               `json:"name,omitempty"`
	Location     string               `json:"location,omitempty"`
	Builtin      bool                 `json:"builtin,omitempty"`
	Capabilities []DelegateCapability `json:"capabilities,omitempty"`
	Formats      []DelegateFormatInfo `json:"formats,omitempty"`
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
		if p.Builtin {
			sb.WriteString(s.Dim.Render(" (builtin)"))
		} else if p.Location != "" {
			sb.WriteString(s.Dim.Render(" " + p.Location))
		}
		if len(p.Capabilities) > 0 {
			caps := make([]string, len(p.Capabilities))
			for i, c := range p.Capabilities {
				caps[i] = string(c)
			}
			sb.WriteString("\n    ")
			sb.WriteString(s.Dim.Render("capabilities: "))
			sb.WriteString(strings.Join(caps, ", "))
		} else if !p.Builtin {
			sb.WriteString("\n    ")
			sb.WriteString(s.Warning.Render("(unreachable or no delegatable capability)"))
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

// BuildDelegateListOutput builds the delegate list output. ob's own native
// handling is the implicit self-delegate (delegate 0); registered locations
// follow, introspected for their capabilities and formats. A registered
// location that refers to this binary is folded into the self-delegate.
func BuildDelegateListOutput(params DelegateListParams) DelegateListOutput {
	entries := []DelegateListEntry{selfDelegateEntry()}

	delCtx := GetDelegateContext()
	for _, loc := range delCtx.Delegates {
		if isSelf(loc) {
			continue // the in-process self-delegate already covers this
		}
		intro := introspectDelegate(loc)
		entries = append(entries, DelegateListEntry{
			Name:         intro.Name,
			Location:     intro.Location,
			Capabilities: intro.Capabilities,
			Formats:      intro.Formats,
		})
	}

	return DelegateListOutput{Delegates: entries}
}

// selfDelegateEntry is ob's own native handling as a delegate: it provides all
// three capabilities in-process and handles ob's native formats. It is never
// resolved over a transport, so its capabilities are known, not introspected.
func selfDelegateEntry() DelegateListEntry {
	var formats []DelegateFormatInfo
	for _, tok := range getNativeTokens() {
		formats = append(formats, DelegateFormatInfo{Format: tok})
	}
	return DelegateListEntry{
		Name:         "ob",
		Builtin:      true,
		Capabilities: []DelegateCapability{CapInvoke, CapSynthesize, CapInspect},
		Formats:      formats,
	}
}
