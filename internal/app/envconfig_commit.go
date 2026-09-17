package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Injected only by process-recovery tests, never controlled by environment or
// request input in a production executable. A failure after rename is uncertain
// completion: callers must inspect state, not retry anonymous registration.
var envCommitBoundary = func(stage string) error { return nil }

var envCommitSync = func(f *os.File) error { return f.Sync() }
var envCommitReplace = os.Rename

func writeEnvConfigLocked(envPath string, config *EnvConfig) error {
	data, err := jsonvalue.Marshal(config)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeEnvConfigBytesLocked(envPath, data); err != nil {
		return err
	}
	fp := envConfigFingerprint{hash: HashContent(data)}
	config.loaded = &fp
	return nil
}

// Used by explicit rollback to restore exact legacy bytes without projecting
// the old document through a newer typed representation. Caller holds the lock.
func writeEnvConfigBytesLocked(envPath string, data []byte) error {
	path := filepath.Join(envPath, EnvConfigFile)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("environment configuration must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(envPath, ".config-commit-*")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	defer f.Close()
	if err := restrictEnvFile(f); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := envCommitBoundary("temporary-write"); err != nil {
		return err
	}
	if err := envCommitSync(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := envCommitBoundary("before-rename"); err != nil {
		return err
	}
	if err := envCommitReplace(temporary, path); err != nil {
		return err
	}
	if err := envCommitBoundary("after-rename"); err != nil {
		return fmt.Errorf("environment commit may have completed; inspect state before retry: %w", err)
	}
	// File contents were synced before atomic replacement. Directory/power-loss
	// durability is intentionally not claimed by this process-crash contract.
	return nil
}
