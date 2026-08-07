package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestOAuthThemePinsDesignColorRevision(t *testing.T) {
	if oauthThemeDesignRevision != "openbindings/design@ed8a409" {
		t.Fatalf("unexpected Design theme revision %q", oauthThemeDesignRevision)
	}
	var rendered bytes.Buffer
	if err := oauthPages.ExecuteTemplate(&rendered, "oauth-shared-css", nil); err != nil {
		t.Fatalf("render OAuth theme: %v", err)
	}
	css := rendered.String()
	required := []string{
		"--color-active: #111111",
		"--color-active-contrast: #ffffff",
		"--color-error: #b83b32",
		"--color-active: #e5e5e5",
		"--color-active-contrast: #0a0a0a",
		"--color-error: #ff9187",
		"@media (forced-colors: active)",
		"--color-active: Highlight",
		"--color-active-contrast: HighlightText",
	}
	for _, fragment := range required {
		if !strings.Contains(css, fragment) {
			t.Errorf("OAuth theme is missing %q", fragment)
		}
	}
}
