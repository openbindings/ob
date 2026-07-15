package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
)

// ---------------------------------------------------------------------------
// DefaultBindingForOp
// ---------------------------------------------------------------------------

func TestDefaultBindingForOp_NilInterface(t *testing.T) {
	_, got := DefaultBindingForOp("test", nil)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDefaultBindingForOp_NoBindings(t *testing.T) {
	iface := &openbindings.Interface{}
	_, got := DefaultBindingForOp("test", iface)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDefaultBindingForOp_SingleMatch(t *testing.T) {
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.usage1": {Operation: "listPets", Source: "usage1", Ref: "list pets"},
		},
	}
	key, got := DefaultBindingForOp("listPets", iface)
	if got == nil {
		t.Fatal("expected binding, got nil")
	}
	if got.Ref != "list pets" {
		t.Errorf("expected ref 'list pets', got %q", got.Ref)
	}
	if key != "listPets.usage1" {
		t.Errorf("expected key 'listPets.usage1', got %q", key)
	}
}

func TestDefaultBindingForOp_NoMatch(t *testing.T) {
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"createPet.usage1": {Operation: "createPet", Source: "usage1"},
		},
	}
	_, got := DefaultBindingForOp("listPets", iface)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDefaultBindingForOp_PreferenceSelection(t *testing.T) {
	lo := 1.0
	hi := 10.0
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.backup":  {Operation: "listPets", Source: "backup", Ref: "backup-ref", Preference: &lo},
			"listPets.primary": {Operation: "listPets", Source: "primary", Ref: "primary-ref", Preference: &hi},
		},
	}
	_, got := DefaultBindingForOp("listPets", iface)
	if got == nil {
		t.Fatal("expected binding, got nil")
	}
	if got.Source != "primary" {
		t.Errorf("expected source 'primary' (higher preference wins), got %q", got.Source)
	}
}

func TestDefaultBindingForOp_NilPreferenceLosesToExplicit(t *testing.T) {
	explicit := 5.0
	iface := &openbindings.Interface{
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.explicit": {Operation: "listPets", Source: "explicit", Ref: "e", Preference: &explicit},
			"listPets.default":  {Operation: "listPets", Source: "default", Ref: "d"}, // nil → 0 (baseline)
		},
	}
	_, got := DefaultBindingForOp("listPets", iface)
	if got == nil {
		t.Fatal("expected binding, got nil")
	}
	if got.Source != "explicit" {
		t.Errorf("expected source 'explicit' (nil preference loses to explicit positive), got %q", got.Source)
	}
}

// ---------------------------------------------------------------------------
// InvokeOBIOperation
// ---------------------------------------------------------------------------

// writeOBIFile writes a JSON OBI to a temp directory and returns the path.
func writeOBIFile(t *testing.T, dir string, iface map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(iface, "", "  ")
	if err != nil {
		t.Fatalf("marshal OBI: %v", err)
	}
	path := filepath.Join(dir, "interface.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write OBI: %v", err)
	}
	return path
}

func TestInvokeOBIOperation_FileNotFound(t *testing.T) {
	_, _, err := InvokeOBIOperation(context.Background(), "/nonexistent/file.json", "test", "", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestInvokeOBIOperation_OperationNotFound(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
	})

	_, _, err := InvokeOBIOperation(context.Background(), obi, "deletePets", "", nil)
	if err == nil {
		t.Fatal("expected error for missing operation")
	}
}

func TestInvokeOBIOperation_NoBinding(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
	})

	_, _, err := InvokeOBIOperation(context.Background(), obi, "listPets", "", nil)
	if err == nil {
		t.Fatal("expected error for missing binding")
	}
}

func TestInvokeOBIOperation_MissingSource(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
		"bindings": map[string]any{
			"listPets.nonexistent": map[string]any{
				"operation": "listPets",
				"source":    "nonexistent",
			},
		},
	})

	_, _, err := InvokeOBIOperation(context.Background(), obi, "listPets", "", nil)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestInvokeOBIOperation_BindingKeyResolvesOperation(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
		"sources": map[string]any{
			"usage1": map[string]any{
				"bindingSpec": "openbindings.usage@1",
				"location":    "./cli.kdl",
			},
		},
		"bindings": map[string]any{
			"listPets.usage1": map[string]any{
				"operation": "listPets",
				"source":    "usage1",
				"ref":       "list pets",
			},
		},
	})

	// Provide only the binding key (no operation key).
	// The operation should be resolved from the binding entry.
	ch, _, err := InvokeOBIOperation(context.Background(), obi, "", "listPets.usage1", nil)
	// We expect it to proceed past operation/binding resolution. It will fail
	// at the handler level (no actual cli.kdl file), which is fine — we're
	// testing that the binding-based path resolves the operation correctly.
	if err != nil {
		// Resolution errors that indicate "not_found" or "binding_not_found" are bugs.
		errStr := err.Error()
		if strings.Contains(errStr, "not_found") || strings.Contains(errStr, "binding_not_found") {
			t.Fatalf("binding key should have resolved the operation, got: %s", errStr)
		}
		// Other errors (e.g., no delegate) are expected — resolution succeeded.
		return
	}
	for range ch {
	}
}

