package app

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/openbindings/openbindings-go"
)

func fields(kv ...string) map[string]json.RawMessage {
	m := make(map[string]json.RawMessage)
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = json.RawMessage(kv[i+1])
	}
	return m
}

func TestThreeWayMerge_NoChanges(t *testing.T) {
	base := fields("description", `"hello"`)
	local := fields("description", `"hello"`)
	source := fields("description", `"hello"`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge")
	}
	if mr.HasChanges() {
		t.Error("expected no changes")
	}
	if len(mr.Merged) != 1 {
		t.Errorf("expected 1 merged fields, got %d", len(mr.Merged))
	}
}

func TestThreeWayMerge_SourceUpdated(t *testing.T) {
	base := fields("description", `"hello"`)
	local := fields("description", `"hello"`)
	source := fields("description", `"updated hello"`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge")
	}
	if !mr.HasChanges() {
		t.Error("expected changes from source")
	}
	if len(mr.Updated) != 1 || mr.Updated[0] != "description" {
		t.Errorf("expected description updated, got %v", mr.Updated)
	}
	// Verify source value was accepted.
	if string(mr.Merged["description"]) != `"updated hello"` {
		t.Errorf("expected source description, got %s", mr.Merged["description"])
	}
}

func TestThreeWayMerge_UserChanged(t *testing.T) {
	base := fields("description", `"hello"`)
	local := fields("description", `"my custom"`)
	source := fields("description", `"hello"`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge")
	}
	if mr.HasChanges() {
		t.Error("expected no source changes (user change preserved)")
	}
	if len(mr.Preserved) != 1 || mr.Preserved[0] != "description" {
		t.Errorf("expected description preserved, got %v", mr.Preserved)
	}
	if string(mr.Merged["description"]) != `"my custom"` {
		t.Errorf("expected local description, got %s", mr.Merged["description"])
	}
}

func TestThreeWayMerge_BothChangedSame(t *testing.T) {
	base := fields("description", `"hello"`)
	local := fields("description", `"both agree"`)
	source := fields("description", `"both agree"`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge when both changed to same value")
	}
}

func TestThreeWayMerge_Conflict(t *testing.T) {
	base := fields("description", `"hello"`)
	local := fields("description", `"my version"`)
	source := fields("description", `"source version"`)

	mr := ThreeWayMerge(base, local, source)

	if mr.IsClean() {
		t.Error("expected conflict")
	}
	if len(mr.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(mr.Conflicts))
	}
	c := mr.Conflicts[0]
	if c.Field != "description" {
		t.Errorf("expected conflict on description, got %s", c.Field)
	}
	// Local value should be kept.
	if string(mr.Merged["description"]) != `"my version"` {
		t.Errorf("expected local value kept, got %s", mr.Merged["description"])
	}
}

func TestThreeWayMerge_SourceAddsField(t *testing.T) {
	base := fields("description", `"hello"`)
	local := fields("description", `"hello"`)
	source := fields("description", `"hello"`, "deprecated", `true`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge")
	}
	if len(mr.Added) != 1 || mr.Added[0] != "deprecated" {
		t.Errorf("expected deprecated added, got %v", mr.Added)
	}
	if string(mr.Merged["deprecated"]) != `true` {
		t.Errorf("expected deprecated=true, got %s", mr.Merged["deprecated"])
	}
}

func TestThreeWayMerge_UserAddsField(t *testing.T) {
	base := fields("deprecated", `false`)
	local := fields("deprecated", `false`, "description", `"user added"`)
	source := fields("deprecated", `false`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge")
	}
	if len(mr.Preserved) != 1 || mr.Preserved[0] != "description" {
		t.Errorf("expected description preserved, got %v", mr.Preserved)
	}
}

func TestThreeWayMerge_SourceRemovesField(t *testing.T) {
	base := fields("description", `"hello"`, "deprecated", `true`)
	local := fields("description", `"hello"`, "deprecated", `true`)
	source := fields("description", `"hello"`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge")
	}
	if _, ok := mr.Merged["deprecated"]; ok {
		t.Error("expected deprecated removed by source")
	}
}

func TestThreeWayMerge_UserRemovesField(t *testing.T) {
	base := fields("description", `"hello"`, "deprecated", `true`)
	local := fields("description", `"hello"`)
	source := fields("description", `"hello"`, "deprecated", `true`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge (user removed, source unchanged)")
	}
	if _, ok := mr.Merged["deprecated"]; ok {
		t.Error("expected deprecated to stay removed (user's choice)")
	}
}

