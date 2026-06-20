package app

import (
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func managedOp(op openbindings.Operation) openbindings.Operation {
	SetXOB(&op.LosslessFields)
	return op
}

func managedBinding(be openbindings.BindingEntry) openbindings.BindingEntry {
	SetXOB(&be.LosslessFields)
	return be
}

func TestPullSourceInto_CreateOverwritePruneLeaveAuthored(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"getA": managedOp(openbindings.Operation{Description: "old"}), // source-owned: overwrite
			"getB": managedOp(openbindings.Operation{}),                   // source-owned, dropped from source: prune
			"mine": {Description: "hand-authored"},                        // hand-authored: must survive
		},
		Bindings: map[string]openbindings.BindingEntry{
			"getA.api": managedBinding(openbindings.BindingEntry{Operation: "getA", Source: "api", Ref: "getA"}),
			"getB.api": managedBinding(openbindings.BindingEntry{Operation: "getB", Source: "api", Ref: "getB"}),
		},
	}
	derived := DeriveResult{
		Operations: map[string]openbindings.Operation{
			"getA": {Description: "new"},   // overwrite
			"getC": {Description: "fresh"}, // add
		},
		Bindings: map[string]openbindings.BindingEntry{
			"getA.api": {Operation: "getA", Source: "api", Ref: "getA"},
			"getC.api": {Operation: "getC", Source: "api", Ref: "getC"},
		},
	}

	var out SourcePullOutput
	pullSourceInto(iface, "api", derived, &out)

	if iface.Operations["getA"].Description != "new" {
		t.Errorf("getA should be overwritten, got %q", iface.Operations["getA"].Description)
	}
	if _, ok := iface.Operations["getC"]; !ok {
		t.Error("getC should be added")
	}
	if _, ok := iface.Operations["getB"]; ok {
		t.Error("getB should be pruned (dropped from source)")
	}
	if _, ok := iface.Operations["mine"]; !ok {
		t.Error("hand-authored 'mine' must survive a pull")
	}
	if _, ok := iface.Bindings["getB.api"]; ok {
		t.Error("getB.api binding should be pruned")
	}
	if _, ok := iface.Bindings["getC.api"]; !ok {
		t.Error("getC.api binding should be added")
	}
}

func TestPullSourceInto_DoesNotClobberHandAuthored(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"shared": {Description: "hand-authored"}, // no x-ob
		},
		Bindings: map[string]openbindings.BindingEntry{},
	}
	derived := DeriveResult{
		Operations: map[string]openbindings.Operation{
			"shared": {Description: "from source"}, // collides with the hand-authored op
		},
		Bindings: map[string]openbindings.BindingEntry{},
	}

	var out SourcePullOutput
	pullSourceInto(iface, "api", derived, &out)

	if iface.Operations["shared"].Description != "hand-authored" {
		t.Error("a derived op must not clobber a hand-authored op with the same key")
	}
	if len(out.Warnings) == 0 {
		t.Error("expected a warning about the collision")
	}
}

func TestPullSourceInto_KeepsOtherTransportBinding(t *testing.T) {
	// getA is bound to both 'api' and 'grpc'; pulling 'api' must not prune the
	// grpc binding or the op (still bound elsewhere).
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"getA": managedOp(openbindings.Operation{}),
		},
		Bindings: map[string]openbindings.BindingEntry{
			"getA.api":  managedBinding(openbindings.BindingEntry{Operation: "getA", Source: "api", Ref: "getA"}),
			"getA.grpc": managedBinding(openbindings.BindingEntry{Operation: "getA", Source: "grpc", Ref: "GetA"}),
		},
	}
	// 'api' no longer derives getA.
	derived := DeriveResult{Operations: map[string]openbindings.Operation{}, Bindings: map[string]openbindings.BindingEntry{}}

	var out SourcePullOutput
	pullSourceInto(iface, "api", derived, &out)

	if _, ok := iface.Bindings["getA.api"]; ok {
		t.Error("getA.api should be pruned")
	}
	if _, ok := iface.Bindings["getA.grpc"]; !ok {
		t.Error("getA.grpc (other transport) must survive")
	}
	if _, ok := iface.Operations["getA"]; !ok {
		t.Error("getA must survive: still bound by grpc")
	}
}
