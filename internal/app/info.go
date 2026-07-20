package app

import (
	"fmt"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// SoftwareIdentity contains identity and metadata for a piece of software.
// It mirrors the software-descriptor interface's schema, which is
// a generic identity contract — any software satisfying it can
// return one. Do not add OB-specific fields (like supported spec range)
// here; they belong on OB-specific interfaces or are derivable from the OBI
// document itself.
type SoftwareIdentity struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Maintainer  string `json:"maintainer,omitempty"`
}

// RenderSoftwareIdentity returns a human-friendly styled representation of
// SoftwareIdentity. Pure renderer: no ob-specific extras. Use RenderObInfo
// when rendering ob's own info.
func RenderSoftwareIdentity(sw SoftwareIdentity) string {
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

// RenderObInfo renders ob's own SoftwareIdentity with the supported spec
// version range appended. The range comes from the SDK's
// MinSupportedVersion / MaxTestedVersion constants, not from any
// wire-level field. Use RenderSoftwareIdentity for rendering generic
// SoftwareIdentity values from other sources.
func RenderObInfo(sw SoftwareIdentity) string {
	s := Styles
	out := RenderSoftwareIdentity(sw)
	var sb strings.Builder
	sb.WriteString(out)
	sb.WriteString("\n  ")
	sb.WriteString(s.Bullet.Render("•"))
	sb.WriteString(" ")
	sb.WriteString(s.Dim.Render("Spec:       "))
	sb.WriteString(formatSpecRange())
	return sb.String()
}

// SpecRange returns the supported OpenBindings spec range as prose
// ("0.2.0", or "0.1.0..0.2.0" across versions) for identity surfaces
// (describe, --version).
func SpecRange() string { return formatSpecRange() }

// formatSpecRange returns "0.1.0" when min == max, "0.1.0..0.2.0" otherwise.
func formatSpecRange() string {
	min, max := openbindings.SupportedRange()
	if min == max {
		return min
	}
	return fmt.Sprintf("%s..%s", min, max)
}

// Info returns ob's own software identity and metadata.
func Info() SoftwareIdentity {
	return SoftwareIdentity{
		Name:        "OpenBindings CLI",
		Version:     OBVersion,
		Description: "Reference implementation for authoring, validating, invoking, and serving OpenBindings interfaces.",
		Homepage:    "https://openbindings.com",
		Repository:  "https://github.com/openbindings/ob",
		Maintainer:  "OpenBindings Project",
	}
}
