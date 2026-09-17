package app

import (
	"strings"
	"testing"
)

// The served/wire-facing read path (status, synthesize/addSource, pull,
// merge) resolves source locations that may be caller-supplied. An exec:
// address reached that way must be default-denied (USAGE-P-02) unless the
// operator explicitly authorized it — otherwise a wire document could make
// the CLI execute an arbitrary command. This locks the gate that lives in
// ReadSourceContent.
func TestReadSourceContent_ExecDefaultDenied(t *testing.T) {
	envConfigTestEnv(t) // fresh environment: nothing authorized

	_, err := ReadSourceContent("exec:touch /tmp/ob-should-not-run", "")
	if err == nil {
		t.Fatal("unauthorized exec ref was executed; want refusal")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("want authorization refusal, got: %v", err)
	}
}

func TestReadSourceContent_ExecAllowedAfterAuthorize(t *testing.T) {
	envConfigTestEnv(t)

	// Authorize the exact address the operator would type. `true` is a
	// standard no-op command present on the test platforms.
	addr := "exec:true"
	if err := RecordAuthorizedExec(addr); err != nil {
		t.Fatalf("authorize exec: %v", err)
	}
	if _, err := ReadSourceContent(addr, ""); err != nil {
		t.Fatalf("authorized exec ref should run, got: %v", err)
	}
}

func TestDelegateRegistrationDoesNotAuthorizeExec(t *testing.T) {
	path := envConfigTestEnv(t)
	config, err := LoadEnvConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	config.Delegates = []DelegateRecord{{Location: "exec:delegate-not-authorized", Operations: []string{}}}
	if err := SaveEnvConfig(path, config); err != nil {
		t.Fatal(err)
	}
	if authorizeExecAddress([]string{"delegate-not-authorized"}) {
		t.Fatal("legacy registration still grants executable authority")
	}
	if err := RecordAuthorizedExec("exec:delegate-not-authorized"); err != nil {
		t.Fatal(err)
	}
	if !authorizeExecAddress([]string{"delegate-not-authorized"}) {
		t.Fatal("explicit authorization no longer works")
	}
}
