package app

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/openbindings/openbindings-go"
)

// --- List tests ---

func TestOperationList_Empty(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{}))

	result, err := OperationList(obiPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 operations, got %d", len(result))
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
	if len(result) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(result))
	}

	// Verify sorted by key.
	if result[0].Key != "hello" {
		t.Errorf("expected first op 'hello', got %q", result[0].Key)
	}
	if result[1].Key != "info" {
		t.Errorf("expected second op 'info', got %q", result[1].Key)
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
	if len(result) != 2 {
		t.Fatalf("expected 2 operations with tag 'admin', got %d", len(result))
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
	if len(result[0].Bindings) != 2 {
		t.Errorf("expected 2 bindings, got %d", len(result[0].Bindings))
	}
}

func TestOperationList_Render(t *testing.T) {
	output := OperationListOutput{
		{Key: "hello", Operation: openbindings.Operation{Description: "Say hello", Tags: []string{"greet"}}, Bindings: []string{"hello.openapi"}},
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
			"hello.src1": map[string]any{"operation": "hello", "source": "src1"},
			"hello.src2": map[string]any{"operation": "hello", "source": "src2"},
			"other.src1": map[string]any{"operation": "other", "source": "src1"},
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

	out, err := OperationRemove(obiPath, []string{"nonexistent"})
	if err != nil {
		t.Fatalf("removing an absent operation should succeed (tolerant): %v", err)
	}
	if len(out.Removed) != 0 {
		t.Errorf("nothing should be reported removed, got %v", out.Removed)
	}
}

func TestOperationRemove_PartialNotFound(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"hello": map[string]any{},
	}))

	// One exists, one doesn't — the present one is removed, the absent one is a no-op.
	out, err := OperationRemove(obiPath, []string{"hello", "nonexistent"})
	if err != nil {
		t.Fatalf("partial remove should succeed: %v", err)
	}
	if len(out.Removed) != 1 || out.Removed[0] != "hello" {
		t.Errorf("only hello should be reported removed, got %v", out.Removed)
	}

	iface, _ := loadInterfaceFile(obiPath)
	if _, ok := iface.Operations["hello"]; ok {
		t.Error("hello should have been removed")
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

// --- Alias tests ---

func TestOperationAliasAdd_Basic(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{},
	}))

	result, err := OperationAliasAdd(obiPath, "acme.fetch", []string{"openbindings.key-value-store.get"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Key != "acme.fetch" || result.Action != "added" {
		t.Errorf("unexpected result: %+v", result)
	}

	iface, _ := loadInterfaceFile(obiPath)
	op := iface.Operations["acme.fetch"]
	if len(op.Aliases) != 1 || op.Aliases[0] != "openbindings.key-value-store.get" {
		t.Errorf("expected alias added, got %v", op.Aliases)
	}
}

func TestOperationAliasAdd_Collision(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{"aliases": []string{"openbindings.key-value-store.get"}},
		"acme.other": map[string]any{},
	}))

	// Adding an alias already claimed by another operation must fail (flat namespace).
	_, err := OperationAliasAdd(obiPath, "acme.other", []string{"openbindings.key-value-store.get"})
	if err == nil {
		t.Fatal("expected collision error for alias already in use")
	}
}

func TestOperationAliasAdd_ResolveByAlias(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{"aliases": []string{"openbindings.key-value-store.get"}},
	}))

	// The operation may be referenced by an existing alias; result uses the canonical key.
	result, err := OperationAliasAdd(obiPath, "openbindings.key-value-store.get", []string{"x.y.z"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Key != "acme.fetch" {
		t.Errorf("expected canonical key acme.fetch, got %q", result.Key)
	}
}

func TestOperationAliasAdd_OwnKey(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{},
	}))

	_, err := OperationAliasAdd(obiPath, "acme.fetch", []string{"acme.fetch"})
	if err == nil {
		t.Fatal("expected error aliasing an operation to its own key")
	}
}

func TestOperationAliasRemove_Basic(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{"aliases": []string{"a.b.c", "d.e.f"}},
	}))

	_, err := OperationAliasRemove(obiPath, "acme.fetch", []string{"a.b.c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	op := iface.Operations["acme.fetch"]
	if len(op.Aliases) != 1 || op.Aliases[0] != "d.e.f" {
		t.Errorf("expected only d.e.f remaining, got %v", op.Aliases)
	}
}

func TestOperationAliasRemove_NotPresent(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{},
	}))

	_, err := OperationAliasRemove(obiPath, "acme.fetch", []string{"nope"})
	if err == nil {
		t.Fatal("expected error removing a non-present alias")
	}
}

func TestOperationAliasList_All(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{"aliases": []string{"openbindings.key-value-store.get"}},
		"acme.plain": map[string]any{},
	}))

	result, err := OperationAliasList(obiPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only operations with aliases appear in the satisfaction map.
	if len(result) != 1 || result[0].Key != "acme.fetch" {
		t.Errorf("expected only acme.fetch, got %+v", result)
	}
}

func TestOperationAliasList_Scoped(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.plain": map[string]any{},
	}))

	// Scoped to a single op, it's listed even with no aliases.
	result, err := OperationAliasList(obiPath, "acme.plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].Key != "acme.plain" {
		t.Errorf("expected acme.plain listed, got %+v", result)
	}
}

// --- Add with aliases (the --alias path + OBI-D-04 key guard) ---

func TestOperationAdd_WithAliases(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{}))

	_, err := OperationAdd(OperationAddInput{
		OBIPath: obiPath,
		Key:     "acme.set",
		Aliases: []string{"openbindings.key-value-store.set"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if op := iface.Operations["acme.set"]; len(op.Aliases) != 1 {
		t.Errorf("expected 1 alias, got %v", op.Aliases)
	}
}

func TestOperationAdd_KeyCollidesWithAlias(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{"aliases": []string{"openbindings.key-value-store.get"}},
	}))

	// A new operation's key must not collide with an existing alias (OBI-D-04).
	_, err := OperationAdd(OperationAddInput{OBIPath: obiPath, Key: "openbindings.key-value-store.get"})
	if err == nil {
		t.Fatal("expected error: key collides with an existing alias")
	}
}

// --- Rename into the flat key+alias namespace (the OBI-D-04 fix) ---

func TestOperationRename_IntoExistingAlias(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "test.obi.json", minimalInterface(map[string]any{
		"acme.fetch": map[string]any{"aliases": []string{"openbindings.key-value-store.get"}},
		"acme.other": map[string]any{},
	}))

	// Renaming into a name that is another operation's alias must fail; the old
	// behavior (checking only the operations map) would have produced an
	// invalid document.
	_, err := OperationRename(obiPath, "acme.other", "openbindings.key-value-store.get")
	if err == nil {
		t.Fatal("expected error renaming into an existing alias (flat-namespace collision)")
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
		{"other.src", "hello", "greet", "other.src"},           // no match
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
