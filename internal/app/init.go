package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// EnvConfig represents environment-level configuration stored in .openbindings/config.json.
type EnvConfig struct {
	// Delegates is the delegate registry: one record per registered delegate,
	// in registration order. The self-delegate is builtin, never persisted.
	Delegates []DelegateRecord `json:"delegates,omitempty"`
}

// Init creates an OpenBindings environment directory with a default config
// and returns the created environment's status (the initializeEnvironment
// contract output). If global is true, initializes the openbindings directory
// under the user config dir instead of a local .openbindings/.
func Init(global bool) (*EnvironmentStatus, error) {
	envDir := EnvDir
	envType := "local"

	if global {
		globalPath, err := GlobalConfigPath()
		if err != nil {
			return nil, err
		}
		envDir = globalPath
		envType = "global"
	}

	// An environment exists when its config file does. The bare directory is
	// not the marker: sibling features (the context store lives under the
	// global config dir) may have created it without initializing anything.
	if _, err := os.Stat(filepath.Join(envDir, EnvConfigFile)); err == nil {
		return nil, ExitResult{Code: 1, Message: envDir + " is already initialized", ToStderr: true}
	}

	if err := createDefaultEnvironment(envDir); err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(envDir)
	if err != nil {
		absPath = envDir
	}

	// A fresh environment holds no delegates; the context count reflects the
	// user's context store, which is user-scoped rather than per-environment.
	contextCount := 0
	if summaries, err := ListContexts(); err == nil {
		contextCount = len(summaries)
	}

	return &EnvironmentStatus{
		EnvironmentType: envType,
		EnvironmentPath: absPath,
		ContextCount:    contextCount,
	}, nil
}

// FindEnvironment walks up from the current directory looking for .openbindings/.
// Returns the path to the .openbindings/ directory if found, or the global ~/.config/openbindings/ path.
// Also returns a boolean indicating whether a local environment was found.
func FindEnvironment() (string, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}

	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", false, err
	}

	dir := cwd
	for {
		envPath := filepath.Join(dir, EnvDir)
		if info, err := os.Stat(envPath); err == nil && info.IsDir() {
			return envPath, true, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	globalPath, err := GlobalConfigPath()
	if err != nil {
		return "", false, err
	}

	return globalPath, false, nil
}

// LoadEnvConfig loads the environment configuration from config.json.
func LoadEnvConfig(envPath string) (*EnvConfig, error) {
	configPath := filepath.Join(envPath, EnvConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &EnvConfig{}, nil
		}
		return nil, err
	}

	var config EnvConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// EnvironmentStatus holds the status of an OpenBindings environment.
// ContextCount reflects the user's context store (contexts are user-scoped,
// not per-environment); the delegate count is the environment's own registry.
type EnvironmentStatus struct {
	EnvironmentType string `json:"environmentType"`
	EnvironmentPath string `json:"environmentPath"`
	DelegateCount   int    `json:"delegateCount"`
	ContextCount    int    `json:"contextCount"`
}

// Render returns a human-friendly representation.
func (s *EnvironmentStatus) Render() string {
	return fmt.Sprintf("Environment: %s (%s)\nDelegates: %d\nContexts: %d",
		s.EnvironmentType, s.EnvironmentPath, s.DelegateCount, s.ContextCount)
}

// GetEnvironmentStatus returns the current environment status: a snapshot of
// the active .openbindings/ environment (type, path, registered delegates)
// plus the user's stored-context count. Counts only — see `delegate list` /
// `context list` for details.
func GetEnvironmentStatus() (*EnvironmentStatus, error) {
	envPath, isLocal, err := FindEnvironment()
	if err != nil {
		return nil, err
	}

	config, _ := LoadEnvConfig(envPath)
	delegateCount := 0
	if config != nil {
		delegateCount = len(config.Delegates)
	}

	contextCount := 0
	if summaries, err := ListContexts(); err == nil {
		contextCount = len(summaries)
	}

	envType := "global"
	if isLocal {
		envType = "local"
	}
	return &EnvironmentStatus{
		EnvironmentType: envType,
		EnvironmentPath: envPath,
		DelegateCount:   delegateCount,
		ContextCount:    contextCount,
	}, nil
}

// SaveEnvConfig saves the environment configuration to config.json atomically.
func SaveEnvConfig(envPath string, config *EnvConfig) error {
	configPath := filepath.Join(envPath, EnvConfigFile)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return AtomicWriteFile(configPath, data, FilePerm)
}

// FindEnvPath finds the environment path, returning an error if none exists.
func FindEnvPath() (string, error) {
	envPath, _, err := FindEnvironment()
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(envPath); err != nil {
		return "", os.ErrNotExist
	}

	return envPath, nil
}

// EnsureGlobalEnvironment creates the global openbindings environment if it doesn't exist.
func EnsureGlobalEnvironment() error {
	globalEnvPath, err := GlobalConfigPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(globalEnvPath); err == nil {
		return nil
	}

	return createDefaultEnvironment(globalEnvPath)
}

// createDefaultEnvironment creates an environment directory with a default config.
func createDefaultEnvironment(envDir string) error {
	if err := os.MkdirAll(envDir, DirPerm); err != nil {
		return err
	}

	// A fresh environment has an empty registry: the self-delegate is builtin,
	// and every external delegate is an explicit, resolvable registration.
	return SaveEnvConfig(envDir, &EnvConfig{})
}
