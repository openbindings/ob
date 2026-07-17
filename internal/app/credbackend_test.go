package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// useFileBackend points the credential seam at a fresh JSON file under a temp
// dir (via OB_CREDENTIALS_FILE) and isolates the config-file side too. It also
// suppresses the unencrypted-storage notice by default (marking it already
// shown); the notice test opts back in. Returns the credentials file path.
func useFileBackend(t *testing.T) string {
	t.Helper()
	keyring.MockInit() // reset provider; nothing should reach it while the file backend is selected
	credsPath := filepath.Join(t.TempDir(), "creds", "credentials.json")
	t.Setenv(EnvCredentialsFile, credsPath)

	dir := t.TempDir()
	contextsDirFunc = func() (string, error) { return dir, nil }
	t.Cleanup(func() { contextsDirFunc = defaultContextsDir })

	credsNoticeMu.Lock()
	credsNoticeShown = true
	credsNoticeMu.Unlock()
	t.Cleanup(func() {
		credsNoticeMu.Lock()
		credsNoticeShown = false
		credsNoticeMu.Unlock()
	})
	return credsPath
}

func TestFileBackend_RoundTripPerTarget(t *testing.T) {
	credsPath := useFileBackend(t)

	a := "https://api.alpha.com"
	b := "https://api.beta.com"
	if err := SaveContextCredentials(a, map[string]any{"bearerToken": "tok-a"}); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := SaveContextCredentials(b, map[string]any{"apiKey": "key-b"}); err != nil {
		t.Fatalf("save b: %v", err)
	}

	// The file exists and is the selected backend, not the keychain.
	if _, err := os.Stat(credsPath); err != nil {
		t.Fatalf("credentials file not created: %v", err)
	}
	if _, kerr := keyring.Get(KeychainService, a); kerr == nil {
		t.Errorf("credentials leaked into the keychain despite OB_CREDENTIALS_FILE")
	}

	// Each target round-trips independently.
	got, err := LoadContextCredentials(a)
	if err != nil {
		t.Fatalf("load a: %v", err)
	}
	if got["bearerToken"] != "tok-a" {
		t.Errorf("load a = %v, want bearerToken tok-a", got)
	}
	got, err = LoadContextCredentials(b)
	if err != nil {
		t.Fatalf("load b: %v", err)
	}
	if got["apiKey"] != "key-b" {
		t.Errorf("load b = %v, want apiKey key-b", got)
	}

	// Delete one; the other survives.
	if err := DeleteContextCredentials(a); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if got, _ := LoadContextCredentials(a); got != nil {
		t.Errorf("a survived delete: %v", got)
	}
	if got, _ := LoadContextCredentials(b); got == nil || got["apiKey"] != "key-b" {
		t.Errorf("b did not survive a's delete: %v", got)
	}

	// A missing target loads as (nil, nil), not an error.
	if got, err := LoadContextCredentials("https://api.missing.com"); err != nil || got != nil {
		t.Errorf("missing target = (%v, %v), want (nil, nil)", got, err)
	}
}

// http:// and https:// targets share one normalized key across the file
// backend, matching the keychain backend's contract.
func TestFileBackend_HTTPTargetRoundTrip(t *testing.T) {
	useFileBackend(t)

	if err := SaveContextCredentials("http://127.0.0.1:8123", map[string]any{"bearerToken": "orders-secret"}); err != nil {
		t.Fatal(err)
	}
	for _, lookup := range []string{"http://127.0.0.1:8123", "https://127.0.0.1:8123"} {
		cred, err := LoadContextCredentials(lookup)
		if err != nil {
			t.Fatal(err)
		}
		if cred == nil || cred["bearerToken"] != "orders-secret" {
			t.Errorf("LoadContextCredentials(%q) = %v, want the stored token", lookup, cred)
		}
	}
	if err := DeleteContextCredentials("https://127.0.0.1:8123"); err != nil {
		t.Fatal(err)
	}
	if cred, _ := LoadContextCredentials("http://127.0.0.1:8123"); cred != nil {
		t.Errorf("credentials survived delete under the normalized key: %v", cred)
	}
}

