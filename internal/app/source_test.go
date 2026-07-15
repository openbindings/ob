package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/openbindings-go"
)

func TestSourceAdd_Basic(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal OBI.
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))

	// Create a dummy artifact file.
	artifactPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(artifactPath, []byte("# dummy"), 0644)

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.usage@1",
		Location: artifactPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Format != "openbindings.usage@1" {
		t.Errorf("expected format usage@2.13.1, got %q", result.Format)
	}
	if result.Key == "" {
		t.Error("expected derived key, got empty")
	}

	// Verify the OBI file was updated.
	data, _ := os.ReadFile(obiPath)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	sources, ok := parsed["sources"].(map[string]any)
	if !ok {
		t.Fatal("expected sources in OBI")
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(sources))
	}
}

func TestSourceAdd_KeyCollision(t *testing.T) {
	dir := t.TempDir()

	// Create OBI with existing source.
	obiData := map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"myKey": map[string]any{
				"bindingSpec": "openbindings.usage@1",
				"location":    "existing.kdl",
			},
		},
	}
	obiPath := writeInterface(t, dir, "my.obi.json", obiData)

	_, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.usage@1",
		Location: "other.kdl",
		Key:      "myKey", // collides with existing
	})

	if err == nil {
		t.Fatal("expected key collision error")
	}
}

func TestSourceAdd_ExplicitKey(t *testing.T) {
	dir := t.TempDir()

	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))

	// Create the source file so SourceAdd can read it for hashing.
	srcPath := filepath.Join(dir, "cli.kdl")
	os.WriteFile(srcPath, []byte("# dummy"), 0644)

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.usage@1",
		Location: srcPath,
		Key:      "myCli",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Key != "myCli" {
		t.Errorf("expected key 'myCli', got %q", result.Key)
	}
}

func TestSourceList_Empty(t *testing.T) {
	dir := t.TempDir()

	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))

	result, err := SourceList(obiPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 sources, got %d", len(result))
	}

	// Wire shape: a sourceless interface lists as [], never null.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("expected empty list to marshal as [], got %s", data)
	}
}

func TestSourceList_WithSources(t *testing.T) {
	dir := t.TempDir()

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"restApi": map[string]any{
				"bindingSpec": "openbindings.openapi@1",
				"location":    "api.yaml",
			},
			"cliSpec": map[string]any{
				"bindingSpec": "openbindings.usage@1",
				"location":    "cli.kdl",
			},
		},
	}
	obiPath := writeInterface(t, dir, "my.obi.json", obiData)

	result, err := SourceList(obiPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result))
	}

	// Verify sorted by key.
	if result[0].Key != "cliSpec" {
		t.Errorf("expected first source 'cliSpec', got %q", result[0].Key)
	}
	if result[1].Key != "restApi" {
		t.Errorf("expected second source 'restApi', got %q", result[1].Key)
	}
}

func TestSourceRemove_Basic(t *testing.T) {
	dir := t.TempDir()

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations":   map[string]any{},
		"sources": map[string]any{
			"myApi": map[string]any{
				"bindingSpec": "openbindings.openapi@1",
				"location":    "api.yaml",
			},
		},
	}
	obiPath := writeInterface(t, dir, "my.obi.json", obiData)

	result, err := SourceRemove(obiPath, "myApi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Key != "myApi" {
		t.Errorf("expected key 'myApi', got %q", result.Key)
	}
	if result.RemovedBindings != 0 {
		t.Errorf("expected 0 removed bindings, got %d", result.RemovedBindings)
	}

	// Verify removed from file.
	iface, _ := loadInterfaceFile(obiPath)
	if len(iface.Sources) != 0 {
		t.Errorf("expected 0 sources after removal, got %d", len(iface.Sources))
	}
}

func TestSourceRemove_NotFound(t *testing.T) {
	dir := t.TempDir()

	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))

	out, err := SourceRemove(obiPath, "nonexistent")
	if err != nil {
		t.Fatalf("removing an absent source should succeed (tolerant): %v", err)
	}
	if out.RemovedBindings != 0 {
		t.Errorf("nothing should be removed, got %d", out.RemovedBindings)
	}
}

func TestSourceRemove_CleansUpBindings(t *testing.T) {
	dir := t.TempDir()

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"greet": map[string]any{},
		},
		"sources": map[string]any{
			"myApi": map[string]any{
				"bindingSpec": "openbindings.openapi@1",
				"location":    "api.yaml",
			},
		},
		"bindings": map[string]any{
			"greet.myApi": map[string]any{"operation": "greet", "source": "myApi", "ref": "GET /greet"},
		},
	}
	obiPath := writeInterface(t, dir, "my.obi.json", obiData)

	result, err := SourceRemove(obiPath, "myApi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RemovedBindings != 1 {
		t.Errorf("expected 1 removed binding, got %d", result.RemovedBindings)
	}

	// Bindings should be gone from the file.
	iface, _ := loadInterfaceFile(obiPath)
	if len(iface.Bindings) != 0 {
		t.Errorf("expected 0 bindings after removal, got %d", len(iface.Bindings))
	}

	// Operation should still exist.
	if _, exists := iface.Operations["greet"]; !exists {
		t.Error("expected operation 'greet' to be preserved")
	}
}

