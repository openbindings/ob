package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// The wire lane reads what the contract lane correctly refuses: a drifted
// service fails OBI-T-08 under `operation invoke`, while `binding invoke
// <obi> <binding-key>` returns the source's own value — post-decode,
// pre-outputTransform, unvalidated, because the wire lane sits below the operation boundary (no T-07/T-08 subject)
// below the contract boundary.
func TestBindingInvoke_WireLaneReadsWhatT08Refuses(t *testing.T) {
	drifted := `{"orders":[{"quantity":2},{"quantity":"three"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(drifted))
	}))
	defer srv.Close()

	spec := fmt.Sprintf(`{"openapi":"3.1.0","info":{"title":"Orders","version":"1"},"servers":[{"url":%q}],
	  "paths":{"/orders":{"get":{"operationId":"listOrders","responses":{"200":{"description":"ok","content":{"application/json":{"schema":
	  {"type":"object","properties":{"orders":{"type":"array","items":{"type":"object","properties":{"quantity":{"type":"integer"}},"required":["quantity"]}}},"required":["orders"]}}}}}}}}}`, srv.URL)

	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	obiPath := filepath.Join(dir, "orders.obi.json")
	if err := runOB(t, "synthesize", specPath, "--name", "Orders", "-o", obiPath); err != nil {
		t.Fatalf("synthesize: %v", err)
	}

	// Contract lane: T-08 refuses through the abstract, code-only error surface.
	stderr := captureStderr(t, func() {
		root := NewRoot()
		root.SetArgs([]string{"operation", "invoke", obiPath, "listOrders"})
		err := root.Execute()
		if er, ok := err.(app.ExitResult); !ok || er.Code == 0 {
			t.Errorf("contract lane must refuse the drifted output, got %v", err)
		}
	})
	if !strings.Contains(stderr, "ERR_OPERATION_VALIDATION_FAILED") {
		t.Errorf("expected T-08 refusal, got: %s", stderr)
	}
	if strings.Contains(stderr, "listOrders.openapi") {
		t.Errorf("abstract refusal leaked the selected binding identity: %s", stderr)
	}

	// Wire lane: reads the drifted truth, exit 0.
	root2 := NewRoot()
	root2.SetArgs([]string{"binding", "invoke", obiPath, "listOrders.openapi"})
	err2 := root2.Execute()
	er2, ok2 := err2.(app.ExitResult)
	if !ok2 || er2.Code != 0 {
		t.Fatalf("wire lane must read the drifted service, got %v", err2)
	}
	var got map[string]any
	if uerr := json.Unmarshal([]byte(er2.Message), &got); uerr != nil {
		t.Fatalf("wire lane must emit the raw value as JSON: %v (%s)", uerr, er2.Message)
	}
	orders, _ := got["orders"].([]any)
	if len(orders) != 2 {
		t.Fatalf("wire truth lost: %s", er2.Message)
	}
	if q := orders[1].(map[string]any)["quantity"]; q != "three" {
		t.Errorf("the nonconforming value must survive untouched, got %v", q)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// was written (the invoke event loop writes terminal errors to stderr
// directly, not through the ExitResult).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
