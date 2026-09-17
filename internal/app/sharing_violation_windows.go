//go:build windows

package app

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isSharingViolation reports the transient Windows condition in which a file
// is being replaced by an atomic rename and is momentarily openable by nobody.
func isSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