func TestSourceRemove_WarnsUnboundOps(t *testing.T) {
	dir := t.TempDir()

	obiData := map[string]any{
		"openbindings": "0.2.0",
		"id":           "test",
		"operations": map[string]any{
			"greet":   map[string]any{},
			"goodbye": map[string]any{},
		},
		"sources": map[string]any{
			"rest": map[string]any{
				"bindingSpec": "openbindings.openapi@1",
				"location":    "api.yaml",
			},
			"events": map[string]any{
				"bindingSpec": "openbindings.asyncapi@1",
				"location":    "events.yaml",
			},
		},
		"bindings": map[string]any{
			"greet.rest":   map[string]any{"operation": "greet", "source": "rest", "ref": "GET /greet"},
			"greet.events": map[string]any{"operation": "greet", "source": "events", "ref": "#/greet"},
			"goodbye.rest": map[string]any{"operation": "goodbye", "source": "rest", "ref": "GET /goodbye"},
		},
	}
	obiPath := writeInterface(t, dir, "my.obi.json", obiData)

	result, err := SourceRemove(obiPath, "rest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RemovedBindings != 2 {
		t.Errorf("expected 2 removed bindings, got %d", result.RemovedBindings)
	}

	// "greet" still has a binding via "events", so not unbound.
	// "goodbye" has no remaining bindings, so it's unbound.
	if len(result.UnboundOps) != 1 {
		t.Fatalf("expected 1 unbound op, got %d: %v", len(result.UnboundOps), result.UnboundOps)
	}
	if result.UnboundOps[0] != "goodbye" {
		t.Errorf("expected unbound op 'goodbye', got %q", result.UnboundOps[0])
	}

	// Verify file state.
	iface, _ := loadInterfaceFile(obiPath)
	if len(iface.Bindings) != 1 {
		t.Errorf("expected 1 remaining binding, got %d", len(iface.Bindings))
	}
	if len(iface.Operations) != 2 {
		t.Errorf("expected 2 operations preserved, got %d", len(iface.Operations))
	}
}

func TestSourceAdd_RelativePath(t *testing.T) {
	dir := t.TempDir()

	// Create subdirectory structure.
	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)

	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))
	artifactPath := filepath.Join(subDir, "cli.kdl")
	os.WriteFile(artifactPath, []byte("# dummy"), 0644)

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.usage@1",
		Location: artifactPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ref should be stored relative to OBI directory.
	if filepath.IsAbs(result.Ref) {
		t.Errorf("expected relative ref path, got %q", result.Ref)
	}
}

func TestSourceList_RenderOutput(t *testing.T) {
	output := SourceListOutput{
		{Key: "cliSpec", Source: openbindings.Source{BindingSpec: "openbindings.usage@1", Location: "cli.kdl"}},
		{Key: "restApi", Source: openbindings.Source{BindingSpec: "openbindings.openapi@1", Location: "api.yaml"}},
	}

	rendered := output.Render()
	if rendered == "" {
		t.Error("expected non-empty render output")
	}
}

func TestSourceList_RenderEmpty(t *testing.T) {
	output := SourceListOutput{}
	rendered := output.Render()
	if rendered == "" {
		t.Error("expected non-empty render output for empty list")
	}
}

// THE FLIP (D-05 ruling): a local file artifact embeds by default — the
// document is conformant immediately, and the local path lives in x-ob.ref
// as the pull path.
func TestSourceAdd_LocalFileEmbedsByDefault(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))
	artifactPath := filepath.Join(dir, "cli.kdl")
	if err := os.WriteFile(artifactPath, []byte("name \"tool\""), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.usage@1",
		Location: artifactPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Resolve != ResolveModeContent {
		t.Errorf("local file must embed by default, got resolve=%q", result.Resolve)
	}

	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	src := iface.Sources[result.Key]
	if src.Location != "" {
		t.Errorf("embedded source must carry no spec location, got %q", src.Location)
	}
	if s, ok := src.Content.(string); !ok || s != "name \"tool\"" {
		t.Errorf("artifact text must be embedded, got %T %v", src.Content, src.Content)
	}
	meta, err := GetSourceMeta(src)
	if err != nil || meta == nil {
		t.Fatalf("x-ob meta: %v", err)
	}
	if meta.Ref != "cli.kdl" {
		t.Errorf("pull path must ride x-ob.ref relative to the OBI, got %q", meta.Ref)
	}
	if meta.ContentHash == "" {
		t.Error("embed lane must seal the artifact bytes with a contentHash")
	}
}

