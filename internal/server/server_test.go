package server

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustNewServer(t *testing.T, token string) *Server {
	t.Helper()
	srv, err := New(Config{
		Port:   0,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Token:  token,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func mustNewServerWithOrigins(t *testing.T, token string, origins []string) *Server {
	t.Helper()
	srv, err := New(Config{
		Port:           0,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Token:          token,
		AllowedOrigins: origins,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// callRecorder is a simple handler that records whether it was called.
type callRecorder struct {
	called bool
}

func (cr *callRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cr.called = true
		w.WriteHeader(http.StatusOK)
	}
}

// --- Auth middleware tests ---

func TestAuthMiddleware_ValidToken(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Authorization", "bEaReR test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !rec.called {
		t.Error("inner handler was not called")
	}
}

func TestHandler_UsesStructuredErrorsAndSecurityHeaders(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	req := httptest.NewRequest("GET", "/describe", nil)
	req.Host = "localhost"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["code"] != "unauthorized" || body["error"] == "" {
		t.Fatalf("error response = %#v", body)
	}
	for name, want := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := w.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if rec.called {
		t.Error("inner handler should not have been called")
	}
}

func TestAuthMiddleware_WrongToken(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if rec.called {
		t.Error("inner handler should not have been called")
	}
}

func TestAuthMiddleware_WellKnownExempt(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/.well-known/openbindings", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !rec.called {
		t.Error("inner handler was not called for exempt path")
	}
}

func TestAuthMiddleware_HealthzExempt(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !rec.called {
		t.Error("inner handler was not called for exempt path")
	}
}

func TestAuthMiddleware_WorkbenchAssetsExempt(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/assets/workbench.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !rec.called {
		t.Error("inner handler was not called for public workbench asset")
	}
}

func TestAuthMiddleware_SpoofedUpgradeOnNonWSRouteRejected(t *testing.T) {
	// A spoofed `Upgrade: websocket` header must NOT exempt a non-WebSocket
	// route from bearer-token auth. Only the /bindings/invoke handler does
	// in-message auth; every other route would otherwise run unauthenticated.
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	// Deliberately no Authorization header.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if rec.called {
		t.Error("inner handler should not have been called for spoofed-upgrade non-WS route")
	}
}

func TestAuthMiddleware_SpoofedUpgradeHeaderOnWSRouteWithoutConnectionRejected(t *testing.T) {
	// Even on the WS route, a lone Upgrade header without a genuine
	// `Connection: upgrade` token is not a real upgrade and must still be
	// authenticated, so a forged header alone can't bypass auth there either.
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/bindings/invoke", nil)
	req.Header.Set("Upgrade", "websocket") // no Connection: upgrade
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if rec.called {
		t.Error("inner handler should not have been called for non-upgrade request")
	}
}

func TestAuthMiddleware_GenuineUpgradeOnWSRouteExempt(t *testing.T) {
	// A genuine WebSocket upgrade to the invocation route is exempt from
	// header auth (the handler re-authenticates the upgrade request).
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/bindings/invoke", nil)
	req.Header.Set("Connection", "keep-alive, Upgrade")
	req.Header.Set("Upgrade", "websocket")
	// No Authorization header: auth is in the first WS message.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !rec.called {
		t.Error("inner handler was not called for genuine WS upgrade on the WS route")
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	cases := []struct {
		name       string
		connection string
		upgrade    string
		want       bool
	}{
		{"genuine simple", "Upgrade", "websocket", true},
		{"genuine multi-token", "keep-alive, Upgrade", "websocket", true},
		{"case-insensitive", "upgrade", "WebSocket", true},
		{"missing connection", "", "websocket", false},
		{"connection without upgrade token", "keep-alive", "websocket", false},
		{"upgrade not websocket", "Upgrade", "h2c", false},
		{"no headers", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/bindings/invoke", nil)
			if tc.connection != "" {
				req.Header.Set("Connection", tc.connection)
			}
			if tc.upgrade != "" {
				req.Header.Set("Upgrade", tc.upgrade)
			}
			if got := IsWebSocketUpgrade(req); got != tc.want {
				t.Errorf("IsWebSocketUpgrade() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- CORS middleware tests ---

func TestCORSMiddleware_AllowedOrigin(t *testing.T) {
	s := mustNewServerWithOrigins(t, "tok", []string{"http://localhost:3000"})
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:3000")
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods header missing")
	}
	if !rec.called {
		t.Error("inner handler was not called")
	}
}

func TestCORSMiddleware_DisallowedOrigin(t *testing.T) {
	s := mustNewServerWithOrigins(t, "tok", []string{"http://localhost:3000"})
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if !rec.called {
		t.Error("inner handler was not called (CORS should not block non-preflight)")
	}
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	s := mustNewServerWithOrigins(t, "tok", []string{"http://localhost:3000"})
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("OPTIONS", "/describe", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if rec.called {
		t.Error("inner handler should not be called for OPTIONS preflight")
	}
}

func TestCORSMiddleware_PreflightDisallowedOrigin(t *testing.T) {
	s := mustNewServerWithOrigins(t, "tok", []string{"http://localhost:3000"})
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("OPTIONS", "/describe", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for disallowed origin", got)
	}
}

func TestCORSMiddleware_AutoLocalhostDefault(t *testing.T) {
	s := mustNewServer(t, "tok") // no AllowedOrigins configured
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	origins := []string{
		"http://localhost:5173",
		"http://localhost:3000",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:8080",
		"https://localhost:5173",
		"https://127.0.0.1:20290",
		"http://[::1]:5173",
	}
	for _, origin := range origins {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/describe", nil)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
			}
		})
	}
}

func TestCORSMiddleware_AutoLocalhostRejectsRemote(t *testing.T) {
	s := mustNewServer(t, "tok") // no AllowedOrigins configured
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	origins := []string{
		"http://evil.com",
		"http://192.168.1.1:3000",
	}
	for _, origin := range origins {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/describe", nil)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("Access-Control-Allow-Origin = %q, want empty for %q", got, origin)
			}
		})
	}
}

func TestCORSMiddleware_RemoteHTTPSRequiresExplicitAllow(t *testing.T) {
	s := mustNewServer(t, "tok")
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	for _, origin := range []string{"https://example.com", "https://app.example.net", "https://anything.dev"} {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/describe", nil)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("Access-Control-Allow-Origin = %q, want empty for non-allowlisted origin %q", got, origin)
			}
		})
	}
}

