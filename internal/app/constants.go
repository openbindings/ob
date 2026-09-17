// Package app - constants.go centralizes magic strings and configuration values.
package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// Directory and file paths for the OpenBindings CLI (ob) configuration.
const (
	// EnvDir is the project-local environment directory name.
	EnvDir = ".openbindings"

	// GlobalConfigDir is the application subdirectory within the OS config directory.
	GlobalConfigDir = "openbindings"

	// EnvConfigFile is the environment-level configuration file.
	EnvConfigFile = "config.json"

	// ContextsDir is the subdirectory for named context config files.
	ContextsDir = "contexts"

	// KeychainService is the service name used in the OS keychain.
	KeychainService = "openbindings"
)

// GlobalConfigPath returns the platform-appropriate global config directory
// for OpenBindings (e.g. ~/.config/openbindings on Linux,
// ~/Library/Application Support/openbindings on macOS).
func GlobalConfigPath() (string, error) {
	if dir, set, err := applicationDirectory("OB_CONFIG_DIR"); set {
		return dir, err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config directory: %w", err)
	}
	return filepath.Join(configDir, GlobalConfigDir), nil
}

// Application-specific paths avoid changing process-wide HOME semantics for
// headless instances and isolated consumers. Invalid explicit paths never fall
// back to another environment. Paths name the application directory itself.
func applicationDirectory(name string) (string, bool, error) {
	dir, set := os.LookupEnv(name)
	if !set {
		return "", false, nil
	}
	if !filepath.IsAbs(dir) {
		return "", true, fmt.Errorf("%s must name an absolute directory", name)
	}
	return filepath.Clean(dir), true, nil
}

// File permissions.
const (
	// DirPerm is the permission mode for directories.
	DirPerm = 0o755

	// FilePerm is the permission mode for regular files.
	FilePerm = 0o644
)

// Probe status values.
const (
	ProbeStatusIdle    = "idle"
	ProbeStatusProbing = "probing"
	ProbeStatusOK      = "ok"
	ProbeStatusBad     = "bad"
)

// Operation run status values.
const (
	RunStatusIdle      = "idle"
	RunStatusRunning   = "running"
	RunStatusStreaming = "streaming"
	RunStatusSuccess   = "success"
	RunStatusError     = "error"
)
