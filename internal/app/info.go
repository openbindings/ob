package app

import (
	"fmt"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// SoftwareInfo contains identity and metadata for a piece of software.
type SoftwareInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Maintainer  string `json:"maintainer,omitempty"`
	// SpecRange is the range of OpenBindings spec versions this build
	// understands, sourced from the SDK (openbindings-go's
	// MinSupportedVersion / MaxTestedVersion constants). Formatted as
	// "min..max" (or just "min" when min == max).
	SpecRange string `json:"specRange,omitempty"`
}

// RenderSoftwareInfo returns a human-friendly styled representation of SoftwareInfo.
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

	if sw.SpecRange != "" {
		sb.WriteString("\n  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Dim.Render("Spec:       "))
		sb.WriteString(sw.SpecRange)
	}

	return sb.String()
}

// specRange formats the SDK's supported spec range for display.
// "0.1.0" when min == max; "0.1.0..0.2.0" when a genuine range.
func specRange() string {
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
		SpecRange:   specRange(),
	}
}
