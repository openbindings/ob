package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go/invoke"
)

// ContextConfig holds structured non-credential fields of a URL-keyed
// context. Headers, cookies, environment values, and metadata may still be
// sensitive, so the file is written with credential-store permissions.
type ContextConfig struct {
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	Cookies     map[string]string `json:"cookies,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	// Configuration holds binding-specification configuration points
	// (config.value answers: a server selection, a channel address, …),
	// keyed by point name. Configuration is NOT a credential — it routes to
	// this on-disk config side, not the keychain — but the contract notes
	// configuration may be sensitive according to its meaning (the file
	// already carries credential-store permissions); secret-bearing
	// configuration belongs in credentials, not here.
	Configuration map[string]any `json:"configuration,omitempty"`
}

// ContextSummary is a compact representation for listing contexts.
type ContextSummary struct {
	URL                string `json:"url"`
	HasCredentials     bool   `json:"hasCredentials"`
	HeaderCount        int    `json:"headerCount,omitempty"`
	CookieCount        int    `json:"cookieCount,omitempty"`
	EnvCount           int    `json:"envCount,omitempty"`
	MetadataCount      int    `json:"metadataCount,omitempty"`
	ConfigurationCount int    `json:"configurationCount,omitempty"`
	LoadError          string `json:"loadError,omitempty"`
}

// contextsDirFunc is the resolver for the contexts directory.
// Override in tests to use a temp directory.
var contextsDirFunc = defaultContextsDir

func defaultContextsDir() (string, error) {
	globalPath, err := GlobalConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(globalPath, ContextsDir), nil
}

func contextsDir() (string, error) {
	return contextsDirFunc()
}

// normalizeContextKey canonicalizes HTTP URLs to HTTPS for consistent lookup.
// Non-HTTP URLs (exec:, grpc://, ws://, etc.) are returned as-is.
func normalizeContextKey(key string) string {
	if strings.HasPrefix(key, "http://") {
		return "https://" + key[len("http://"):]
	}
	return key
}

// contextFilename returns a filesystem-safe filename for a URL.
// Uses a readable prefix (up to 40 chars) plus a short hash for uniqueness.
func contextFilename(rawURL string) string {
	url := normalizeContextKey(rawURL)
	h := sha256.Sum256([]byte(url))
	hashSuffix := hex.EncodeToString(h[:8])

	safe := strings.NewReplacer(
		"://", "_",
		"/", "_",
		":", "_",
		"?", "_",
		"&", "_",
		"=", "_",
		" ", "_",
		"#", "_",
	).Replace(url)

	if len(safe) > 40 {
		safe = safe[:40]
	}
	safe = strings.ReplaceAll(safe, "..", "_")
	safe = strings.TrimRight(safe, "_.")

	return safe + "_" + hashSuffix + ".json"
}

// contextConfigPath returns the JSON file path for a URL-keyed context.
func contextConfigPath(url string) (string, error) {
	dir, err := contextsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, contextFilename(url)), nil
}

// LoadContextConfig reads the structured config for a URL-keyed context.
// Returns an empty config (not an error) if no context exists for the URL.
func LoadContextConfig(rawURL string) (ContextConfig, error) {
	url := normalizeContextKey(rawURL)
	path, err := contextConfigPath(url)
	if err != nil {
		return ContextConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ContextConfig{URL: url}, nil
		}
		return ContextConfig{}, fmt.Errorf("reading context config for %q: %w", url, err)
	}
	var cfg ContextConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ContextConfig{}, fmt.Errorf("parsing context config for %q: %w", url, err)
	}
	cfg.URL = url
	return cfg, nil
}

// SaveContextConfig writes structured context with owner-only permissions;
// non-credential does not imply non-sensitive.
func SaveContextConfig(rawURL string, cfg ContextConfig) error {
	url := normalizeContextKey(rawURL)
	dir, err := contextsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("creating contexts directory: %w", err)
	}
	cfg.URL = url
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling context config: %w", err)
	}
	path := filepath.Join(dir, contextFilename(url))
	// AtomicWriteFile normally preserves an existing mode. Context files from
	// older releases may be 0644, so harden them before replacement instead of
	// preserving an unsafe legacy mode.
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		if chmodErr := os.Chmod(path, CredsFilePerm); chmodErr != nil {
			return fmt.Errorf("securing context config for %q: %w", url, chmodErr)
		}
	}
	return AtomicWriteFile(path, data, CredsFilePerm)
}

// LoadContextCredentials reads a URL's credentials from the active credential
// backend (OS keychain by default; a JSON file when OB_CREDENTIALS_FILE is
// set). Returns nil (not an error) if no credentials are stored. The returned
// map uses well-known field names (bearerToken, apiKey, basic). The key is
// normalized (http → https) at this boundary, matching the config-file store,
// so credentials set with an http:// target resolve when the same origin is
// looked up under either scheme.
func LoadContextCredentials(url string) (map[string]any, error) {
	return activeCredentialBackend().Load(normalizeContextKey(url))
}

// SaveContextCredentials writes a URL's credentials to the active credential
// backend (OS keychain by default; a JSON file when OB_CREDENTIALS_FILE is
// set). The map should use well-known field names (bearerToken, apiKey,
// basic). The key is normalized (http → https) at this boundary; see
// LoadContextCredentials.
func SaveContextCredentials(url string, cred map[string]any) error {
	return activeCredentialBackend().Save(normalizeContextKey(url), cred)
}

// DeleteContextCredentials removes a URL's credentials from the active
// credential backend. The key is normalized (http → https) at this boundary;
// see LoadContextCredentials.
func DeleteContextCredentials(url string) error {
	return activeCredentialBackend().Delete(normalizeContextKey(url))
}

// credentialsExist reports whether the active backend holds credentials for a
// URL. It never surfaces a backend error: an unavailable keychain or
// unreadable credentials file reports "no credentials" here, and the loud
// error is reserved for the Load/Save/Delete paths that actually move a
// secret. Used by the has-credentials flags of the listing/summary lanes.
func credentialsExist(url string) bool {
	return activeCredentialBackend().Has(normalizeContextKey(url))
}

// LoadContext returns the unified context payload for a target URL, matching
// the Context schema ob stores as the document-store value. Credential fields
// (bearerToken, apiKey, basic, ...) sit alongside transport fields (headers,
// cookies, environment, metadata) in a single opaque map. Internally the
// implementation splits storage — secrets to the OS keychain, transport
// fields to the on-disk config file — but that split is not part of the
// outward contract.
//
// Hierarchical matching: tries the exact URL first, then walks up the path
// (like cookies) to find the most specific match. Returns nil if no context
// exists for the URL.
func LoadContext(rawURL string) (map[string]any, error) {
	if rawURL == "" {
		return nil, nil
	}
	targetURL := normalizeContextKey(rawURL)

	matchedURL := resolveContextURL(targetURL)
	if matchedURL == "" {
		return nil, nil
	}

	cfg, err := LoadContextConfig(matchedURL)
	if err != nil {
		return nil, err
	}
	cred, err := LoadContextCredentials(matchedURL)
	if err != nil {
		return nil, err
	}

	return mergeConfigAndCredentials(&cfg, cred), nil
}

// resolveContextURL finds the best matching context URL for a target.
// Tries exact match first, then walks up the URL path hierarchy.
// For example, for "https://api.example.com/v1/spec.json", tries:
//  1. https://api.example.com/v1/spec.json  (exact)
//  2. https://api.example.com/v1
//  3. https://api.example.com
func resolveContextURL(targetURL string) string {
	if ContextExists(targetURL) {
		return targetURL
	}

	// For non-HTTP URLs (exec:, file paths), only exact match
	if !strings.Contains(targetURL, "://") {
		return ""
	}

	// Split into origin + path and walk up
	schemeEnd := strings.Index(targetURL, "://")
	if schemeEnd < 0 {
		return ""
	}
	rest := targetURL[schemeEnd+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return "" // Already just origin, no match found
	}

	origin := targetURL[:schemeEnd+3+slashIdx]
	pathPart := rest[slashIdx:]

	// Walk up path segments
	for pathPart != "" {
		lastSlash := strings.LastIndex(pathPart, "/")
		if lastSlash <= 0 {
			break
		}
		pathPart = pathPart[:lastSlash]
		candidate := origin + pathPart
		if ContextExists(candidate) {
			return candidate
		}
	}

	// Try just the origin (scheme + host)
	if ContextExists(origin) {
		return origin
	}

	return ""
}

// mergeConfigAndCredentials combines the two storage lanes into one Context
// payload. The split is structural, not a secrecy classification.
func mergeConfigAndCredentials(cfg *ContextConfig, cred map[string]any) map[string]any {
	hasConfig := cfg != nil && (len(cfg.Headers) > 0 || len(cfg.Cookies) > 0 || len(cfg.Environment) > 0 || len(cfg.Metadata) > 0 || len(cfg.Configuration) > 0)
	if len(cred) == 0 && !hasConfig {
		return nil
	}
	out := make(map[string]any, len(cred)+5)
	for k, v := range cred {
		out[k] = v
	}
	if cfg != nil {
		if len(cfg.Headers) > 0 {
			out["headers"] = stringMapToAny(cfg.Headers)
		}
		if len(cfg.Cookies) > 0 {
			out["cookies"] = stringMapToAny(cfg.Cookies)
		}
		if len(cfg.Environment) > 0 {
			out["environment"] = stringMapToAny(cfg.Environment)
		}
		if len(cfg.Metadata) > 0 {
			out["metadata"] = cfg.Metadata
		}
		if len(cfg.Configuration) > 0 {
			out["configuration"] = cfg.Configuration
		}
	}
	return out
}

// DeleteContext removes both the config file and keychain entry for a URL-keyed context.
func DeleteContext(url string) error {
	path, err := contextConfigPath(url)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing context config for %q: %w", url, err)
	}
	return DeleteContextCredentials(url)
}

// ContextExists returns true if a context exists for the URL.
func ContextExists(url string) bool {
	path, err := contextConfigPath(url)
	if err != nil {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return credentialsExist(url)
}

// ListContexts returns summaries of all URL-keyed contexts. The result is
// always a non-nil slice (the listContexts wire output is a bare array,
// never null).
func ListContexts() ([]ContextSummary, error) {
	summaries := []ContextSummary{}
	dir, err := contextsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return summaries, nil
		}
		return nil, fmt.Errorf("reading contexts directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg ContextConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			summaries = append(summaries, ContextSummary{
				URL:       e.Name(),
				LoadError: err.Error(),
			})
			continue
		}
		if cfg.URL == "" {
			cfg.URL = strings.TrimSuffix(e.Name(), ".json")
		}
		summaries = append(summaries, ContextSummary{
			URL:                cfg.URL,
			HasCredentials:     credentialsExist(cfg.URL),
			HeaderCount:        len(cfg.Headers),
			CookieCount:        len(cfg.Cookies),
			EnvCount:           len(cfg.Environment),
			MetadataCount:      len(cfg.Metadata),
			ConfigurationCount: len(cfg.Configuration),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].URL < summaries[j].URL
	})

	return summaries, nil
}

// GetContextSummary returns a redacted summary for a single URL-keyed context.
func GetContextSummary(rawURL string) (ContextSummary, error) {
	targetURL := normalizeContextKey(rawURL)
	cfg, err := LoadContextConfig(targetURL)
	if err != nil {
		if os.IsNotExist(err) {
			if credentialsExist(targetURL) {
				return ContextSummary{URL: targetURL, HasCredentials: true}, nil
			}
			return ContextSummary{URL: targetURL}, nil
		}
		return ContextSummary{URL: targetURL, LoadError: err.Error()}, err
	}
	return ContextSummary{
		URL:                targetURL,
		HasCredentials:     credentialsExist(targetURL),
		HeaderCount:        len(cfg.Headers),
		CookieCount:        len(cfg.Cookies),
		EnvCount:           len(cfg.Environment),
		MetadataCount:      len(cfg.Metadata),
		ConfigurationCount: len(cfg.Configuration),
	}, nil
}

// cliContextStore implements invoke.ContextStore by wrapping the CLI's
// file+keychain persistence. The SDK and drivers call this through the
// ContextStore interface — they never import this package directly.
//
// The store backs the document-store interface; ob's values are unified Context
// payloads: credential fields (bearerToken, apiKey, basic, ...) alongside
// transport fields (headers, cookies, environment, metadata) in a single
// opaque map. The implementation splits storage internally — secrets to the
// OS keychain, transport fields to the on-disk config file — but that split
// is not part of the contract.
type cliContextStore struct{}

// NewCLIContextStore returns a ContextStore backed by the CLI's file-system
// config and OS keychain.
func NewCLIContextStore() invoke.ContextStore { return &cliContextStore{} }

func (s *cliContextStore) Get(_ context.Context, key string) (map[string]any, error) {
	ctx, err := LoadContext(key)
	if err != nil || len(ctx) > 0 {
		return ctx, err
	}
	// Bridge key conventions: the binding-invoker interface's challenge keys are
	// normalized origins (host[:port], no scheme, no path — the same identity
	// invoke.NormalizeEndpoint derives), while `ob context set <url>`
	// persists under the URL the user supplied (commonly with a path). The
	// hierarchical resolver only walks path-specific→general, so an origin
	// challenge can't reach a context saved at a deeper URL. Resolve by origin
	// identity: find a stored context whose endpoint normalizes to the same
	// origin as the challenge key.
	if !strings.Contains(key, "://") {
		if matched := findStoredContextByOrigin(key); matched != "" {
			return LoadContext(matched)
		}
	}
	return ctx, nil
}

// findStoredContextByOrigin returns the stored context URL whose endpoint
// shares the challenge key's normalized origin (host[:port]), or "" when none
// match.
//
// Several stored URLs can share one origin at different paths, and the choice
// among them used to be "shortest wins". That silently lost credentials: a
// context holding nothing but a header at the bare origin outranked the one
// holding the bearer token at `/api/v3`, so a caller who had stored exactly the
// right credential was told CONTEXT_REQUIRED with the value sitting in the
// store. It cost a long investigation to find, because every visible piece —
// the store, the satisfaction rule, the resolver — was individually correct.
//
// So candidates carrying credential material are preferred, and among equals
// the MOST specific (longest) URL wins rather than the least. Both halves are
// deterministic, and an origin with only non-credential contexts still resolves
// exactly as before.
//
// This is a heuristic, and worth naming as one: the ContextStore contract hands
// Get a key and not the challenge, so this layer cannot ask the question it
// actually wants to ask — "which of these satisfies the requirement in front of
// me?". Plumbing the challenge through would answer it properly; that is a
// contract change, not a bug fix, and belongs in its own decision.
func findStoredContextByOrigin(originKey string) string {
	want := invoke.NormalizeEndpoint(originKey)
	if want == "" {
		return ""
	}
	summaries, err := ListContexts()
	if err != nil {
		return ""
	}
	best, bestHasCredential := "", false
	for _, sum := range summaries {
		if invoke.NormalizeEndpoint(sum.URL) != want {
			continue
		}
		candidate, loadErr := LoadContext(sum.URL)
		if loadErr != nil {
			continue
		}
		hasCredential := contextCarriesCredential(candidate)
		if best == "" ||
			(hasCredential && !bestHasCredential) ||
			(hasCredential == bestHasCredential && len(sum.URL) > len(best)) {
			best, bestHasCredential = sum.URL, hasCredential
		}
	}
	return best
}

// contextCarriesCredential reports whether a stored context holds anything that
// could answer an auth requirement. It is deliberately a membership test over
// the well-known field names rather than a satisfaction check: this function
// cannot see the challenge, so it can only ask whether a candidate is the kind
// of context a credential challenge would want.
func contextCarriesCredential(ctx map[string]any) bool {
	for _, field := range []string{
		"bearerToken", "apiKey", "apiKeys", "basic", "accessToken", "credentials",
	} {
		if value, present := ctx[field]; present && value != nil {
			return true
		}
	}
	return false
}

func (s *cliContextStore) Set(_ context.Context, key string, value map[string]any) error {
	if !strings.Contains(key, "://") {
		key = "https://" + key
	}
	return SaveUnifiedContext(key, value)
}

func (s *cliContextStore) Delete(_ context.Context, key string) error {
	if !strings.Contains(key, "://") {
		key = "https://" + key
	}
	return DeleteContext(key)
}

// DetectLegacyContexts checks for old-style named context files (those without
// a URL field). Returns the names of legacy contexts found.
func DetectLegacyContexts() []string {
	dir, err := contextsDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var legacy []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg ContextConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if cfg.URL == "" {
			legacy = append(legacy, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return legacy
}
