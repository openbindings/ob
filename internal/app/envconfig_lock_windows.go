package app

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
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

// envFileIsPrivate answers whether the file grants access to this user alone.
//
// It compares what the DACL MEANS, not how it serializes. Windows sets control
// flags of its own on a descriptor it returns — SE_DACL_AUTO_INHERITED in
// particular — so the SDDL text of a DACL read back is not the SDDL text that
// established it ("D:PAI(...)" versus "D:P(...)"), and a string comparison
// rejects a file whose access is exactly right. That made a re-published
// migration backup unreadable on Windows even though restrictEnvFile had just
// written it.
//
// Owner-only means: inheritance is blocked, and the only grant is full access
// to this user. Anything else, including an extra allowed entry, a denial or a
// second user, is not private.
func envFileIsPrivate(f *os.File) (bool, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, err
	}
	actual, err := windows.GetNamedSecurityInfo(f.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	control, _, err := actual.Control()
	if err != nil {
		return false, err
	}
	// An unprotected DACL inherits from the directory, so the file's access is
	// not owner-only however its own entries read.
	if control&windows.SE_DACL_PROTECTED == 0 {
		return false, nil
	}
	dacl, _, err := actual.DACL()
	if err != nil {
		return false, err
	}
	if dacl == nil || dacl.AceCount != 1 {
		return false, nil
	}
	// The expected grant comes from the same descriptor restrictEnvFile
	// applies, so the two cannot drift apart.
	expected, err := ownerOnlyDescriptor()
	if err != nil {
		return false, err
	}
	expectedDACL, _, err := expected.DACL()
	if err != nil || expectedDACL == nil || expectedDACL.AceCount != 1 {
		return false, err
	}
	var want, got *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(expectedDACL, 0, &want); err != nil {
		return false, err
	}
	if err := windows.GetAce(dacl, 0, &got); err != nil {
		return false, err
	}
	if got.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || got.Mask != want.Mask {
		return false, nil
	}
	sid := (*windows.SID)(unsafe.Pointer(&got.SidStart))
	return sid.Equals(user.User.Sid), nil
}
