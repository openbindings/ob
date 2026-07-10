package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func sourceOwnedOp(op openbindings.Operation) openbindings.Operation {
	SetXOB(&op.LosslessFields)
	return op
}

func sourceOwnedBinding(be openbindings.BindingEntry) openbindings.BindingEntry {
	SetXOB(&be.LosslessFields)
	return be
}

func TestPullSourceInto_CreateOverwritePruneLeaveAuthored(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"getA": sourceOwnedOp(openbindings.Operation{Description: "old"}), // source-owned: overwrite
			"getB": sourceOwnedOp(openbindings.Operation{}),                   // source-owned, dropped from source: prune
			"mine": {Description: "hand-authored"},                            // hand-authored: must survive
		},
		Bindings: map[string]openbindings.BindingEntry{
			"getA.api": sourceOwnedBinding(openbindings.BindingEntry{Operation: "getA", Source: "api", Ref: "getA"}),
			"getB.api": sourceOwnedBinding(openbindings.BindingEntry{Operation: "getB", Source: "api", Ref: "getB"}),
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
	pullSourceInto(iface, "api", derived, nil, &out)

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
	pullSourceInto(iface, "api", derived, nil, &out)

	if iface.Operations["shared"].Description != "hand-authored" {
		t.Error("a derived op must not clobber a hand-authored op with the same key")
	}
	if len(out.Warnings) == 0 {
		t.Error("expected a warning about the collision")
	}
}

func TestSameContent(t *testing.T) {
	// Same content, one carrying x-ob provenance — must compare equal (x-ob ignored).
	a := sourceOwnedOp(openbindings.Operation{Description: "x", Input: map[string]any{"type": "object"}})
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
	clean := OBIStatusOutput{Sources: []SourceStatus{{Tracked: true, InSync: true}}}
	if clean.HasDrift() {
		t.Error("an in-sync tracked source is not drift")
	}
	drift := OBIStatusOutput{Sources: []SourceStatus{{Tracked: true, InSync: false}}}
	if !drift.HasDrift() {
		t.Error("an out-of-sync tracked source is drift")
	}
	hand := OBIStatusOutput{Sources: []SourceStatus{{Tracked: false, InSync: false}}}
	if hand.HasDrift() {
		t.Error("a hand-authored source is never drift")
	}
}

func TestPullSourceInto_KeepsOtherTransportBinding(t *testing.T) {
	// getA is bound to both 'api' and 'grpc'; pulling 'api' must not prune the
	// grpc binding or the op (still bound elsewhere).
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"getA": sourceOwnedOp(openbindings.Operation{}),
		},
		Bindings: map[string]openbindings.BindingEntry{
			"getA.api":  sourceOwnedBinding(openbindings.BindingEntry{Operation: "getA", Source: "api", Ref: "getA"}),
			"getA.grpc": sourceOwnedBinding(openbindings.BindingEntry{Operation: "getA", Source: "grpc", Ref: "GetA"}),
		},
	}
	// 'api' no longer derives getA.
	derived := DeriveResult{Operations: map[string]openbindings.Operation{}, Bindings: map[string]openbindings.BindingEntry{}}

	var out SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &out)

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

// --- SourcePull end-to-end (file in, file out) ---

