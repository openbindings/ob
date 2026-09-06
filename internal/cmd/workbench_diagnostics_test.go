package cmd

import (
	"fmt"
	"github.com/openbindings/ob/internal/app"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"
)

func TestWorkbenchDiagnosticsBoundedAndSingleRead(t *testing.T) {
	bank := &workbenchDiagnostics{}
	id := "00000000-0000-0000-0000-000000000001"
	if bank.begin(id) == nil || bank.begin(id) != nil {
		t.Fatal("duplicate id replaced evidence")
	}
	if bank.begin("secret") != nil {
		t.Fatal("accepted an arbitrary key")
	}
	for i := 2; i <= 128; i++ {
		if bank.begin(fmt.Sprintf("00000000-0000-0000-0000-%012d", i)) == nil {
			t.Fatal(i)
		}
	}
	if bank.begin("00000000-0000-0000-0000-000000000129") != nil {
		t.Fatal("unbounded collector retention")
	}
	bank.entries[id] = workbenchDiagnosticEntry{time.Now().Add(-3 * time.Minute), invoke.NewDiagnosticCollector(8)}
	if bank.begin("00000000-0000-0000-0000-000000000129") == nil {
		t.Fatal("expired slot not reclaimed")
	}
}

func TestWorkbenchDiagnosticsAuthenticatedAndSeparateFromFrames(t *testing.T) {
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()
	ts := testEnv(t)
	defer ts.Close()
	id := "00000000-0000-0000-0000-000000000010"
	conn := dialFrameWSAt(t, t.Context(), ts, "/operations/invoke?diagnostics="+id, "test-token")
	sendFrame(t, t.Context(), conn, map[string]any{"kind": "open", "input": map[string]any{"interface": echoOperationInterface(), "operation": "echo"}})
	sendFrame(t, t.Context(), conn, map[string]any{"kind": "input", "value": map[string]any{"secret": "never-in-diagnostics"}})
	sendFrame(t, t.Context(), conn, map[string]any{"kind": "close"})
	_, terminal := collectUntilTerminal(t, t.Context(), conn)
	if terminal.Error == nil || terminal.Error["code"] != invoke.ErrCodeOperationValidationFailed || terminal.Error["data"] != nil {
		t.Fatalf("portable error changed: %+v", terminal)
	}
	endpoint := ts.URL + "/workbench/diagnostics/" + id
	response, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unprotected evidence: %d", response.StatusCode)
	}
	request, _ := http.NewRequest("GET", endpoint, nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(data), `"phase":"input"`) || strings.Contains(string(data), "never-in-diagnostics") || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe/missing diagnostics: %d %s", response.StatusCode, data)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("diagnostics replayed")
	}
}
