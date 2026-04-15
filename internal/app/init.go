package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// EnvConfig represents environment-level configuration stored in .openbindings/config.json.
type EnvConfig struct {
	Delegates               []string `json:"delegates,omitempty"`
	RemovedDefaultDelegates []string `json:"removedDefaultDelegates,omitempty"`
}

// InitResult is returned by Init.
type InitResult struct {
	Initialized     string `json:"initialized"`
	EnvironmentPath string `json:"environmentPath"`
	Global          bool   `json:"global,omitempty"`
}

// Render returns a human-readable summary.
func (r InitResult) Render() string {
	return "Initialized " + r.EnvironmentPath + "/"
}

// Init creates an OpenBindings environment directory with a default config.
// If global is true, initializes at ~/.config/openbindings/ instead of .openbindings/.
func Init(global bool) (*InitResult, error) {
	envDir := EnvDir

	if global {
		globalPath, err := GlobalConfigPath()
		if err != nil {
			return nil, err
		}
		envDir = globalPath
	}

	if _, err := os.Stat(envDir); err == nil {
		return nil, ExitResult{Code: 1, Message: envDir + " already exists", ToStderr: true}
	}

	if err := createDefaultEnvironment(envDir); err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(envDir)
	if err != nil {
		absPath = envDir
	}

	return &InitResult{
		Initialized:     "environment",
		EnvironmentPath: absPath,
		Global:          global,
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
			config := &EnvConfig{Delegates: append([]string(nil), defaultDelegates...)}
			return config, nil
		}
		return nil, err
	}

	var config EnvConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if migrateDefaultDelegates(&config) {
		_ = SaveEnvConfig(envPath, &config)
	}

	return &config, nil
}

// EnvironmentStatus holds the status of an OpenBindings environment.
type EnvironmentStatus struct {
	EnvironmentType string `json:"environmentType"`
	EnvironmentPath string `json:"environmentPath"`
	DelegateCount   int    `json:"delegateCount"`
}

// GetEnvironmentStatus returns the current environment status.
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

	envType := "global"
	if isLocal {
		envType = "local"
	}
	return &EnvironmentStatus{
		EnvironmentType: envType,
		EnvironmentPath: envPath,
		DelegateCount:   delegateCount,
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

	config := EnvConfig{
		Delegates: append([]string(nil), defaultDelegates...),
	}
	return SaveEnvConfig(envDir, &config)
}