func TestSourcePull_PartialPull(t *testing.T) {
	dir := t.TempDir()

	kdlA := `min_usage_version "2.0.0"
bin "a"
cmd "one" help="First" {}
`
	kdlB := `min_usage_version "2.0.0"
bin "b"
cmd "two" help="Second" {}
`
	os.WriteFile(filepath.Join(dir, "a.kdl"), []byte(kdlA), 0644)
	os.WriteFile(filepath.Join(dir, "b.kdl"), []byte(kdlB), 0644)

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"name":         "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"srcA": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./a.kdl",
				"x-ob": map[string]any{
					"ref":     "./a.kdl",
					"resolve": "location",
				},
			},
			"srcB": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./b.kdl",
				"x-ob": map[string]any{
					"ref":     "./b.kdl",
					"resolve": "location",
				},
			},
		},
	}
	obiPath := writeInterface(t, dir, "interface.json", obiData)

	result, err := SourcePull(SourcePullInput{
		OBIPath:    obiPath,
		SourceKeys: []string{"srcA"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 1 || result.Sources[0] != "srcA" {
		t.Errorf("expected only srcA pulled, got %v", result.Sources)
	}
}

func TestSourcePull_SkipsHandAuthored(t *testing.T) {
	dir := t.TempDir()

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"name":         "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"manual": map[string]any{
				"format":   "openapi@3.1",
				"location": "https://api.example.com/openapi.json",
				// No x-ob — hand-authored.
			},
		},
	}
	obiPath := writeInterface(t, dir, "interface.json", obiData)

	result, err := SourcePull(SourcePullInput{OBIPath: obiPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 0 {
		t.Errorf("expected 0 pulled, got %d", len(result.Sources))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

func TestSourcePull_Pure(t *testing.T) {
	dir := t.TempDir()

	kdl := `min_usage_version "2.0.0"
bin "hello"
cmd "hello" help="Say hello" {}
`
	os.WriteFile(filepath.Join(dir, "cli.kdl"), []byte(kdl), 0644)

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"name":         "test",
		"operations": map[string]any{
			"hello": map[string]any{"x-ob": map[string]any{}},
		},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "location",
				},
			},
		},
		"bindings": map[string]any{
			"hello.usage": map[string]any{
				"operation": "hello",
				"source":    "usage",
				"ref":       "hello",
				"x-ob":      map[string]any{},
			},
		},
	}
	obiPath := writeInterface(t, dir, "interface.json", obiData)
	purePath := filepath.Join(dir, "pure.json")

	_, err := SourcePull(SourcePullInput{
		OBIPath:    obiPath,
		OutputPath: purePath,
		Pure:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the pure output has no x-ob anywhere.
	data, err := os.ReadFile(purePath)
	if err != nil {
		t.Fatalf("read pure output: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	sources := parsed["sources"].(map[string]any)
	if _, ok := sources["usage"].(map[string]any)["x-ob"]; ok {
		t.Error("expected x-ob stripped from source in pure output")
	}
	ops := parsed["operations"].(map[string]any)
	for key, op := range ops {
		if _, ok := op.(map[string]any)["x-ob"]; ok {
			t.Errorf("expected x-ob stripped from operation %q in pure output", key)
		}
	}
}

func TestSourcePull_PureRequiresOutput(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "interface.json", minimalInterface(map[string]any{}))

	_, err := SourcePull(SourcePullInput{OBIPath: obiPath, Pure: true})
	if err == nil {
		t.Error("expected error: --pure without -o must refuse to strip x-ob in place")
	}
}

func TestSourcePull_ContentMode(t *testing.T) {
	dir := t.TempDir()

	srcContent := `min_usage_version "2.0.0"
bin "hello"
cmd "greet" help="Say hi" {}
`
	os.WriteFile(filepath.Join(dir, "cli.kdl"), []byte(srcContent), 0644)

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"name":         "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":  "usage@2.0.0",
				"content": "old content",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "content",
				},
			},
		},
	}
	obiPath := writeInterface(t, dir, "interface.json", obiData)

	if _, err := SourcePull(SourcePullInput{OBIPath: obiPath}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back and verify the embedded content was refreshed from ref.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	src := parsed["sources"].(map[string]any)["usage"].(map[string]any)
	content, ok := src["content"].(string)
	if !ok {
		t.Fatalf("expected string content, got %T", src["content"])
	}
	if content != srcContent {
		t.Errorf("expected updated content, got %q", content)
	}
}

func TestSourcePull_NonexistentSourceKey(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "interface.json", minimalInterface(map[string]any{}))

	_, err := SourcePull(SourcePullInput{
		OBIPath:    obiPath,
		SourceKeys: []string{"nonexistent"},
	})
	if err == nil {
		t.Error("expected error for nonexistent source key")
	}
}

// TestSourcePull_OBIMissingBindingsMap covers OBIs that have sources (e.g. from
// "source add") but no "bindings" or "operations" keys — pull must not panic
// and should populate them.
func TestSourcePull_OBIMissingBindingsMap(t *testing.T) {
	dir := t.TempDir()

	kdlContent := `min_usage_version "2.0.0"
bin "app"
cmd "greet" help="Say hello" {}
`
	os.WriteFile(filepath.Join(dir, "cli.kdl"), []byte(kdlContent), 0644)

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"name":         "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"usage": map[string]any{
				"format":   "usage@2.0.0",
				"location": "./cli.kdl",
				"x-ob": map[string]any{
					"ref":     "./cli.kdl",
					"resolve": "location",
				},
			},
		},
		// No "bindings" key at all.
	}
	obiPath := writeInterface(t, dir, "interface.json", obiData)

	if _, err := SourcePull(SourcePullInput{OBIPath: obiPath}); err != nil {
		t.Fatalf("pull should not fail: %v", err)
	}

	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	if parsed["bindings"] == nil {
		t.Fatal("expected bindings to be populated after pull")
	}
	if len(parsed["bindings"].(map[string]any)) == 0 {
		t.Error("expected at least one binding after pull")
	}
	if len(parsed["operations"].(map[string]any)) == 0 {
		t.Error("expected at least one operation after pull")
	}
}

