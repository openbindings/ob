package server

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Windows certificate-store identifier, not a security decision.
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openbindings/ob/internal/servecontract"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// wsInvokePaths are the routes whose handlers perform their own
// upgrade-request authentication (Authorization header or a bearer WebSocket
// subprotocol for browsers, which cannot set headers on upgrades) and therefore
// must receive upgrade requests that carry no Authorization header. The
// auth-middleware exemption is scoped to exactly this path. It must match the
// route registered on the mux (see internal/cmd/serve_routes.go).
var wsInvokePaths = map[string]struct{}{
	"/bindings/invoke":   {},
	"/operations/invoke": {},
}

// RequestIDFromContext extracts the request ID set by the request ID middleware.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// Config holds the configuration for an ob start instance.
type Config struct {
	Port           int
	AllowedOrigins []string
	Logger         *slog.Logger
	Token          string
	TLS            bool
	// OnReady is called after all requested listeners have been bound and the
	// serving goroutines have started accepting requests. Interactive
	// hosts can therefore present the actual URLs when a requested port was
	// unavailable without making Server responsible for terminal UI.
	OnReady func(ReadyInfo)
	// TrustLocalCA explicitly authorizes ob to install its locally-generated
	// HTTPS CA into the platform trust store. It has no effect unless TLS is
	// enabled. Keeping this separate from TLS prevents serving HTTPS from
	// implicitly becoming a privileged system mutation.
	TrustLocalCA bool
	// StrictPort makes a busy requested HTTP port a hard error instead of
	// falling back to a nearby free port. It applies to the primary HTTP
	// listener only; the derived HTTPS listener keeps its best-effort probing.
	StrictPort bool
}

// ReadyInfo identifies the listeners an ob start server successfully bound.
// HTTPURL is always present. HTTPSURL is present only when TLS setup and
// listener binding both succeeded.
type ReadyInfo struct {
	HTTPURL  string
	HTTPSURL string
	// RequestedPort is the port the caller asked for (Config.Port); HTTPPort
	// is the port the HTTP listener actually bound. They differ when the
	// requested port was busy and the server fell back to a nearby free port,
	// which hosts must announce to the user.
	RequestedPort int
	HTTPPort      int
}

// Server is the ob start HTTP server.
type Server struct {
	token              string
	validateOAuthToken func(string) bool
	mux                *http.ServeMux
	config             Config
	logger             *slog.Logger
	// httpSrv is published atomically so callers (and tests) can read its
	// configuration once ListenAndServe has constructed it without racing
	// against the goroutine that owns the underlying http.Server.
	httpSrv atomic.Pointer[http.Server]
}

// New creates a new Server. If cfg.Token is set, it is used as the session
// token; otherwise a cryptographically random token is generated.
func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	token := cfg.Token
	if token == "" {
		var err error
		token, err = generateToken()
		if err != nil {
			return nil, fmt.Errorf("generating session token: %w", err)
		}
	}

	s := &Server{
		token:  token,
		mux:    http.NewServeMux(),
		config: cfg,
		logger: cfg.Logger,
	}
	return s, nil
}

// Token returns the session token clients must present.
func (s *Server) Token() string {
	return s.token
}

// SetOAuthValidator sets the callback used to validate OAuth2 access tokens.
// This keeps Server decoupled from oauthStore internals; the store owns TTL logic.
func (s *Server) SetOAuthValidator(fn func(string) bool) {
	s.validateOAuthToken = fn
}

// IsValidToken checks the static session token and, if set, the OAuth validator.
// The static session token comparison is constant-time to avoid leaking byte
// matches via response timing.
func (s *Server) IsValidToken(token string) bool {
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1 {
		return true
	}
	if s.validateOAuthToken != nil {
		return s.validateOAuthToken(token)
	}
	return false
}

// Mux returns the underlying ServeMux for registering routes.
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// RegisterSessionRoutes wires the auth-transport routes (the cookie
// exchange's token redemption). These are serve-transport mechanics like
// /healthz, not operations — the published command surface is unchanged.
func (s *Server) RegisterSessionRoutes() {
	s.mux.HandleFunc("GET /session/token", s.handleSessionToken)
}

