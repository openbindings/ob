package app

import (
	"path/filepath"
	"testing"
)

func TestNewInterface_Basic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.obi.json")
	result, err := NewInterface(NewInterfaceInput{Path: path, Name: "Foo", Version: "0.1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OpenBindings == "" {
		t.Error("openbindings version should default")
	}
	iface, err := loadInterfaceFile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if iface.Name != "Foo" || iface.Version != "0.1.0" {
		t.Errorf("unexpected name/version: %q %q", iface.Name, iface.Version)
	}
	if iface.Operations == nil {
		t.Error("operations map should be initialized")
	}
}

func TestNewInterface_RefusesExistingWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.obi.json")
	if _, err := NewInterface(NewInterfaceInput{Path: path, Name: "A"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := NewInterface(NewInterfaceInput{Path: path, Name: "B"}); err == nil {
		t.Fatal("expected refusal to overwrite existing file without --force")
	}
	if _, err := NewInterface(NewInterfaceInput{Path: path, Name: "B", Force: true}); err != nil {
		t.Fatalf("--force should overwrite: %v", err)
	}
	iface, _ := loadInterfaceFile(path)
	if iface.Name != "B" {
		t.Error("--force should overwrite the file")
	}
}

func TestNewInterface_VersionNotDropped(t *testing.T) {
	// Regression: the old `create --yes` dropped --version; `new` must keep it.
	path := filepath.Join(t.TempDir(), "x.obi.json")
	if _, err := NewInterface(NewInterfaceInput{Path: path, Version: "9.9.9"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(path)
	if iface.Version != "9.9.9" {
		t.Errorf("version must be preserved, got %q", iface.Version)
	}
}

func TestMetaSet_EditsFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.obi.json")
	_, _ = NewInterface(NewInterfaceInput{Path: path, Name: "Old", Version: "0.1.0"})
	n, v := "New", "0.2.0"
	if _, err := MetaSet(MetaSetInput{Path: path, Name: &n, Version: &v}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(path)
	if iface.Name != "New" || iface.Version != "0.2.0" {
		t.Errorf("meta set did not apply: %q %q", iface.Name, iface.Version)
	}
}

func TestMetaSet_OnlyChangesProvidedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.obi.json")
	_, _ = NewInterface(NewInterfaceInput{Path: path, Name: "Keep", Version: "0.1.0", Description: "desc"})
	v := "0.2.0"
	if _, err := MetaSet(MetaSetInput{Path: path, Version: &v}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	iface, _ := loadInterfaceFile(path)
	if iface.Name != "Keep" {
		t.Error("name should be unchanged")
	}
	if iface.Version != "0.2.0" {
		t.Error("version should change")
	}
	if iface.Description != "desc" {
		t.Error("description should be unchanged")
	}
}
