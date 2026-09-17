//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package app

import (
	"errors"
	"os"
)

func tryEnvConfigLock(*os.File) (bool, error) {
	return false, errors.New("environment locking is unsupported on this OS")
}
func unlockEnvConfig(*os.File) {}
func restrictEnvFile(*os.File) error {
	return errors.New("private environment files are unsupported on this OS")
}
func envFileIsPrivate(*os.File) (bool, error) {
	return false, errors.New("private environment files are unsupported on this OS")
}
