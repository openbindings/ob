package cmd

import (
	"bytes"
	"strings"
	"testing"
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