// A pull must never destroy the author's satisfaction aliases: they are
// spec-level author data (OBI-T-12) that no binding source owns. The DX field
// test observed a plain re-pull wiping aliases and reporting "updated" on an
// unchanged source.
func TestPullSourceInto_PreservesAuthoredAliases(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{},
		Bindings:   map[string]openbindings.BindingEntry{},
	}
	derived := DeriveResult{
		Operations: map[string]openbindings.Operation{
			"listPets": {Description: "list"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.api": {Operation: "listPets", Source: "api", Ref: "#/paths/~1pets/get"},
		},
	}
	var first SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &first)

	// The author declares satisfaction of a published contract.
	op := iface.Operations["listPets"]
	op.Aliases = []string{"acme.pet-catalog.list"}
	iface.Operations["listPets"] = op

	// An unchanged re-pull neither destroys the alias nor reports drift.
	var second SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &second)
	if got := iface.Operations["listPets"].Aliases; len(got) != 1 || got[0] != "acme.pet-catalog.list" {
		t.Fatalf("authored alias destroyed by an unchanged pull: %v", got)
	}
	if len(second.OperationsUpdated) != 0 {
		t.Errorf("unchanged pull reported updates: %v", second.OperationsUpdated)
	}

	// A real source change updates the op AND carries the alias.
	changed := DeriveResult{
		Operations: map[string]openbindings.Operation{
			"listPets": {Description: "list all pets"},
		},
		Bindings: derived.Bindings,
	}
	var third SourcePullOutput
	pullSourceInto(iface, "api", changed, nil, &third)
	got := iface.Operations["listPets"]
	if got.Description != "list all pets" {
		t.Errorf("source change not applied: %q", got.Description)
	}
	if len(got.Aliases) != 1 || got.Aliases[0] != "acme.pet-catalog.list" {
		t.Fatalf("authored alias lost across a source change: %v", got.Aliases)
	}
	// The merge base stays the PURE derivation: authored names never leak in.
	base, err := GetBase(got.LosslessFields)
	if err != nil {
		t.Fatalf("read base: %v", err)
	}
	if _, ok := base["aliases"]; ok {
		t.Error("authored alias leaked into the x-ob merge base")
	}
	// And the next unchanged pull is again a full no-op.
	var fourth SourcePullOutput
	pullSourceInto(iface, "api", changed, nil, &fourth)
	if len(fourth.OperationsUpdated) != 0 {
		t.Errorf("post-change unchanged pull reported updates: %v", fourth.OperationsUpdated)
	}
}

// A derived name the author deliberately removed stays removed when the
// source derives it again (three-way merge against the recorded base).
func TestPullSourceInto_AuthorRemovedDerivedTagStaysRemoved(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{},
		Bindings:   map[string]openbindings.BindingEntry{},
	}
	derived := DeriveResult{
		Operations: map[string]openbindings.Operation{
			"getPet": {Tags: []string{"internal", "pets"}},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"getPet.api": {Operation: "getPet", Source: "api", Ref: "#/paths/~1pets~1{id}/get"},
		},
	}
	var first SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &first)

	// The author prunes a derived tag.
	op := iface.Operations["getPet"]
	op.Tags = []string{"pets"}
	iface.Operations["getPet"] = op

	var second SourcePullOutput
	pullSourceInto(iface, "api", derived, nil, &second)
	if tags := iface.Operations["getPet"].Tags; len(tags) != 1 || tags[0] != "pets" {
		t.Fatalf("author-removed tag resurrected by pull: %v", tags)
	}
	if len(second.OperationsUpdated) != 0 {
		t.Errorf("unchanged pull reported updates: %v", second.OperationsUpdated)
	}
}

// Live-vs-file classification is by ref SHAPE, not format: a file-backed grpc
// source pulled through the live branch re-derives an interface and embeds
// THAT in place of the artifact text (observed corrupting embedded protos).
func TestNeedsLiveDiscovery_ClassifiesByRefShape(t *testing.T) {
	cases := []struct {
		format, ref string
		want        bool
	}{
		{"grpc", "./blend.proto", false},
		{"grpc@1.0", "proto/blend.proto", false},
		{"grpc", "localhost:9090", true},
		{"grpc", "internal.host:8443", true},
		{"mcp@2025-11-25", "https://mcp.example.com", true},
		{"openapi@3.1", "./openapi.json", false},
		{"usage@2.0", "./cli.usage.kdl", false},
	}
	for _, c := range cases {
		if got := needsLiveDiscovery(c.format, c.ref); got != c.want {
			t.Errorf("needsLiveDiscovery(%q, %q) = %v, want %v", c.format, c.ref, got, c.want)
		}
	}
}

