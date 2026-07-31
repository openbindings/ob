package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression for the SSRF gap: the source-content read path (status, source
// pull, synthesize/addSource, merge) resolves refs that may come from an
// untrusted document. It must apply the same outbound policy /resolve does —
// block private/link-local/metadata ranges — or an embedded ref like
// http://169.254.169.254/... is a blind SSRF.
func TestSSRF_ReadSourceContent_BlocksPrivateRanges(t *testing.T) {
	// Cloud-metadata and RFC1918 addresses must be refused BEFORE any dial,
	// so this returns immediately rather than hanging on a 30s timeout.
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/internal",
		"http://192.168.1.1/admin",
		"file:///etc/passwd",
	} {
		_, err := ReadSourceContent(u, "")
		if err == nil {
			t.Fatalf("%s: expected SSRF refusal, got none", u)
		}
	}
}

// Loopback stays reachable — resolving locally-running services is a primary
// use case and the guard deliberately allows it.
func TestSSRF_ReadSourceContent_AllowsLoopback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"openbindings":"0.2.0"}`))
	}))
	defer ts.Close()

	data, err := ReadSourceContent(ts.URL, "")
	if err != nil {
		t.Fatalf("loopback fetch should succeed: %v", err)
	}
	if !strings.Contains(string(data), "openbindings") {
		t.Fatal("unexpected body")
	}
}

func TestSSRF_RedirectToBlockedRange(t *testing.T) {
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer redirector.Close()

	_, err := ReadSourceContent(redirector.URL, "")
	if err == nil {
		t.Fatal("BYPASS: fetch succeeded against link-local target")
	}
	msg := err.Error()
	if strings.Contains(msg, "private/internal IP addresses are not allowed") {
		t.Logf("GUARD HELD per-hop: %v", msg)
		return
	}
	t.Errorf("BYPASS CONFIRMED: guard not consulted on redirect hop; client attempted the internal dial: %v", msg)
}
