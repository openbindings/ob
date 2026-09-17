//go:build !windows

package app

// isSharingViolation is Windows-only: on POSIX an atomic rename never hides a
// published file from a reader, so there is nothing transient to retry.
func isSharingViolation(error) bool { return false }
