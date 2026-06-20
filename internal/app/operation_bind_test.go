package app

import "testing"

func TestOperationDetach_Basic(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{"x-ob": map[string]any{}, "description": "d"},
	}))
	if _, err := OperationDetach(obiPath, "getA"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if HasXOB(iface.Operations["getA"].LosslessFields) {
		t.Error("getA should be hand-authored (no x-ob) after detach")
	}
}

func TestOperationDetach_AlreadyHandAuthored(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{},
	}))
	if _, err := OperationDetach(obiPath, "getA"); err == nil {
		t.Fatal("expected error detaching an already hand-authored operation")
	}
}

func TestOperationSet_HandAuthored(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{"description": "old"},
	}))
	desc := "new"
	if _, err := OperationSet(OperationSetInput{OBIPath: obiPath, Op: "getA", Description: &desc}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if iface.Operations["getA"].Description != "new" {
		t.Errorf("description not updated, got %q", iface.Operations["getA"].Description)
	}
}

func TestOperationSet_SourceOwnedRequiresOwn(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{"x-ob": map[string]any{}, "description": "old"},
	}))
	desc := "new"
	if _, err := OperationSet(OperationSetInput{OBIPath: obiPath, Op: "getA", Description: &desc}); err == nil {
		t.Fatal("expected error editing a source-owned operation without --own")
	}
	if _, err := OperationSet(OperationSetInput{OBIPath: obiPath, Op: "getA", Description: &desc, Own: true}); err != nil {
		t.Fatalf("unexpected error with --own: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if iface.Operations["getA"].Description != "new" {
		t.Error("description not updated with --own")
	}
	if HasXOB(iface.Operations["getA"].LosslessFields) {
		t.Error("--own should detach (strip x-ob)")
	}
}

func bindFixture(t *testing.T, withBinding bool) string {
	t.Helper()
	data := map[string]any{
		"openbindings": "0.2.0", "name": "T", "version": "0.1.0",
		"operations": map[string]any{"greet": map[string]any{}},
		"sources":    map[string]any{"api": map[string]any{"format": "openapi@3.1", "location": "nope.yaml"}},
	}
	if withBinding {
		data["bindings"] = map[string]any{
			"greet.api": map[string]any{"operation": "greet", "source": "api", "ref": "old"},
		}
	}
	return writeInterface(t, t.TempDir(), "t.obi.json", data)
}

func TestOperationBind_Basic(t *testing.T) {
	obiPath := bindFixture(t, false)
	result, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Ref: "getGreeting"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BindingKey != "greet.api" {
		t.Errorf("binding key = %q, want greet.api", result.BindingKey)
	}
	iface, _ := loadInterfaceFile(obiPath)
	be, ok := iface.Bindings["greet.api"]
	if !ok || be.Operation != "greet" || be.Ref != "getGreeting" {
		t.Errorf("binding not created correctly: %+v", be)
	}
}

func TestOperationBind_OpNotFound(t *testing.T) {
	obiPath := bindFixture(t, false)
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "ghost", Source: "api", Ref: "x"}); err == nil {
		t.Fatal("expected error: operation not found")
	}
}

func TestOperationBind_SourceNotRegistered(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{"greet": map[string]any{}}))
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Ref: "x"}); err == nil {
		t.Fatal("expected error: source not registered")
	}
}

func TestOperationBind_ExistingWithoutForce(t *testing.T) {
	obiPath := bindFixture(t, true)
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Ref: "new"}); err == nil {
		t.Fatal("expected error: existing binding without --force")
	}
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Ref: "new", Force: true}); err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if iface.Bindings["greet.api"].Ref != "new" {
		t.Error("--force should re-point the binding ref to new")
	}
}

func TestOperationUnbind_Basic(t *testing.T) {
	obiPath := bindFixture(t, true)
	if _, err := OperationUnbind(obiPath, "greet", "api"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if _, ok := iface.Bindings["greet.api"]; ok {
		t.Error("binding should be removed")
	}
	if _, ok := iface.Operations["greet"]; !ok {
		t.Error("operation should remain after unbind")
	}
}

func TestOperationUnbind_NotFound(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{"greet": map[string]any{}}))
	if _, err := OperationUnbind(obiPath, "greet", "api"); err == nil {
		t.Fatal("expected error: no such binding")
	}
}