// Handler returns the full middleware-wrapped handler chain.
// Useful for testing routes with auth, CORS, and logging applied.
func (s *Server) Handler() http.Handler {
	return s.buildMiddlewareChain(s.mux)
}

// ListenAndServe binds HTTP (and optionally HTTPS) to localhost and
// serves until the context is cancelled.
//
// HTTP is the zero-configuration default. When TLS is explicitly enabled, ob
// adds HTTPS on `config.Port + 1` using a locally-generated CA. Generating and
// serving with that CA does not modify platform trust. Installation into the
// system trust store occurs only when TrustLocalCA is also explicitly enabled.
// If TLS setup fails, HTTP still serves so the user is never fully blocked.
func (s *Server) ListenAndServe(ctx context.Context) error {
	handler := s.buildMiddlewareChain(s.mux)

	attempts := 10
	if s.config.StrictPort {
		attempts = 1
	}
	httpListener, httpPort, err := bindLocalhostPort(s.config.Port, attempts)
	if err != nil {
		if s.config.StrictPort {
			return fmt.Errorf("port %d is unavailable and strict-port is set (no fallback): %w", s.config.Port, err)
		}
		return fmt.Errorf("HTTP bind failed (tried %d ports starting at %d): %w", attempts, s.config.Port, err)
	}

	httpSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.httpSrv.Store(httpSrv)

	var httpsSrv *http.Server
	var httpsListener net.Listener
	httpsPort := 0

	if s.config.TLS {
		tlsCfg, tlsErr := ensureLocalhostTLS(s.logger, s.config.TrustLocalCA)
		if tlsErr != nil {
			s.logger.Warn("HTTPS disabled — HTTP remains available",
				"reason", tlsErr,
				"fix", "resolve the certificate error or continue over loopback HTTP",
			)
		} else {
			rawHTTPS, p, bindErr := bindLocalhostPort(httpPort+1, 10)
			if bindErr != nil {
				s.logger.Warn("HTTPS port bind failed — HTTP remains available", "error", bindErr)
			} else {
				httpsListener = tls.NewListener(rawHTTPS, tlsCfg)
				httpsPort = p
				httpsSrv = &http.Server{
					Handler:           handler,
					TLSConfig:         tlsCfg,
					ReadHeaderTimeout: 10 * time.Second,
					IdleTimeout:       120 * time.Second,
				}
			}
			if !s.config.TrustLocalCA {
				if dir, pathErr := obTLSDir(); pathErr == nil {
					s.logger.Warn(
						"HTTPS local CA was not installed into system trust",
						"ca", filepath.Join(dir, "ob-ca.crt"),
						"fix", "trust the certificate manually or restart with --trust-local-ca after reviewing the risk",
					)
				}
			}
		}
	}

	s.logger.Info("server starting", "http", fmt.Sprintf("http://localhost:%d", httpPort))
	if httpsSrv != nil {
		s.logger.Info("server starting", "https", fmt.Sprintf("https://localhost:%d", httpsPort))
	}
	if len(s.config.AllowedOrigins) > 0 {
		s.logger.Info("CORS configured", "origins", s.config.AllowedOrigins)
	} else {
		s.logger.Info("CORS allowing localhost origins (use --allow-origin to add others)")
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- httpSrv.Serve(httpListener)
	}()
	if httpsSrv != nil {
		go func() {
			errCh <- httpsSrv.Serve(httpsListener)
		}()
	}
	if s.config.OnReady != nil {
		ready := ReadyInfo{
			HTTPURL:       fmt.Sprintf("http://127.0.0.1:%d", httpPort),
			RequestedPort: s.config.Port,
			HTTPPort:      httpPort,
		}
		if httpsSrv != nil {
			ready.HTTPSURL = fmt.Sprintf("https://127.0.0.1:%d", httpsPort)
		}
		s.config.OnReady(ready)
	}

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.logger.Info("shutting down")
		shutErr := httpSrv.Shutdown(shutCtx)
		if httpsSrv != nil {
			if e := httpsSrv.Shutdown(shutCtx); e != nil && shutErr == nil {
				shutErr = e
			}
		}
		return shutErr
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// bindLocalhostPort tries up to `attempts` consecutive ports starting at
// `startPort`. Returns the listener, the port it actually bound (read back
// from the listener, so a startPort of 0 reports the kernel-assigned port),
// or an error.
func bindLocalhostPort(startPort, attempts int) (net.Listener, int, error) {
	port := startPort
	var lastErr error
	for i := 0; i < attempts; i++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			if addr, ok := l.Addr().(*net.TCPAddr); ok {
				return l, addr.Port, nil
			}
			return l, port, nil
		}
		lastErr = err
		port++
	}
	return nil, 0, lastErr
}

