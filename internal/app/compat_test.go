package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
		"id":          "test",
		"operations":  ops,
	}
}

func TestCompatibilityCheck_IdenticalInterfaces(t *testing.T) {
	dir := t.TempDir()

	iface := minimalInterface(map[string]any{
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

	target := writeInterface(t, dir, "target.json", iface)
	candidate := writeInterface(t, dir, "candidate.json", iface)

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if !report.Compatible {
		t.Errorf("expected compatible, got incompatible")
	}
	if len(report.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(report.Operations))
	}

	op := report.Operations[0]
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

func TestCompatibilityCheck_MissingOperation(t *testing.T) {
	dir := t.TempDir()

	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
		"farewell": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	}))

	candidate := writeInterface(t, dir, "candidate.json", minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	}))

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if report.Compatible {
		t.Error("expected incompatible (missing operation)")
	}
	// 1 of 2 matched and compatible → partial conformance.
	if report.Conformance != ConformancePartial {
		t.Errorf("expected conformance=partial, got %q", report.Conformance)
	}

	// Find the missing operation.
	var found bool
	for _, op := range report.Operations {
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

func TestCompatibilityCheck_PartialConformance(t *testing.T) {
	dir := t.TempDir()

	// Target has 3 operations; candidate only provides 2 (both compatible).
	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
		"farewell": map[string]any{
			"input": map[string]any{"type": "object"},
		},
		"wave": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	}))

	candidate := writeInterface(t, dir, "candidate.json", minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
		"farewell": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	}))

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if report.Compatible {
		t.Error("expected not fully compatible (missing 1 operation)")
	}
	if report.Conformance != ConformancePartial {
		t.Errorf("expected conformance=partial, got %q", report.Conformance)
	}
	if report.Coverage.Total != 3 {
		t.Errorf("expected total=3, got %d", report.Coverage.Total)
	}
	if report.Coverage.Matched != 2 {
		t.Errorf("expected matched=2, got %d", report.Coverage.Matched)
	}
	if report.Coverage.Compatible != 2 {
		t.Errorf("expected compatible=2, got %d", report.Coverage.Compatible)
	}
	if report.Coverage.Incompatible != 0 {
		t.Errorf("expected incompatible=0, got %d", report.Coverage.Incompatible)
	}
}

func TestCompatibilityCheck_FullConformance(t *testing.T) {
	dir := t.TempDir()

	iface := minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	})

	target := writeInterface(t, dir, "target.json", iface)
	candidate := writeInterface(t, dir, "candidate.json", iface)

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Conformance != ConformanceFull {
		t.Errorf("expected conformance=full, got %q", report.Conformance)
	}
	if !report.Compatible {
		t.Error("expected compatible=true for full conformance")
	}
}

func TestCompatibilityCheck_NoneConformance(t *testing.T) {
	dir := t.TempDir()

	// Target requires "name" (string), candidate has "name" (integer) → incompatible match.
	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
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

	candidate := writeInterface(t, dir, "candidate.json", minimalInterface(map[string]any{
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

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Conformance != ConformanceNone {
		t.Errorf("expected conformance=none, got %q", report.Conformance)
	}
	if report.Compatible {
		t.Error("expected compatible=false")
	}
	if report.Coverage.Incompatible != 1 {
		t.Errorf("expected incompatible=1, got %d", report.Coverage.Incompatible)
	}
}

func TestCompatibilityCheck_IncompatibleInputSchema(t *testing.T) {
	dir := t.TempDir()

	// Target requires "name" (string), candidate requires "name" (integer).
	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
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

	candidate := writeInterface(t, dir, "candidate.json", minimalInterface(map[string]any{
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

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Compatible {
		t.Error("expected incompatible (type mismatch in input)")
	}

	op := report.Operations[0]
	if op.Input != SlotIncompatible {
		t.Errorf("expected input=incompatible, got %q", op.Input)
	}
	if len(op.Details) == 0 {
		t.Error("expected details about input incompatibility")
	}
}

func TestCompatibilityCheck_UnspecifiedSlots(t *testing.T) {
	dir := t.TempDir()

	// Method with no input or output schemas — both slots should be unspecified.
	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"ping": map[string]any{},
	}))

	candidate := writeInterface(t, dir, "candidate.json", minimalInterface(map[string]any{
		"ping": map[string]any{},
	}))

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if !report.Compatible {
		t.Error("expected compatible (all slots unspecified)")
	}

	op := report.Operations[0]
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

func TestCompatibilityCheck_ResolveError(t *testing.T) {
	report := CompatibilityCheck(CompatInput{
		Target:    "/nonexistent/target.json",
		Candidate: "/nonexistent/candidate.json",
	})

	if report.Error == nil {
		t.Fatal("expected resolve error")
	}
	if report.Error.Code != "resolve_error" {
		t.Errorf("expected code 'resolve_error', got %q", report.Error.Code)
	}
}

func TestCompatibilityCheck_JSONOutput(t *testing.T) {
	dir := t.TempDir()

	iface := minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{"type": "object"},
		},
	})

	target := writeInterface(t, dir, "target.json", iface)
	candidate := writeInterface(t, dir, "candidate.json", iface)

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	// Verify the report serializes to valid JSON.
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal report: %v", err)
	}

	// Verify it round-trips.
	var decoded CompatibilityReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal report: %v", err)
	}
	if decoded.Compatible != report.Compatible {
		t.Error("round-trip changed Compatible field")
	}
	if len(decoded.Operations) != len(report.Operations) {
		t.Error("round-trip changed Operations count")
	}
}

