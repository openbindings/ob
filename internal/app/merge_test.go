package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/openbindings-go"
)

func TestMerge_AddOperation(t *testing.T) {
	dir := t.TempDir()

	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))
	source := writeInterface(t, dir, "source.json", minimalInterface(map[string]any{
		"greet":   map[string]any{},
		"goodbye": map[string]any{},
	}))

	result, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		All:           true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Applied != 1 {
		t.Errorf("expected 1 applied, got %d", result.Applied)
	}

	// Verify the operation was added to the file.
	iface, err := loadInterfaceFile(target)
	if err != nil {
		t.Fatalf("reload target: %v", err)
	}
	if _, ok := iface.Operations["goodbye"]; !ok {
		t.Error("expected 'goodbye' operation in target")
	}
}

func TestMerge_UpdateOperation_PreservesUserFields(t *testing.T) {
	dir := t.TempDir()

	// Target has description and aliases (user-authored fields).
	targetData := map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"greet": map[string]any{
				"description": "My custom description",
				"input": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	target := writeInterface(t, dir, "target.json", targetData)

	// Source has updated input schema.
	sourceData := map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"greet": map[string]any{
				"input": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"email": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	source := writeInterface(t, dir, "source.json", sourceData)

	result, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		All:           true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Applied != 1 {
		t.Errorf("expected 1 applied (update), got %d", result.Applied)
	}

	// Verify description was preserved.
	iface, err := loadInterfaceFile(target)
	if err != nil {
		t.Fatalf("reload target: %v", err)
	}
	op := iface.Operations["greet"]
	if op.Description != "My custom description" {
		t.Errorf("expected description preserved, got %q", op.Description)
	}
	// Verify input schema was updated (has email property).
	if op.Input == nil {
		t.Fatal("expected input schema")
	}
	props, ok := op.Input["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in input")
	}
	if _, hasEmail := props["email"]; !hasEmail {
		t.Error("expected 'email' property from source schema")
	}
}

func TestMerge_DryRun_DoesNotWrite(t *testing.T) {
	dir := t.TempDir()

	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))
	source := writeInterface(t, dir, "source.json", minimalInterface(map[string]any{
		"greet":   map[string]any{},
		"goodbye": map[string]any{},
	}))

	// Read original content.
	origData, _ := os.ReadFile(target)

	result, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		All:           true,
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.DryRun {
		t.Error("expected DryRun flag in output")
	}

	// Verify file was NOT modified.
	newData, _ := os.ReadFile(target)
	if string(origData) != string(newData) {
		t.Error("dry-run should not modify the file")
	}
}

func TestMerge_RemoveBinding(t *testing.T) {
	dir := t.TempDir()

	// Target has an operation bound to a source.
	targetData := map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"operations": map[string]any{
			"greet":   map[string]any{},
			"goodbye": map[string]any{},
		},
		"bindings": map[string]any{
			"goodbye.mySource": map[string]any{"operation": "goodbye", "source": "mySource", "ref": "bye"},
		},
	}
	target := writeInterface(t, dir, "target.json", targetData)

	// Source only has greet — goodbye was removed.
	source := writeInterface(t, dir, "source.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))

	result, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		All:           true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify binding was removed but operation is preserved.
	iface, err := loadInterfaceFile(target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := iface.Operations["goodbye"]; !ok {
		t.Error("operation should be preserved after binding removal")
	}
	for _, b := range iface.Bindings {
		if b.Operation == "goodbye" {
			t.Error("binding for 'goodbye' should have been removed")
		}
	}

	// Should have applied the remove.
	hasRemove := false
	for _, e := range result.Entries {
		if e.Operation == "goodbye" && e.Action == MergeUnbind && e.Applied {
			hasRemove = true
		}
	}
	if !hasRemove {
		t.Error("expected remove entry for 'goodbye'")
	}
}

func TestMerge_AtomicWrite(t *testing.T) {
	dir := t.TempDir()

	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))
	source := writeInterface(t, dir, "source.json", minimalInterface(map[string]any{
		"greet":   map[string]any{},
		"goodbye": map[string]any{},
	}))

	_, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		All:           true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the file is valid JSON.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestMerge_OutPath(t *testing.T) {
	dir := t.TempDir()

	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))
	source := writeInterface(t, dir, "source.json", minimalInterface(map[string]any{
		"greet":   map[string]any{},
		"goodbye": map[string]any{},
	}))

	outPath := filepath.Join(dir, "output.json")

	_, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		All:           true,
		OutPath:       outPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output file exists.
	iface, err := loadInterfaceFile(outPath)
	if err != nil {
		t.Fatalf("load output: %v", err)
	}
	if _, ok := iface.Operations["goodbye"]; !ok {
		t.Error("expected 'goodbye' in output file")
	}
}