// HTTPServer returns the underlying *http.Server once ListenAndServe has been
// called, or nil if the server is not yet listening. Safe for concurrent use.
func (s *Server) HTTPServer() *http.Server {
	return s.httpSrv.Load()
}

func (s *Server) buildMiddlewareChain(h http.Handler) http.Handler {
	h = s.authMiddleware(h)
	h = s.corsMiddleware(h)
	h = s.hostValidation(h)
	h = s.requestIDMiddleware(h)
	h = securityHeaders(h)
	h = s.loggingMiddleware(h)
	return h
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateRequestID()
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hostValidation rejects requests with non-localhost Host headers (DNS rebinding defense).
func (s *Server) hostValidation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := hostWithoutPort(r.Host)
		if IsLoopbackHost(host) {
			next.ServeHTTP(w, r)
		} else {
			writeServerError(w, http.StatusForbidden, "invalid_host", "non-localhost Host header is forbidden")
		}
	})
}

func hostWithoutPort(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
	}
	return hostport
}

// corsMiddleware handles CORS preflight and sets headers for allowed origins.
//
// ob start binds to localhost only and requires a session token on protected
// requests. CORS remains an independent defense-in-depth boundary: loopback
// origins are allowed by default, while every remote origin requires an exact
// --allow-origin entry. Possession of a token does not grant an arbitrary
// website permission to drive a local process-capable API.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
		}
		if origin != "" && s.AllowsOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "3600")

			// Private Network Access (PNA). Chrome sends this preflight header
			// when a public-ish origin (HTTP pages that Chrome doesn't treat
			// as loopback, HTTPS pages) tries to reach a private IP. Without
			// the matching response header, the fetch is blocked with a
			// confusing CSP-shaped error. Opt-in because ob binds to
			// loopback only and the session token is the real auth boundary.
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				w.Header().Set("Access-Control-Allow-Private-Network", "true")
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AllowsOrigin applies the shared browser-origin policy used by both CORS and
// WebSocket upgrades. Non-browser clients normally send no Origin header.
func (s *Server) AllowsOrigin(origin string) bool {
	for _, allowed := range s.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return isLocalhostOrigin(origin)
}

// IsLoopbackHost returns true if the hostname (without port or scheme) is a
// loopback address: localhost, 127.0.0.1, [::1], or ::1. Used for host
// validation, CORS, OAuth redirect URI checks, and WebSocket origin checks.
func IsLoopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "[::1]" || host == "::1"
}

// IsWebSocketUpgrade reports whether r is a genuine WebSocket upgrade request:
// it must carry "upgrade" as a token in the (possibly comma-separated, possibly
// repeated) Connection header AND an Upgrade header of "websocket", both matched
// case-insensitively per RFC 6455 / RFC 7230. A lone spoofed Upgrade header is
// not sufficient, so this cannot be used to slip past auth on its own.
func IsWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, v := range r.Header.Values("Connection") {
		for _, token := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

// isLocalhostOrigin returns true for origins like http(s)://localhost:PORT or
// http(s)://127.0.0.1:PORT. Used for the default CORS policy.
func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return false
	}
	return IsLoopbackHost(u.Hostname())
}

