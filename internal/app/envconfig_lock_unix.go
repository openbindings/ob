//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package app

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryEnvConfigLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func unlockEnvConfig(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }

func restrictEnvFile(f *os.File) error { return f.Chmod(0600) }

func envFileIsPrivate(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	return info.Mode().Perm() == 0600, nil
}
