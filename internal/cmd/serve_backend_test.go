package cmd

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/app"
)

func invokeServedOperation(t *testing.T, ctx context.Context, iface *openbindings.Interface, operation string, input any) any {
	t.Helper()
	sig := openbindings.NewOperationSignature[any, any]("openbindings.ob." + operation)
	inv := openbindings.Invoke(ctx, app.DefaultInvoker(), iface, sig,
		openbindings.WithContext(map[string]any{"bearerToken": "test-token"}))
	if input != nil {
		if err := inv.Write(ctx, input); err != nil {
			t.Fatalf("%s write: %v", operation, err)
		}
	}
	if err := inv.Close(); err != nil {
		t.Fatalf("%s close: %v", operation, err)
	}
	outputs := inv.Outputs()
	out, err := outputs.Read(ctx)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		t.Fatalf("%s output: %v", operation, err)
	}
	if _, err := outputs.Read(ctx); err == nil {
		outputs.Stop()
		t.Fatalf("%s output: expected zero or one output, got more", operation)
	} else if !errors.Is(err, io.EOF) {
		t.Fatalf("%s output: %v", operation, err)
	}
	return out
}

// This is the flagship backend path: discover ob start's OBI, operation-invoke
// its generated OpenAPI binding, feed the returned document into a second
// authoring operation, and get another valid document back.
func TestServeBackend_DogfoodDocumentAuthoringViaOBI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OB_CREDENTIALS_FILE", filepath.Join(home, "credentials.json"))

	ts := testEnv(t)
	defer ts.Close()

	served, err := app.ResolveInterface(ts.URL)
	if err != nil {
		t.Fatalf("resolve served OBI: %v", err)
	}
	if got, want := len(served.Operations), 50; got != want {
		t.Fatalf("served operations = %d, want %d", got, want)
	}

	ctx := t.Context()
	created := invokeServedOperation(t, ctx, served, "newInterface", map[string]any{
		"name": "Dogfood API", "version": "1.0.0",
	})
	createdMap, ok := created.(map[string]any)
	if !ok {
		t.Fatalf("newInterface output = %T, want object", created)
	}
	if createdMap["name"] != "Dogfood API" {
		t.Fatalf("newInterface name = %#v", createdMap["name"])
	}

	updated := invokeServedOperation(t, ctx, served, "addOperation", map[string]any{
		"interface":   created,
		"key":         "hello",
		"description": "Say hello.",
		"input":       map[string]any{"type": "string"},
		"output":      map[string]any{"type": "string"},
	})
	updatedMap, ok := updated.(map[string]any)
	if !ok {
		t.Fatalf("addOperation output = %T, want object", updated)
	}
	operations, ok := updatedMap["operations"].(map[string]any)
	if !ok {
		t.Fatalf("operations = %#v", updatedMap["operations"])
	}
	if _, ok := operations["hello"]; !ok {
		t.Fatalf("updated interface is missing hello: %#v", operations)
	}

	listed := invokeServedOperation(t, ctx, served, "listOperations", map[string]any{"interface": updated})
	entries, ok := listed.([]any)
	if !ok || len(entries) != 1 || entries[0].(map[string]any)["key"] != "hello" {
		t.Fatalf("listOperations output = %#v", listed)
	}

	renamed := invokeServedOperation(t, ctx, served, "renameOperation", map[string]any{
		"interface": updated, "oldKey": "hello", "newKey": "greet",
	})
	renamedOps := renamed.(map[string]any)["operations"].(map[string]any)
	if _, ok := renamedOps["greet"]; !ok {
		t.Fatalf("renameOperation output is missing greet: %#v", renamedOps)
	}

	metadata := invokeServedOperation(t, ctx, served, "setMetadata", map[string]any{
		"interface": renamed, "description": "Authored entirely through ob start.",
	})
	if got := metadata.(map[string]any)["description"]; got != "Authored entirely through ob start." {
		t.Fatalf("setMetadata description = %#v", got)
	}

	removed := invokeServedOperation(t, ctx, served, "removeOperation", map[string]any{
		"interface": metadata, "keys": []any{"greet"},
	})
	if got := len(removed.(map[string]any)["operations"].(map[string]any)); got != 0 {
		t.Fatalf("removeOperation left %d operations", got)
	}

	contextKey := "https://dogfood.example.test/api"
	setResult := invokeServedOperation(t, ctx, served, "setContext", map[string]any{
		"key":   contextKey,
		"value": map[string]any{"metadata": map[string]any{"suite": "ob-start"}},
	})
	if setResult != nil {
		t.Fatalf("setContext output = %#v, want null from 204", setResult)
	}
	stored := invokeServedOperation(t, ctx, served, "getContext", map[string]any{"key": contextKey})
	if stored.(map[string]any)["metadata"].(map[string]any)["suite"] != "ob-start" {
		t.Fatalf("getContext output = %#v", stored)
	}
	removeResult := invokeServedOperation(t, ctx, served, "removeContext", map[string]any{"key": contextKey})
	if removeResult != nil {
		t.Fatalf("removeContext output = %#v, want null from 204", removeResult)
	}
}
