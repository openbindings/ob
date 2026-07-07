package cmd

import (
	"strings"
	"testing"
)

// The data-face flags refuse typos loudly (a misspelled lane or channel can
// never silently change behavior); valid inputs compile per-axis.
func TestBuildInvokeConfig(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		c, err := buildInvokeConfig("json", "0,1", []string{"source=stdin-dash", "extra=file"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Decode != "json" {
			t.Errorf("Decode = %q", c.Decode)
		}
		if len(c.OKExits) != 2 || c.OKExits[0] != 0 || c.OKExits[1] != 1 {
			t.Errorf("OKExits = %v", c.OKExits)
		}
		if c.Routes["source"] != "stdin-dash" || c.Routes["extra"] != "file" {
			t.Errorf("Routes = %v", c.Routes)
		}
	})

	t.Run("unknown decode lane refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("jsonn", "", nil); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Errorf("expected --decode refusal, got %v", err)
		}
	})

	t.Run("unknown channel refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("", "", []string{"f=pipe"}); err == nil || !strings.Contains(err.Error(), "channel") {
			t.Errorf("expected --route channel refusal, got %v", err)
		}
	})

	t.Run("malformed route refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("", "", []string{"noequals"}); err == nil {
			t.Errorf("expected field=channel refusal, got %v", err)
		}
	})

	t.Run("non-integer exit refused", func(t *testing.T) {
		if _, err := buildInvokeConfig("", "0,x", nil); err == nil || !strings.Contains(err.Error(), "ok-exit") {
			t.Errorf("expected --ok-exit refusal, got %v", err)
		}
	})
}

// The --input house grammar: inline JSON, @file, and - (stdin). Inline
// invalid JSON is refused.
func TestReadInvokeInput_Inline(t *testing.T) {
	v, err := readInvokeInput(`{"limit":10}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok || m["limit"] != float64(10) {
		t.Errorf("parsed = %#v", v)
	}

	if _, err := readInvokeInput("{bad"); err == nil {
		t.Error("expected invalid inline JSON to be refused")
	}

	if v, err := readInvokeInput(""); err != nil || v != nil {
		t.Errorf("empty input should be nil/no-error, got %v / %v", v, err)
	}
}
