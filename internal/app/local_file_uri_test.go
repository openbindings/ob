package app

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalFileURIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "space %2F#é.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	encoded, err := localPathFileURL(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded, "%252F%23") || strings.Contains(encoded, " ") || strings.Contains(encoded, "\\") {
		t.Fatalf("not encoded as URI path data: %s", encoded)
	}
	u, err := url.Parse(encoded)
	if err != nil || u.Host != "" || u.Scheme != "file" {
		t.Fatalf("file URI: %s %v", encoded, err)
	}
	decoded, err := fileURLLocalPath(encoded)
	if err != nil || decoded != path {
		t.Fatalf("round trip %q => %q, %v", path, decoded, err)
	}
	data, err := os.ReadFile(decoded)
	if err != nil || string(data) != "{}" {
		t.Fatalf("read: %s %v", data, err)
	}
	if runtime.GOOS == "windows" && !strings.HasPrefix(u.Path, "/"+filepath.VolumeName(path)+"/") {
		t.Fatalf("drive missing: %s", encoded)
	}
}

func TestLocalFileURIUnsupportedPaths(t *testing.T) {
	for _, raw := range []string{`C:relative.json`, `\\server\share\a.json`, `\\?\C:\a.json`} {
		if _, err := localPathFileURL(raw); err == nil {
			t.Fatalf("unsupported path accepted: %s", raw)
		}
	}
	for _, raw := range []string{"file://untrusted.example/share/a.json", "file:relative.json"} {
		if _, err := fileURLLocalPath(raw); err == nil {
			t.Fatalf("unsupported URI accepted: %s", raw)
		}
	}
}

func TestSourceHostPortClassification(t *testing.T) {
	for _, path := range []string{`C:\Users\person\api.json`, `c:/workspace/api.json`, `Z:\a %2F#é.json`, "file:///C:/workspace/api.json", "https://api.example:443/openapi.json"} {
		if isHostPort(path) {
			t.Errorf("source path/URI misclassified as host:port: %q", path)
		}
	}
	for _, address := range []string{"localhost:8080", "api.example:https", "[::1]:50051", "a:80"} {
		if !isHostPort(address) {
			t.Errorf("network address rejected: %q", address)
		}
	}
	path := filepath.Join(t.TempDir(), "source %2F#é.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if !IsEmbeddableLocalFile(path, "") {
		t.Fatalf("readable local source is not embeddable: %q", path)
	}
	uri, err := localPathFileURL(path)
	if err != nil {
		t.Fatal(err)
	}
	if IsEmbeddableLocalFile(uri, "") {
		t.Fatalf("explicit URI must retain the existing location mode: %q", uri)
	}
}
