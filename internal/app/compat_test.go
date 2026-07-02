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

// --- operation-level compatibility engine (conform's scaffold/replace gate) ---

func TestCompareOps_IdenticalInterfaces(t *testing.T) {
	m := minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []any{"name"},
			},
			"output": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
			},
		},
	})

	reports := compareOps(ifaceFromMap(t, m), ifaceFromMap(t, m))

	if len(reports) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(reports))
	}
	op := reports[0]
	if op.Operation != "greet" {
		t.Errorf("expected operation 'greet', got %q", op.Operation)
	}
	if !op.Matched {
		t.Error("expected operation to be matched")
	}
	if op.Input != SlotCompatible {
		t.Errorf("expected input=compatible, got %q", op.Input)
	}
	if op.Output != SlotCompatible {
		t.Errorf("expected output=compatible, got %q", op.Output)
	}
	if !op.Compatible {
		t.Error("expected operation to be compatible")
	}
}

func TestCompareOps_MissingOperation(t *testing.T) {
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet":    map[string]any{"input": map[string]any{"type": "object"}},
		"farewell": map[string]any{"input": map[string]any{"type": "object"}},
	}))
	candidate := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{"input": map[string]any{"type": "object"}},
	}))

	reports := compareOps(target, candidate)

	var found bool
	for _, op := range reports {
		if op.Operation == "farewell" {
			found = true
			if op.Matched {
				t.Error("expected farewell to be unmatched")
			}
			if op.Compatible {
				t.Error("expected farewell to be incompatible")
			}
		}
	}
	if !found {
		t.Error("farewell operation not in report")
	}
}

func TestCompareOps_IncompatibleInputSchema(t *testing.T) {
	// Target requires "name" (string), candidate requires "name" (integer).
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []any{"name"},
			},
		},
	}))
	candidate := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "integer"},
				},
				"required": []any{"name"},
			},
		},
	}))

	reports := compareOps(target, candidate)

	op := reports[0]
	if op.Input != SlotIncompatible {
		t.Errorf("expected input=incompatible, got %q", op.Input)
	}
	if op.Compatible {
		t.Error("expected operation to be incompatible (type mismatch in input)")
	}
	if len(op.Details) == 0 {
		t.Error("expected details about input incompatibility")
	}
}

func TestCompareOps_UnspecifiedSlots(t *testing.T) {
	// Operation with no input or output schemas — both slots unspecified, and
	// unspecified is compatible.
	m := minimalInterface(map[string]any{"ping": map[string]any{}})

	reports := compareOps(ifaceFromMap(t, m), ifaceFromMap(t, m))

	op := reports[0]
	if op.Input != SlotUnspecified {
		t.Errorf("expected input=unspecified, got %q", op.Input)
	}
	if op.Output != SlotUnspecified {
		t.Errorf("expected output=unspecified, got %q", op.Output)
	}
	if !op.Compatible {
		t.Error("expected operation to be compatible (unspecified is ok)")
	}
}

func TestCompareOps_NormalizationErrorSurfaced(t *testing.T) {
	// Target uses an outside-profile keyword (pattern).
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type":    "object",
				"pattern": "^[a-z]+$",
			},
		},
	}))
	candidate := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	}))

	reports := compareOps(target, candidate)

	op := reports[0]
	if op.Input != SlotIncompatible {
		t.Errorf("expected input=incompatible for outside-profile keyword, got %q", op.Input)
	}
	// Details should mention normalization failure, not just "incompatible".
	if len(op.Details) == 0 {
		t.Fatal("expected details about normalization error")
	}
	found := false
	for _, d := range op.Details {
		if len(d) > 0 && (contains(d, "normalized") || contains(d, "outside profile")) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected detail mentioning normalization failure, got: %v", op.Details)
	}
}

// TestCompareOps_AliasMatch verifies that a candidate operation under a
// different key corresponds to a target operation by carrying the target's
// name as an alias (the spec's correspondence mechanism, OBI-T-12).
func TestCompareOps_AliasMatch(t *testing.T) {
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"listPets": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer"},
				},
			},
		},
	}))
	// Candidate uses a DIFFERENT key but aliases the target's operation name.
	candidate := ifaceFromMap(t, map[string]any{
		"openbindings": "0.1.0",
		"name":         "candidate",
		"operations": map[string]any{
			"fetchAnimals": map[string]any{
				"aliases": []any{"listPets"},
				"input": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{"type": "integer"},
					},
				},
			},
		},
	})

	reports := compareOps(target, candidate)

	if len(reports) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(reports))
	}
	op := reports[0]
	if !op.Matched {
		t.Error("expected listPets to match via the candidate's alias, but it was not matched")
	}
	if !op.Compatible {
		t.Errorf("expected compatible, got incompatible: %v", op.Details)
	}
}

// TestCompareOps_DirectKeyMatch verifies that a candidate sharing the target's
// operation key matches directly.
func TestCompareOps_DirectKeyMatch(t *testing.T) {
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"listPets": map[string]any{},
	}))
	candidate := ifaceFromMap(t, minimalInterface(map[string]any{
		"listPets": map[string]any{},
	}))

	reports := compareOps(target, candidate)

	if !reports[0].Matched {
		t.Error("expected listPets to match by direct key")
	}
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
