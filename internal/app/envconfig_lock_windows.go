package app

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func tryEnvConfigLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func unlockEnvConfig(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}

func ownerOnlyDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
}

func restrictEnvFile(f *os.File) error {
	// Chmod(0600) does not establish owner-only access on Windows. Set a
	// protected DACL on this empty, exclusively created temporary file BEFORE
	// writing retained documents. The containing directory is operator-trusted.
	sd, err := ownerOnlyDescriptor()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(f.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func envFileIsPrivate(f *os.File) (bool, error) {
	expected, err := ownerOnlyDescriptor()
	if err != nil {
		return false, err
	}
	actual, err := windows.GetNamedSecurityInfo(f.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	return actual.String() == expected.String(), nil
}