// A grpc source backed by a .proto FILE must refresh through the file lane:
// the live-discovery branch re-derives an interface and embeds THAT marshaled
// interface in place of the artifact text (observed in the DX field test
// corrupting embedded protos on every pull, with the hash updated to seal it).
func TestSourcePull_FileBackedGrpcRefreshesEmbeddedProtoText(t *testing.T) {
	dir := t.TempDir()
	proto := `syntax = "proto3";
package tiny;
service Tiny {
  rpc Ping(PingRequest) returns (PingReply);
}
message PingRequest { string msg = 1; }
message PingReply { string msg = 1; }
`
	if err := os.WriteFile(filepath.Join(dir, "tiny.proto"), []byte(proto), 0o644); err != nil {
		t.Fatal(err)
	}

	obiPath := writeInterface(t, dir, "iface.obi.json", map[string]any{
		"openbindings": "0.2.0",
		"name":         "tiny",
		"version":      "0.0.1",
		"operations":   map[string]any{},
		"bindings":     map[string]any{},
		"sources": map[string]any{
			"svc": map[string]any{
				"format":  "grpc",
				"content": "stale embedded text",
				"x-ob": map[string]any{
					"ref":         "tiny.proto",
					"resolve":     "content",
					"contentHash": "sha256:stale",
				},
			},
		},
	})

	if _, err := SourcePull(SourcePullInput{OBIPath: obiPath}); err != nil {
		t.Fatalf("pull: %v", err)
	}

	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	src := iface.Sources["svc"]
	content, ok := src.Content.(string)
	if !ok {
		t.Fatalf("embedded grpc content must remain artifact TEXT, got %T", src.Content)
	}
	if content != proto {
		if strings.Contains(content, `"openbindings"`) {
			t.Fatal("pull embedded a marshaled derived interface in place of the .proto text")
		}
		t.Fatalf("embedded content is not the refreshed proto text:\n%s", content)
	}
	meta, err := GetSourceMeta(src)
	if err != nil {
		t.Fatal(err)
	}
	if want := HashContent([]byte(proto)); meta.ContentHash != want {
		t.Errorf("contentHash must seal the artifact bytes: got %s want %s", meta.ContentHash, want)
	}
	// And the derivation itself worked from the file lane.
	if len(iface.Operations) == 0 {
		t.Error("expected operations derived from the .proto file")
	}
}

// The embed lane elides recorded bases; the full SourcePull cycle must
// reconstruct them from the OLD embedded content (captured before the
// refresh replaces it) so the authored overlay still three-way-merges:
// author-added aliases survive, author-removed derived tags stay removed.
func TestSourcePull_EmbedLane_ReconstructedBaseOverlay(t *testing.T) {
	dir := t.TempDir()
	spec := `{"openapi":"3.1.0","info":{"title":"T","version":"1"},"paths":{"/pets":{"get":{"operationId":"listPets","tags":["internal","pets"],"responses":{"200":{"description":"ok"}}}}}}`
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	iface, err := SynthesizeInterface(SynthesizeInterfaceInput{
		Sources: []SynthesizeInterfaceSource{{Format: "openapi@3.1", Location: specPath}},
		Name:    "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	obiPath := filepath.Join(dir, "t.obi.json")
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		t.Fatal(err)
	}

	// Author overlay: add a satisfaction alias, remove a derived tag.
	doc, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	op := doc.Operations["listPets"]
	if base, _ := GetBase(op.LosslessFields); base != nil {
		t.Fatal("embed-mode synthesis must elide the recorded base")
	}
	op.Aliases = []string{"acme.pet-catalog.list"}
	op.Tags = []string{"pets"} // author removed "internal"
	doc.Operations["listPets"] = op
	if err := WriteInterfaceFile(obiPath, doc); err != nil {
		t.Fatal(err)
	}

	// A real source change forces a refresh through the overlay path.
	changed := `{"openapi":"3.1.0","info":{"title":"T","version":"1"},"paths":{"/pets":{"get":{"operationId":"listPets","summary":"List pets","tags":["internal","pets"],"responses":{"200":{"description":"ok"}}}}}}`
	if err := os.WriteFile(specPath, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SourcePull(SourcePullInput{OBIPath: obiPath}); err != nil {
		t.Fatal(err)
	}

	got, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	pulled := got.Operations["listPets"]
	if len(pulled.Aliases) != 1 || pulled.Aliases[0] != "acme.pet-catalog.list" {
		t.Errorf("authored alias must survive the embed-lane refresh: %v", pulled.Aliases)
	}
	if len(pulled.Tags) != 1 || pulled.Tags[0] != "pets" {
		t.Errorf("author-removed derived tag must stay removed (reconstructed base): %v", pulled.Tags)
	}
	if base, _ := GetBase(pulled.LosslessFields); base != nil {
		t.Error("refresh must keep the base elided in embed mode")
	}
}
