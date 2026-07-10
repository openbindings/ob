package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const statusTestSpec = `{"openapi":"3.1.0","info":{"title":"Tiny","version":"1.0.0"},"servers":[{"url":"http://localhost:1"}],"paths":{"/things":{"get":{"operationId":"listThings","responses":{"200":{"description":"ok"}}}}}}`

// statusTestOBI writes a spec file and an embed-mode OBI tracking it, with the
// contentHash sealing the CURRENT spec bytes, and returns the OBI path.
func statusTestOBI(t *testing.T, dir string) string {
	t.Helper()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(statusTestSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	var content map[string]any
	if err := json.Unmarshal([]byte(statusTestSpec), &content); err != nil {
		t.Fatal(err)
	}
	return writeInterface(t, dir, "iface.obi.json", map[string]any{
		"openbindings": "0.2.0",
		"name":         "tiny",
		"version":      "0.0.1",
		"operations":   map[string]any{},
		"bindings":     map[string]any{},
		"sources": map[string]any{
			"api": map[string]any{
				"format":  "openapi@3.1",
				"content": content,
				"x-ob": map[string]any{
					"ref":         "spec.json",
					"resolve":     "content",
					"contentHash": HashContent([]byte(statusTestSpec)),
				},
			},
		},
	})
}

// In embed mode the embedded copy is the invocation authority. A spec edit
// that changes no derived operation (a server URL) must still surface as
// content drift — the DX field test observed a stale embedded server URL
// passing the validate+status CI gate while invoke hit the wrong host.
func TestOBIStatus_EmbedContentDriftDetected(t *testing.T) {
	dir := t.TempDir()
	obiPath := statusTestOBI(t, dir)

	// Content-only edit: server URL changes, derived operations don't.
	edited := map[string]any{}
	if err := json.Unmarshal([]byte(statusTestSpec), &edited); err != nil {
		t.Fatal(err)
	}
	edited["servers"] = []any{map[string]any{"url": "http://localhost:2"}}
	data, _ := json.Marshal(edited)
	if err := os.WriteFile(filepath.Join(dir, "spec.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := OBIStatus(OBIStatusInput{OBIPath: obiPath})
	if err != nil {
		t.Fatal(err)
	}
	src := out.Sources[0]
	if !src.ContentDrift {
		t.Error("content-only spec change must surface as ContentDrift")
	}
	if src.InSync {
		t.Error("content drift must not report in sync")
	}
	if !out.HasDrift() {
		t.Error("the CI gate (HasDrift) must fire on content drift")
	}
}

// A corrupted embedded copy with an UNCHANGED source file is invisible to the
// contentHash (which seals the file, not the copy); the copy itself must be
// verified against a fresh parse.
func TestOBIStatus_CorruptedEmbedDetected(t *testing.T) {
	dir := t.TempDir()
	obiPath := statusTestOBI(t, dir)

	// Mangle the embedded copy only; the spec file stays byte-identical.
	raw, err := os.ReadFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	src := doc["sources"].(map[string]any)["api"].(map[string]any)
	src["content"].(map[string]any)["openapi"] = "not-a-version"
	mangled, _ := json.Marshal(doc)
	if err := os.WriteFile(obiPath, mangled, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := OBIStatus(OBIStatusInput{OBIPath: obiPath})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Sources[0].ContentDrift {
		t.Error("a corrupted embedded copy must surface as ContentDrift even when the file hash matches")
	}
	if !out.HasDrift() {
		t.Error("the CI gate (HasDrift) must fire on a corrupted embed")
	}
}

// A re-homed embedded document (registry copy, another checkout) has an
// unreachable pull path but remains fully usable. That is its own state:
// no "out of sync", no pull advice, and the CI drift gate must NOT fire.
func TestOBIStatus_PullPathUnreachableIsNotDrift(t *testing.T) {
	dir := t.TempDir()
	obiPath := statusTestOBI(t, dir)
	if err := os.Remove(filepath.Join(dir, "spec.json")); err != nil {
		t.Fatal(err)
	}

	out, err := OBIStatus(OBIStatusInput{OBIPath: obiPath})
	if err != nil {
		t.Fatal(err)
	}
	src := out.Sources[0]
	if !src.PullPathUnreachable {
		t.Error("missing pull path with embedded content must report PullPathUnreachable")
	}
	if src.Error != "" {
		t.Errorf("unreachable pull path is a state, not an error: %s", src.Error)
	}
	if out.HasDrift() {
		t.Error("an unreachable pull path must not fire the CI drift gate: the document is self-contained")
	}
}

// A location-mode source whose file is missing is a real error: the location
// IS the resolution path, so the document is broken here, not self-contained.
func TestOBIStatus_LocationModeMissingFileIsError(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "iface.obi.json", map[string]any{
		"openbindings": "0.2.0",
		"name":         "tiny",
		"version":      "0.0.1",
		"operations":   map[string]any{},
		"bindings":     map[string]any{},
		"sources": map[string]any{
			"api": map[string]any{
				"format":   "openapi@3.1",
				"location": "spec.json",
				"x-ob": map[string]any{
					"ref":         "spec.json",
					"resolve":     "location",
					"contentHash": "sha256:whatever",
				},
			},
		},
	})

	out, err := OBIStatus(OBIStatusInput{OBIPath: obiPath})
	if err != nil {
		t.Fatal(err)
	}
	src := out.Sources[0]
	if src.Error == "" {
		t.Error("location-mode missing artifact must be an error")
	}
	if src.PullPathUnreachable {
		t.Error("location mode has no embedded fallback; this is not the self-contained state")
	}
}

// An incomplete pull reports its failures as Failed (never conflated with
// by-design skips) so the command lane can exit non-zero.
func TestSourcePull_UnreadableSourceIsFailed(t *testing.T) {
	dir := t.TempDir()
	obiPath := statusTestOBI(t, dir)
	if err := os.Remove(filepath.Join(dir, "spec.json")); err != nil {
		t.Fatal(err)
	}

	out, err := SourcePull(SourcePullInput{OBIPath: obiPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failed) != 1 || out.Failed[0] != "api" {
		t.Errorf("unreadable tracked source must be Failed, got failed=%v skipped=%v", out.Failed, out.Skipped)
	}
	if len(out.Sources) != 0 {
		t.Errorf("nothing was pulled, got %v", out.Sources)
	}
}
