package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func setupContextTestDir(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	contextsDirFunc = func() (string, error) { return dir, nil }
	t.Cleanup(func() { contextsDirFunc = defaultContextsDir })

	// Tests must not depend on the developer's local delegate config
	// (~/.openbindings/config.json or any walked-up .openbindings/
	// directory). Override GetDelegateContext to return an empty list
	// so ExecuteOperationWithContext does not try to probe `exec:ob`
	// or any other delegate the user happens to have configured. The
	// previous behavior caused environment-dependent test failures
	// where the binary's own OBI sources (which use relative paths)
	// could not resolve from the test's cwd.
	getDelegateContextFunc = func() DelegateContext { return DelegateContext{} }
	t.Cleanup(func() { getDelegateContextFunc = defaultGetDelegateContext })

	return dir
}

func TestSaveAndLoadContextConfig(t *testing.T) {
	setupContextTestDir(t)

	cfg := ContextConfig{
		Headers:     map[string]string{"Authorization": "Bearer tok"},
		Cookies:     map[string]string{"session": "abc123"},
		Environment: map[string]string{"API_URL": "https://example.com"},
		Metadata:    map[string]any{"region": "us-east-1"},
	}

	url := "https://api.example.com/openapi.json"
	if err := SaveContextConfig(url, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	loaded, err := LoadContextConfig(url)
	if err != nil {
		t.Fatalf("LoadContextConfig: %v", err)
	}

	if loaded.URL != url {
		t.Errorf("URL mismatch: got %q, want %q", loaded.URL, url)
	}
	if loaded.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("header mismatch: got %q", loaded.Headers["Authorization"])
	}
	if loaded.Cookies["session"] != "abc123" {
		t.Errorf("cookie mismatch: got %q", loaded.Cookies["session"])
	}
	if loaded.Environment["API_URL"] != "https://example.com" {
		t.Errorf("env mismatch: got %q", loaded.Environment["API_URL"])
	}
	if loaded.Metadata["region"] != "us-east-1" {
		t.Errorf("metadata mismatch: got %v", loaded.Metadata["region"])
	}
}

func TestLoadContextConfig_NotFound(t *testing.T) {
	setupContextTestDir(t)

	url := "https://nonexistent.example.com"
	cfg, err := LoadContextConfig(url)
	if err != nil {
		t.Fatalf("expected nil error for missing config, got: %v", err)
	}
	if cfg.URL != url {
		t.Errorf("URL mismatch: got %q, want %q", cfg.URL, url)
	}
	if cfg.Headers != nil || cfg.Cookies != nil || cfg.Environment != nil || cfg.Metadata != nil {
		t.Errorf("expected empty config, got: %+v", cfg)
	}
}

