package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/zalando/go-keyring"
)

// ContextConfig holds the non-secret fields of a URL-keyed context.
// Persisted as JSON in ~/.config/openbindings/contexts/<sanitized-url>.json.
type ContextConfig struct {
	URL             string                      `json:"url"`
	Headers         map[string]string           `json:"headers,omitempty"`
	Cookies         map[string]string           `json:"cookies,omitempty"`
	Environment     map[string]string           `json:"environment,omitempty"`
	Metadata        map[string]any              `json:"metadata,omitempty"`
	SourceOverrides map[string]*ContextOverride `json:"sourceOverrides,omitempty"`
}

// ContextOverride holds per-source context overrides within a target context.
type ContextOverride struct {
	Headers     map[string]string `json:"headers,omitempty"`
	Cookies     map[string]string `json:"cookies,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

// ContextSummary is a compact representation for listing contexts.
type ContextSummary struct {
	URL            string `json:"url"`
	HasCredentials bool   `json:"hasCredentials"`
	HeaderCount    int    `json:"headerCount,omitempty"`
	CookieCount    int    `json:"cookieCount,omitempty"`
	EnvCount       int    `json:"envCount,omitempty"`
	MetadataCount  int    `json:"metadataCount,omitempty"`
	SourceCount    int    `json:"sourceCount,omitempty"`
	LoadError      string `json:"loadError,omitempty"`
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

// LoadContextConfig reads the non-secret config for a URL-keyed context.
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

// SaveContextConfig writes the non-secret config for a URL-keyed context.
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
	return AtomicWriteFile(path, data, FilePerm)
}

// keychainKey returns the keychain key for a URL-keyed context.
// For source-level overrides, appends the source name.
func keychainKey(url, source string) string {
	if source != "" {
		return url + "\x00" + source
	}
	return url
}

// LoadContextCredentials reads credentials from the OS keychain for a URL.
// Returns nil (not an error) if no credentials are stored.
// The returned map uses well-known field names (bearerToken, apiKey, basic).
func LoadContextCredentials(url string) (map[string]any, error) {
	return loadKeychainCredentials(keychainKey(url, ""))
}

// LoadSourceContextCredentials reads credentials for a source-level override.
func LoadSourceContextCredentials(url, source string) (map[string]any, error) {
	return loadKeychainCredentials(keychainKey(url, source))
}

func loadKeychainCredentials(key string) (map[string]any, error) {
	secret, err := keyring.Get(KeychainService, key)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("reading keychain for context %q: %w", key, err)
	}
	var cred map[string]any
	if err := json.Unmarshal([]byte(secret), &cred); err != nil {
		return nil, fmt.Errorf("parsing keychain credentials for context %q: %w", key, err)
	}
	return cred, nil
}

// SaveContextCredentials writes credentials to the OS keychain for a URL.
// The map should use well-known field names (bearerToken, apiKey, basic).
func SaveContextCredentials(url string, cred map[string]any) error {
	return saveKeychainCredentials(keychainKey(url, ""), cred)
}

// SaveSourceContextCredentials writes credentials for a source-level override.
func SaveSourceContextCredentials(url, source string, cred map[string]any) error {
	return saveKeychainCredentials(keychainKey(url, source), cred)
}

func saveKeychainCredentials(key string, cred map[string]any) error {
	if len(cred) == 0 {
		return deleteKeychainCredentials(key)
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}
	if err := keyring.Set(KeychainService, key, string(data)); err != nil {
		return fmt.Errorf("writing keychain for context %q: %w", key, err)
	}
	return nil
}

// DeleteContextCredentials removes credentials from the OS keychain.
func DeleteContextCredentials(url string) error {
	return deleteKeychainCredentials(keychainKey(url, ""))
}

func deleteKeychainCredentials(key string) error {
	err := keyring.Delete(KeychainService, key)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("deleting keychain for context %q: %w", key, err)
	}
	return nil
}

// LoadContext loads binding context and execution options from config file +
// keychain for a target URL. Uses hierarchical matching: tries the exact URL
// first, then walks up the path (like cookies) to find the most specific match.
// Context contains credentials (opaque, well-known fields).
// Options contains developer-configured headers, cookies, env, metadata.
func LoadContext(rawURL string) (bindCtx map[string]any, opts *openbindings.ExecutionOptions, err error) {
	if rawURL == "" {
		return nil, nil, nil
	}
	targetURL := normalizeContextKey(rawURL)

	matchedURL := resolveContextURL(targetURL)
	if matchedURL == "" {
		return nil, nil, nil
	}

	cfg, err := LoadContextConfig(matchedURL)
	if err != nil {
		return nil, nil, err
	}
	cred, err := LoadContextCredentials(matchedURL)
	if err != nil {
		return nil, nil, err
	}

	return cred, configToOptions(&cfg), nil
}

// resolveContextURL finds the best matching context URL for a target.
// Tries exact match first, then walks up the URL path hierarchy.
// For example, for "https://api.example.com/v1/spec.json", tries:
//   1. https://api.example.com/v1/spec.json  (exact)
//   2. https://api.example.com/v1
//   3. https://api.example.com
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

// LoadContextForSource loads context for a specific source within a target.
// Source-level overrides are merged on top of the target-level context.
// Source-level credentials replace (not merge with) target-level credentials.
func LoadContextForSource(targetURL, sourceName string) (bindCtx map[string]any, opts *openbindings.ExecutionOptions, err error) {
	if targetURL == "" {
		return nil, nil, nil
	}
	targetURL = normalizeContextKey(targetURL)

	matchedURL := resolveContextURL(targetURL)
	if matchedURL == "" {
		return nil, nil, nil
	}

	cfg, err := LoadContextConfig(matchedURL)
	if err != nil {
		return nil, nil, err
	}
	cred, err := LoadContextCredentials(matchedURL)
	if err != nil {
		return nil, nil, err
	}

	baseOpts := configToOptions(&cfg)

	if sourceName == "" {
		return cred, baseOpts, nil
	}

	override, ok := cfg.SourceOverrides[sourceName]
	if !ok || override == nil {
		return cred, baseOpts, nil
	}

	srcCred, err := LoadSourceContextCredentials(matchedURL, sourceName)
	if err == nil && len(srcCred) > 0 {
		cred = srcCred
	}

	mergedOpts := mergeOptions(baseOpts, override)
	return cred, mergedOpts, nil
}

// configToOptions converts a ContextConfig's non-credential fields to ExecutionOptions.
func configToOptions(cfg *ContextConfig) *openbindings.ExecutionOptions {
	if cfg == nil {
		return nil
	}
	if len(cfg.Headers) == 0 && len(cfg.Cookies) == 0 && len(cfg.Environment) == 0 && len(cfg.Metadata) == 0 {
		return nil
	}
	return &openbindings.ExecutionOptions{
		Headers:     cfg.Headers,
		Cookies:     cfg.Cookies,
		Environment: cfg.Environment,
		Metadata:    cfg.Metadata,
	}
}

// mergeOptions merges a ContextOverride on top of base ExecutionOptions.
func mergeOptions(base *openbindings.ExecutionOptions, override *ContextOverride) *openbindings.ExecutionOptions {
	if override == nil {
		return base
	}
	var bh, bc, be map[string]string
	var bm map[string]any
	if base != nil {
		bh, bc, be, bm = base.Headers, base.Cookies, base.Environment, base.Metadata
	}
	merged := &openbindings.ExecutionOptions{
		Headers:     mergeMaps(bh, override.Headers),
		Cookies:     mergeMaps(bc, override.Cookies),
		Environment: mergeMaps(be, override.Environment),
		Metadata:    mergeAnyMaps(bm, override.Metadata),
	}
	if len(merged.Headers) == 0 && len(merged.Cookies) == 0 && len(merged.Environment) == 0 && len(merged.Metadata) == 0 {
		return nil
	}
	return merged
}

func mergeMaps(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	result := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}

func mergeAnyMaps(base, overlay map[string]any) map[string]any {
	if len(overlay) == 0 {
		return base
	}
	result := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}

// DeleteContext removes both the config file and keychain entry for a URL-keyed context.
func DeleteContext(url string) error {
	path, err := contextConfigPath(url)
	if err != nil {
		return err
	}
	// Also delete source-level keychain entries
	cfg, _ := LoadContextConfig(url)
	if cfg.SourceOverrides != nil {
		for src := range cfg.SourceOverrides {
			_ = deleteKeychainCredentials(keychainKey(url, src))
		}
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
	_, err = keyring.Get(KeychainService, keychainKey(url, ""))
	return err == nil
}

// ListContexts returns summaries of all URL-keyed contexts.
func ListContexts() ([]ContextSummary, error) {
	dir, err := contextsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading contexts directory: %w", err)
	}

	var summaries []ContextSummary
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
		hasCreds := false
		if _, kerr := keyring.Get(KeychainService, keychainKey(cfg.URL, "")); kerr == nil {
			hasCreds = true
		}
		summaries = append(summaries, ContextSummary{
			URL:            cfg.URL,
			HasCredentials: hasCreds,
			HeaderCount:    len(cfg.Headers),
			CookieCount:    len(cfg.Cookies),
			EnvCount:       len(cfg.Environment),
			MetadataCount:  len(cfg.Metadata),
			SourceCount:    len(cfg.SourceOverrides),
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
			hasCreds := false
			if _, kerr := keyring.Get(KeychainService, keychainKey(targetURL, "")); kerr == nil {
				hasCreds = true
			}
			if hasCreds {
				return ContextSummary{URL: targetURL, HasCredentials: true}, nil
			}
			return ContextSummary{URL: targetURL}, nil
		}
		return ContextSummary{URL: targetURL, LoadError: err.Error()}, err
	}
	hasCreds := false
	if _, kerr := keyring.Get(KeychainService, keychainKey(targetURL, "")); kerr == nil {
		hasCreds = true
	}
	return ContextSummary{
		URL:            targetURL,
		HasCredentials: hasCreds,
		HeaderCount:    len(cfg.Headers),
		CookieCount:    len(cfg.Cookies),
		EnvCount:       len(cfg.Environment),
		MetadataCount:  len(cfg.Metadata),
		SourceCount:    len(cfg.SourceOverrides),
	}, nil
}

// cliContextStore implements openbindings.ContextStore by wrapping the CLI's
// existing file+keychain persistence. The SDK and executors call this through
// the ContextStore interface — they never import this package directly.
type cliContextStore struct{}

// NewCLIContextStore returns a ContextStore backed by the CLI's file-system
// config and OS keychain.
func NewCLIContextStore() openbindings.ContextStore { return &cliContextStore{} }

func (s *cliContextStore) Get(_ context.Context, key string) (map[string]any, error) {
	cred, _, err := LoadContext(key)
	return cred, err
}

func (s *cliContextStore) Set(_ context.Context, key string, value map[string]any) error {
	return SaveContextCredentials(key, value)
}

func (s *cliContextStore) Delete(_ context.Context, key string) error {
	return DeleteContextCredentials(key)
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
