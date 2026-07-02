package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// writeInterface writes a minimal OpenBindings interface JSON to a temp file.
func writeInterface(t *testing.T, dir, name string, iface map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(iface, "", "  ")
	if err != nil {
		t.Fatalf("marshal interface %s: %v", name, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write interface %s: %v", name, err)
	}
	return path
}

// minimalInterface returns a minimal valid OpenBindings interface map.
func minimalInterface(ops map[string]any) map[string]any {
	return map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations":   ops,
	}
}

// ifaceFromMap builds an in-memory interface from a raw map via JSON round-trip.
func ifaceFromMap(t *testing.T, m map[string]any) *openbindings.Interface {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var iface openbindings.Interface
	if err := json.Unmarshal(data, &iface); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &iface
}

// --- reportCompatibility wire lane (v1 report over inline documents) ---

// TestComparisonCheck_DocIn drives the served operation's lane: inline
// documents instead of locators, straight into the v1 report.
func TestComparisonCheck_DocIn(t *testing.T) {
	m := minimalInterface(map[string]any{
		"greet": map[string]any{"input": map[string]any{"type": "object"}},
	})

	report := ComparisonCheck(ComparisonInput{
		LeftInterface:  ifaceFromMap(t, m),
		RightInterface: ifaceFromMap(t, m),
	})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if report.FormatVersion != "ob-comparison-report/v1" {
		t.Errorf("format_version = %q", report.FormatVersion)
	}
	if report.Summary.Verdict != "compatible" {
		t.Errorf("verdict = %q, want compatible", report.Summary.Verdict)
	}
	if len(report.Operations) != 1 {
		t.Fatalf("expected 1 operation delta, got %d", len(report.Operations))
	}

	// A missing operation on the right must break the verdict.
	report = ComparisonCheck(ComparisonInput{
		LeftInterface: ifaceFromMap(t, minimalInterface(map[string]any{
			"greet":    map[string]any{},
			"farewell": map[string]any{},
		})),
		RightInterface: ifaceFromMap(t, minimalInterface(map[string]any{
			"greet": map[string]any{},
		})),
	})
	if report.Summary.Verdict == "compatible" {
		t.Error("expected verdict != compatible when an operation is missing")
	}
}

func TestComparisonCheck_ResolveError(t *testing.T) {
	report := ComparisonCheck(ComparisonInput{
		Left:  "/nonexistent/target.json",
		Right: "/nonexistent/candidate.json",
	})

	if report.Error == nil {
		t.Fatal("expected resolve error")
	}
	if report.Error.Code != "resolve_error" {
		t.Errorf("expected code 'resolve_error', got %q", report.Error.Code)
	}
	if report.Summary.Verdict != "indeterminate" {
		t.Errorf("verdict = %q, want indeterminate", report.Summary.Verdict)
	}
}

// contains is a simple substring check for test assertions.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