func TestCompatibilityCheck_NormalizationErrorSurfaced(t *testing.T) {
	dir := t.TempDir()

	// Target uses an outside-profile keyword (pattern).
	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type":    "object",
				"pattern": "^[a-z]+$",
			},
		},
	}))

	candidate := writeInterface(t, dir, "candidate.json", minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type": "object",
			},
		},
	}))

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	op := report.Operations[0]
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

// TestCompatibilityCheck_SatisfiesMatch verifies that satisfies/roles
// declarations are used as the preferred matching mechanism.
func TestCompatibilityCheck_SatisfiesMatch(t *testing.T) {
	dir := t.TempDir()

	// Target interface has operation "listPets".
	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"listPets": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer"},
				},
			},
		},
	}))

	// Candidate uses a DIFFERENT key but declares satisfies.
	targetPath := filepath.Join(dir, "target.json")
	candidateMap := map[string]any{
		"openbindings": "0.1.0",
		"id":           "candidate",
		"roles": map[string]any{
			"pet-api": targetPath,
		},
		"operations": map[string]any{
			"fetchAnimals": map[string]any{
				"satisfies": []any{
					map[string]any{
						"role":      "pet-api",
						"operation": "listPets",
					},
				},
				"input": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{"type": "integer"},
					},
				},
			},
		},
	}
	candidate := writeInterface(t, dir, "candidate.json", candidateMap)

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	if len(report.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(report.Operations))
	}

	op := report.Operations[0]
	if !op.Matched {
		t.Error("expected listPets to match via satisfies declaration, but it was not matched")
	}
	if !op.Compatible {
		t.Errorf("expected compatible, got incompatible: %v", op.Details)
	}
}

// TestCompatibilityCheck_SatisfiesMatchByAlias verifies that satisfies can
// reference a target operation by alias.
func TestCompatibilityCheck_SatisfiesMatchByAlias(t *testing.T) {
	dir := t.TempDir()

	targetMap := map[string]any{
		"openbindings": "0.1.0",
		"id":           "target",
		"operations": map[string]any{
			"listPets": map[string]any{
				"aliases": []any{"getPets"},
				"input": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{"type": "integer"},
					},
				},
			},
		},
	}
	target := writeInterface(t, dir, "target.json", targetMap)

	candidateMap := map[string]any{
		"openbindings": "0.1.0",
		"id":           "candidate",
		"roles": map[string]any{
			"pet-api": filepath.Join(dir, "target.json"),
		},
		"operations": map[string]any{
			"fetchAnimals": map[string]any{
				"satisfies": []any{
					map[string]any{
						"role":      "pet-api",
						"operation": "getPets", // alias, not primary key
					},
				},
				"input": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{"type": "integer"},
					},
				},
			},
		},
	}
	candidate := writeInterface(t, dir, "candidate.json", candidateMap)

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	op := report.Operations[0]
	if !op.Matched {
		t.Error("expected listPets to match via satisfies referencing alias 'getPets'")
	}
}

// TestCompatibilityCheck_SatisfiesNoRoleFallsBack verifies that when
// satisfies references a role key that doesn't match the target,
// the algorithm falls back to key/alias matching.
func TestCompatibilityCheck_SatisfiesNoRoleFallsBack(t *testing.T) {
	dir := t.TempDir()

	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"listPets": map[string]any{},
	}))

	candidateMap := map[string]any{
		"openbindings": "0.1.0",
		"id":           "candidate",
		"roles": map[string]any{
			"other-api": "/some/other/path.json", // does NOT match target
		},
		"operations": map[string]any{
			// Same key — should match by fallback
			"listPets": map[string]any{
				"satisfies": []any{
					map[string]any{
						"role":      "other-api",
						"operation": "listPets",
					},
				},
			},
		},
	}
	candidate := writeInterface(t, dir, "candidate.json", candidateMap)

	report := CompatibilityCheck(CompatInput{Target: target, Candidate: candidate})

	if report.Error != nil {
		t.Fatalf("unexpected error: %v", report.Error.Message)
	}
	op := report.Operations[0]
	if !op.Matched {
		t.Error("expected listPets to match via key fallback even though satisfies points elsewhere")
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
