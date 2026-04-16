package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.1.1", true},
		{"0.1.1", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", true},
		{"0.9.9", "1.0.0", true},
		{"1.0.0", "0.9.9", false},
		{"v0.1.0", "v0.1.1", true}, // leading v tolerated
		{"0.1.0-rc1", "0.1.0", false}, // pre-release suffix ignored
		{"0.1.0", "0.1.0+build1", false},
		{"not-a-version", "0.1.0", false}, // parse error → false
		{"0.1.0", "also-garbage", false},
		{"0.1", "0.1.1", false}, // non-3-part → parse error
	}
	for _, tc := range cases {
		if got := versionLess(tc.a, tc.b); got != tc.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"0.1.0", []int{0, 1, 0}},
		{"v10.20.30", []int{10, 20, 30}},
		{"0.1.0-rc1", []int{0, 1, 0}},
		{"0.1.0+abc", []int{0, 1, 0}},
		{"1.2", nil},
		{"abc", nil},
		{"1.2.x", nil},
		{"-1.0.0", nil},
	}
	for _, tc := range cases {
		got := parseVersion(tc.in)
		if tc.want == nil && got != nil {
			t.Errorf("parseVersion(%q) = %v, want nil", tc.in, got)
		}
		if tc.want != nil && (len(got) != 3 || got[0] != tc.want[0] || got[1] != tc.want[1] || got[2] != tc.want[2]) {
			t.Errorf("parseVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestUpdateCheckCacheRoundTrip(t *testing.T) {
	// Redirect cache dir to a temp location.
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	// On macOS, os.UserCacheDir consults HOME to build
	// ~/Library/Caches. Override HOME too so we don't scribble
	// outside the sandbox.
	t.Setenv("HOME", tmp)

	in := updateCheckCache{Latest: "0.1.1", CheckedAt: time.Now().UTC().Truncate(time.Second)}
	if err := writeUpdateCache(in); err != nil {
		t.Fatal(err)
	}
	out, ok := readUpdateCache()
	if !ok {
		t.Fatal("readUpdateCache returned !ok")
	}
	if out.Latest != in.Latest {
		t.Errorf("Latest = %q, want %q", out.Latest, in.Latest)
	}
	if !out.CheckedAt.Equal(in.CheckedAt) {
		t.Errorf("CheckedAt = %v, want %v", out.CheckedAt, in.CheckedAt)
	}
}

func TestFetchLatestRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "ob-cli" {
			t.Errorf("missing User-Agent header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.2.3"})
	}))
	defer srv.Close()

	origURL := updateCheckURL
	updateCheckURL = srv.URL
	defer func() { updateCheckURL = origURL }()

	v, err := fetchLatestRelease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v != "0.2.3" {
		t.Errorf("v = %q, want %q", v, "0.2.3")
	}
}

func TestFetchLatestReleaseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprintln(w, "nope")
	}))
	defer srv.Close()

	origURL := updateCheckURL
	updateCheckURL = srv.URL
	defer func() { updateCheckURL = origURL }()

	if _, err := fetchLatestRelease(t.Context()); err == nil {
		t.Error("expected error on 500, got nil")
	}
}

func TestStartUpdateCheckSkipsWhenOptedOut(t *testing.T) {
	t.Setenv("OB_NO_UPDATE_CHECK", "1")
	// Should return a no-op immediately; no HTTP request.
	done := StartUpdateCheck("0.1.0")
	done()
}

func TestStartUpdateCheckSkipsDevBuild(t *testing.T) {
	done := StartUpdateCheck("dev")
	done()
	done = StartUpdateCheck("")
	done()
}

func TestUpdateCachePathUnderCacheDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	t.Setenv("HOME", tmp)
	path, err := updateCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != updateCacheFile {
		t.Errorf("basename = %q, want %q", filepath.Base(path), updateCacheFile)
	}
}