func TestCORSMiddleware_ExplicitRemoteHTTPSOriginAllowed(t *testing.T) {
	const origin = "https://app.example.com"
	s := mustNewServerWithOrigins(t, "tok", []string{origin})
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
}

func TestCORSMiddleware_HTTPNonLocalhostRejected(t *testing.T) {
	s := mustNewServer(t, "tok")
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for http:// non-localhost", got)
	}
}

// --- Host validation tests ---

func TestHostValidation_Localhost(t *testing.T) {
	hosts := []string{"localhost", "LOCALHOST:8080", "127.0.0.1", "127.0.0.1:9090", "[::1]", "[::1]:8080"}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			s := mustNewServer(t, "tok")
			rec := &callRecorder{}
			handler := s.hostValidation(rec.handler())

			req := httptest.NewRequest("GET", "/describe", nil)
			req.Host = host
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("host %q: status = %d, want %d", host, w.Code, http.StatusOK)
			}
			if !rec.called {
				t.Errorf("host %q: inner handler was not called", host)
			}
		})
	}
}

func TestHostValidation_Disallowed(t *testing.T) {
	hosts := []string{"evil.com", "evil.com:8080", "10.0.0.1", "0.0.0.0"}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			s := mustNewServer(t, "tok")
			rec := &callRecorder{}
			handler := s.hostValidation(rec.handler())

			req := httptest.NewRequest("GET", "/describe", nil)
			req.Host = host
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("host %q: status = %d, want %d", host, w.Code, http.StatusForbidden)
			}
			if rec.called {
				t.Errorf("host %q: inner handler should not have been called", host)
			}
		})
	}
}

// --- Request ID middleware test ---

func TestRequestIDMiddleware(t *testing.T) {
	s := mustNewServer(t, "tok")

	var ctxID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := s.requestIDMiddleware(inner)
	req := httptest.NewRequest("GET", "/describe", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	headerID := w.Header().Get("X-Request-Id")
	if headerID == "" {
		t.Fatal("X-Request-Id header is empty")
	}
	if len(headerID) != 16 {
		t.Errorf("X-Request-Id length = %d, want 16 hex chars", len(headerID))
	}
	if ctxID != headerID {
		t.Errorf("context ID = %q, header ID = %q; want equal", ctxID, headerID)
	}
}

// --- statusWriter tests ---

func TestStatusWriter_Flusher(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	if _, ok := interface{}(sw).(http.Flusher); !ok {
		t.Fatal("statusWriter does not implement http.Flusher")
	}

	sw.Flush()
}

func TestStatusWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	sw.WriteHeader(http.StatusNotFound)

	if sw.status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", sw.status, http.StatusNotFound)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("underlying recorder code = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStatusWriter_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	if got := sw.Unwrap(); got != rec {
		t.Error("Unwrap did not return the underlying ResponseWriter")
	}
}

