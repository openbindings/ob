package app

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStylesMapDesignRolesToNativeANSI(t *testing.T) {
	if terminalFoundationsDesignRevision != "openbindings/design@3ef2505" {
		t.Fatalf("unexpected Design foundations revision %q", terminalFoundationsDesignRevision)
	}
	t.Setenv("NO_COLOR", "")
	s := initStyles()

	if !s.Header.GetBold() {
		t.Fatal("header should retain terminal-native emphasis")
	}
	want := []struct {
		name  string
		style lipgloss.Style
		color lipgloss.Color
	}{
		{"key", s.Key, "6"},
		{"muted", s.Dim, "8"},
		{"success", s.Success, "2"},
		{"warning", s.Warning, "3"},
		{"danger", s.Error, "1"},
		{"added", s.Added, "2"},
		{"removed", s.Removed, "1"},
		{"bullet", s.Bullet, "8"},
	}
	for _, role := range want {
		t.Run(role.name, func(t *testing.T) {
			if got := role.style.GetForeground(); got != role.color {
				t.Fatalf("foreground = %#v, want ANSI %q", got, role.color)
			}
		})
	}
}

func TestStylesNoColorEmitsNoPresentationAttributes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	s := initStyles()
	all := []lipgloss.Style{
		s.Header, s.Key, s.Dim, s.Success, s.Warning,
		s.Error, s.Added, s.Removed, s.Bullet,
	}
	for i, style := range all {
		if style.GetForeground() != (lipgloss.NoColor{}) || style.GetBold() {
			t.Fatalf("style %d retains presentation under NO_COLOR", i)
		}
	}
}
