package app

import (
	"testing"
)

// alwaysYes is a confirm callback that accepts every prompted change.
func alwaysYes(string, string) bool { return true }

// TestConform_ScaffoldsMissingOperation verifies that an operation present in
// the reference interface but absent from the target is scaffolded into the
// target keyed by the contract operation name, with its schemas copied.
func TestConform_ScaffoldsMissingOperation(t *testing.T) {
	dir := t.TempDir()

	contract := minimalInterface(map[string]any{
		"set": map[string]any{
			"description": "Store a value",
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":   map[string]any{"type": "string"},
					"value": map[string]any{"type": "string"},
				},
				"required": []any{"key", "value"},
			},
		},
	})
	contractPath := writeInterface(t, dir, "contract.json", contract)
	targetPath := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{}))

	out := Conform(ConformInput{
		InterfaceLocator: contractPath,
		TargetPath:       targetPath,
		Yes:              true,
	}, alwaysYes)

	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error.Message)
	}
	if !out.Modified {
		t.Error("expected target to be modified")
	}
	if len(out.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(out.Actions))
	}
	if out.Actions[0].Operation != "set" || out.Actions[0].Action != "scaffold" {
		t.Errorf("expected scaffold of 'set', got %s/%s", out.Actions[0].Operation, out.Actions[0].Action)
	}

	// The scaffolded operation is keyed by the contract name, with copied schema.
	loaded, err := loadInterfaceFile(targetPath)
	if err != nil {
		t.Fatalf("load target: %v", err)
	}
	op, ok := loaded.Operations["set"]
	if !ok {
		t.Fatal("expected scaffolded operation keyed 'set'")
	}
	if op.Description != "Store a value" {
		t.Errorf("description = %q, want copied from contract", op.Description)
	}
	if op.Input == nil {
		t.Error("expected input schema copied from contract")
	}
}

// TestConform_ReplaceAddsAliasWhenKeysDiffer verifies that when the target
// satisfies a contract operation under a different key (matched via the
// contract operation's alias) but with a drifted schema, conform replaces the
// schema AND declares the correspondence by appending the contract operation
// name as an alias (OBI-T-12).
func TestConform_ReplaceAddsAliasWhenKeysDiffer(t *testing.T) {
	dir := t.TempDir()

	// Contract op "set" aliases "store" so it resolves to the target's "store".
	contract := minimalInterface(map[string]any{
		"set": map[string]any{
			"aliases": []any{"store"},
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
				"required": []any{"key"},
			},
		},
	})
	// Target op keyed "store" with a drifted (incompatible) input: it requires a
	// field the contract caller never sends, and carries no alias yet.
	target := minimalInterface(map[string]any{
		"store": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"identifier": map[string]any{"type": "integer"},
				},
				"required": []any{"identifier"},
			},
		},
	})
	contractPath := writeInterface(t, dir, "contract.json", contract)
	targetPath := writeInterface(t, dir, "target.json", target)

	out := Conform(ConformInput{
		InterfaceLocator: contractPath,
		TargetPath:       targetPath,
		Yes:              true,
	}, alwaysYes)

	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error.Message)
	}
	if !out.Modified {
		t.Error("expected target to be modified")
	}
	if len(out.Actions) != 1 || out.Actions[0].Action != "replace" {
		t.Fatalf("expected a single replace action, got %+v", out.Actions)
	}

	loaded, err := loadInterfaceFile(targetPath)
	if err != nil {
		t.Fatalf("load target: %v", err)
	}

	// Replacement happens in place under the original key, not a new "set" key.
	if _, ok := loaded.Operations["set"]; ok {
		t.Error("conform should not create a new 'set' key; it replaces under 'store'")
	}
	op, ok := loaded.Operations["store"]
	if !ok {
		t.Fatal("expected operation to remain keyed 'store'")
	}

	// The contract name is now declared as an alias on the matched operation.
	if !containsString(op.Aliases, "set") {
		t.Errorf("expected alias 'set' appended to declare correspondence, got %v", op.Aliases)
	}

	// The schema was replaced with the contract's (now requires "key", not "identifier").
	props, _ := op.Input.(map[string]any)["properties"].(map[string]any)
	if _, ok := props["key"]; !ok {
		t.Errorf("expected input schema replaced with contract's (key property), got %v", op.Input)
	}
}

