package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/openbindings/openbindings-go/invoke"
)

func TestBoundCLIFrameOperationsUnbound(t *testing.T) {
	for _, path := range []string{"ob.bound.obi.json", "../server/serve.obi.json"} {
		iface, err := resolveInterface(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, short := range []string{"invokeBinding", "invokeOperation"} {
			key := "openbindings.ob." + short
			op, ok := iface.Operations[key]
			if !ok || op.Input == nil || op.Output == nil {
				t.Fatalf("%s: frame operation removed or weakened: %s", path, key)
			}
			count := 0
			for _, binding := range iface.Bindings {
				if binding.Operation != key {
					continue
				}
				count++
				if path == "ob.bound.obi.json" || binding.Source != "asyncapi" {
					t.Fatalf("%s: misleading frame realization: %+v", path, binding)
				}
			}
			if path != "ob.bound.obi.json" && count != 1 {
				t.Fatalf("missing genuine served stream for %s", key)
			}
			if path == "ob.bound.obi.json" {
				// A real SDK operation invocation must refuse, not route to a
				// collapsed native command via an invented unary/frame adapter.
				call := invoke.Invoke(t.Context(), DefaultInvoker(), iface, invoke.NewOperationSignature[any, any](key))
				defer call.Cancel()
				_, err := call.Outputs().Read(t.Context())
				var failure *invoke.InvocationError
				if !errors.As(err, &failure) || failure.Code != invoke.ErrCodeBindingNotFound {
					t.Fatalf("unbound frame operation %s: want binding-not-found, got %v", key, err)
				}
				for _, configured := range BoundCLIHookTable(iface).DecodeJSON {
					if configured == key {
						t.Fatalf("unbound operation still has Usage recipe: %s", key)
					}
				}
			}
		}
		if _, old := iface.Operations["openbindings.ob.resolveDelegate"]; old {
			t.Fatal("retired shared resolution claim survived")
		}
		for _, op := range iface.Operations {
			for _, alias := range op.Aliases {
				if alias == "openbindings.delegate-manager.resolveDelegate" {
					t.Fatal("native diagnostic masquerades as shared manager resolution")
				}
			}
		}
	}
	recipe, err := os.ReadFile("../../docs/bound-cli-recipe.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"openbindings.ob.invokeBinding", "openbindings.ob.invokeOperation"} {
		if strings.Contains(string(recipe), "| `"+key+"`") {
			t.Fatalf("unbound frame operation advertised in recipe: %s", key)
		}
	}
}
