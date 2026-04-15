package app

import (
	"strings"
)

// SoftwareInfo contains identity and metadata for a piece of software.
type SoftwareInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Maintainer  string `json:"maintainer,omitempty"`
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

	return sb.String()
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
