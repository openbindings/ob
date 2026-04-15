package app

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Styles defines the visual styles used across CLI output.
// These are initialized once and respect terminal capabilities.
var Styles = initStyles()

type styles struct {
	// Headers and titles
	Header lipgloss.Style

	// Key items (tokens, identifiers, commands)
	Key lipgloss.Style

	// Descriptions and secondary text
	Dim lipgloss.Style

	// Success indicators
	Success lipgloss.Style

	// Warning indicators
	Warning lipgloss.Style

	// Error indicators
	Error lipgloss.Style

	// Diff: added items
	Added lipgloss.Style

	// Diff: removed items
	Removed lipgloss.Style

	// Bullet point
	Bullet lipgloss.Style
}

func initStyles() styles {
	// Check if we should use color
	// Respect NO_COLOR env var (https://no-color.org/)
	noColor := os.Getenv("NO_COLOR") != ""

	if noColor {
		return styles{
			Header:  lipgloss.NewStyle(),
			Key:     lipgloss.NewStyle(),
			Dim:     lipgloss.NewStyle(),
			Success: lipgloss.NewStyle(),
			Warning: lipgloss.NewStyle(),
			Error:   lipgloss.NewStyle(),
			Added:   lipgloss.NewStyle(),
			Removed: lipgloss.NewStyle(),
			Bullet:  lipgloss.NewStyle(),
		}
	}

	return styles{
		Header:  lipgloss.NewStyle().Bold(true),
		Key:     lipgloss.NewStyle().Foreground(lipgloss.Color("6")),  // Cyan
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),  // Gray
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),  // Green
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),  // Yellow
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")),  // Red
		Added:   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),  // Green
		Removed: lipgloss.NewStyle().Foreground(lipgloss.Color("1")),  // Red
		Bullet:  lipgloss.NewStyle().Foreground(lipgloss.Color("8")),  // Gray
	}
}
