package app

import (
	"encoding/json"
	"os"
	"testing"
)

// --- List tests ---

func TestOperationList_Empty(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{}))

	result, err := OperationList(obiPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Operations) != 0 {
		t.Errorf("expected 0 operations, got %d", len(result.Operations))
	}
}

func TestOperationList_WithOperations(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{"description": "Say hello"},
		"info":  map[string]any{"tags": []string{"admin"}},
	}))

	result, err := OperationList(obiPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(result.Operations))
	}

	// Verify sorted by key.
	if result.Operations[0].Key != "hello" {
		t.Errorf("expected first op 'hello', got %q", result.Operations[0].Key)
	}
	if result.Operations[1].Key != "info" {
		t.Errorf("expected second op 'info', got %q", result.Operations[1].Key)
	}
}

func TestOperationList_TagFilter(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
		"info":  map[string]any{"tags": []string{"admin"}},
		"reset": map[string]any{"tags": []string{"admin", "danger"}},
	}))

	result, err := OperationList(obiPath, "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Operations) != 2 {
		t.Fatalf("expected 2 operations with tag 'admin', got %d", len(result.Operations))
	}
}

func TestOperationList_BindingCount(t *testing.T) {
	dir := t.TempDir()
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"operations": map[string]any{
			"hello": map[string]any{},
		},
		"bindings": map[string]any{
			"hello.src1": map[string]any{"operation": "hello", "source": "src1"},
			"hello.src2": map[string]any{"operation": "hello", "source": "src2"},
		},
	}
	obiPath := writeInterface(t, dir, "test.obi.json", obiData)

	result, err := OperationList(obiPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Operations[0].BindingCount != 2 {
		t.Errorf("expected 2 bindings, got %d", result.Operations[0].BindingCount)
	}
}

func TestOperationList_Render(t *testing.T) {
	output := OperationListOutput{
		Operations: []OperationEntry{
			{Key: "hello", Description: "Say hello", Tags: []string{"greet"}, BindingCount: 1},
		},
	}
	rendered := output.Render()
	if rendered == "" {
		t.Error("expected non-empty render output")
	}
}

func TestOperationList_RenderEmpty(t *testing.T) {
	output := OperationListOutput{}
	rendered := output.Render()
	if rendered == "" {
		t.Error("expected non-empty render output for empty list")
	}
}

// --- Rename tests ---

func TestOperationRename_Basic(t *testing.T) {
	dir := t.TempDir()
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"operations": map[string]any{
			"hello": map[string]any{"description": "Say hello"},
		},
		"bindings": map[string]any{
			"hello.usage": map[string]any{"operation": "hello", "source": "usage", "ref": "hello"},
		},
	}
	obiPath := writeInterface(t, dir, "test.obi.json", obiData)

	result, err := OperationRename(obiPath, "hello", "greet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.OldKey != "hello" || result.NewKey != "greet" {
		t.Errorf("expected hello → greet, got %q → %q", result.OldKey, result.NewKey)
	}
	if result.BindingsUpdated != 1 {
		t.Errorf("expected 1 binding updated, got %d", result.BindingsUpdated)
	}

	// Verify the file was updated correctly.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	ops := parsed["operations"].(map[string]any)
	if _, ok := ops["hello"]; ok {
		t.Error("old key 'hello' should not exist")
	}
	if _, ok := ops["greet"]; !ok {
		t.Error("new key 'greet' should exist")
	}

	bindings := parsed["bindings"].(map[string]any)
	if _, ok := bindings["hello.usage"]; ok {
		t.Error("old binding key 'hello.usage' should not exist")
	}
	greetBinding, ok := bindings["greet.usage"].(map[string]any)
	if !ok {
		t.Fatal("expected 'greet.usage' binding")
	}
	if greetBinding["operation"] != "greet" {
		t.Errorf("expected binding operation 'greet', got %v", greetBinding["operation"])
	}
}

func TestOperationRename_NotFound(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
	}))

	_, err := OperationRename(obiPath, "nonexistent", "new")
	if err == nil {
		t.Fatal("expected error for nonexistent operation")
	}
}

func TestOperationRename_TargetExists(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
		"greet": map[string]any{},
	}))

	_, err := OperationRename(obiPath, "hello", "greet")
	if err == nil {
		t.Fatal("expected error when target key already exists")
	}
}

func TestOperationRename_SameKey(t *testing.T) {
	_, err := OperationRename("/nonexistent", "hello", "hello")
	if err == nil {
		t.Fatal("expected error for same key rename")
	}
}

func TestOperationRename_NoBindings(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
	}))

	result, err := OperationRename(obiPath, "hello", "greet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BindingsUpdated != 0 {
		t.Errorf("expected 0 bindings updated, got %d", result.BindingsUpdated)
	}
}