// SessionCookieName carries the run token as an HttpOnly SameSite=Strict
// cookie so a browser authenticated once (via the URL-fragment handoff)
// stays authenticated for the lifetime of the server run — new tabs, browser
// relaunches, history clicks — without the fragment. The cookie's value IS
// the run token, so a restart that rotates the token invalidates every
// outstanding cookie by construction; Max-Age only bounds how long dead
// cookies linger. HttpOnly keeps scripts from reading it; SameSite=Strict
// keeps other sites from sending it; the domain scoping keeps a
// DNS-rebound host from ever receiving it.
const SessionCookieName = "ob_start_session"

// sessionCookieMaxAge bounds stale-cookie lingering, not authority: a
// rotated token invalidates the cookie regardless of age.
const sessionCookieMaxAge = 7 * 24 * time.Hour

// authMiddleware requires a valid credential on all requests except public
// endpoints (the workbench root and assets, well-known, healthz, the served
// openapi/asyncapi specs, and the OAuth authorize/token endpoints). The
// credential is the Bearer token, or equivalently the session cookie the
// server itself issued (the cookie exchange, rev 17.19): a successful
// bearer request re-issues the cookie so the browser's authority tracks the
// run without the page ever re-handling the fragment.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if path == "/" || strings.HasPrefix(path, "/assets/") ||
			path == "/.well-known/openbindings" || path == "/healthz" ||
			path == "" ||
			path == "/openapi.yaml" || path == "/asyncapi.yaml" {
			next.ServeHTTP(w, r)
			return
		}
		if path == "/oauth/authorize" || path == "/oauth/token" {
			next.ServeHTTP(w, r)
			return
		}
		if path == "/session/token" {
			// The redemption window authenticates itself (cookie-only) and
			// answers 204 when there is nothing to redeem: an unauthed visit
			// probing for a cookie is the NORMAL first-run case, not an
			// error, and a 401 here would put noise in every such console.
			next.ServeHTTP(w, r)
			return
		}

		// The WebSocket invocation endpoint authenticates the upgrade request
		// itself, accepting the token from either the Authorization header or
		// a bearer WebSocket subprotocol (browsers can't set headers on
		// upgrade requests). Let genuine upgrade requests to those routes through to
		// the handler, which authenticates before accepting the upgrade. This
		// exemption is scoped to the exact route AND requires a real WebSocket
		// upgrade (Connection: upgrade + Upgrade: websocket), so a spoofed
		// Upgrade header on any other route — or on a non-upgrade request to
		// this route — still hits token auth.
		if _, ok := wsInvokePaths[path]; ok && IsWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}

		if token, ok := bearerToken(r.Header.Get("Authorization")); ok && s.IsValidToken(token) {
			// The exchange: a bearer-authenticated request proves the page
			// holds the fragment handoff, so the browser earns the durable
			// form of the same authority.
			s.setSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}

		if cookie, err := r.Cookie(SessionCookieName); err == nil && s.IsValidToken(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}

		s.logger.Warn("auth failure",
			"method", r.Method,
			"path", path,
			"remote_addr", r.RemoteAddr,
			"request_id", RequestIDFromContext(r.Context()),
		)
		writeServerError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value == s.token {
		return // Already current; re-setting every request is noise.
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    s.token,
		Path:     "/",
		MaxAge:   int(sessionCookieMaxAge / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})
}

// handleSessionToken redeems the session cookie for the run token so the
// page can present it as a bearer everywhere the existing plumbing expects
// one (the invocation frames carry it as context for ob's self-referential
// bindings). The route is middleware-exempt and authenticates itself,
// cookie-only: a valid cookie answers with the very secret it encodes, and
// anything else answers 204 — no cookie is the normal first-run state, not
// an error, so it earns no error status and no console noise.
func (s *Server) handleSessionToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || !s.IsValidToken(cookie.Value) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": s.token})
}

func bearerToken(authorization string) (string, bool) {
	parts := strings.Fields(authorization)
	returnToken := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		returnToken = parts[1]
	}
	return returnToken, returnToken != ""
}

func writeServerError(w http.ResponseWriter, status int, code servecontract.ErrorCode, message string) {
	servecontract.WriteError(w, status, code, message)
}