// --- Server constructor tests ---

func TestNew_WithToken(t *testing.T) {
	srv, err := New(Config{
		Port:   0,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Token:  "my-fixed-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if srv.Token() != "my-fixed-token" {
		t.Errorf("Token() = %q, want %q", srv.Token(), "my-fixed-token")
	}
}

func TestNew_GeneratesToken(t *testing.T) {
	srv, err := New(Config{
		Port:   0,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	tok := srv.Token()
	if len(tok) != 64 {
		t.Errorf("generated token length = %d, want 64 hex chars", len(tok))
	}

	// Verify uniqueness by generating a second server.
	srv2, err := New(Config{
		Port:   0,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if srv.Token() == srv2.Token() {
		t.Error("two servers generated the same token")
	}
}

func TestNew_MuxNotNil(t *testing.T) {
	srv := mustNewServer(t, "tok")
	if srv.Mux() == nil {
		t.Error("Mux() returned nil")
	}
}

func TestLocalCertificateLifecycle(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	caCertPath := filepath.Join(dir, "ca.crt")
	caKeyPath := filepath.Join(dir, "ca.key")

	caCert, caKey, created, err := loadOrCreateCA(caCertPath, caKeyPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !caCert.IsCA {
		t.Fatalf("created = %v, IsCA = %v; want true, true", created, caCert.IsCA)
	}
	if !isConstrainedLocalCA(caCert) {
		t.Fatal("local CA is not constrained to localhost with zero intermediate depth")
	}
	if lifetime := caCert.NotAfter.Sub(caCert.NotBefore); lifetime > 366*24*time.Hour {
		t.Fatalf("local CA lifetime = %v, want at most 366 days", lifetime)
	}
	if err := caCert.CheckSignatureFrom(caCert); err != nil {
		t.Fatalf("CA is not self-signed: %v", err)
	}
	assertPrivateFile(t, caKeyPath)

	reloadedCert, _, created, err := loadOrCreateCA(caCertPath, caKeyPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	if created || !bytes.Equal(reloadedCert.Raw, caCert.Raw) {
		t.Fatalf("valid CA was not reused (created = %v)", created)
	}

	leafCertPath := filepath.Join(dir, "localhost.crt")
	leafKeyPath := filepath.Join(dir, "localhost.key")
	leafPair, err := loadOrMintLeaf(leafCertPath, leafKeyPath, caCert, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf := parseLeaf(t, leafPair)
	verifyLocalLeaf(t, leaf, caCert)
	assertPrivateFile(t, leafKeyPath)

	reusedPair, err := loadOrMintLeaf(leafCertPath, leafKeyPath, caCert, caKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parseLeaf(t, reusedPair).Raw, leaf.Raw) {
		t.Fatal("valid leaf certificate was not reused")
	}

	// Rotating the CA must rotate the leaf too; otherwise the server presents a
	// certificate its newly installed CA cannot validate.
	otherCert, otherKey, _, err := loadOrCreateCA(filepath.Join(dir, "other.crt"), filepath.Join(dir, "other.key"), logger)
	if err != nil {
		t.Fatal(err)
	}
	rotatedPair, err := loadOrMintLeaf(leafCertPath, leafKeyPath, otherCert, otherKey)
	if err != nil {
		t.Fatal(err)
	}
	rotatedLeaf := parseLeaf(t, rotatedPair)
	if bytes.Equal(rotatedLeaf.Raw, leaf.Raw) {
		t.Fatal("leaf certificate was reused after CA rotation")
	}
	verifyLocalLeaf(t, rotatedLeaf, otherCert)
}

func TestLocalTLSWithoutTrustDoesNotInspectOrInstallSystemCA(t *testing.T) {
	// The trust-store decision is deliberately visible at the API boundary:
	// without explicit authorization, a freshly-created local CA is usable by
	// the TLS listener without calling caIsSystemTrusted, purgeStaleCAs, or
	// installCASystemWide. Running under a temporary home also proves all
	// generated material stays in user-controlled test storage.
	t.Setenv("HOME", t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg, err := ensureLocalhostTLS(logger, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("TLS certificates = %d, want 1", len(cfg.Certificates))
	}
}

func TestCAIsSystemTrustedFor(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cert, _, _, err := loadOrCreateCA(filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key"), logger)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(dir, "installed.crt")
	if err := os.WriteFile(installed, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0644); err != nil {
		t.Fatal(err)
	}

	if !caIsSystemTrustedFor("linux", cert, logger, installed, func(string, ...string) error { return nil }) {
		t.Fatal("matching Linux trust-store certificate was not recognized")
	}
	other, _, _, err := loadOrCreateCA(filepath.Join(dir, "other.crt"), filepath.Join(dir, "other.key"), logger)
	if err != nil {
		t.Fatal(err)
	}
	if caIsSystemTrustedFor("linux", other, logger, installed, func(string, ...string) error { return nil }) {
		t.Fatal("stale Linux trust-store certificate was accepted")
	}

	var command string
	var args []string
	runner := func(name string, got ...string) error {
		command, args = name, append([]string(nil), got...)
		return nil
	}
	if !caIsSystemTrustedFor("windows", cert, logger, "", runner) {
		t.Fatal("successful Windows trust lookup was rejected")
	}
	wantThumbprint := fmt.Sprintf("%X", sha1.Sum(cert.Raw)) //nolint:gosec // certificate-store identifier
	if command != "certutil" || !reflect.DeepEqual(args, []string{"-verifystore", "Root", wantThumbprint}) {
		t.Fatalf("Windows trust command = %q %q", command, args)
	}
	if caIsSystemTrustedFor("windows", cert, logger, "", func(string, ...string) error { return errors.New("not found") }) {
		t.Fatal("failed Windows trust lookup was accepted")
	}
	if caIsSystemTrustedFor("plan9", cert, logger, "", runner) || caIsSystemTrustedFor("linux", nil, logger, installed, runner) {
		t.Fatal("unsupported platform or nil certificate was accepted")
	}
}

func parseLeaf(t *testing.T, pair tls.Certificate) *x509.Certificate {
	t.Helper()
	if pair.Leaf != nil {
		return pair.Leaf
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func verifyLocalLeaf(t *testing.T, leaf, root *x509.Certificate) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("verify localhost leaf: %v", err)
	}
	for _, host := range []string{"127.0.0.1", "::1"} {
		if err := leaf.VerifyHostname(host); err != nil {
			t.Fatalf("verify leaf hostname %s: %v", host, err)
		}
	}
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("%s permissions = %04o, want 0600", path, got)
	}
}

func TestListenAndServe_SetsTimeouts(t *testing.T) {
	srv := mustNewServer(t, "tok")
	srv.config.Port = 0
	readyCh := make(chan ReadyInfo, 1)
	srv.config.OnReady = func(ready ReadyInfo) {
		readyCh <- ready
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	// Wait for the server to publish its underlying http.Server. Polling is
	// safer than a fixed sleep on slow machines and avoids racing on the field.
	var httpSrv *http.Server
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if httpSrv = srv.HTTPServer(); httpSrv != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if httpSrv == nil {
		t.Fatal("httpSrv not created within 2s")
	}
	if httpSrv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", httpSrv.ReadHeaderTimeout)
	}
	if httpSrv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", httpSrv.IdleTimeout)
	}
	select {
	case ready := <-readyCh:
		if !strings.HasPrefix(ready.HTTPURL, "http://127.0.0.1:") {
			t.Errorf("HTTPURL = %q, want bound loopback URL", ready.HTTPURL)
		}
		if ready.HTTPSURL != "" {
			t.Errorf("HTTPSURL = %q without TLS, want empty", ready.HTTPSURL)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnReady was not called within 2s")
	}

	cancel()
	<-errCh
}

// TestListenAndServe_ReportsPortFallback pins the ReadyInfo contract for the
// busy-port case: when the requested port cannot be bound and the server falls
// back to a nearby free port, ReadyInfo must carry both the requested and the
// actually bound port so interactive hosts can announce the substitution.
func TestListenAndServe_ReportsPortFallback(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	requested := busy.Addr().(*net.TCPAddr).Port

	srv := mustNewServer(t, "tok")
	srv.config.Port = requested
	readyCh := make(chan ReadyInfo, 1)
	srv.config.OnReady = func(ready ReadyInfo) { readyCh <- ready }

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	select {
	case ready := <-readyCh:
		if ready.RequestedPort != requested {
			t.Errorf("RequestedPort = %d, want %d", ready.RequestedPort, requested)
		}
		if ready.HTTPPort == 0 || ready.HTTPPort == requested {
			t.Errorf("HTTPPort = %d, want a fallback port different from busy %d", ready.HTTPPort, requested)
		}
		if want := fmt.Sprintf("http://127.0.0.1:%d", ready.HTTPPort); ready.HTTPURL != want {
			t.Errorf("HTTPURL = %q, want %q (the actually bound port)", ready.HTTPURL, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnReady was not called within 2s")
	}

	cancel()
	<-errCh
}

// TestListenAndServe_ReadyInfoCarriesActualPort covers the non-fallback case:
// requested and bound ports agree, so hosts print no substitution notice.
func TestListenAndServe_ReadyInfoCarriesActualPort(t *testing.T) {
	// Reserve a free port, release it, then ask the server for it. (Not
	// entirely race-free, but loopback ports freed this instant are not
	// reused by other tests in this package.)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	requested := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	srv := mustNewServer(t, "tok")
	srv.config.Port = requested
	readyCh := make(chan ReadyInfo, 1)
	srv.config.OnReady = func(ready ReadyInfo) { readyCh <- ready }

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	select {
	case ready := <-readyCh:
		if ready.RequestedPort != requested || ready.HTTPPort != requested {
			t.Errorf("ports = requested %d bound %d, want both %d", ready.RequestedPort, ready.HTTPPort, requested)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnReady was not called within 2s")
	}

	cancel()
	<-errCh
}

// TestListenAndServe_StrictPortRefusesFallback pins strict-port semantics: a
// busy requested port is a hard error — no adjacent ports are probed and no
// listener starts.
func TestListenAndServe_StrictPortRefusesFallback(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	requested := busy.Addr().(*net.TCPAddr).Port

	srv := mustNewServer(t, "tok")
	srv.config.Port = requested
	srv.config.StrictPort = true
	srv.config.OnReady = func(ReadyInfo) {
		t.Error("OnReady must not fire when strict-port binding fails")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = srv.ListenAndServe(ctx)
	if err == nil {
		t.Fatal("expected a hard error for a busy port under strict-port")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", requested)) {
		t.Errorf("error should name the busy port %d: %v", requested, err)
	}
}

// --- The cookie exchange (rev 17.19) ---

func TestAuthMiddleware_BearerIssuesSessionCookie(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == SessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatalf("bearer auth must issue the session cookie; got %v", cookies)
	}
	if session.Value != "test-token-123" {
		t.Errorf("cookie value = %q, want the run token", session.Value)
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" {
		t.Errorf("cookie attributes wrong: HttpOnly=%v SameSite=%v Path=%q",
			session.HttpOnly, session.SameSite, session.Path)
	}
}

func TestAuthMiddleware_CookieAuthenticates(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "test-token-123"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !rec.called {
		t.Errorf("cookie auth: status = %d, called = %v; want 200, true", w.Code, rec.called)
	}
	// Cookie-only auth does not re-issue the cookie (nothing to exchange).
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookieName {
			t.Errorf("cookie-authed request should not re-set the cookie")
		}
	}
}

func TestAuthMiddleware_StaleCookieRejected(t *testing.T) {
	// A cookie from a previous run (rotated token) is not a credential.
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/describe", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "previous-run-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized || rec.called {
		t.Errorf("stale cookie: status = %d, called = %v; want 401, false", w.Code, rec.called)
	}
}

func TestSessionTokenRoute_RedeemsCookieForToken(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	s.RegisterSessionRoutes()

	req := httptest.NewRequest("GET", "/session/token", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "test-token-123"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store (the body is a secret)", cc)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["token"] != "test-token-123" {
		t.Errorf("token = %q", body["token"])
	}

	// Without a cookie the window is EMPTY, not an error: probing for a
	// cookie is the normal first-run case, and 204 keeps consoles quiet.
	anon := httptest.NewRequest("GET", "/session/token", nil)
	anon.Host = "localhost"
	anonW := httptest.NewRecorder()
	s.Handler().ServeHTTP(anonW, anon)
	if anonW.Code != http.StatusNoContent {
		t.Errorf("anonymous redemption: status = %d, want 204", anonW.Code)
	}
	if anonW.Body.Len() != 0 {
		t.Errorf("anonymous redemption leaked a body: %q", anonW.Body.String())
	}

	// A stale cookie (rotated token) is the same empty window.
	stale := httptest.NewRequest("GET", "/session/token", nil)
	stale.Host = "localhost"
	stale.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "previous-run-token"})
	staleW := httptest.NewRecorder()
	s.Handler().ServeHTTP(staleW, stale)
	if staleW.Code != http.StatusNoContent {
		t.Errorf("stale redemption: status = %d, want 204", staleW.Code)
	}
}