func TestOperationRename_MultipleBindings(t *testing.T) {
	dir := t.TempDir()
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"operations": map[string]any{
			"hello": map[string]any{},
			"other": map[string]any{},
		},
		"bindings": map[string]any{
			"hello.src1":  map[string]any{"operation": "hello", "source": "src1"},
			"hello.src2":  map[string]any{"operation": "hello", "source": "src2"},
			"other.src1":  map[string]any{"operation": "other", "source": "src1"},
		},
	}
	obiPath := writeInterface(t, dir, "test.obi.json", obiData)

	result, err := OperationRename(obiPath, "hello", "greet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BindingsUpdated != 2 {
		t.Errorf("expected 2 bindings updated, got %d", result.BindingsUpdated)
	}

	// Verify other operation's binding was not affected.
	iface, _ := loadInterfaceFile(obiPath)
	if _, ok := iface.Bindings["other.src1"]; !ok {
		t.Error("other.src1 binding should still exist")
	}
}

// --- Remove tests ---

func TestOperationRemove_Basic(t *testing.T) {
	dir := t.TempDir()
	obiData := map[string]any{
		"openbindings": "0.1.0",
		"operations": map[string]any{
			"hello": map[string]any{},
			"info":  map[string]any{},
		},
		"bindings": map[string]any{
			"hello.src": map[string]any{"operation": "hello", "source": "src"},
		},
	}
	obiPath := writeInterface(t, dir, "test.obi.json", obiData)

	result, err := OperationRemove(obiPath, []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Removed) != 1 || result.Removed[0] != "hello" {
		t.Errorf("expected removed [hello], got %v", result.Removed)
	}
	if result.BindingsRemoved != 1 {
		t.Errorf("expected 1 binding removed, got %d", result.BindingsRemoved)
	}

	// Verify file.
	iface, _ := loadInterfaceFile(obiPath)
	if _, ok := iface.Operations["hello"]; ok {
		t.Error("hello should be removed")
	}
	if _, ok := iface.Operations["info"]; !ok {
		t.Error("info should still exist")
	}
	if len(iface.Bindings) != 0 {
		t.Errorf("expected 0 bindings, got %d", len(iface.Bindings))
	}
}

func TestOperationRemove_Multiple(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
		"info":  map[string]any{},
		"help":  map[string]any{},
	}))

	result, err := OperationRemove(obiPath, []string{"hello", "info"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(result.Removed))
	}

	iface, _ := loadInterfaceFile(obiPath)
	if len(iface.Operations) != 1 {
		t.Errorf("expected 1 remaining operation, got %d", len(iface.Operations))
	}
}

func TestOperationRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
	}))

	_, err := OperationRemove(obiPath, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent operation")
	}
}

func TestOperationRemove_PartialNotFound(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
	}))

	// One exists, one doesn't — should fail before removing any.
	_, err := OperationRemove(obiPath, []string{"hello", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for partially nonexistent operations")
	}

	// Verify nothing was removed.
	iface, _ := loadInterfaceFile(obiPath)
	if _, ok := iface.Operations["hello"]; !ok {
		t.Error("hello should still exist (atomic failure)")
	}
}

func TestOperationRemove_Empty(t *testing.T) {
	_, err := OperationRemove("/nonexistent", []string{})
	if err == nil {
		t.Fatal("expected error for empty keys")
	}
}

func TestOperationRemove_Render(t *testing.T) {
	output := OperationRemoveOutput{
		Removed:         []string{"hello"},
		BindingsRemoved: 2,
	}
	rendered := output.Render()
	if rendered == "" {
		t.Error("expected non-empty render output")
	}
}

func TestOperationRemove_RenderMultiple(t *testing.T) {
	output := OperationRemoveOutput{
		Removed: []string{"hello", "info"},
	}
	rendered := output.Render()
	if rendered == "" {
		t.Error("expected non-empty render output")
	}
}

// --- renameBindingKey tests ---

func TestRenameBindingKey(t *testing.T) {
	tests := []struct {
		bindingKey string
		oldOp      string
		newOp      string
		want       string
	}{
		{"hello.usage", "hello", "greet", "greet.usage"},
		{"hello.src", "hello", "greet", "greet.src"},
		{"other.src", "hello", "greet", "other.src"},         // no match
		{"helloWorld.src", "hello", "greet", "helloWorld.src"}, // prefix but not at dot boundary
	}

	for _, tt := range tests {
		got := renameBindingKey(tt.bindingKey, tt.oldOp, tt.newOp)
		if got != tt.want {
			t.Errorf("renameBindingKey(%q, %q, %q) = %q, want %q",
				tt.bindingKey, tt.oldOp, tt.newOp, got, tt.want)
		}
	}
}
