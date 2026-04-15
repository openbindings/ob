package app

// ExitResult lets CLI handlers control exit code + whether output goes to stderr.
// This keeps command output clean while still using `error` as the control flow.
// Note: ExitResult is also used for successful output (Code: 0) — the name
// reflects that it controls process exit, not that something went wrong.
type ExitResult struct {
	Code     int
	Message  string
	ToStderr bool
}

func (e ExitResult) Error() string   { return e.Message }
func (e ExitResult) ExitCode() int   { return e.Code }
func (e ExitResult) UseStderr() bool { return e.ToStderr }

// exitText creates an ExitResult with the given code, message, and stderr flag.
func exitText(code int, message string, toStderr bool) error {
	return ExitResult{Code: code, Message: message, ToStderr: toStderr}
}

// usageExit creates an ExitResult for usage errors (code 2, stderr).
func usageExit(message string) error {
	return ExitResult{Code: 2, Message: message, ToStderr: true}
}
