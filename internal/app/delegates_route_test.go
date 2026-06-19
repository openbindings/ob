package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestDelegateOpKey(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			// Keyed bare, with the namespaced interface op as an alias.
			"createInterface": {Aliases: []string{"openbindings.interface-creator.createInterface"}},
			// Keyed by the namespaced interface op directly.
			"openbindings.source-inspector.inspectSource": {},
			"somethingElse": {},
		},
	}

	t.Run("matches by alias", func(t *testing.T) {
		key, ok := delegateOpKey(iface, createOpNames...)
		if !ok || key != "createInterface" {
			t.Fatalf("expected createInterface (via alias), got %q ok=%v", key, ok)
		}
	})

	t.Run("matches by namespaced key", func(t *testing.T) {
		key, ok := delegateOpKey(iface, inspectOpNames...)
		if !ok || key != "openbindings.source-inspector.inspectSource" {
			t.Fatalf("expected the inspect op key, got %q ok=%v", key, ok)
		}
	})

	t.Run("no match", func(t *testing.T) {
		if _, ok := delegateOpKey(iface, "openbindings.binding-invoker.invokeBinding", "invokeBinding"); ok {
			t.Fatal("expected no match for invokeBinding")
		}
	})
}