func TestThreeWayMerge_ConflictUserRemovedSourceChanged(t *testing.T) {
	base := fields("deprecated", `false`, "description", `"hello"`)
	local := fields("deprecated", `false`)
	source := fields("deprecated", `false`, "description", `"updated"`)

	mr := ThreeWayMerge(base, local, source)

	if mr.IsClean() {
		t.Error("expected conflict (user removed, source changed)")
	}
	if len(mr.Conflicts) != 1 || mr.Conflicts[0].Field != "description" {
		t.Errorf("expected conflict on description, got %v", mr.Conflicts)
	}
}

func TestThreeWayMerge_MixedChanges(t *testing.T) {
	// Source updates input, user changes description. Both should merge cleanly.
	base := fields("description", `"hello"`, "input", `{"type":"object"}`)
	local := fields("description", `"my desc"`, "input", `{"type":"object"}`)
	source := fields("description", `"hello"`, "input", `{"type":"object","required":["name"]}`)

	mr := ThreeWayMerge(base, local, source)

	if !mr.IsClean() {
		t.Error("expected clean merge (non-overlapping changes)")
	}
	// User's description preserved.
	if string(mr.Merged["description"]) != `"my desc"` {
		t.Errorf("expected user description, got %s", mr.Merged["description"])
	}
	// Source's input accepted.
	if string(mr.Merged["input"]) != `{"type":"object","required":["name"]}` {
		t.Errorf("expected source input, got %s", mr.Merged["input"])
	}
}

func TestThreeWayMerge_NilBase(t *testing.T) {
	// First sync — no base. ThreeWayMerge with nil base treats every
	// field as new from both sides, so equal values merge cleanly and
	// unequal values produce conflicts (local wins).
	local := fields("deprecated", `false`, "description", `"local"`)
	source := fields("deprecated", `false`, "description", `"source"`)

	mr := ThreeWayMerge(nil, local, source)

	// Both added "deprecated" with same value: fine.
	// Both added "description" with different values: conflict.
	if mr.IsClean() {
		t.Error("expected conflict on description")
	}
}

// TestMergeOperation_NilBase_PreservesLocalOnlyFields is the regression
// test for the bug where the first sync after `ob create` would discard
// hand-authored local-only operation fields (satisfies, aliases,
// deprecated, tags). The bootstrap writes `x-ob: {}` with no recorded
// base, so GetBase returns nil. Previously MergeOperation short-circuited
// to "return source as-is" in that case, wiping every local-only field.
//
// Contract for nil-base merge:
//   - Fields source has → source wins (recovers the prior "first sync
//     overwrites local from source" behavior, important for the legacy
//     `ob create` → edit source → sync flow)
//   - Fields only local has → preserved (the user added them; source
//     can't have an opinion about them)
//
// Hand-editing a field that ALSO exists in source while base is nil is
// not safe — the heuristic can't tell apart "user authored this" from
// "this came from a previous sync." For that case the user must either
// (a) sync once so create.go records a base, or (b) manually populate
// the x-ob.base field.
func TestMergeOperation_NilBase_PreservesLocalOnlyFields(t *testing.T) {
	local := openbindings.Operation{
		Description: "From source",
		Satisfies: []openbindings.Satisfies{
			{Role: "software-descriptor", Operation: "getInfo"},
		},
		Aliases:    []string{"info", "about"},
		Deprecated: true,
		Tags:       []string{"system"},
	}
	source := openbindings.Operation{
		Description: "Return identity and metadata about this software",
	}

	merged, _, err := MergeOperation(nil, local, source)
	if err != nil {
		t.Fatalf("MergeOperation returned error: %v", err)
	}

	// Local-only fields preserved.
	if len(merged.Satisfies) != 1 {
		t.Fatalf("expected satisfies to be preserved, got %d entries", len(merged.Satisfies))
	}
	if merged.Satisfies[0].Role != "software-descriptor" || merged.Satisfies[0].Operation != "getInfo" {
		t.Errorf("expected satisfies preserved verbatim, got %+v", merged.Satisfies[0])
	}
	if len(merged.Aliases) != 2 || merged.Aliases[0] != "info" || merged.Aliases[1] != "about" {
		t.Errorf("expected aliases preserved, got %v", merged.Aliases)
	}
	if !merged.Deprecated {
		t.Error("expected deprecated:true preserved")
	}
	if len(merged.Tags) != 1 || merged.Tags[0] != "system" {
		t.Errorf("expected tags preserved, got %v", merged.Tags)
	}

	// description is in both — without a base, source wins (legacy
	// behavior preserved). This is the only field type that's "unsafe"
	// to hand-edit before the first sync records a base.
	if merged.Description != "Return identity and metadata about this software" {
		t.Errorf("expected source description (no base = source wins on shared fields), got %q", merged.Description)
	}
}

