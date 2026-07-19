package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// The streaming invoke lane cannot honor the global output flags: -o
// (write-to-file) has no place on a stdout stream, and -F selects only the
// aggregate-vs-streaming shape (json or unset). Both used to be silently
// ignored; they now refuse loudly (ExitResult code 2) rather than drop a flag
// the user passed.
func TestOperationInvoke_RefusesUnhonoredOutputFlags(t *testing.T) {
	dir := t.TempDir()
	obiPath := filepath.Join(dir, "iface.obi.json")
	if err := runOB(t, "new", obiPath); err != nil {
		t.Fatalf("new: %v", err)
	}

	// -o on the stream: refused before any dispatch.
	err := runOB(t, "operation", "invoke", obiPath, "someOp", "-o", filepath.Join(dir, "out.json"))
	if er, ok := err.(app.ExitResult); !ok || er.Code != 2 {
		t.Fatalf("expected -o refusal (ExitResult code 2), got %v", err)
	}

	// -F to a shape the lane does not produce: refused.
	err = runOB(t, "operation", "invoke", obiPath, "someOp", "-F", "yaml")
	if er, ok := err.(app.ExitResult); !ok || er.Code != 2 {
		t.Fatalf("expected -F yaml refusal (ExitResult code 2), got %v", err)
	}
}

// -o on an editing command redirects the SUMMARY, not the document; pointing
// it at the document being edited used to overwrite the OBI with the summary
// envelope (observed in the DX field test: a 166 KB document reduced to a
// 98-byte summary with exit 0). It must refuse instead, with the edit itself
// preserved.
func TestEditingCommand_RefusesOutputOverEditedDocument(t *testing.T) {
	dir := t.TempDir()
	obiPath := filepath.Join(dir, "iface.obi.json")
	if err := runOB(t, "new", obiPath); err != nil {
		t.Fatalf("new: %v", err)
	}

	err := runOB(t, "operation", "add", obiPath, "healthCheck", "-o", obiPath)
	er, ok := err.(app.ExitResult)
	if !ok || er.Code != 2 {
		t.Fatalf("expected refusal (ExitResult code 2), got %v", err)
	}

	// The edit is applied in place and the document survives as a document.
	data, rerr := os.ReadFile(obiPath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	var doc struct {
		Openbindings string                     `json:"openbindings"`
		Operations   map[string]json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("document destroyed: %v", err)
	}
	if doc.Openbindings == "" {
		t.Fatal("document destroyed: no openbindings field (summary envelope written over it?)")
	}
	if _, okOp := doc.Operations["healthCheck"]; !okOp {
		t.Error("the edit itself should have been applied in place")
	}

	// A distinct -o path is a legitimate summary redirect.
	sumPath := filepath.Join(dir, "summary.json")
	if err := runOB(t, "operation", "add", obiPath, "ping", "-o", sumPath); err != nil {
		t.Fatalf("summary redirect to a distinct path should succeed: %v", err)
	}
	if _, err := os.Stat(sumPath); err != nil {
		t.Errorf("summary file not written: %v", err)
	}
}