func TestFileBackend_CreatedFileIs0600(t *testing.T) {
	credsPath := useFileBackend(t)

	if err := SaveContextCredentials("https://api.example.com", map[string]any{"bearerToken": "x"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(credsPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != CredsFilePerm {
		t.Errorf("credentials file perms = %#o, want %#o", perm, CredsFilePerm)
	}
}

func TestFileBackend_RefusesInsecurePermissions(t *testing.T) {
	credsPath := useFileBackend(t)

	if err := os.MkdirAll(filepath.Dir(credsPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(credsPath, []byte(`{"https://api.example.com":{"bearerToken":"x"}}`), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Both read and write refuse a group/other-accessible file, loudly, with
	// the exact remediation.
	_, loadErr := LoadContextCredentials("https://api.example.com")
	if loadErr == nil {
		t.Fatalf("Load did not refuse an insecure-permission file")
	}
	saveErr := SaveContextCredentials("https://api.example.com", map[string]any{"bearerToken": "y"})
	if saveErr == nil {
		t.Fatalf("Save did not refuse an insecure-permission file")
	}
	for _, err := range []error{loadErr, saveErr} {
		msg := err.Error()
		if !strings.Contains(msg, "insecure permissions") || !strings.Contains(msg, "chmod 600 "+credsPath) {
			t.Errorf("refusal message not actionable: %q", msg)
		}
	}
}

func TestFileBackend_NoticeOncePerProcess(t *testing.T) {
	useFileBackend(t)
	// Opt back into the notice for this test (useFileBackend suppresses it).
	credsNoticeMu.Lock()
	credsNoticeShown = false
	credsNoticeMu.Unlock()

	out := captureStderr(t, func() {
		if err := SaveContextCredentials("https://api.one.com", map[string]any{"bearerToken": "a"}); err != nil {
			t.Fatalf("save 1: %v", err)
		}
		if err := SaveContextCredentials("https://api.two.com", map[string]any{"bearerToken": "b"}); err != nil {
			t.Fatalf("save 2: %v", err)
		}
		// A delete is not "storing credentials" and must not print the notice.
		if err := DeleteContextCredentials("https://api.one.com"); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})

	const marker = "storing credentials unencrypted at"
	if n := strings.Count(out, marker); n != 1 {
		t.Errorf("notice printed %d times, want exactly 1\nstderr:\n%s", n, out)
	}
}

// The file backend is selected ONLY by OB_CREDENTIALS_FILE. When it is unset,
// a keychain failure surfaces the improved error and never silently writes a
// file anywhere.
func TestExplicitSelectionOnly_KeychainErrorNeverTouchesFile(t *testing.T) {
	t.Setenv(EnvCredentialsFile, "") // keychain backend
	dir := t.TempDir()
	contextsDirFunc = func() (string, error) { return dir, nil }
	t.Cleanup(func() { contextsDirFunc = defaultContextsDir })

	keyring.MockInitWithError(errors.New("exit status 154"))
	t.Cleanup(keyring.MockInit)

	err := SaveContextCredentials("https://api.example.com", map[string]any{"bearerToken": "x"})
	if err == nil {
		t.Fatalf("expected a keychain error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "OS keychain unavailable") ||
		!strings.Contains(msg, "exit status 154") ||
		!strings.Contains(msg, EnvCredentialsFile) {
		t.Errorf("keychain error not remediating: %q", msg)
	}

	// No credentials file was created as a fallback, anywhere under the temp tree.
	if found := findCredentialsJSON(t, dir); found != "" {
		t.Errorf("keychain failure silently wrote a file: %s", found)
	}

	// The read path is equally loud (not a silent nil).
	if _, err := LoadContextCredentials("https://api.example.com"); err == nil {
		t.Errorf("Load swallowed an unavailable keychain")
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stderr = old
	return <-done
}

func findCredentialsJSON(t *testing.T, root string) string {
	t.Helper()
	var found string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && strings.HasSuffix(path, "credentials.json") {
			found = path
		}
		return nil
	})
	return found
}
