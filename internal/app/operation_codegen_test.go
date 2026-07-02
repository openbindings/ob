package app

import (
	"encoding/json"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestOperationSetCodegenName_HandAuthored(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"openbindings.binding-invoker.invokeBinding": map[string]any{"description": "d"},
	}))

	if _, err := OperationSetCodegenName(obiPath, "openbindings.binding-invoker.invokeBinding", "invokeBinding"); err != nil {
		t.Fatalf("set codegen name: %v", err)
	}

	iface, _ := loadInterfaceFile(obiPath)
	op := iface.Operations["openbindings.binding-invoker.invokeBinding"]
	if got := GetCodegenName(op.LosslessFields); got != "invokeBinding" {
		t.Errorf("codegen name = %q, want %q", got, "invokeBinding")
	}
	// The key is untouched; the override lives only in x-ob.
	if _, ok := iface.Operations["openbindings.binding-invoker.invokeBinding"]; !ok {
		t.Error("operation key must be unchanged by a codegen-name override")
	}
}

func TestOperationSetCodegenName_SourceOwnedPreservesBaseAndStaysSourceOwned(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{
			"x-ob":        map[string]any{"base": map[string]any{"description": "d"}},
			"description": "d",
		},
	}))

	if _, err := OperationSetCodegenName(obiPath, "getA", "getAlpha"); err != nil {
		t.Fatalf("set codegen name: %v", err)
	}

	iface, _ := loadInterfaceFile(obiPath)
	lf := iface.Operations["getA"].LosslessFields
	// Setting the hint must NOT detach a source-owned operation...
	if !HasXOB(lf) {
		t.Error("source-owned op should remain source-owned after a codegen-name change")
	}
	// ...must preserve the merge base...
	if base, _ := GetBase(lf); base == nil {
		t.Error("base snapshot must be preserved alongside the codegen name")
	}
	// ...and must store the hint.
	if got := GetCodegenName(lf); got != "getAlpha" {
		t.Errorf("codegen name = %q, want %q", got, "getAlpha")
	}
}

func TestOperationSetCodegenName_ClearRemovesEmptyXOB(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{"description": "d"},
	}))

	// Set, then clear.
	if _, err := OperationSetCodegenName(obiPath, "getA", "getAlpha"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := OperationSetCodegenName(obiPath, "getA", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}

	iface, _ := loadInterfaceFile(obiPath)
	lf := iface.Operations["getA"].LosslessFields
	if GetCodegenName(lf) != "" {
		t.Error("codegen name should be cleared")
	}
	// A hand-authored op should not be left carrying a bare x-ob: {} marker,
	// which would misreport it as source-owned.
	if HasXOB(lf) {
		t.Error("clearing the only x-ob field should remove x-ob entirely")
	}
}

func TestSetBase_PreservesCodegenName(t *testing.T) {
	var lf openbindings.LosslessFields
	if err := SetCodegenName(&lf, "invokeBinding"); err != nil {
		t.Fatalf("set codegen name: %v", err)
	}
	// A later sync stamps a fresh base; it must not drop the author's hint.
	if err := SetBase(&lf, map[string]json.RawMessage{"description": json.RawMessage(`"d"`)}); err != nil {
		t.Fatalf("set base: %v", err)
	}
	if got := GetCodegenName(lf); got != "invokeBinding" {
		t.Errorf("SetBase dropped the codegen name: got %q", got)
	}
	if base, _ := GetBase(lf); base == nil {
		t.Error("base should be set")
	}
}

func TestIsSourceOwned(t *testing.T) {
	mk := func(xob string) openbindings.LosslessFields {
		if xob == "" {
			return openbindings.LosslessFields{}
		}
		return openbindings.LosslessFields{Extensions: map[string]json.RawMessage{"x-ob": json.RawMessage(xob)}}
	}
	cases := []struct {
		name string
		xob  string
		want bool
	}{
		{"no x-ob", "", false},
		{"empty marker", `{}`, true},
		{"base snapshot", `{"base":{"description":"d"}}`, true},
		{"codegen-name only", `{"codegenName":"foo"}`, false},
		{"base + codegen-name", `{"base":{},"codegenName":"foo"}`, true},
	}
	for _, c := range cases {
		if got := IsSourceOwned(mk(c.xob)); got != c.want {
			t.Errorf("%s: IsSourceOwned = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestOperationSet_CodegenNameOnlyOpEditableWithoutOwn is the regression guard
// for the bug where a codegen-name override made a hand-authored operation look
// source-owned: a plain edit must succeed and must not drop the override.
func TestOperationSet_CodegenNameOnlyOpEditableWithoutOwn(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{"description": "old"},
	}))
	if _, err := OperationSetCodegenName(obiPath, "getA", "getAlpha"); err != nil {
		t.Fatalf("set codegen name: %v", err)
	}

	desc := "new"
	if _, err := OperationSet(OperationSetInput{OBIPath: obiPath, Op: "getA", Description: &desc}); err != nil {
		t.Fatalf("plain edit of a codegen-name-only op should not require --own: %v", err)
	}

	iface, _ := loadInterfaceFile(obiPath)
	op := iface.Operations["getA"]
	if op.Description != "new" {
		t.Errorf("description not updated, got %q", op.Description)
	}
	if got := GetCodegenName(op.LosslessFields); got != "getAlpha" {
		t.Errorf("plain edit dropped the codegen name: got %q", got)
	}
}

func TestOperationDetach_CodegenNameOnlyIsHandAuthored(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{"x-ob": map[string]any{"codegenName": "getAlpha"}, "description": "d"},
	}))
	if _, err := OperationDetach(obiPath, "getA"); err == nil {
		t.Fatal("detaching a codegen-name-only (not source-owned) op should error")
	}
}

// TestOperationSet_OwnDetachPreservesCodegenName: detaching a genuinely
// source-owned op (base present) via --own strips the base but keeps the
// author's codegen-name override.
func TestOperationSet_OwnDetachPreservesCodegenName(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "t.obi.json", minimalInterface(map[string]any{
		"getA": map[string]any{
			"x-ob":        map[string]any{"base": map[string]any{"description": "d"}, "codegenName": "getAlpha"},
			"description": "d",
		},
	}))
	desc := "new"
	if _, err := OperationSet(OperationSetInput{OBIPath: obiPath, Op: "getA", Description: &desc, Own: true}); err != nil {
		t.Fatalf("--own edit: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	lf := iface.Operations["getA"].LosslessFields
	if IsSourceOwned(lf) {
		t.Error("op should be hand-authored (base stripped) after --own detach")
	}
	if got := GetCodegenName(lf); got != "getAlpha" {
		t.Errorf("--own detach dropped the codegen name: got %q", got)
	}
}

func TestCarryCodegenName(t *testing.T) {
	// existing carries an override; fresh (source-derived) does not.
	var existing, fresh openbindings.Operation
	if err := SetCodegenName(&existing.LosslessFields, "invokeBinding"); err != nil {
		t.Fatalf("seed existing: %v", err)
	}
	var warns []string
	carryCodegenName(existing, &fresh, "k", &warns)
	if got := GetCodegenName(fresh.LosslessFields); got != "invokeBinding" {
		t.Errorf("carry failed: fresh codegen name = %q", got)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}

	// No override on existing → fresh stays clean (no spurious x-ob).
	var existing2, fresh2 openbindings.Operation
	carryCodegenName(existing2, &fresh2, "k", &warns)
	if HasXOB(fresh2.LosslessFields) {
		t.Error("no override should leave fresh with no x-ob")
	}
}
