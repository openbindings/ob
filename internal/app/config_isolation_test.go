package app

import (
	"path/filepath"
	"testing"
)

func TestConfigDirectoryOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "explicit-config")
	t.Setenv("OB_CONFIG_DIR", want)
	got, err := GlobalConfigPath()
	if err != nil || got != want {
		t.Fatalf("explicit application directory: got %q, %v; want %q", got, err, want)
	}
}

func TestConfigDirectoryRejectsRelativeOverride(t *testing.T) {
	t.Setenv("OB_CONFIG_DIR", "relative-config")
	if _, err := GlobalConfigPath(); err == nil {
		t.Fatal("relative config override must fail, not fall through to personal state")
	}
}

func TestCacheDirectoryOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "explicit-cache")
	t.Setenv("OB_CACHE_DIR", dir)
	got, err := updateCachePath()
	if err != nil || got != filepath.Join(dir, updateCacheFile) {
		t.Fatalf("explicit application cache: got %q, %v", got, err)
	}
}
