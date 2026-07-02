package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	_, err := Init(false)
	if err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	envPath := filepath.Join(tmpDir, EnvDir)
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		t.Error(".openbindings/ directory not created")
	}

	configPath := filepath.Join(envPath, EnvConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error(".openbindings/config.json not created")
	}

	config, err := LoadEnvConfig(envPath)
	if err != nil {
		t.Fatalf("failed to load env config: %v", err)
	}
	// A fresh environment has an empty registry: the self-delegate is builtin,
	// and every external delegate is an explicit, resolvable registration.
	if len(config.Delegates) != 0 {
		t.Errorf("expected an empty delegate registry, got %d", len(config.Delegates))
	}
}

func TestInit_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	_ = os.MkdirAll(filepath.Join(tmpDir, EnvDir), DirPerm)

	_, err := Init(false)
	if err == nil {
		t.Error("expected error when .openbindings/ already exists")
	}
}

func TestFindEnvironment_Local(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	envPath := filepath.Join(tmpDir, EnvDir)
	_ = os.MkdirAll(envPath, DirPerm)

	subDir := filepath.Join(tmpDir, "sub", "dir")
	_ = os.MkdirAll(subDir, DirPerm)

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(subDir)

	found, isLocal, err := FindEnvironment()
	if err != nil {
		t.Fatalf("FindEnvironment() failed: %v", err)
	}
	if !isLocal {
		t.Error("expected local environment")
	}
	if found != envPath {
		t.Errorf("found %q, expected %q", found, envPath)
	}
}

func TestLoadSaveEnvConfig(t *testing.T) {
	tmpDir := t.TempDir()

	config, err := LoadEnvConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadEnvConfig() failed: %v", err)
	}
	if len(config.Delegates) != 0 {
		t.Errorf("expected an empty registry when no config exists, got %d", len(config.Delegates))
	}

	config.Delegates = append(config.Delegates, DelegateRecord{
		Location:   "exec:my-tool",
		Name:       "my-tool",
		Operations: []string{"acme.tool.doThing"},
	})
	if err := SaveEnvConfig(tmpDir, config); err != nil {
		t.Fatalf("SaveEnvConfig() failed: %v", err)
	}

	loaded, err := LoadEnvConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadEnvConfig() failed: %v", err)
	}
	if len(loaded.Delegates) != 1 || loaded.Delegates[0].Location != "exec:my-tool" {
		t.Errorf("expected the saved record to round-trip, got %+v", loaded.Delegates)
	}
	if !carriesOperation(loaded.Delegates[0].Operations, "acme.tool.doThing") {
		t.Errorf("record operations should round-trip, got %v", loaded.Delegates[0].Operations)
	}
}
