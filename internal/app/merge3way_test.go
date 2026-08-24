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
		Aliases:     []string{"info", "about"},
		Deprecated:  true,
		Tags:        []string{"system"},
	}
	source := openbindings.Operation{
		Description: "Return identity and metadata about this software",
	}

	merged, _, err := MergeOperation(nil, local, source)
	if err != nil {
		t.Fatalf("MergeOperation returned error: %v", err)
	}

	// Local-only fields preserved.
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

// TestSynthesizeInterface_PopulatesBaseForFirstSync is the integration-level
// regression test for the bootstrap-then-edit-then-sync flow: the very first
// sync must see an exact three-way base, never the legacy nil-base heuristic.
//
// The base's STORAGE differs by lane. Location mode records x-ob.base on
// every source-owned object. Embed mode (the flip's default for local files)
// elides the copies — the embedded content IS the last-synced artifact — and
// reconstructBases rebuilds an equivalent base on demand.
func TestSynthesizeInterface_PopulatesBaseForFirstSync(t *testing.T) {
	dir := t.TempDir()

	kdl := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hello" {}
`
	writeUsageFile(t, dir, "cli.kdl", kdl)

	// Location mode (explicit published pointer): bases are recorded.
	locIface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Sources: []SynthesizeInterfaceSource{
			{BindingSpec: usageFormat, Location: filepath.Join(dir, "cli.kdl"), OutputLocation: "https://example.com/cli.kdl"},
		},
		Name: "app",
	})
	if err != nil {
		t.Fatalf("create (location mode): %v", err)
	}
	for opKey, op := range locIface.Operations {
		if !HasXOB(op.LosslessFields) {
			t.Errorf("operation %q: expected x-ob marker after create", opKey)
			continue
		}
		base, gerr := GetBase(op.LosslessFields)
		if gerr != nil {
			t.Errorf("operation %q: GetBase error: %v", opKey, gerr)
			continue
		}
		if base == nil {
			t.Errorf("operation %q: location mode must record x-ob.base", opKey)
			continue
		}
		fields, ferr := ObjectToFieldMap(op)
		if ferr != nil {
			t.Fatalf("operation %q: ObjectToFieldMap: %v", opKey, ferr)
		}
		if len(base) != len(fields) {
			t.Errorf("operation %q: base has %d fields, op has %d", opKey, len(base), len(fields))
		}
	}
	for bindKey, b := range locIface.Bindings {
		base, gerr := GetBase(b.LosslessFields)
		if gerr != nil || base == nil {
			t.Errorf("binding %q: location mode must record x-ob.base (err=%v)", bindKey, gerr)
		}
	}

	// Embed mode (the default for a local file): bases are elided and
	// reconstruct on demand, equal to the objects' own fields.
	embIface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Sources: []SynthesizeInterfaceSource{
			{BindingSpec: usageFormat, Location: filepath.Join(dir, "cli.kdl")},
		},
		Name: "app",
	})
	if err != nil {
		t.Fatalf("create (embed mode): %v", err)
	}
	var srcKey string
	for k := range embIface.Sources {
		srcKey = k
	}
	if !sourceEmbedsContent(embIface, srcKey) {
		t.Fatalf("local-file synthesis must embed by default")
	}
	rb, ok := reconstructBases(embIface, srcKey)
	if !ok {
		t.Fatal("embed-mode bases must reconstruct from the embedded content")
	}
	for opKey, op := range embIface.Operations {
		if !HasXOB(op.LosslessFields) {
			t.Errorf("operation %q: expected x-ob marker after create", opKey)
			continue
		}
		if base, _ := GetBase(op.LosslessFields); base != nil {
			t.Errorf("operation %q: embed mode must elide the recorded base", opKey)
		}
		recon := baseForOp(op.LosslessFields, opKey, &rb)
		if recon == nil {
			t.Errorf("operation %q: no reconstructed base", opKey)
			continue
		}
		fields, ferr := ObjectToFieldMap(op)
		if ferr != nil {
			t.Fatalf("operation %q: ObjectToFieldMap: %v", opKey, ferr)
		}
		if len(recon) != len(fields) {
			t.Errorf("operation %q: reconstructed base has %d fields, op has %d", opKey, len(recon), len(fields))
		}
	}
	for bindKey, b := range embIface.Bindings {
		if base, _ := GetBase(b.LosslessFields); base != nil {
			t.Errorf("binding %q: embed mode must elide the recorded base", bindKey)
		}
		if rb.binds[bindKey] == nil {
			t.Errorf("binding %q: no reconstructed base", bindKey)
		}
	}
}

// TestMergeBinding_NilBase_PreservesLocalOnlyFields is the binding-side
// equivalent. Local-only fields like a hand-authored description used to be
// wiped on the first sync after bootstrap.
func TestMergeBinding_NilBase_PreservesLocalOnlyFields(t *testing.T) {
	local := openbindings.BindingEntry{
		Operation:   "getMe",
		Source:      "openapi",
		Selector:    "#/paths/~1v0~1account~1me/get",
		Description: "hand-authored note",
	}
	source := openbindings.BindingEntry{
		Operation: "getMe",
		Source:    "openapi",
		Selector:  "#/paths/~1v0~1account~1me/get",
	}

	merged, _, err := MergeBinding(nil, local, source)
	if err != nil {
		t.Fatalf("MergeBinding returned error: %v", err)
	}

	// description is local-only (source has none) → preserved.
	if merged.Description != "hand-authored note" {
		t.Errorf("expected local description preserved, got %q", merged.Description)
	}
	// ref is in both → source wins (but matches anyway).
	if merged.Selector != "#/paths/~1v0~1account~1me/get" {
		t.Errorf("expected ref preserved, got %q", merged.Selector)
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
