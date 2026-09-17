package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

// EnvConfig represents environment-level configuration stored in .openbindings/config.json.
type EnvConfig struct {
	// DelegateRegistry is the new native registry block. It stays raw here so
	// unrelated environment writers cannot project away metadata they do not own.
	DelegateRegistry json.RawMessage            `json:"delegateRegistry,omitempty"`
	Extra            map[string]json.RawMessage `json:"-"`
	loaded           *envConfigFingerprint
	// Delegates is the delegate registry: one record per registered delegate,
	// in registration order. The self-delegate is builtin, never persisted.
	Delegates []DelegateRecord `json:"delegates,omitempty"`

	// AuthorizedExec is the USAGE-P-02 authorization list: exec addresses
	// (exact strings, e.g. "exec:mytool usage") the operator has explicitly
	// authorized this environment to dereference. Recorded by explicit
	// operator action (typing the address at intake); the default for any
	// unlisted address is refusal.
	AuthorizedExec []string `json:"authorizedExec,omitempty"`
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

// envConfigFingerprint identifies a loaded snapshot. Conditional whole-document
// saves compare it while holding the same lock as transactional mutations.
type envConfigFingerprint struct {
	hash   string
	absent bool
}

// loadEnvConfigFingerprinted reads one atomically published snapshot.
func loadEnvConfigFingerprinted(envPath string) (*EnvConfig, envConfigFingerprint, error) {
	configPath := filepath.Join(envPath, EnvConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			fp := envConfigFingerprint{absent: true}
			return &EnvConfig{loaded: &fp}, fp, nil
		}
		return nil, envConfigFingerprint{}, err
	}

	var config EnvConfig
	if err := jsonvalue.Unmarshal(data, &config); err != nil {
		return nil, envConfigFingerprint{}, err
	}
	fp := envConfigFingerprint{hash: HashContent(data)}
	config.loaded = &fp
	return &config, fp, nil
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

// SaveEnvConfig conditionally saves a loaded snapshot. A fresh value may only
// create an absent config. Callers applying mutations use mutateEnvConfig;
// stale full-document saves are refused, never merged by guessing ownership.
func SaveEnvConfig(envPath string, config *EnvConfig) error {
	if config == nil {
		return errors.New("nil environment configuration")
	}
	_, err := withEnvConfigLock(envPath, func() (struct{}, error) {
		_, current, err := loadEnvConfigFingerprinted(envPath)
		if err != nil {
			return struct{}{}, err
		}
		expected := envConfigFingerprint{absent: true}
		if config.loaded != nil {
			expected = *config.loaded
		}
		if current != expected {
			return struct{}{}, errEnvConfigConflict
		}
		return struct{}{}, writeEnvConfigLocked(envPath, config)
	})
	return err
}

// errEnvConfigConflict refuses a stale whole-document SaveEnvConfig call.
var errEnvConfigConflict = errors.New("environment config changed concurrently")

// errEnvConfigNoop lets a mutateEnvConfig callback report "nothing to
// change" (e.g. unregistering a location that isn't registered): the cycle
// returns without ever attempting a save, so a genuine no-op never contends
// for the write or bumps the file's mtime.
var errEnvConfigNoop = errors.New("no change")

// mutateEnvConfig serializes the ENTIRE reload/validate/mutate/commit cycle
// across processes. Callbacks must not recursively acquire this same lock.
// This is advisory local-filesystem coordination among participating versions;
// legacy binaries must be quiesced before conversion.
func mutateEnvConfig[T any](envPath string, mutate func(*EnvConfig) (T, error)) (T, error) {
	var zero T
	return withEnvConfigLock(envPath, func() (T, error) {
		config, _, err := loadEnvConfigFingerprinted(envPath)
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
		if err := writeEnvConfigLocked(envPath, config); err != nil {
			return zero, err
		}
		return result, nil
	})
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
