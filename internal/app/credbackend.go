package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// EnvCredentialsFile is the environment variable that opts a process into
// file-backed credential storage. When it names a path, every credential
// read/write/delete routes to that JSON file instead of the OS keychain;
// when it is unset, the keychain is used, exactly as before. The file backend
// is only ever selected by this explicit signal — the keychain never silently
// falls through to a file when it errors.
const EnvCredentialsFile = "OB_CREDENTIALS_FILE"

// CredsFilePerm is the permission mode for the file-backed credentials store:
// owner read/write only, ssh-private-key style. A looser mode on an existing
// file is refused loudly rather than tightened silently.
const CredsFilePerm = 0o600

// credentialBackend is the seam between the two places ob can persist the
// secret half of a context: the OS keychain (default) or an explicit,
// opt-in JSON file (headless / CI). Selected once, in activeCredentialBackend,
// by the presence of OB_CREDENTIALS_FILE — never as a keychain fallback.
//
// Keys are already normalized (normalizeContextKey) by the callers in
// contextstore.go before they reach a backend, so both backends store under
// the same identity a lookup will use.
type credentialBackend interface {
	// Load returns the credential field-bag stored under key, or (nil, nil)
	// when nothing is stored. A backend that is unusable (keychain
	// unavailable, credentials file with insecure permissions) returns a
	// loud, remediating error.
	Load(key string) (map[string]any, error)
	// Save writes the credential field-bag under key. An empty bag deletes
	// the entry (mirroring the keychain's historical behavior).
	Save(key string, cred map[string]any) error
	// Delete removes the entry for key. Absence is not an error.
	Delete(key string) error
	// Has reports whether a credential entry exists for key. It swallows
	// backend errors (returning false) so best-effort listings never explode
	// on an unavailable keychain or unreadable file; the loud errors surface
	// on the Load/Save/Delete paths that actually move a secret.
	Has(key string) bool
}

// activeCredentialBackend selects the credential backend for this process.
// The file backend is chosen only when OB_CREDENTIALS_FILE explicitly names a
// path; otherwise the OS keychain. This is the single selection site — the
// three public credential functions all route through it — and it is never a
// fallback: a keychain error does not cause a file to be touched.
func activeCredentialBackend() credentialBackend {
	if path := credentialsFilePath(); path != "" {
		return &fileBackend{path: path}
	}
	return keyringBackend{}
}

// credentialsFilePath returns the configured file-backend path (from
// OB_CREDENTIALS_FILE), or "" when the keychain backend is in effect.
func credentialsFilePath() string {
	return strings.TrimSpace(os.Getenv(EnvCredentialsFile))
}

// keyringBackend persists credentials to the OS keychain via go-keyring.
type keyringBackend struct{}

func (keyringBackend) Load(key string) (map[string]any, error) {
	secret, err := keyring.Get(KeychainService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, nil
		}
		return nil, keychainUnavailableError(err)
	}
	var cred map[string]any
	if err := json.Unmarshal([]byte(secret), &cred); err != nil {
		return nil, fmt.Errorf("parsing keychain credentials for context %q: %w", key, err)
	}
	return cred, nil
}

func (b keyringBackend) Save(key string, cred map[string]any) error {
	if len(cred) == 0 {
		return b.Delete(key)
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}
	if err := keyring.Set(KeychainService, key, string(data)); err != nil {
		return keychainUnavailableError(err)
	}
	return nil
}

func (keyringBackend) Delete(key string) error {
	err := keyring.Delete(KeychainService, key)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return keychainUnavailableError(err)
	}
	return nil
}

func (keyringBackend) Has(key string) bool {
	_, err := keyring.Get(KeychainService, key)
	return err == nil
}

