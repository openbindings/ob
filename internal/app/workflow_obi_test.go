package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// These tests simulate a user managing an OBI across multiple usage-spec source
// files: create from multiple sources, edit sources, sync, add source, merge
// --from-sources, and ensure hand-authored parts are preserved.
// Only the usage executor is used; other format executors can be added later.

const usageFormat = "usage@2.0.0"

// writeUsageFile writes a usage spec to dir/name and returns the full path.
func writeUsageFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestWorkflow_CreateFromMultipleSources(t *testing.T) {
	dir := t.TempDir()

	greetKDL := `min_usage_version "2.0.0"
bin "greet"
cmd "hello" help="Say hello" {
  arg "[name]" help="Who to greet"
}
cmd "version" help="Print version" {}
`
	toolsKDL := `min_usage_version "2.0.0"
bin "tools"
cmd "info" help="Show tool information" {}
cmd "help" help="Show help" {
  arg "[topic]" help="Topic"
}
`
	writeUsageFile(t, dir, "greet.usage.kdl", greetKDL)
	writeUsageFile(t, dir, "tools.usage.kdl", toolsKDL)

	iface, err := CreateInterface(CreateInterfaceInput{
		Sources: []CreateInterfaceSource{
			{Format: usageFormat, Location: filepath.Join(dir, "greet.usage.kdl")},
			{Format: usageFormat, Location: filepath.Join(dir, "tools.usage.kdl")},
		},
		Name: "multi",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if iface == nil {
		t.Fatal("expected interface")
	}

	wantOps := map[string]bool{"hello": true, "version": true, "info": true, "help": true}
	for op := range wantOps {
		if _, ok := iface.Operations[op]; !ok {
			t.Errorf("missing operation %q", op)
		}
	}
	if len(iface.Operations) != 4 {
		t.Errorf("expected 4 operations, got %d", len(iface.Operations))
	}

	if len(iface.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(iface.Sources))
	}
	for key, src := range iface.Sources {
		meta, err := GetSourceMeta(src)
		if err != nil {
			t.Errorf("source %q: get meta: %v", key, err)
		}
		if meta == nil {
			t.Errorf("source %q: expected x-ob metadata for sync", key)
		}
	}

	if len(iface.Bindings) != 4 {
		t.Errorf("expected 4 bindings, got %d", len(iface.Bindings))
	}

	obiPath := filepath.Join(dir, "interface.json")
	if err := WriteInterfaceToPath(obiPath, iface, ""); err != nil {
		t.Fatalf("write OBI: %v", err)
	}

	loaded, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatalf("load OBI: %v", err)
	}
	if len(loaded.Sources) != 2 {
		t.Errorf("loaded: expected 2 sources, got %d", len(loaded.Sources))
	}
}

func TestWorkflow_EditSourceThenSync(t *testing.T) {
	dir := t.TempDir()

	kdl1 := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hello" {
  arg "[name]" help="Name"
}
`
	path1 := writeUsageFile(t, dir, "cli.kdl", kdl1)

	iface, err := CreateInterface(CreateInterfaceInput{
		Sources: []CreateInterfaceSource{
			{Format: usageFormat, Location: path1},
		},
		Name: "app",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	obiPath := filepath.Join(dir, "interface.json")
	if err := WriteInterfaceToPath(obiPath, iface, ""); err != nil {
		t.Fatalf("write OBI: %v", err)
	}

	kdl2 := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hello to someone" {
  arg "[name]" help="Name"
}
`
	if err := os.WriteFile(path1, []byte(kdl2), 0644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	result, err := Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source synced, got %d", len(result.Sources))
	}
	if len(result.OperationsUpdated) < 1 {
		t.Errorf("expected at least 1 operation updated, got %v", result.OperationsUpdated)
	}

	loaded, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatalf("load OBI: %v", err)
	}
	op, ok := loaded.Operations["greet"]
	if !ok {
		t.Fatal("missing operation greet")
	}
	if op.Description != "Say hello to someone" {
		t.Errorf("description = %q, want %q", op.Description, "Say hello to someone")
	}
}

