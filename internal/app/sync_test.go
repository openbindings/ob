package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSync_FullSync(t *testing.T) {
	dir := t.TempDir()

	// Create a source file.
	srcContent := "bin \"hello\" { }"
	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte(srcContent), 0644)

	contentHash := HashContent([]byte(srcContent))

	// Create an OBI with x-ob metadata on the source.
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "test",
		"operations": map[string]any{
			"hello": map[string]any{"x-ob": map[string]any{}},
		},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":         "./cli.kdl",
					"resolve":     "location",
					"contentHash": contentHash,
					"lastSynced":  "2025-01-01T00:00:00Z",
					"obVersion":   "0.0.1",
				},
			},
		},
		"bindings": map[string]any{
			"hello.usage": map[string]any{
				"operation": "hello",
				"source":    "usage",
				"ref":       "hello",
				"x-ob":      map[string]any{},
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)

	result, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source synced, got %d", len(result.Sources))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
}

func TestSync_PartialSync(t *testing.T) {
	dir := t.TempDir()

	// Create two source files.
	os.WriteFile(filepath.Join(dir, "a.kdl"), []byte("bin a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.kdl"), []byte("bin b"), 0644)

	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"srcA": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./a.kdl",
				"x-ob": map[string]any{
					"ref":     "./a.kdl",
					"resolve": "location",
				},
			},
			"srcB": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./b.kdl",
				"x-ob": map[string]any{
					"ref":     "./b.kdl",
					"resolve": "location",
				},
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)

	// Sync only srcA.
	result, err := Sync(SyncInput{
		OBIPath:    obiPath,
		SourceKeys: []string{"srcA"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sources) != 1 {
		t.Errorf("expected 1 synced, got %d", len(result.Sources))
	}
}

func TestSync_SkipsHandAuthored(t *testing.T) {
	dir := t.TempDir()

	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"manual": map[string]any{
				"format":   "openapi@3.1",
				"location": "https://api.example.com/openapi.json",
				// No x-ob — hand-authored.
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)

	result, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sources) != 0 {
		t.Errorf("expected 0 synced, got %d", len(result.Sources))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

func TestSync_Pure(t *testing.T) {
	dir := t.TempDir()

	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte("bin hello"), 0644)

	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "test",
		"operations": map[string]any{
			"hello": map[string]any{"x-ob": map[string]any{}},
		},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "location",
				},
			},
		},
		"bindings": map[string]any{
			"hello.usage": map[string]any{
				"operation": "hello",
				"source":    "usage",
				"ref":       "hello",
				"x-ob":      map[string]any{},
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)
	purePath := filepath.Join(dir, "pure.json")

	result, err := Sync(SyncInput{
		OBIPath:    obiPath,
		OutputPath: purePath,
		Pure:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pure {
		t.Error("expected pure=true in result")
	}

	// Verify the pure output has no x-ob.
	data, err := os.ReadFile(purePath)
	if err != nil {
		t.Fatalf("read pure output: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	// Check sources have no x-ob.
	sources := parsed["sources"].(map[string]any)
	src := sources["usage"].(map[string]any)
	if _, ok := src["x-ob"]; ok {
		t.Error("expected x-ob stripped from source in pure output")
	}

	// Check operations have no x-ob.
	ops := parsed["operations"].(map[string]any)
	op := ops["hello"].(map[string]any)
	if _, ok := op["x-ob"]; ok {
		t.Error("expected x-ob stripped from operation in pure output")
	}
}

func TestSync_ContentMode(t *testing.T) {
	dir := t.TempDir()

	srcContent := `bin "hello" { cmd "greet" { help "Say hi" } }`
	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte(srcContent), 0644)

	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":  "usage@2.0.0",
				"content": "old content",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "content",
				},
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)

	_, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back and verify content was updated.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	sources := parsed["sources"].(map[string]any)
	src := sources["usage"].(map[string]any)

	content, ok := src["content"].(string)
	if !ok {
		t.Fatalf("expected string content, got %T", src["content"])
	}
	if content != srcContent {
		t.Errorf("expected updated content, got %q", content)
	}
}

func TestSync_NonexistentSourceKey(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "interface.json", minimalInterface(map[string]any{}))

	_, err := Sync(SyncInput{
		OBIPath:    obiPath,
		SourceKeys: []string{"nonexistent"},
	})
	if err == nil {
		t.Error("expected error for nonexistent source key")
	}
}