func TestDeleteContext(t *testing.T) {
	setupContextTestDir(t)

	url := "https://api.doomed.com"
	cfg := ContextConfig{
		Headers: map[string]string{"X-Test": "value"},
	}
	if err := SaveContextConfig(url, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	path, err := contextConfigPath(url)
	if err != nil {
		t.Fatalf("contextConfigPath: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file should exist: %v", err)
	}

	if err := DeleteContext(url); err != nil {
		t.Fatalf("DeleteContext: %v", err)
	}

	loaded, err := LoadContextConfig(url)
	if err != nil {
		t.Fatalf("LoadContextConfig after delete: %v", err)
	}
	if loaded.Headers != nil {
		t.Errorf("expected empty config after delete, got: %+v", loaded)
	}
}

func TestContextFilename(t *testing.T) {
	url := "https://api.example.com/v1/openapi.json"
	name := contextFilename(url)

	if !hasJSONSuffix(name) {
		t.Errorf("expected .json suffix, got %q", name)
	}
	if len(name) > 60 {
		t.Errorf("filename too long: %d chars, got %q", len(name), name)
	}

	url2 := "https://api.example.com/v2/openapi.json"
	name2 := contextFilename(url2)
	if name == name2 {
		t.Errorf("different URLs should produce different filenames: %q", name)
	}
}

func hasJSONSuffix(s string) bool {
	return len(s) > 5 && s[len(s)-5:] == ".json"
}

func TestSaveContextConfig_OverwritesExisting(t *testing.T) {
	setupContextTestDir(t)

	url := "https://api.overwrite.com"
	cfg1 := ContextConfig{
		Headers: map[string]string{"X-Old": "old"},
	}
	if err := SaveContextConfig(url, cfg1); err != nil {
		t.Fatalf("SaveContextConfig (first): %v", err)
	}

	cfg2 := ContextConfig{
		Headers: map[string]string{"X-New": "new"},
	}
	if err := SaveContextConfig(url, cfg2); err != nil {
		t.Fatalf("SaveContextConfig (second): %v", err)
	}

	loaded, err := LoadContextConfig(url)
	if err != nil {
		t.Fatalf("LoadContextConfig: %v", err)
	}
	if _, ok := loaded.Headers["X-Old"]; ok {
		t.Errorf("old header should not be present")
	}
	if loaded.Headers["X-New"] != "new" {
		t.Errorf("new header mismatch: got %q", loaded.Headers["X-New"])
	}
}

func TestSaveContextConfig_EmptyConfig(t *testing.T) {
	setupContextTestDir(t)

	url := "https://empty.example.com"
	if err := SaveContextConfig(url, ContextConfig{}); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	loaded, err := LoadContextConfig(url)
	if err != nil {
		t.Fatalf("LoadContextConfig: %v", err)
	}
	if loaded.Headers != nil || loaded.Cookies != nil {
		t.Errorf("expected empty maps to be nil, got: %+v", loaded)
	}
	if loaded.URL != url {
		t.Errorf("URL mismatch: got %q, want %q", loaded.URL, url)
	}
}

func TestSourceOverrides(t *testing.T) {
	setupContextTestDir(t)

	url := "https://api.multi.com/spec.json"
	cfg := ContextConfig{
		Headers: map[string]string{"X-Base": "base-val"},
		SourceOverrides: map[string]*ContextOverride{
			"payments-v2": {
				Headers: map[string]string{"X-Source": "source-val"},
			},
		},
	}
	if err := SaveContextConfig(url, cfg); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	loaded, err := LoadContextConfig(url)
	if err != nil {
		t.Fatalf("LoadContextConfig: %v", err)
	}
	if loaded.SourceOverrides == nil {
		t.Fatal("expected source overrides")
	}
	ov, ok := loaded.SourceOverrides["payments-v2"]
	if !ok {
		t.Fatal("expected payments-v2 override")
	}
	if ov.Headers["X-Source"] != "source-val" {
		t.Errorf("source header mismatch: got %q", ov.Headers["X-Source"])
	}
}

func TestListContexts(t *testing.T) {
	setupContextTestDir(t)

	urls := []string{
		"https://api.alpha.com",
		"https://api.beta.com",
	}
	for _, u := range urls {
		cfg := ContextConfig{
			Headers: map[string]string{"X-Test": u},
		}
		if err := SaveContextConfig(u, cfg); err != nil {
			t.Fatalf("SaveContextConfig(%q): %v", u, err)
		}
	}

	summaries, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	for _, s := range summaries {
		if s.URL == "" {
			t.Error("expected non-empty URL in summary")
		}
	}
}

func TestContextExists(t *testing.T) {
	setupContextTestDir(t)

	url := "https://api.exists.com"
	if ContextExists(url) {
		t.Error("context should not exist before creation")
	}

	if err := SaveContextConfig(url, ContextConfig{Headers: map[string]string{"X": "v"}}); err != nil {
		t.Fatalf("SaveContextConfig: %v", err)
	}

	if !ContextExists(url) {
		t.Error("context should exist after creation")
	}
}

func TestMigrationDetection(t *testing.T) {
	dir := setupContextTestDir(t)

	oldData := []byte(`{"headers":{"X-Old":"val"}}`)
	if err := os.WriteFile(filepath.Join(dir, "my-old-context.json"), oldData, 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	summaries, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].HeaderCount != 1 {
		t.Errorf("expected 1 header, got %d", summaries[0].HeaderCount)
	}
}