// loggingMiddleware logs every request with method, path, status, duration, and request ID.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFromContext(r.Context()),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support hijacking")
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ensureLocalhostTLS returns a tls.Config for localhost using a local CA.
// Trust-store installation is a distinct, explicit decision:
//
//  1. Load or create the CA at ~/.ob/tls/ob-ca.crt.
//  2. When trustLocalCA is true, verify the CA is trusted by the system
//     (macOS: present in the
//     System keychain; Linux: in /etc/ssl/certs; Windows: Root store).
//     If not — or if it's only in the user-login keychain from an earlier
//     broken install — purge stale copies and re-install system-wide.
//  3. Load or mint a leaf cert signed by the CA with 90-day validity.
//
// With trustLocalCA false this function never reads from or writes to the
// system trust store and never executes a privilege-elevation command.
func ensureLocalhostTLS(logger *slog.Logger, trustLocalCA bool) (*tls.Config, error) {
	dir, err := obTLSDir()
	if err != nil {
		return nil, err
	}

	caKeyPath := filepath.Join(dir, "ob-ca.key")
	caCertPath := filepath.Join(dir, "ob-ca.crt")
	leafCertPath := filepath.Join(dir, "localhost.crt")
	leafKeyPath := filepath.Join(dir, "localhost.key")

	caCert, caKey, created, err := loadOrCreateCA(caCertPath, caKeyPath, logger)
	if err != nil {
		return nil, fmt.Errorf("CA setup: %w", err)
	}

	// Trust-store inspection and mutation are both forbidden unless the caller
	// explicitly authorized them. `created` is relevant only on that path.
	needInstall := trustLocalCA && (created || !caIsSystemTrusted(caCert, logger))
	if trustLocalCA && needInstall {
		if err := purgeStaleCAs(logger); err != nil {
			logger.Warn("could not purge stale CA entries", "error", err)
		}
		if err := installCASystemWide(caCertPath, logger); err != nil {
			return nil, fmt.Errorf("installing CA: %w", err)
		}
	}

	leafCert, err := loadOrMintLeaf(leafCertPath, leafKeyPath, caCert, caKey)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{leafCert},
	}, nil
}

// loadOrCreateCA returns the CA cert+key. Reports `created=true` if a
// new CA was generated on disk (caller uses this to trigger install).
func loadOrCreateCA(certPath, keyPath string, logger *slog.Logger) (*x509.Certificate, *ecdsa.PrivateKey, bool, error) {
	if certPEM, err := os.ReadFile(certPath); err == nil {
		if keyPEM, err := os.ReadFile(keyPath); err == nil {
			block, _ := pem.Decode(certPEM)
			if block != nil {
				caCert, err := x509.ParseCertificate(block.Bytes)
				now := time.Now()
				if err == nil && isConstrainedLocalCA(caCert) && now.After(caCert.NotBefore) && now.Before(caCert.NotAfter) && caCert.CheckSignatureFrom(caCert) == nil {
					keyBlock, _ := pem.Decode(keyPEM)
					if keyBlock != nil {
						caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
						if err == nil && caKey.PublicKey.Equal(caCert.PublicKey) {
							if err := os.Chmod(keyPath, 0600); err != nil {
								return nil, nil, false, fmt.Errorf("securing CA key permissions: %w", err)
							}
							return caCert, caKey, false, nil
						}
					}
				}
			}
		}
	}

	logger.Info("creating local CA for HTTPS (one-time setup)")

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, false, fmt.Errorf("generating CA key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, false, fmt.Errorf("generating CA serial: %w", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:                serial,
		NotBefore:                   time.Now().Add(-5 * time.Minute),
		NotAfter:                    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid:       true,
		IsCA:                        true,
		MaxPathLen:                  0,
		MaxPathLenZero:              true,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"localhost"},
		PermittedIPRanges: []*net.IPNet{
			{IP: net.IP{127, 0, 0, 0}, Mask: net.CIDRMask(8, 32)},
			{IP: net.IPv6loopback, Mask: net.CIDRMask(128, 128)},
		},
		Subject: pkix.Name{
			Organization: []string{"OpenBindings"},
			CommonName:   "OpenBindings Local CA",
		},
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, false, fmt.Errorf("creating CA cert: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, false, fmt.Errorf("parsing CA cert: %w", err)
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	if err := os.WriteFile(certPath, caCertPEM, 0644); err != nil {
		return nil, nil, false, fmt.Errorf("writing CA cert: %w", err)
	}
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, nil, false, fmt.Errorf("marshaling CA key: %w", err)
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})
	if err := os.WriteFile(keyPath, caKeyPEM, 0600); err != nil {
		return nil, nil, false, fmt.Errorf("writing CA key: %w", err)
	}

	return caCert, caKey, true, nil
}

