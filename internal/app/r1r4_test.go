package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// R1: an operation answers to its key AND any alias equally (OBI-D-04/T-12),
// so invoke's name resolution must accept an alias.
func TestOperationKeyForName_ResolvesAlias(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"getWidget": {Aliases: []string{"openbindings.catalog.get", "widget.fetch"}},
			"listAll":   {},
		},
	}
	cases := map[string]string{
		"getWidget":                "getWidget", // by key
		"openbindings.catalog.get": "getWidget", // by alias
		"widget.fetch":             "getWidget", // by another alias
		"listAll":                  "listAll",
		"nope":                     "", // unknown
	}
	for name, want := range cases {
		if got := operationKeyForName(name, iface); got != want {
			t.Errorf("operationKeyForName(%q) = %q, want %q", name, got, want)
		}
	}
}

// R4: pre-promotion drafts (bare, dotless tokens) are marked; minted OB
// identifiers, the core version marker, and namespaced third-party identifiers
// are not.
func TestIsDraftBindingSpec(t *testing.T) {
	cases := map[string]bool{
		"graphql":                true,
		"future-binding@^1.0.0":  true,
		"openbindings.graphql@2": false,
		"openbindings.graphql@1": false,
		"openbindings.openapi@1": false,
		"openbindings.grpc@1":    false,
		"openbindings@0.2.0":     false, // core version marker, not a binding spec
		"acme.grpc@1":            false, // namespaced third-party identifier
	}
	for tok, want := range cases {
		if got := isDraftBindingSpec(tok); got != want {
			t.Errorf("isDraftBindingSpec(%q) = %v, want %v", tok, got, want)
		}
	}
}
