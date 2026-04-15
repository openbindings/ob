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
	if len(config.Delegates) != len(defaultDelegates) {
		t.Errorf("expected %d delegates, got %d", len(defaultDelegates), len(config.Delegates))
	}
	for i, d := range defaultDelegates {
		if config.Delegates[i] != d {
			t.Errorf("delegate[%d] = %q, want %q", i, config.Delegates[i], d)
		}
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
	if len(config.Delegates) != len(defaultDelegates) {
		t.Errorf("expected %d default delegates, got %d", len(defaultDelegates), len(config.Delegates))
	}

	config.Delegates = append(config.Delegates, "exec:my-tool")
	err = SaveEnvConfig(tmpDir, config)
	if err != nil {
		t.Fatalf("SaveEnvConfig() failed: %v", err)
	}

	loaded, err := LoadEnvConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadEnvConfig() failed: %v", err)
	}
	if len(loaded.Delegates) != len(defaultDelegates)+1 {
		t.Errorf("expected %d delegates after add, got %d", len(defaultDelegates)+1, len(loaded.Delegates))
	}
}

func TestMigrateDefaultDelegates(t *testing.T) {
	config := &EnvConfig{}
	changed := migrateDefaultDelegates(config)
	if !changed {
		t.Error("expected migration to modify empty config")
	}
	if len(config.Delegates) != len(defaultDelegates) {
		t.Errorf("expected %d delegates, got %d", len(defaultDelegates), len(config.Delegates))
	}

	config.RemovedDefaultDelegates = []string{"exec:ob"}
	config.Delegates = nil
	changed = migrateDefaultDelegates(config)
	if !changed {
		t.Error("expected migration with removal")
	}
	for _, d := range config.Delegates {
		if d == "exec:ob" {
			t.Error("removed delegate should not be re-added")
		}
	}
}