func TestWorkflow_SourceAddThenMergeFromSources(t *testing.T) {
	dir := t.TempDir()

	writeUsageFile(t, dir, "main.usage.kdl", `min_usage_version "2.0.0"
bin "main"
cmd "run" help="Run something" {}
`)
	iface, err := CreateInterface(CreateInterfaceInput{
		Sources: []CreateInterfaceSource{
			{Format: usageFormat, Location: filepath.Join(dir, "main.usage.kdl")},
		},
		Name: "svc",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	obiPath := filepath.Join(dir, "interface.json")
	if err := WriteInterfaceToPath(obiPath, iface, ""); err != nil {
		t.Fatalf("write OBI: %v", err)
	}

	extraPath := writeUsageFile(t, dir, "extra.usage.kdl", `min_usage_version "2.0.0"
bin "extra"
cmd "status" help="Show status" {}
cmd "logs" help="Show logs" {
  arg "[n]" help="Lines"
}
`)
	_, err = SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   usageFormat,
		Location: extraPath,
		Key:      "extraSpec",
	})
	if err != nil {
		t.Fatalf("source add: %v", err)
	}

	mergeOut, err := Merge(MergeInput{
		TargetPath:  obiPath,
		FromSources: true,
		All:         true,
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	loaded, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatalf("load OBI: %v", err)
	}
	if _, ok := loaded.Operations["run"]; !ok {
		t.Error("missing operation run")
	}
	if _, ok := loaded.Operations["status"]; !ok {
		t.Error("missing operation status after merge --from-sources")
	}
	if _, ok := loaded.Operations["logs"]; !ok {
		t.Error("missing operation logs after merge --from-sources")
	}
	if mergeOut.Applied < 2 {
		t.Errorf("expected at least 2 merge entries applied, got %d", mergeOut.Applied)
	}
}

func TestWorkflow_HandAuthoredOperationPreservedAfterSync(t *testing.T) {
	dir := t.TempDir()

	greetKDL := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="From spec" {}
`
	writeUsageFile(t, dir, "cli.kdl", greetKDL)

	iface, err := CreateInterface(CreateInterfaceInput{
		Sources: []CreateInterfaceSource{
			{Format: usageFormat, Location: filepath.Join(dir, "cli.kdl")},
		},
		Name: "app",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	obiPath := filepath.Join(dir, "interface.json")
	if err := WriteInterfaceToPath(obiPath, iface, ""); err != nil {
		t.Fatalf("write OBI: %v", err)
	}

	data, err := os.ReadFile(obiPath)
	if err != nil {
		t.Fatalf("read OBI: %v", err)
	}
	var obi map[string]any
	if err := json.Unmarshal(data, &obi); err != nil {
		t.Fatalf("parse OBI: %v", err)
	}
	ops := obi["operations"].(map[string]any)
	ops["customOp"] = map[string]any{
		"description": "Hand-authored operation",
	}
	data, _ = json.MarshalIndent(obi, "", "  ")
	if err := os.WriteFile(obiPath, data, 0644); err != nil {
		t.Fatalf("write OBI: %v", err)
	}

	updatedKDL := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Updated from spec" {}
`
	if err := os.WriteFile(filepath.Join(dir, "cli.kdl"), []byte(updatedKDL), 0644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	_, err = Sync(SyncInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	loaded, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatalf("load OBI: %v", err)
	}
	if loaded.Operations["greet"].Description != "Updated from spec" {
		t.Errorf("greet description = %q", loaded.Operations["greet"].Description)
	}
	custom, ok := loaded.Operations["customOp"]
	if !ok {
		t.Fatal("hand-authored operation customOp missing after sync")
	}
	if custom.Description != "Hand-authored operation" {
		t.Errorf("customOp description = %q", custom.Description)
	}
	if HasXOB(custom.LosslessFields) {
		t.Error("hand-authored operation should not have x-ob after sync")
	}
}

func TestWorkflow_SourceListAndRemove(t *testing.T) {
	dir := t.TempDir()

	writeUsageFile(t, dir, "a.kdl", `min_usage_version "2.0.0"
bin "a"
cmd "one" help="First" {}
`)
	writeUsageFile(t, dir, "b.kdl", `min_usage_version "2.0.0"
bin "b"
cmd "two" help="Second" {}
`)

	iface, err := CreateInterface(CreateInterfaceInput{
		Sources: []CreateInterfaceSource{
			{Format: usageFormat, Location: filepath.Join(dir, "a.kdl")},
			{Format: usageFormat, Location: filepath.Join(dir, "b.kdl")},
		},
		Name: "multi",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	obiPath := filepath.Join(dir, "interface.json")
	if err := WriteInterfaceToPath(obiPath, iface, ""); err != nil {
		t.Fatalf("write OBI: %v", err)
	}

	listOut, err := SourceList(obiPath)
	if err != nil {
		t.Fatalf("source list: %v", err)
	}
	if len(listOut.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(listOut.Sources))
	}

	var removeKey string
	for _, e := range listOut.Sources {
		if e.Format == usageFormat {
			removeKey = e.Key
			break
		}
	}
	if removeKey == "" {
		t.Fatal("could not find a source key to remove")
	}

	removeOut, err := SourceRemove(obiPath, removeKey)
	if err != nil {
		t.Fatalf("source remove: %v", err)
	}
	if removeOut.Key != removeKey {
		t.Errorf("remove key = %q", removeOut.Key)
	}

	listOut2, err := SourceList(obiPath)
	if err != nil {
		t.Fatalf("source list after remove: %v", err)
	}
	if len(listOut2.Sources) != 1 {
		t.Errorf("expected 1 source after remove, got %d", len(listOut2.Sources))
	}
}