func TestInvokeOBIOperation_BindingKeyNotFound(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
	})

	_, _, err := InvokeOBIOperation(context.Background(), obi, "", "nonexistent.binding", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent binding key")
	}
}

func TestInvokeOBIOperation_InputTransformError(t *testing.T) {
	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"listPets": map[string]any{},
		},
		"sources": map[string]any{
			"usage1": map[string]any{
				"bindingSpec": "openbindings.usage@1",
				"location":    "./cli.kdl",
			},
		},
		"bindings": map[string]any{
			"listPets.usage1": map[string]any{
				"operation":      "listPets",
				"source":         "usage1",
				"ref":            "list pets",
				"inputTransform": "$$$$invalid$$$$",
			},
		},
	})

	_, _, err := InvokeOBIOperation(context.Background(), obi, "listPets", "", map[string]any{"limit": 10})
	if err == nil {
		t.Fatal("expected error for bad input transform")
	}
}

// TestDriveBindingTearsDownOnCancel is a regression test for the WS-disconnect
// leak: when the consumer abandons the output channel (a disconnected client),
// cancelling the lifetime ctx must unblock driveBinding's goroutine and close
// the channel, rather than parking forever on a full, unread channel.
func TestDriveBindingTearsDownOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	invoke := func(c context.Context, _ map[string]any) openbindings.Invocation[any, any] {
		inv := openbindings.NewInvocationImpl[any, any](c)
		go func() {
			// Infinite producer that honors the handle's terminal model:
			// EmitOutput returns a terminal error once the invocation is
			// cancelled, ending the loop.
			for i := 0; ; i++ {
				if err := inv.EmitOutput(i); err != nil {
					return
				}
			}
		}()
		return inv
	}

	ch := driveBinding(ctx, invoke, nil, nil, nil)

	// Confirm the stream is flowing, then ABANDON it (stop draining) so the
	// 16-slot buffer fills and driveBinding parks on its send.
	<-ch
	<-ch

	cancel()

	// The goroutine must exit and close ch promptly. Drain whatever is buffered
	// until the close; a leak would hang here.
	done := make(chan struct{})
	go func() {
		for range ch { //nolint:revive // draining to the close
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("driveBinding did not tear down after cancel (goroutine leak)")
	}
}

// OBI-T-07 on the app-driven path: schema-violating input fails BEFORE any
// dispatch. Regression: the binding path bypassed the SDK operation layer's
// validation, so the mutation executed (side effect!) and the defect was
// then reported as an OUTPUT validation failure blaming the response.
func TestInvokeOBIOperation_InvalidInputNeverReachesWire(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	obi := writeOBIFile(t, dir, map[string]any{
		"openbindings": "0.2.0",
		"operations": map[string]any{
			"createOrder": map[string]any{
				"input": map[string]any{
					"type":       "object",
					"properties": map[string]any{"quantity": map[string]any{"type": "integer"}},
				},
			},
		},
		"sources": map[string]any{
			"api": map[string]any{
				"bindingSpec": "openbindings.openapi@1",
				"content":     map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": "t", "version": "1"}, "servers": []any{map[string]any{"url": srv.URL}}, "paths": map[string]any{"/orders": map[string]any{"post": map[string]any{"operationId": "createOrder", "responses": map[string]any{"200": map[string]any{"description": "ok"}}}}}},
			},
		},
		"bindings": map[string]any{
			"createOrder.api": map[string]any{"operation": "createOrder", "source": "api", "ref": "#/paths/~1orders/post"},
		},
	})

	_, _, err := InvokeOBIOperation(context.Background(), obi, "createOrder", "", map[string]any{"quantity": "five"})
	if err == nil {
		t.Fatal("expected input validation failure")
	}
	if !strings.Contains(err.Error(), "input validation failed") {
		t.Errorf("error should name input validation, got: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("schema-violating input reached the wire: %d requests dispatched", got)
	}

	// The conforming message still dispatches.
	events, _, err := InvokeOBIOperation(context.Background(), obi, "createOrder", "", map[string]any{"quantity": 5})
	if err != nil {
		t.Fatalf("valid input refused: %v", err)
	}
	for range events {
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("valid input should dispatch exactly once, got %d", got)
	}
}
