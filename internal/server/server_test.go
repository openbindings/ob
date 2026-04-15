package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

	req := httptest.NewRequest("GET", "/info", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !rec.called {
		t.Error("inner handler was not called")
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	s := mustNewServer(t, "test-token-123")
	rec := &callRecorder{}
	handler := s.authMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/info", nil)
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

	req := httptest.NewRequest("GET", "/info", nil)
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

// --- CORS middleware tests ---

func TestCORSMiddleware_AllowedOrigin(t *testing.T) {
	s := mustNewServerWithOrigins(t, "tok", []string{"http://localhost:3000"})
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/info", nil)
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

	req := httptest.NewRequest("GET", "/info", nil)
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

	req := httptest.NewRequest("OPTIONS", "/info", nil)
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

	req := httptest.NewRequest("OPTIONS", "/info", nil)
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
	}
	for _, origin := range origins {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/info", nil)
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
			req := httptest.NewRequest("GET", "/info", nil)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("Access-Control-Allow-Origin = %q, want empty for %q", got, origin)
			}
		})
	}
}

func TestCORSMiddleware_AnyHTTPSOriginAllowed(t *testing.T) {
	s := mustNewServer(t, "tok")
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	for _, origin := range []string{"https://example.com", "https://panjir.com", "https://anything.dev"} {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/info", nil)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
			}
		})
	}
}

func TestCORSMiddleware_HTTPNonLocalhostRejected(t *testing.T) {
	s := mustNewServer(t, "tok")
	rec := &callRecorder{}
	handler := s.corsMiddleware(rec.handler())

	req := httptest.NewRequest("GET", "/info", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for http:// non-localhost", got)
	}
}

// --- Host validation tests ---

func TestHostValidation_Localhost(t *testing.T) {
	hosts := []string{"localhost", "localhost:8080", "127.0.0.1", "127.0.0.1:9090", "[::1]:8080"}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			s := mustNewServer(t, "tok")
			rec := &callRecorder{}
			handler := s.hostValidation(rec.handler())

			req := httptest.NewRequest("GET", "/info", nil)
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

			req := httptest.NewRequest("GET", "/info", nil)
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
	req := httptest.NewRequest("GET", "/info", nil)
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

func TestListenAndServe_SetsTimeouts(t *testing.T) {
	srv := mustNewServer(t, "tok")
	srv.config.Port = 0

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

	cancel()
	<-errCh
}
