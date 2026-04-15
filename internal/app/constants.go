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
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config directory: %w", err)
	}
	return filepath.Join(configDir, GlobalConfigDir), nil
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
	RunStatusStreaming  = "streaming"
	RunStatusSuccess   = "success"
	RunStatusError     = "error"
)
