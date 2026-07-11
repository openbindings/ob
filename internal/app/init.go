package app

import (
	"encoding/json"
	"errors"
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
	config, _, err := loadEnvConfigFingerprinted(envPath)
	return config, err
}

// envConfigFingerprint identifies the on-disk config.json content at a point
// in time: the sha256 of its raw bytes, or the absent sentinel for "no file
// yet". Comparable with ==; used by mutateEnvConfig to detect whether the
// file changed between a load and its save.
type envConfigFingerprint struct {
	hash   string
	absent bool
}

// loadEnvConfigFingerprinted is LoadEnvConfig plus the fingerprint of what it
// read, for callers that need to detect a later change to the same file
// (mutateEnvConfig's optimistic-concurrency guard).
func loadEnvConfigFingerprinted(envPath string) (*EnvConfig, envConfigFingerprint, error) {
	configPath := filepath.Join(envPath, EnvConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &EnvConfig{}, envConfigFingerprint{absent: true}, nil
		}
		return nil, envConfigFingerprint{}, err
	}

	var config EnvConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, envConfigFingerprint{}, err
	}
	return &config, envConfigFingerprint{hash: HashContent(data)}, nil
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

// errEnvConfigConflict signals that config.json changed on disk between a
// mutateEnvConfig attempt's load and its save: another process's own
// load-mutate-save cycle landed in between. It never escapes mutateEnvConfig;
// a caller only sees the loud error after retries are exhausted.
var errEnvConfigConflict = errors.New("environment config changed concurrently")

// errEnvConfigNoop lets a mutateEnvConfig callback report "nothing to
// change" (e.g. unregistering a location that isn't registered): the cycle
// returns without ever attempting a save, so a genuine no-op never contends
// for the write or bumps the file's mtime.
var errEnvConfigNoop = errors.New("no change")

// maxEnvConfigMutateAttempts bounds mutateEnvConfig's retry loop. Three
// attempts absorbs an ordinary race between two concurrent `ob` invocations
// without looping indefinitely against a stuck or pathological writer.
const maxEnvConfigMutateAttempts = 3

// saveEnvConfigIfUnchanged writes config back to envPath's config.json only
// if the file's on-disk content still matches the fingerprint captured at
// load. AtomicWriteFile already makes the write itself atomic (temp file +
// rename); this closes the separate load-mutate-save window a second,
// independent `ob` process could land its own write inside, which an atomic
// write alone does not guard against.
func saveEnvConfigIfUnchanged(envPath string, fp envConfigFingerprint, config *EnvConfig) error {
	_, current, err := loadEnvConfigFingerprinted(envPath)
	if err != nil {
		return err
	}
	if current != fp {
		return errEnvConfigConflict
	}
	return SaveEnvConfig(envPath, config)
}

// mutateEnvConfig performs one load-mutate-save cycle against the
// environment config with an optimistic-concurrency guard: it captures the
// file's fingerprint at load, lets mutate apply its change, and refuses the
// save (via saveEnvConfigIfUnchanged) if another process's cycle wrote the
// file in between. On a refusal it retries — reloading the fresh on-disk
// state and re-running mutate against it, so a retried registration or
// preference update is genuinely reapplied against current data rather than
// blindly repeated — up to maxEnvConfigMutateAttempts times, then fails
// loudly rather than silently clobbering the other writer's update.
//
// mutate returns the domain result the caller ultimately wants (e.g. the
// updated DelegateRecord, or whether a record was removed) alongside any
// config mutation; returning errEnvConfigNoop skips the save entirely (nothing
// changed, so nothing to write and nothing that can conflict).
//
// This is the config load/save seam every registry-mutating operation must
// go through — RegisterDelegate, UnregisterDelegate, SetDelegatePreference —
// instead of each doing its own unguarded LoadEnvConfig-mutate-SaveEnvConfig.
func mutateEnvConfig[T any](envPath string, mutate func(*EnvConfig) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for attempt := 0; attempt < maxEnvConfigMutateAttempts; attempt++ {
		config, fp, err := loadEnvConfigFingerprinted(envPath)
		if err != nil {
			return zero, err
		}
		result, err := mutate(config)
		if err != nil {
			if errors.Is(err, errEnvConfigNoop) {
				return result, nil
			}
			return zero, err
		}
		if err := saveEnvConfigIfUnchanged(envPath, fp, config); err != nil {
			if errors.Is(err, errEnvConfigConflict) {
				lastErr = err
				continue
			}
			return zero, err
		}
		return result, nil
	}
	return zero, exitText(1, fmt.Sprintf(
		"environment config at %s changed concurrently; gave up after %d attempts: %v",
		envPath, maxEnvConfigMutateAttempts, lastErr), true)
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
