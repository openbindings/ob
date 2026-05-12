package app

import (
	"fmt"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// SoftwareInfo contains identity and metadata for a piece of software.
// It mirrors the openbindings.software-descriptor role's schema, which is
// a generic identity contract — any software implementing the role can
// return one. Do not add OB-specific fields (like supported spec range)
// here; they belong on OB-specific roles or are derivable from the OBI
// document itself.
type SoftwareInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Maintainer  string `json:"maintainer,omitempty"`
}

// RenderSoftwareInfo returns a human-friendly styled representation of
// SoftwareInfo. Pure renderer: no ob-specific extras. Use RenderObInfo
// when rendering ob's own info.
func RenderSoftwareInfo(sw SoftwareInfo) string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render(sw.Name))
	if sw.Version != "" {
		sb.WriteString(s.Dim.Render(" v" + sw.Version))
	}

	if sw.Description != "" {
		sb.WriteString("\n")
		sb.WriteString(sw.Description)
	}

	if sw.Homepage != "" || sw.Repository != "" || sw.Maintainer != "" {
		sb.WriteString("\n")
	}

	if sw.Maintainer != "" {
		sb.WriteString("\n  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Dim.Render("Maintainer: "))
		sb.WriteString(sw.Maintainer)
	}

	if sw.Homepage != "" {
		sb.WriteString("\n  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Dim.Render("Homepage:   "))
		sb.WriteString(s.Key.Render(sw.Homepage))
	}

	if sw.Repository != "" {
		sb.WriteString("\n  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Dim.Render("Repository: "))
		sb.WriteString(s.Key.Render(sw.Repository))
	}

	return sb.String()
}

// RenderObInfo renders ob's own SoftwareInfo with the supported spec
// version range appended. The range comes from the SDK's
// MinSupportedVersion / MaxTestedVersion constants, not from any
// wire-level field. Use RenderSoftwareInfo for rendering generic
// SoftwareInfo values from other sources.
func RenderObInfo(sw SoftwareInfo) string {
	s := Styles
	out := RenderSoftwareInfo(sw)
	var sb strings.Builder
	sb.WriteString(out)
	sb.WriteString("\n  ")
	sb.WriteString(s.Bullet.Render("•"))
	sb.WriteString(" ")
	sb.WriteString(s.Dim.Render("Spec:       "))
	sb.WriteString(formatSpecRange())
	return sb.String()
}

// formatSpecRange returns "0.1.0" when min == max, "0.1.0..0.2.0" otherwise.
func formatSpecRange() string {
	min, max := openbindings.SupportedRange()
	if min == max {
		return min
	}
	return fmt.Sprintf("%s..%s", min, max)
}

// Info returns ob's own software identity and metadata.
func Info() SoftwareInfo {
	return SoftwareInfo{
		Name:        "OpenBindings CLI",
		Version:     OBVersion,
		Description: "Reference implementation for creating, browsing, and executing OpenBindings interfaces.",
		Homepage:    "https://openbindings.com",
		Repository:  "https://github.com/openbindings/ob",
		Maintainer:  "OpenBindings Project",
	}
}
