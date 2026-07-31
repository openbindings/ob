package cmd

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

func TestWorkbenchURLCarriesTokenOnlyInFragment(t *testing.T) {
	got := workbenchURL("http://127.0.0.1:20290/", "token with spaces")
	want := "http://127.0.0.1:20290/#token=token+with+spaces"
	if got != want {
		t.Fatalf("workbenchURL() = %q, want %q", got, want)
	}
	if strings.Contains(strings.SplitN(got, "#", 2)[0], "token") {
		t.Fatalf("token leaked before URL fragment: %q", got)
	}
}

func TestPrintStartSummaryInteractive(t *testing.T) {
	var out bytes.Buffer
	printStartSummary(&out, startSummary{
		WorkbenchURL: "http://127.0.0.1:20290/#token=secret",
		InterfaceURL: "http://127.0.0.1:20290/.well-known/openbindings",
		HTTPURL:      "http://127.0.0.1:20290",
		Interactive:  true,
	})

	got := out.String()
	for _, want := range []string{
		"OpenBindings is ready",
		"Workbench  http://127.0.0.1:20290/#token=secret",
		"Interface  http://127.0.0.1:20290/.well-known/openbindings",
		"Press Ctrl+C to stop",
		"Add --verbose",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "time=") || strings.Contains(got, "level=") {
		t.Errorf("interactive summary looks like structured logs:\n%s", got)
	}
}

func TestPrintStartSummaryNonInteractiveDoesNotContainCredential(t *testing.T) {
	var out bytes.Buffer
	printStartSummary(&out, startSummary{
		WorkbenchURL: "http://127.0.0.1:20290/#token=secret",
		InterfaceURL: "http://127.0.0.1:20290/.well-known/openbindings",
		HTTPURL:      "http://127.0.0.1:20290",
		Interactive:  false,
	})

	got := out.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "#token=") {
		t.Fatalf("noninteractive output leaked a workbench credential:\n%s", got)
	}
	for _, want := range []string{
		"ob start listening on http://127.0.0.1:20290",
		"OpenBindings interface: http://127.0.0.1:20290/.well-known/openbindings",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

// TestStartRejectsInvalidPortFlag pins the --port flag to the same validation
// OB_START_PORT gets (parsePort): an out-of-range port is a usage error up
// front, not ten futile bind attempts at 99999..100008.
func TestStartRejectsInvalidPortFlag(t *testing.T) {
	for _, bad := range []string{"99999", "0", "-1"} {
		t.Run(bad, func(t *testing.T) {
			cmd := newStartCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--port", bad})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected --port %s to be rejected before binding", bad)
			}
			var exit app.ExitResult
			if !errors.As(err, &exit) || exit.Code != 2 {
				t.Fatalf("expected a usage ExitResult (code 2), got %T: %v", err, err)
			}
			if !strings.Contains(err.Error(), bad) {
				t.Fatalf("error should name the invalid port %s: %v", bad, err)
			}
		})
	}
}

// TestStartStrictPortFailsWhenBusy pins the --strict-port contract: a busy
// requested port exits with a hard error instead of falling back.
func TestStartStrictPortFailsWhenBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	cmd := newStartCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--port", strconv.Itoa(port), "--strict-port"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected a hard error for a busy port under --strict-port")
	}
	var exit app.ExitResult
	if !errors.As(err, &exit) || exit.Code != 1 {
		t.Fatalf("expected an ExitResult with code 1, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("error should name the busy port %d: %v", port, err)
	}
}

// TestPrintStartSummaryAnnouncesPortFallback: when the server bound a port
// other than the requested one, both interactive and non-interactive output
// must say so.
func TestPrintStartSummaryAnnouncesPortFallback(t *testing.T) {
	for _, interactive := range []bool{true, false} {
		name := "noninteractive"
		if interactive {
			name = "interactive"
		}
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			printStartSummary(&out, startSummary{
				WorkbenchURL:  "http://127.0.0.1:20292/",
				InterfaceURL:  "http://127.0.0.1:20292/.well-known/openbindings",
				HTTPURL:       "http://127.0.0.1:20292",
				Interactive:   interactive,
				RequestedPort: 20290,
				ActualPort:    20292,
			})
			if got := out.String(); !strings.Contains(got, "Port 20290 was in use — serving on 20292.") {
				t.Fatalf("summary does not announce the port fallback:\n%s", got)
			}
		})
	}
}

// TestPrintStartSummarySilentWithoutPortFallback: no substitution, no notice.
func TestPrintStartSummarySilentWithoutPortFallback(t *testing.T) {
	for _, interactive := range []bool{true, false} {
		var out bytes.Buffer
		printStartSummary(&out, startSummary{
			WorkbenchURL:  "http://127.0.0.1:20290/",
			InterfaceURL:  "http://127.0.0.1:20290/.well-known/openbindings",
			HTTPURL:       "http://127.0.0.1:20290",
			Interactive:   interactive,
			RequestedPort: 20290,
			ActualPort:    20290,
		})
		if got := out.String(); strings.Contains(got, "was in use") {
			t.Fatalf("summary announces a fallback that did not happen:\n%s", got)
		}
	}
}

func TestPrintStartSummaryReportsTokenFileWithoutCredential(t *testing.T) {
	var out bytes.Buffer
	printStartSummary(&out, startSummary{
		WorkbenchURL: "http://127.0.0.1:20290/",
		InterfaceURL: "http://127.0.0.1:20290/.well-known/openbindings",
		HTTPURL:      "http://127.0.0.1:20290",
		TokenFile:    "/tmp/ob-start.token",
		Interactive:  false,
	})

	got := out.String()
	if !strings.Contains(got, "Session token written to /tmp/ob-start.token") {
		t.Fatalf("summary did not report token file:\n%s", got)
	}
	if strings.Contains(got, "#token=") {
		t.Fatalf("token-file summary included an authenticated URL:\n%s", got)
	}
}