func TestMerge_RefMigration(t *testing.T) {
	dir := t.TempDir()

	// Target has no schemas.
	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))

	// Source has a schema and an operation that references it.
	sourceData := map[string]any{
		"openbindings": "0.1.0",
		"id":           "test",
		"schemas": map[string]any{
			"Greeting": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
			},
		},
		"operations": map[string]any{
			"greet": map[string]any{
				"output": map[string]any{
					"$ref": "#/schemas/Greeting",
				},
			},
			"farewell": map[string]any{
				"output": map[string]any{
					"$ref": "#/schemas/Greeting",
				},
			},
		},
	}
	source := writeInterface(t, dir, "source.json", sourceData)

	_, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		All:           true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify schemas were migrated.
	iface, err := loadInterfaceFile(target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if iface.Schemas == nil {
		t.Fatal("expected schemas pool in target")
	}
	if _, ok := iface.Schemas["Greeting"]; !ok {
		t.Error("expected 'Greeting' schema migrated to target")
	}
}

func TestMerge_PromptFunc(t *testing.T) {
	dir := t.TempDir()

	target := writeInterface(t, dir, "target.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))
	source := writeInterface(t, dir, "source.json", minimalInterface(map[string]any{
		"greet":   map[string]any{},
		"goodbye": map[string]any{},
		"hello":   map[string]any{},
	}))

	// Accept only "goodbye", reject "hello".
	promptCalls := 0
	result, err := Merge(MergeInput{
		TargetPath:    target,
		SourceLocator: source,
		PromptFunc: func(entry MergeEntry) (bool, error) {
			promptCalls++
			return entry.Operation == "goodbye", nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if promptCalls != 2 {
		t.Errorf("expected 2 prompt calls, got %d", promptCalls)
	}
	if result.Applied != 1 {
		t.Errorf("expected 1 applied, got %d", result.Applied)
	}

	// Verify only goodbye was added.
	iface, err := loadInterfaceFile(target)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := iface.Operations["goodbye"]; !ok {
		t.Error("expected 'goodbye' to be added")
	}
	if _, ok := iface.Operations["hello"]; ok {
		t.Error("expected 'hello' to NOT be added")
	}
}

func TestMerge_RenderOutput(t *testing.T) {
	output := MergeOutput{
		Entries: []MergeEntry{
			{Operation: "greet", Action: MergeSkip},
			{Operation: "goodbye", Action: MergeAdd, Applied: true},
			{Operation: "update", Action: MergeUpdate, Applied: true, Details: []string{"input schema differs"}},
		},
		Applied: 2,
		Skipped: 1,
	}

	rendered := output.Render()
	if rendered == "" {
		t.Error("expected non-empty render output")
	}
}

func TestCollectRefs(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user": map[string]any{
				"$ref": "#/schemas/User",
			},
			"address": map[string]any{
				"$ref": "#/schemas/Address",
			},
			"name": map[string]any{
				"type": "string",
			},
		},
		"items": map[string]any{
			"$ref": "#/schemas/Item",
		},
	}

	refs := collectRefs(schema)
	expected := map[string]bool{"User": true, "Address": true, "Item": true}
	for _, ref := range refs {
		if !expected[ref] {
			t.Errorf("unexpected ref %q", ref)
		}
		delete(expected, ref)
	}
	for ref := range expected {
		t.Errorf("missing ref %q", ref)
	}
}

func TestCollectRefs_Nil(t *testing.T) {
	refs := collectRefs(nil)
	if len(refs) != 0 {
		t.Errorf("expected no refs for nil schema, got %d", len(refs))
	}
}

// Suppress unused import warning for openbindings.
var _ openbindings.Interface
