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

func TestSameContent(t *testing.T) {
	// Same content, one carrying x-ob provenance — must compare equal (x-ob ignored).
	a := managedOp(openbindings.Operation{Description: "x", Input: map[string]any{"type": "object"}})
	b := openbindings.Operation{Description: "x", Input: map[string]any{"type": "object"}}
	if !sameContent(a, b) {
		t.Error("operations with identical content (ignoring x-ob) should be equal")
	}
	// Different content — must compare unequal.
	c := openbindings.Operation{Description: "y"}
	if sameContent(a, c) {
		t.Error("operations with different content should not be equal")
	}
}

func TestOBIStatusOutput_HasDrift(t *testing.T) {
	clean := OBIStatusOutput{Sources: []SourceStatus{{Managed: true, InSync: true}}}
	if clean.HasDrift() {
		t.Error("an in-sync managed source is not drift")
	}
	drift := OBIStatusOutput{Sources: []SourceStatus{{Managed: true, InSync: false}}}
	if !drift.HasDrift() {
		t.Error("an out-of-sync managed source is drift")
	}
	hand := OBIStatusOutput{Sources: []SourceStatus{{Managed: false, InSync: false}}}
	if hand.HasDrift() {
		t.Error("a hand-authored source is never drift")
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
