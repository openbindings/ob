package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const envLockTimeout = 5 * time.Second

var envLockContended = func() {}

// The lock file is stable: replacing config.json must not replace the lock's
// inode. Never unlink a lock file or infer stale ownership from its contents.
func withEnvConfigLock[T any](envPath string, apply func() (T, error)) (T, error) {
	var zero T
	path := filepath.Join(envPath, ".config.lock")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return zero, errors.New("environment lock must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return zero, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return zero, fmt.Errorf("open environment lock: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return zero, err
	}
	named, err := os.Lstat(path)
	if err != nil || !named.Mode().IsRegular() || !os.SameFile(opened, named) {
		return zero, errors.New("environment lock changed while opening")
	}
	deadline := time.Now().Add(envLockTimeout)
	for {
		acquired, err := tryEnvConfigLock(f)
		if err != nil {
			return zero, fmt.Errorf("lock environment: %w", err)
		}
		if acquired {
			break
		}
		envLockContended()
		if !time.Now().Before(deadline) {
			return zero, errors.New("environment lock timed out; no lock was stolen")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer unlockEnvConfig(f)
	return apply()
}
