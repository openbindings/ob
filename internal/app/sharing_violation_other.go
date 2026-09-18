//go:build !windows

package app

import "os"

// openPublishedHandle opens a file for reading. An atomic rename never hides a
// published file from a POSIX reader: the old inode stays readable until the
// last handle closes, so an ordinary open is already the right sharing mode.
func openPublishedHandle(path string) (*os.File, error) { return os.Open(path) }

// isSharingViolation is Windows-only: there is nothing transient to retry here.
func isSharingViolation(error) bool { return false }
