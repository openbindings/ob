package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

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
		"sources":    map[string]any{"api": map[string]any{"bindingSpec": "openbindings.openapi-3.1@1", "location": "nope.yaml"}},
	}
	if withBinding {
		data["bindings"] = map[string]any{
			"greet.api": map[string]any{"operation": "greet", "source": "api", "selector": "old"},
		}
	}
	return writeInterface(t, t.TempDir(), "t.obi.json", data)
}

func TestOperationBind_Basic(t *testing.T) {
	obiPath := bindFixture(t, false)
	result, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Selector: "getGreeting"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BindingKey != "greet.api" {
		t.Errorf("binding key = %q, want greet.api", result.BindingKey)
	}
	iface, _ := loadInterfaceFile(obiPath)
	be, ok := iface.Bindings["greet.api"]
	if !ok || be.Operation != "greet" || be.Selector != "getGreeting" {
		t.Errorf("binding not created correctly: %+v", be)
	}
}

func TestOperationBind_OpNotFound(t *testing.T) {
	obiPath := bindFixture(t, false)
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "ghost", Source: "api", Selector: "x"}); err == nil {
		t.Fatal("expected error: operation not found")
	}
}

func TestOperationBind_SourceNotRegistered(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{"greet": map[string]any{}}))
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Selector: "x"}); err == nil {
		t.Fatal("expected error: source not registered")
	}
}

func TestOperationBind_ExistingWithoutForce(t *testing.T) {
	obiPath := bindFixture(t, true)
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Selector: "new"}); err == nil {
		t.Fatal("expected error: existing binding without --force")
	}
	if _, err := OperationBind(OperationBindInput{OBIPath: obiPath, Op: "greet", Source: "api", Selector: "new", Force: true}); err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if iface.Bindings["greet.api"].Selector != "new" {
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

func TestOperationUnbindBinding_ArbitraryKey(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"greet": map[string]any{},
	}))
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	iface.Sources = map[string]openbindings.Source{
		"api": {BindingSpec: "example.api@1", Content: []byte(`{}`)},
	}
	iface.Bindings = map[string]openbindings.BindingEntry{
		"friendly-name": {Operation: "greet", Source: "api", Selector: "#/greet"},
	}
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		t.Fatal(err)
	}

	result, err := OperationUnbindBinding(obiPath, "friendly-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BindingKey != "friendly-name" {
		t.Fatalf("binding key = %q, want friendly-name", result.BindingKey)
	}
	updated, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := updated.Bindings["friendly-name"]; exists {
		t.Error("exact binding key should be removed")
	}
	if _, exists := updated.Operations["greet"]; !exists {
		t.Error("operation should remain after exact unbind")
	}
}

// TestOperationDetach_SurgicalBaseRemoval: detaching an operation whose x-ob
// carries a real base snapshot removes the x-ob key entirely (no
// {"base":null} or bare {} residue, which would misreport it as
// source-owned), while an author-set codegenName override keeps the x-ob
// alive with exactly that field.
func TestOperationDetach_SurgicalBaseRemoval(t *testing.T) {
	dir := t.TempDir()

	// Base only: x-ob disappears wholesale.
	obiPath := writeInterface(t, dir, "base-only.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{
			"description": "d",
			"x-ob":        map[string]any{"base": map[string]any{"description": "d"}},
		},
	}))
	if _, err := OperationDetach(obiPath, "getA"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if HasXOB(iface.Operations["getA"].LosslessFields) {
		t.Error("base-only op should carry no x-ob at all after detach")
	}

	// Base + codegenName: ownership goes, the override stays.
	obiPath = writeInterface(t, dir, "base-and-name.obi.json", minimalInterface(map[string]any{
		"getB": map[string]any{
			"description": "d",
			"x-ob": map[string]any{
				"base":        map[string]any{"description": "d"},
				"codegenName": "fetchB",
			},
		},
	}))
	if _, err := OperationDetach(obiPath, "getB"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ = loadInterfaceFile(obiPath)
	lf := iface.Operations["getB"].LosslessFields
	if IsSourceOwned(lf) {
		t.Error("getB should be hand-authored after detach")
	}
	if got := GetCodegenName(lf); got != "fetchB" {
		t.Errorf("codegenName override should survive detach, got %q", got)
	}
	if base, _ := GetBase(lf); base != nil {
		t.Error("base snapshot should be gone after detach")
	}
}