// TestConform_CompatibleReportsNoChange verifies that an operation already
// present and compatible is reported as "compatible" with no modification.
func TestConform_CompatibleReportsNoChange(t *testing.T) {
	dir := t.TempDir()

	op := map[string]any{
		"set": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
				"required": []any{"key"},
			},
		},
	}
	contractPath := writeInterface(t, dir, "contract.json", minimalInterface(op))
	targetPath := writeInterface(t, dir, "target.json", minimalInterface(op))

	out := Conform(ConformInput{
		InterfaceLocator: contractPath,
		TargetPath:       targetPath,
		Yes:              true,
	}, alwaysYes)

	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error.Message)
	}
	if out.Modified {
		t.Error("expected no modification for an already-conformant target")
	}
	if len(out.Actions) != 1 || out.Actions[0].Action != "compatible" {
		t.Fatalf("expected a single compatible action, got %+v", out.Actions)
	}
}

// containsString reports whether s is present in xs.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// TestConform_CompatibleViaAlias verifies pairing through the target's alias:
// an operation under a different key that carries the contract name as an
// alias, with a matching schema, reports "compatible" — no scaffold.
func TestConform_CompatibleViaAlias(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{"type": "string"},
		},
		"required": []any{"key"},
	}
	contract := ifaceFromMap(t, minimalInterface(map[string]any{
		"set": map[string]any{"input": schema},
	}))
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"store": map[string]any{
			"aliases": []any{"set"},
			"input":   schema,
		},
	}))

	out := Conform(ConformInput{
		ContractInterface: contract,
		TargetInterface:   target,
		Yes:               true,
	}, alwaysYes)

	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error.Message)
	}
	if out.Modified {
		t.Error("expected no modification (satisfied via alias)")
	}
	if len(out.Actions) != 1 || out.Actions[0].Action != "compatible" {
		t.Fatalf("expected a single compatible action, got %+v", out.Actions)
	}
}

// TestConform_ReplaceDetailsIncompatible verifies that a paired-but-drifted
// operation is offered for replace, with details naming the failing slot.
func TestConform_ReplaceDetailsIncompatible(t *testing.T) {
	contract := ifaceFromMap(t, minimalInterface(map[string]any{
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
	// Target requires a field the contract caller never sends.
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"tone": map[string]any{"type": "string"},
				},
				"required": []any{"name", "tone"},
			},
		},
	}))

	out := Conform(ConformInput{
		ContractInterface: contract,
		TargetInterface:   target,
		Yes:               true,
	}, alwaysYes)

	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error.Message)
	}
	if len(out.Actions) != 1 || out.Actions[0].Action != "replace" {
		t.Fatalf("expected a single replace action, got %+v", out.Actions)
	}
	if !contains(out.Actions[0].Details, "input") || !contains(out.Actions[0].Details, "incompatible") {
		t.Errorf("expected details naming the incompatible input slot, got %q", out.Actions[0].Details)
	}
	// Doc-in lane: the conformed document rides Result, with the schema replaced.
	if out.Result == nil {
		t.Fatal("expected conformed document in Result for doc-in conform")
	}
	props, _ := out.Result.Operations["greet"].Input.(map[string]any)["properties"].(map[string]any)
	if _, ok := props["tone"]; ok {
		t.Error("expected input schema replaced with the contract's (no tone property)")
	}
}

// TestConform_NoSchemasCompatible verifies that operations with no input or
// output schemas on either side count as conformant (unspecified slots).
func TestConform_NoSchemasCompatible(t *testing.T) {
	m := minimalInterface(map[string]any{"ping": map[string]any{}})

	out := Conform(ConformInput{
		ContractInterface: ifaceFromMap(t, m),
		TargetInterface:   ifaceFromMap(t, m),
		Yes:               true,
	}, alwaysYes)

	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error.Message)
	}
	if len(out.Actions) != 1 || out.Actions[0].Action != "compatible" {
		t.Fatalf("expected a single compatible action, got %+v", out.Actions)
	}
}

// TestConform_UnverifiedTreatedAsDrift verifies that a slot the engine cannot
// verify (differing regex patterns → unverified.regex_containment) is treated
// as drift, not silently reported compatible.
func TestConform_UnverifiedTreatedAsDrift(t *testing.T) {
	contract := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type":    "string",
				"pattern": "^[a-z]+$",
			},
		},
	}))
	target := ifaceFromMap(t, minimalInterface(map[string]any{
		"greet": map[string]any{
			"input": map[string]any{
				"type":    "string",
				"pattern": "^[a-zA-Z]+$",
			},
		},
	}))

	out := Conform(ConformInput{
		ContractInterface: contract,
		TargetInterface:   target,
		Yes:               true,
	}, alwaysYes)

	if out.Error != nil {
		t.Fatalf("unexpected error: %v", out.Error.Message)
	}
	if len(out.Actions) != 1 || out.Actions[0].Action != "replace" {
		t.Fatalf("expected a single replace action for the unverified slot, got %+v", out.Actions)
	}
	if !contains(out.Actions[0].Details, "unverified") {
		t.Errorf("expected details mentioning the unverified verdict, got %q", out.Actions[0].Details)
	}
}
