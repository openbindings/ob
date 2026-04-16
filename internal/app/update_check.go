package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Update-check configuration. Kept as package vars so tests can override.
var (
	updateCheckURL     = "https://api.github.com/repos/openbindings/ob/releases/latest"
	updateCacheFile    = "update-check.json"
	updateCacheTTL     = 24 * time.Hour
	updateCheckTimeout = 2 * time.Second
)

type updateCheckCache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checkedAt"`
}

// StartUpdateCheck fires an async probe of the GitHub releases API and
// returns a function the caller should defer. The returned function
// prints a one-line notification to stderr if a newer version is
// available, or does nothing otherwise. The probe uses a bounded
// timeout so it never delays the user's actual command by more than
// `updateCheckTimeout`.
//
// The check is skipped (returned function is a no-op) when:
//
//   - OB_NO_UPDATE_CHECK=1 is set in the environment.
//   - stderr is not a terminal (CI, scripts, redirected output).
//   - currentVersion is empty or the sentinel "dev" (unversioned local build).
//
// Results are cached for `updateCacheTTL` at
// `$XDG_CACHE_HOME/ob/update-check.json` (or `~/.cache/ob/update-check.json`
// on systems without XDG) so repeated invocations within the same day
// make at most one network request.
func StartUpdateCheck(currentVersion string) func() {
	if os.Getenv("OB_NO_UPDATE_CHECK") == "1" {
		return func() {}
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return func() {}
	}
	if currentVersion == "" || currentVersion == "dev" {
		return func() {}
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)

	var (
		latest string
		mu     sync.Mutex
		done   = make(chan struct{})
	)

	go func() {
		defer close(done)
		defer cancel()

		// Cache hit within TTL: reuse without a network call.
		if cached, ok := readUpdateCache(); ok && time.Since(cached.CheckedAt) < updateCacheTTL {
			mu.Lock()
			latest = cached.Latest
			mu.Unlock()
			return
		}

		v, err := fetchLatestRelease(ctx)
		if err != nil {
			return
		}
		_ = writeUpdateCache(updateCheckCache{Latest: v, CheckedAt: time.Now()})
		mu.Lock()
		latest = v
		mu.Unlock()
	}()

	return func() {
		// Block up to updateCheckTimeout for the probe to finish.
		// In practice this is a no-op on cache hits (resolved instantly)
		// and bounded by the context deadline on cache misses.
		select {
		case <-done:
		case <-time.After(updateCheckTimeout):
		}
		cancel()

		mu.Lock()
		v := latest
		mu.Unlock()
		if v == "" {
			return
		}
		if versionLess(currentVersion, v) {
			fmt.Fprintf(os.Stderr,
				"\n  A new version of ob is available: %s → %s\n  Upgrade: brew upgrade ob\n  Disable this notice: export OB_NO_UPDATE_CHECK=1\n",
				currentVersion, v)
		}
	}
}

func fetchLatestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", updateCheckURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ob-cli")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return strings.TrimPrefix(body.TagName, "v"), nil
}

func readUpdateCache() (updateCheckCache, bool) {
	path, err := updateCachePath()
	if err != nil {
		return updateCheckCache{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateCheckCache{}, false
	}
	var cache updateCheckCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return updateCheckCache{}, false
	}
	if cache.Latest == "" {
		return updateCheckCache{}, false
	}
	return cache, true
}

func writeUpdateCache(cache updateCheckCache) error {
	path, err := updateCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func updateCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ob", updateCacheFile), nil
}

// versionLess reports whether a < b under simple X.Y.Z semver. Pre-release
// suffixes after `-` or build metadata after `+` are ignored for the
// comparison — 0.1.1-rc1 and 0.1.1 are treated as equal. Returns false on
// any parse error so we never falsely claim an update is available.
func versionLess(a, b string) bool {
	pa := parseVersion(a)
	pb := parseVersion(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseVersion(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if idx := strings.IndexAny(s, "-+"); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}
