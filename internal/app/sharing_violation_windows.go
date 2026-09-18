//go:build windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// openPublishedHandle opens a file that another process may replace by atomic
// rename while this handle is open.
//
// Go's os.Open does not pass FILE_SHARE_DELETE, so a reader holding an
// ordinary handle blocks the writer's rename — the mirror image of the reader
// failing, and just as wrong. POSIX gives the property for free: the old inode
// stays readable until the last handle closes, and the rename never waits.
// Asking for FILE_SHARE_DELETE is how Windows says the same thing.
func openPublishedHandle(path string) (*os.File, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := windows.CreateFile(
		wide,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// isSharingViolation reports the transient condition in which a file is being
// replaced and is momentarily openable by nobody. Sharing the handle for
// delete removes the common case; a replacement still has an instant where the
// name resolves to neither file.
func isSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