// isConstrainedLocalCA prevents a previously generated broad root from being
// silently reused. The key is useful only for localhost leaf certificates,
// cannot sign an intermediate CA, and expires after the short local-tool
// lifecycle above. An older unconstrained CA is rotated on the next TLS start;
// explicit --trust-local-ca also purges its stale trust-store copy.
func isConstrainedLocalCA(cert *x509.Certificate) bool {
	if cert == nil || !cert.IsCA || !cert.BasicConstraintsValid ||
		cert.MaxPathLen != 0 || !cert.MaxPathLenZero ||
		!cert.PermittedDNSDomainsCritical ||
		len(cert.PermittedDNSDomains) != 1 ||
		cert.PermittedDNSDomains[0] != "localhost" ||
		len(cert.PermittedIPRanges) != 2 {
		return false
	}
	return cert.PermittedIPRanges[0].String() == "127.0.0.0/8" &&
		cert.PermittedIPRanges[1].String() == "::1/128"
}

func loadOrMintLeaf(certPath, keyPath string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (tls.Certificate, error) {
	if existing, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		leaf, parseErr := x509.ParseCertificate(existing.Certificate[0])
		if parseErr == nil {
			roots := x509.NewCertPool()
			roots.AddCert(caCert)
			_, verifyErr := leaf.Verify(x509.VerifyOptions{
				DNSName:   "localhost",
				Roots:     roots,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			})
			if verifyErr == nil && time.Now().Before(leaf.NotAfter.Add(-24*time.Hour)) {
				if err := os.Chmod(keyPath, 0600); err != nil {
					return tls.Certificate{}, fmt.Errorf("securing leaf key permissions: %w", err)
				}
				existing.Leaf = leaf
				return existing, nil
			}
		}
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating leaf key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generating serial: %w", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("signing leaf cert: %w", err)
	}
	leafCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	if err := os.WriteFile(certPath, leafCertPEM, 0644); err != nil {
		return tls.Certificate{}, fmt.Errorf("writing leaf cert: %w", err)
	}
	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshaling leaf key: %w", err)
	}
	leafKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER})
	if err := os.WriteFile(keyPath, leafKeyPEM, 0600); err != nil {
		return tls.Certificate{}, fmt.Errorf("writing leaf key: %w", err)
	}
	return tls.X509KeyPair(leafCertPEM, leafKeyPEM)
}

// caIsSystemTrusted checks whether the given CA cert is trusted by the
// platform's system trust store. On every ob startup this answers:
// "would a fresh browser process trust a leaf signed by this CA?" If
// false, we re-run install (covers previous broken installs that landed
// in the user login keychain without proper trust settings).
func caIsSystemTrusted(caCert *x509.Certificate, logger *slog.Logger) bool {
	return caIsSystemTrustedFor(runtime.GOOS, caCert, logger,
		"/usr/local/share/ca-certificates/ob-local-ca.crt",
		func(name string, args ...string) error { return exec.Command(name, args...).Run() })
}

