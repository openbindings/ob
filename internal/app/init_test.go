package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if len(config.LegacyDelegates) != 0 || len(config.DelegateRegistry) != 0 {
		t.Errorf("expected an empty delegate registry, got %+v", config)
	}
}

func TestInit_AlreadyInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	// An environment exists when its config file does.
	_ = os.MkdirAll(filepath.Join(tmpDir, EnvDir), DirPerm)
	if err := SaveEnvConfig(filepath.Join(tmpDir, EnvDir), &EnvConfig{}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, err := Init(false)
	if err == nil {
		t.Error("expected error when the environment is already initialized")
	}
}

// TestInit_CompletesBareDirectory verifies that a directory without the
// config marker is completed, not refused: sibling features (the context
// store lives under the global config dir) may create the directory without
// initializing an environment.
func TestInit_CompletesBareDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	_ = os.MkdirAll(filepath.Join(tmpDir, EnvDir), DirPerm)

	status, err := Init(false)
	if err != nil {
		t.Fatalf("expected bare directory to be initialized, got: %v", err)
	}
	if status.EnvironmentType != "local" {
		t.Errorf("environmentType = %q, want local", status.EnvironmentType)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, EnvDir, EnvConfigFile)); err != nil {
		t.Errorf("expected config marker created: %v", err)
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
	if len(config.LegacyDelegates) != 0 {
		t.Errorf("expected an empty registry when no config exists, got %d", len(config.LegacyDelegates))
	}

	// A legacy row is retained byte-for-byte for explicit conversion; it is
	// never projected through a typed record and never interpreted for routing.
	legacy := json.RawMessage(`{"location":"exec:my-tool","name":"my-tool","operations":["acme.tool.doThing"],"preference":9007199254740993}`)
	config.LegacyDelegates = append(config.LegacyDelegates, legacy)
	if err := SaveEnvConfig(tmpDir, config); err != nil {
		t.Fatalf("SaveEnvConfig() failed: %v", err)
	}

	loaded, err := LoadEnvConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadEnvConfig() failed: %v", err)
	}
	var want, got any
	_ = json.Unmarshal(legacy, &want)
	if len(loaded.LegacyDelegates) != 1 {
		t.Fatalf("expected the legacy row to round-trip, got %+v", loaded.LegacyDelegates)
	}
	_ = json.Unmarshal(loaded.LegacyDelegates[0], &got)
	if !reflect.DeepEqual(want, got) || !strings.Contains(string(loaded.LegacyDelegates[0]), "9007199254740993") {
		t.Errorf("legacy row changed in transit: %s", loaded.LegacyDelegates[0])
	}
	if _, err := readRoleState(loaded); !IsDelegateRegistryUnavailable(err) {
		t.Errorf("legacy rows must refuse role-registry use, got %v", err)
	}
}