// keychainUnavailableError turns a raw OS-keychain failure — historically a
// bare `exit status 154` from the macOS security(1) subprocess, with zero
// interpretation — into a real error that names the underlying cause and the
// escape hatch. This is what un-dead-ends an authenticated flow in a
// keychain-less environment (CI, containers, sandboxes): the user learns the
// keychain is the wall and that OB_CREDENTIALS_FILE is the way over it.
func keychainUnavailableError(cause error) error {
	example := "/path/to/credentials.json"
	if gp, err := GlobalConfigPath(); err == nil {
		example = filepath.Join(gp, "credentials.json")
	}
	return fmt.Errorf(
		"OS keychain unavailable (%v); to use file-backed credential storage set %s=<path> (e.g. %s=%s)",
		cause, EnvCredentialsFile, EnvCredentialsFile, example)
}

// fileBackend persists credentials to a single JSON file mapping normalized
// target-URL key → credential field-bag — the same shape the keychain backend
// round-trips per key, gathered into one document. Selected only by an
// explicit OB_CREDENTIALS_FILE; the contents are unencrypted (like
// ~/.aws/credentials), which the process announces once on its first write.
type fileBackend struct {
	path string
}

// credentialsFile is the on-disk shape: URL key → field-bag. The field-bag is
// an open object (bearerToken, apiKey, basic{...}, ...), matching the value
// the keychain stores per key.
type credentialsFile map[string]map[string]any

func (b *fileBackend) Load(key string) (map[string]any, error) {
	all, err := b.read()
	if err != nil {
		return nil, err
	}
	cred := all[key]
	if len(cred) == 0 {
		return nil, nil
	}
	return cred, nil
}

func (b *fileBackend) Save(key string, cred map[string]any) error {
	if len(cred) == 0 {
		return b.Delete(key)
	}
	all, err := b.read()
	if err != nil {
		return err
	}
	all[key] = cred
	if err := b.write(all); err != nil {
		return err
	}
	// Announce plaintext storage once per process, on the first real write.
	credentialsFileNotice(b.path)
	return nil
}

func (b *fileBackend) Delete(key string) error {
	all, err := b.read()
	if err != nil {
		return err
	}
	if _, ok := all[key]; !ok {
		return nil
	}
	delete(all, key)
	return b.write(all)
}

func (b *fileBackend) Has(key string) bool {
	all, err := b.read()
	if err != nil {
		return false
	}
	return len(all[key]) > 0
}

// read loads and validates the credentials file. A missing file is an empty
// store (not an error). An existing file whose permissions grant any access
// to group or other is refused loudly, ssh-style, with the remediation.
func (b *fileBackend) read() (credentialsFile, error) {
	info, err := os.Stat(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return credentialsFile{}, nil
		}
		return nil, fmt.Errorf("reading credentials file %s: %w", b.path, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf(
			"credentials file %s has insecure permissions %#o (accessible by group/other); run: chmod 600 %s",
			b.path, perm, b.path)
	}
	data, err := os.ReadFile(b.path)
	if err != nil {
		return nil, fmt.Errorf("reading credentials file %s: %w", b.path, err)
	}
	all := credentialsFile{}
	if len(bytes.TrimSpace(data)) == 0 {
		return all, nil
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("parsing credentials file %s: %w", b.path, err)
	}
	return all, nil
}

// write persists the store, creating the parent directory (0700) as needed and
// the file itself 0600. AtomicWriteFile preserves an existing file's mode; a
// pre-existing file has already passed the read() permission gate, so the
// preserved mode is 0600-or-stricter.
func (b *fileBackend) write(all credentialsFile) error {
	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating credentials directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}
	data = append(data, '\n')
	return AtomicWriteFile(b.path, data, CredsFilePerm)
}

var (
	credsNoticeMu    sync.Mutex
	credsNoticeShown bool
)

// credentialsFileNotice prints the unencrypted-storage notice to stderr once
// per process, on the first credential write to the file backend.
func credentialsFileNotice(path string) {
	credsNoticeMu.Lock()
	defer credsNoticeMu.Unlock()
	if credsNoticeShown {
		return
	}
	credsNoticeShown = true
	fmt.Fprintf(os.Stderr, "storing credentials unencrypted at %s at your request\n", path)
}