// caIsSystemTrustedFor contains the platform decision with its filesystem path
// and command execution injected, making every platform branch testable on
// every development host.
func caIsSystemTrustedFor(goos string, caCert *x509.Certificate, logger *slog.Logger, linuxCertPath string, run func(string, ...string) error) bool {
	if caCert == nil {
		return false
	}
	switch goos {
	case "darwin":
		// `security verify-cert` with the basic policy checks chain validity
		// against the trust store. A self-signed CA that's been added to the
		// System keychain as a root returns 0. A CA that's only in the user
		// login keychain (the buggy previous install) returns non-zero here,
		// which is what drives auto-reinstall. The `ssl` policy would also
		// enforce serverAuth EKU, which CA certs don't carry — wrong tool.
		tmp, err := writeTempPEM(caCert)
		if err != nil {
			return false
		}
		defer os.Remove(tmp)
		if err := run("security", "verify-cert", "-c", tmp, "-p", "basic"); err != nil {
			logger.Debug("system CA trust check failed, will reinstall", "error", err)
			return false
		}
		return true
	case "linux":
		// Verify the installed file is this CA, not merely a stale file under
		// the expected name.
		data, err := os.ReadFile(linuxCertPath)
		if err != nil {
			return false
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return false
		}
		installed, err := x509.ParseCertificate(block.Bytes)
		return err == nil && installed.Equal(caCert)
	case "windows":
		// certutil accepts a SHA-1 certificate thumbprint as CertId. SHA-1 is
		// used only to locate the exact certificate in the Root store; trust is
		// decided by store membership, not collision resistance here.
		thumbprint := fmt.Sprintf("%X", sha1.Sum(caCert.Raw))
		return run("certutil", "-verifystore", "Root", thumbprint) == nil
	default:
		return false
	}
}

// purgeStaleCAs removes any existing "OpenBindings Local CA" entries from
// both the system and user keychains before a fresh install. Prior broken
// installs can leave orphaned entries (e.g., an earlier ob version wrote
// to the login keychain with admin-domain flags) that confuse browser
// trust resolution when a new CA is installed alongside them.
func purgeStaleCAs(logger *slog.Logger) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	home := os.Getenv("HOME")
	targets := []string{
		"/Library/Keychains/System.keychain",
		filepath.Join(home, "Library", "Keychains", "login.keychain-db"),
	}
	for _, kc := range targets {
		// Loop because `security delete-certificate` deletes one match at a
		// time. Stop when it reports "not found" (non-zero exit).
		for i := 0; i < 10; i++ {
			cmd := exec.Command("security", "delete-certificate", "-c", "OpenBindings Local CA", kc)
			if err := cmd.Run(); err != nil {
				break
			}
			logger.Debug("removed stale CA entry", "keychain", kc)
		}
	}
	return nil
}

// installCASystemWide writes the CA into the platform-appropriate system
// trust store with root trust. This is what Chrome, Safari, and Firefox-
// on-macOS all consult.
//
// macOS: `/Library/Keychains/System.keychain` via `sudo security
// add-trusted-cert -d -r trustRoot`. Requires sudo; prompts for password
// on a terminal, fails fast with a clear error otherwise.
func installCASystemWide(certPath string, logger *slog.Logger) error {
	switch runtime.GOOS {
	case "darwin":
		logger.Info("installing CA in system keychain (you may be prompted for your password)")
		cmd := exec.Command("sudo", "-p", "ob needs your password to install the local HTTPS CA [%u]: ",
			"security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", certPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sudo security add-trusted-cert failed: %w (HTTP still works; re-run `ob start` to retry)", err)
		}
		logger.Info("CA installed successfully — Chrome, Safari, and Firefox will trust HTTPS localhost")
		return nil
	case "linux":
		logger.Info("installing CA in system trust store (may require sudo)")
		dest := "/usr/local/share/ca-certificates/ob-local-ca.crt"
		cpCmd := exec.Command("sudo", "cp", certPath, dest)
		cpCmd.Stdin, cpCmd.Stdout, cpCmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cpCmd.Run(); err != nil {
			return fmt.Errorf("copying cert to %s: %w", dest, err)
		}
		upCmd := exec.Command("sudo", "update-ca-certificates")
		upCmd.Stdin, upCmd.Stdout, upCmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return upCmd.Run()
	case "windows":
		logger.Info("installing CA in Windows certificate store")
		return exec.Command("certutil", "-addstore", "Root", certPath).Run()
	default:
		return fmt.Errorf("unsupported platform %s: manually trust %s", runtime.GOOS, certPath)
	}
}

func writeTempPEM(cert *x509.Certificate) (string, error) {
	f, err := os.CreateTemp("", "ob-ca-verify-*.crt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func obTLSDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ob", "tls")
	return dir, os.MkdirAll(dir, 0700)
}
