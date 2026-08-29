package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// TestSourcePullOutputPath: -o names the DOCUMENT's destination. The summary
// must never be written to that same path — a regression guard for the cmd
// wiring that used to hand the summary to the -o path after SourcePull had
// already written the pulled document there, clobbering it.
func TestSourcePullOutputPath(t *testing.T) {
	dir := t.TempDir()
	openapi := `{"openapi":"3.1.0","info":{"title":"t","version":"1.0.0"},"paths":{"/ping":{"get":{"operationId":"getPing","responses":{"200":{"description":"ok"}}}}}}`
	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), []byte(openapi), 0o644); err != nil {
		t.Fatal(err)
	}
	obiPath := filepath.Join(dir, "iface.obi.json")
	if _, err := app.NewInterface(app.NewInterfaceInput{Path: obiPath, Name: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SourceAdd(app.SourceAddInput{OBIPath: obiPath, Format: "openbindings.openapi-3.1@1", Location: filepath.Join(dir, "openapi.json"), Key: "api"}); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "dist.obi.json")
	if err := runOB(t, "source", "pull", obiPath, "-o", outPath, "--pure"); err != nil {
		t.Fatalf("pull: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("-o must receive a JSON interface document: %v\n%s", err, data)
	}
	ops, _ := doc["operations"].(map[string]any)
	if _, ok := ops["getPing"]; !ok {
		t.Fatalf("-o must receive the pulled interface document, got: %s", data)
	}
}