// An explicit --resolve location keeps the (nonconformant, working-form)
// pointer; explicit intent is honored, never silently overridden.
func TestSourceAdd_ExplicitLocationHonored(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))
	artifactPath := filepath.Join(dir, "cli.kdl")
	if err := os.WriteFile(artifactPath, []byte("# dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.usage@1",
		Location: artifactPath,
		Resolve:  ResolveModeLocation,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Resolve != ResolveModeLocation {
		t.Errorf("explicit --resolve location must be honored, got %q", result.Resolve)
	}
}

// A --uri implies the published pointer: location mode, no silent embed.
func TestSourceAdd_URIKeepsLocationMode(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))
	artifactPath := filepath.Join(dir, "api.json")
	if err := os.WriteFile(artifactPath, []byte(`{"openapi":"3.1.0"}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.openapi@1",
		Location: artifactPath,
		URI:      "https://cdn.example.com/api.json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Resolve != ResolveModeLocation {
		t.Errorf("--uri implies location mode, got %q", result.Resolve)
	}
	iface, _ := loadInterfaceFile(obiPath)
	if src := iface.Sources[result.Key]; src.Location != "https://cdn.example.com/api.json" {
		t.Errorf("spec location must be the published URI, got %q", src.Location)
	}
}

// A URL source with --resolve content fetches and pins the remote artifact
// (the field test found ?embed on a URL dying with a filesystem ENOENT).
func TestSourceAdd_URLFetchEmbed(t *testing.T) {
	spec := `{"openapi":"3.1.0","info":{"title":"T","version":"1"},"paths":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	defer srv.Close()

	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.openapi@1",
		Location: srv.URL + "/openapi.json",
		Key:      "api",
		Resolve:  ResolveModeContent,
	})
	if err != nil {
		t.Fatalf("URL fetch-embed failed: %v", err)
	}
	iface, _ := loadInterfaceFile(obiPath)
	src := iface.Sources[result.Key]
	obj, ok := src.Content.(map[string]any)
	if !ok || obj["openapi"] != "3.1.0" {
		t.Fatalf("remote artifact must be fetched and embedded, got %T", src.Content)
	}
	meta, _ := GetSourceMeta(src)
	if meta.Ref != srv.URL+"/openapi.json" {
		t.Errorf("the URL must ride x-ob.ref for pull refresh, got %q", meta.Ref)
	}
	if meta.ContentHash != HashContent([]byte(spec)) {
		t.Errorf("contentHash must seal the fetched bytes")
	}
}

// A URL source with no explicit resolve stays a pointer (location mode).
func TestSourceAdd_URLDefaultsToLocation(t *testing.T) {
	spec := `{"openapi":"3.1.0"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(spec))
	}))
	defer srv.Close()

	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "openbindings.openapi@1",
		Location: srv.URL + "/openapi.json",
		Key:      "api",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Resolve != ResolveModeLocation {
		t.Errorf("URL source must default to location mode, got %q", result.Resolve)
	}
}

// §6.4 pairing (ratified 2026-07-10): a source may carry BOTH embedded
// content and a location. For service-addressed formats the location is the
// dial address ("pinned contract + invocation target in one source"); for
// document formats it is the canonical origin. --resolve content + --uri is
// the explicit authoring path.
func TestSourceAdd_ContentWithURIPairsBoth(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))
	protoPath := filepath.Join(dir, "tiny.proto")
	proto := "syntax = \"proto3\";\npackage tiny;\nmessage M { string x = 1; }\nservice T { rpc Go(M) returns (M); }\n"
	if err := os.WriteFile(protoPath, []byte(proto), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SourceAdd(SourceAddInput{
		OBIPath:  obiPath,
		Format:   "grpc",
		Location: protoPath,
		Key:      "svc",
		Resolve:  ResolveModeContent,
		URI:      "api.example.com:443",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Resolve != ResolveModeContent {
		t.Errorf("resolve = %q", result.Resolve)
	}
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	src := iface.Sources["svc"]
	if s, ok := src.Content.(string); !ok || s != proto {
		t.Errorf("content must pin the artifact text, got %T", src.Content)
	}
	if src.Location != "api.example.com:443" {
		t.Errorf("location must carry the service address, got %q", src.Location)
	}
	if problems := ValidateDocumentValue(mustDocValue(t, obiPath)); len(problems) > 0 {
		t.Errorf("paired source must validate: %v", problems)
	}
}

// mustDocValue loads an OBI file as an untyped JSON value.
func mustDocValue(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// A pull refresh preserves the paired location (the URI rides x-ob and is
// re-applied on every refresh).
func TestSourcePull_PreservesPairedLocation(t *testing.T) {
	dir := t.TempDir()
	obiPath := writeInterface(t, dir, "my.obi.json", minimalInterface(map[string]any{}))
	protoPath := filepath.Join(dir, "tiny.proto")
	proto := "syntax = \"proto3\";\npackage tiny;\nmessage M { string x = 1; }\nservice T { rpc Go(M) returns (M); }\n"
	if err := os.WriteFile(protoPath, []byte(proto), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SourceAdd(SourceAddInput{
		OBIPath: obiPath, Format: "grpc", Location: protoPath, Key: "svc",
		Resolve: ResolveModeContent, URI: "api.example.com:443",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SourcePull(SourcePullInput{OBIPath: obiPath}); err != nil {
		t.Fatal(err)
	}
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		t.Fatal(err)
	}
	if src := iface.Sources["svc"]; src.Location != "api.example.com:443" {
		t.Errorf("pull must preserve the paired service address, got %q", src.Location)
	}
}