func TestSync_RegeneratesManagedOperations(t *testing.T) {
	dir := t.TempDir()

	// Create a usage spec with one command.
	kdlContent := `min_usage_version "2.0.0"
bin "hello"
cmd "greet" {
  help "Say hello"
  arg "<name>" help="Who to greet"
}
`
	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte(kdlContent), 0644)

	// Build an OBI with x-ob metadata, including managed operations and bindings.
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "hello",
		"operations": map[string]any{
			"greet": map[string]any{
				"description": "OLD description",
				"x-ob":        map[string]any{},
			},
		},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":         "./cli.kdl",
					"resolve":     "location",
					"contentHash": HashContent([]byte(kdlContent)),
				},
			},
		},
		"bindings": map[string]any{
			"greet.usage": map[string]any{
				"operation": "greet",
				"source":    "usage",
				"ref":       "greet",
				"x-ob":      map[string]any{},
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)

	// Modify the source: change the description.
	newKDL := `min_usage_version "2.0.0"
bin "hello"
cmd "greet" {
  help "Say hello to someone"
  arg "<name>" help="Who to greet"
}
`
	os.WriteFile(srcPath, []byte(newKDL), 0644)

	// Sync should detect drift and regenerate.
	result, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source synced, got %d", len(result.Sources))
	}
	for _, w := range result.Warnings {
		t.Logf("warning: %s", w)
	}
	if len(result.OperationsUpdated) < 1 {
		t.Errorf("expected at least 1 operation updated, got %v", result.OperationsUpdated)
	}

	// Read back and verify the operation was updated.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	ops := parsed["operations"].(map[string]any)
	greetOp := ops["greet"].(map[string]any)
	desc := greetOp["description"].(string)
	if desc != "Say hello to someone" {
		t.Errorf("expected updated description, got %q", desc)
	}

	// Verify x-ob is still present on the operation.
	if _, ok := greetOp["x-ob"]; !ok {
		t.Error("expected x-ob still present on managed operation")
	}
}

func TestSync_PreservesHandAuthored(t *testing.T) {
	dir := t.TempDir()

	kdlContent := `min_usage_version "2.0.0"
bin "hello"
cmd "greet" {
  help "Say hello"
}
`
	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte(kdlContent), 0644)

	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "hello",
		"operations": map[string]any{
			"greet": map[string]any{
				"description": "Say hello",
				"x-ob":        map[string]any{},
			},
			"customOp": map[string]any{
				"description": "I am hand-authored",
			},
		},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "location",
				},
			},
		},
		"bindings": map[string]any{
			"greet.usage": map[string]any{
				"operation": "greet",
				"source":    "usage",
				"ref":       "greet",
				"x-ob":      map[string]any{},
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)

	result, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source synced, got %d", len(result.Sources))
	}

	// Verify hand-authored operation is preserved unchanged.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	ops := parsed["operations"].(map[string]any)
	customOp, exists := ops["customOp"]
	if !exists {
		t.Fatal("hand-authored operation 'customOp' should still exist")
	}
	custom := customOp.(map[string]any)
	if custom["description"] != "I am hand-authored" {
		t.Errorf("hand-authored description changed: %v", custom["description"])
	}
	if _, ok := custom["x-ob"]; ok {
		t.Error("hand-authored operation should not have x-ob")
	}
}

func TestSync_AddsNewOperationsFromSource(t *testing.T) {
	dir := t.TempDir()

	// Source with TWO commands.
	kdlContent := `min_usage_version "2.0.0"
bin "hello"
cmd "greet" {
  help "Say hello"
}
cmd "farewell" {
  help "Say goodbye"
}
`
	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte(kdlContent), 0644)

	// OBI only has "greet" — "farewell" is new.
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "hello",
		"operations": map[string]any{
			"greet": map[string]any{
				"description": "Say hello",
				"x-ob":        map[string]any{},
			},
		},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "location",
				},
			},
		},
		"bindings": map[string]any{
			"greet.usage": map[string]any{
				"operation": "greet",
				"source":    "usage",
				"ref":       "greet",
				"x-ob":      map[string]any{},
			},
		},
	}

	obiPath := writeInterface(t, dir, "interface.json", obiData)

	result, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OperationsAdded) < 1 {
		t.Errorf("expected at least 1 new operation, got %v", result.OperationsAdded)
	}
	if len(result.BindingsAdded) < 1 {
		t.Errorf("expected at least 1 new binding, got %v", result.BindingsAdded)
	}

	// Verify "farewell" exists and is managed.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	ops := parsed["operations"].(map[string]any)
	farewell, exists := ops["farewell"]
	if !exists {
		t.Fatal("expected new operation 'farewell' to be added")
	}
	if _, ok := farewell.(map[string]any)["x-ob"]; !ok {
		t.Error("expected new operation to be managed (x-ob present)")
	}
}

// TestSync_OBIMissingBindingsMap covers OBIs that have sources (e.g. from "source add")
// but no "bindings" or "operations" keys — sync must not panic and should populate them.
func TestSync_OBIMissingBindingsMap(t *testing.T) {
	dir := t.TempDir()

	kdlContent := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hello" {}
`
	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte(kdlContent), 0644)

	// OBI with source (x-ob) but no bindings key and empty operations (like after "source add").
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"name":         "test",
		"operations":  map[string]any{},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "location",
				},
			},
		},
		// No "bindings" key at all
	}
	obiPath := writeInterface(t, dir, "interface.json", obiData)

	_, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("sync should not fail: %v", err)
	}

	// Verify bindings and operations were populated.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	if parsed["bindings"] == nil {
		t.Fatal("expected bindings to be populated after sync")
	}
	bindings := parsed["bindings"].(map[string]any)
	if len(bindings) == 0 {
		t.Error("expected at least one binding after sync")
	}
	ops := parsed["operations"].(map[string]any)
	if len(ops) == 0 {
		t.Error("expected at least one operation after sync")
	}
}