// TestCreateInterface_PopulatesBaseForFirstSync is the integration-level
// regression test for the bootstrap-then-edit-then-sync flow. It asserts:
//
//  1. After CreateInterface, every managed operation and binding has a
//     populated x-ob.base (so GetBase returns non-nil).
//  2. The base equals what ObjectToFieldMap(op) produces, so the very
//     first ob sync sees an exact three-way merge instead of falling
//     into the legacy nil-base heuristic.
//
// Combined with the merge3way fix, this means hand-edited local fields
// are preserved by the FIRST sync after create — including hand-edits
// to fields the source also has, which is the case the heuristic can't
// handle on its own.
func TestCreateInterface_PopulatesBaseForFirstSync(t *testing.T) {
	dir := t.TempDir()

	kdl := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hello" {}
`
	writeUsageFile(t, dir, "cli.kdl", kdl)

	iface, err := CreateInterface(CreateInterfaceInput{
		Sources: []CreateInterfaceSource{
			{Format: usageFormat, Location: filepath.Join(dir, "cli.kdl")},
		},
		Name: "app",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Every managed operation has a non-nil base after create.
	for opKey, op := range iface.Operations {
		if !HasXOB(op.LosslessFields) {
			t.Errorf("operation %q: expected x-ob marker after create", opKey)
			continue
		}
		base, err := GetBase(op.LosslessFields)
		if err != nil {
			t.Errorf("operation %q: GetBase error: %v", opKey, err)
			continue
		}
		if base == nil {
			t.Errorf("operation %q: x-ob.base is nil after create (legacy bug); want populated map", opKey)
			continue
		}
		// The base should match what the operation actually contains
		// (modulo x-ob itself, which ObjectToFieldMap strips).
		fields, err := ObjectToFieldMap(op)
		if err != nil {
			t.Fatalf("operation %q: ObjectToFieldMap: %v", opKey, err)
		}
		if len(base) != len(fields) {
			t.Errorf("operation %q: base has %d fields, op has %d", opKey, len(base), len(fields))
		}
	}

	// Every managed binding has a non-nil base after create.
	for bindKey, b := range iface.Bindings {
		if !HasXOB(b.LosslessFields) {
			t.Errorf("binding %q: expected x-ob marker after create", bindKey)
			continue
		}
		base, err := GetBase(b.LosslessFields)
		if err != nil {
			t.Errorf("binding %q: GetBase error: %v", bindKey, err)
			continue
		}
		if base == nil {
			t.Errorf("binding %q: x-ob.base is nil after create (legacy bug); want populated map", bindKey)
		}
	}
}

// TestMergeBinding_NilBase_PreservesLocalOnlyFields is the binding-side
// equivalent. Local-only fields like a hand-authored security override
// used to be wiped on the first sync after bootstrap.
func TestMergeBinding_NilBase_PreservesLocalOnlyFields(t *testing.T) {
	local := openbindings.BindingEntry{
		Operation: "getMe",
		Source:    "openapi",
		Ref:       "#/paths/~1v0~1account~1me/get",
		Security:  "bearer",
	}
	source := openbindings.BindingEntry{
		Operation: "getMe",
		Source:    "openapi",
		Ref:       "#/paths/~1v0~1account~1me/get",
	}

	merged, _, err := MergeBinding(nil, local, source)
	if err != nil {
		t.Fatalf("MergeBinding returned error: %v", err)
	}

	// security is local-only (source has no security) → preserved.
	if merged.Security != "bearer" {
		t.Errorf("expected local security preserved, got %q", merged.Security)
	}
	// ref is in both → source wins (but matches anyway).
	if merged.Ref != "#/paths/~1v0~1account~1me/get" {
		t.Errorf("expected ref preserved, got %q", merged.Ref)
	}
}

func TestJsonEqual_Normalization(t *testing.T) {
	// Same semantic value, different formatting.
	a := json.RawMessage(`{"a":1,"b":2}`)
	b := json.RawMessage(`{"b":2,"a":1}`)
	if !jsonEqual(a, b) {
		t.Error("expected equal after normalization")
	}
}

func TestJsonEqual_Different(t *testing.T) {
	a := json.RawMessage(`"hello"`)
	b := json.RawMessage(`"world"`)
	if jsonEqual(a, b) {
		t.Error("expected not equal")
	}
}
