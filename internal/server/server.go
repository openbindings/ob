package server

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDFromContext extracts the request ID set by the request ID middleware.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// Config holds the configuration for an ob serve instance.
type Config struct {
	Port           int
	AllowedOrigins []string
	Logger         *slog.Logger
	Token          string
	TLS            bool
}

// Server is the ob serve HTTP server.
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

// Handler returns the full middleware-wrapped handler chain.
// Useful for testing routes with auth, CORS, and logging applied.
func (s *Server) Handler() http.Handler {
	return s.buildMiddlewareChain(s.mux)
}

// ListenAndServe binds HTTP (and HTTPS, unless --no-tls) to localhost and
// serves until the context is cancelled.
//
// By default, ob runs two listeners: HTTP on `config.Port` and HTTPS on
// `config.Port + 1`. Clients probe either protocol; whichever the page
// can reach wins. HTTP always works with zero config. HTTPS works once
// the local CA is trusted by the system keychain (auto-installed on
// first run). If TLS setup fails for any reason (declined sudo prompt,
// permission error, non-interactive terminal), HTTP still serves — the
// user is never fully blocked.
func (s *Server) ListenAndServe(ctx context.Context) error {
	handler := s.buildMiddlewareChain(s.mux)

	httpListener, httpPort, err := bindLocalhostPort(s.config.Port, 10)
	if err != nil {
		return fmt.Errorf("HTTP bind failed (tried 10 ports starting at %d): %w", s.config.Port, err)
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
		tlsCfg, tlsErr := ensureLocalhostTLS(s.logger)
		if tlsErr != nil {
			s.logger.Warn("HTTPS disabled — HTTP remains available",
				"reason", tlsErr,
				"fix", "re-run `ob serve` in a terminal to retry cert install",
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
// `startPort`. Returns the listener, the port it bound to, or an error.
func bindLocalhostPort(startPort, attempts int) (net.Listener, int, error) {
	port := startPort
	var lastErr error
	for i := 0; i < attempts; i++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
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
	h = s.loggingMiddleware(h)
	return h
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
		host := r.Host
		if idx := strings.LastIndex(host, ":"); idx >= 0 {
			host = host[:idx]
		}
		if IsLoopbackHost(host) {
			next.ServeHTTP(w, r)
		} else {
			http.Error(w, "forbidden: non-localhost host header", http.StatusForbidden)
		}
	})
}

// corsMiddleware handles CORS preflight and sets headers for allowed origins.
//
// ob serve binds to localhost only and requires a session token on every
// request. CORS is defense-in-depth, not the primary security boundary.
// Any HTTPS origin and any localhost origin are allowed by default so that
// any web app (local or remote) can connect to the user's host without
// the user needing to configure --allow-origin. The session token prevents
// unauthorized access regardless of origin.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	explicit := make(map[string]bool, len(s.config.AllowedOrigins))
	for _, o := range s.config.AllowedOrigins {
		explicit[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (explicit[origin] || isLocalhostOrigin(origin) || strings.HasPrefix(origin, "https://")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "3600")
			w.Header().Set("Vary", "Origin")

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

// IsLoopbackHost returns true if the hostname (without port or scheme) is a
// loopback address: localhost, 127.0.0.1, [::1], or ::1. Used for host
// validation, CORS, OAuth redirect URI checks, and WebSocket origin checks.
func IsLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "[::1]" || host == "::1"
}

// isLocalhostOrigin returns true for origins like http(s)://localhost:PORT or
// http(s)://127.0.0.1:PORT. Used for the default CORS policy.
func isLocalhostOrigin(origin string) bool {
	host := origin
	if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	} else if strings.HasPrefix(host, "http://") {
		host = strings.TrimPrefix(host, "http://")
	} else {
		return false
	}
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return IsLoopbackHost(host)
}

// authMiddleware requires a valid Bearer token on all requests except
// public endpoints (healthz, well-known, OAuth, MCP transport).
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if path == "/" || path == "/.well-known/openbindings" || path == "/healthz" ||
			path == "" ||
			path == "/openapi.yaml" || path == "/asyncapi.yaml" {
			next.ServeHTTP(w, r)
			return
		}
		if path == "/oauth/authorize" || path == "/oauth/token" {
			next.ServeHTTP(w, r)
			return
		}

		// WebSocket upgrades handle auth in the first message (browsers can't
		// set headers on upgrade requests). Let them through to the handler.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if !strings.HasPrefix(auth, "Bearer ") || !s.IsValidToken(token) {
			s.logger.Warn("auth failure",
				"method", r.Method,
				"path", path,
				"remote_addr", r.RemoteAddr,
				"request_id", RequestIDFromContext(r.Context()),
			)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
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

// ensureLocalhostTLS returns a tls.Config for localhost using a locally-
// installed root CA. The full flow, self-healing on every call:
//
//  1. Load or create the CA at ~/.ob/tls/ob-ca.crt.
//  2. Verify the CA is trusted by the system (macOS: present in the
//     System keychain; Linux: in /etc/ssl/certs; Windows: Root store).
//     If not — or if it's only in the user-login keychain from an earlier
//     broken install — purge stale copies and re-install system-wide.
//  3. Load or mint a leaf cert signed by the CA with 90-day validity.
//
// If cert install fails (user declines sudo, non-interactive terminal,
// permission error), returns an error so the caller can log a warning
// and fall back to HTTP-only. The user is never fully blocked.
func ensureLocalhostTLS(logger *slog.Logger) (*tls.Config, error) {
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

	// Verify the CA is actually trusted by the system. If it was created
	// just now (`created=true`) skip the verify — we already know we need
	// to install. If it's existing, check: if a prior install was broken
	// (e.g., landed in the user login keychain without proper trust) the
	// verify flags that and we re-install cleanly.
	needInstall := created || !caIsSystemTrusted(caCert, logger)
	if needInstall {
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

	return &tls.Config{Certificates: []tls.Certificate{leafCert}}, nil
}

// loadOrCreateCA returns the CA cert+key. Reports `created=true` if a
// new CA was generated on disk (caller uses this to trigger install).
func loadOrCreateCA(certPath, keyPath string, logger *slog.Logger) (*x509.Certificate, *ecdsa.PrivateKey, bool, error) {
	if certPEM, err := os.ReadFile(certPath); err == nil {
		if keyPEM, err := os.ReadFile(keyPath); err == nil {
			block, _ := pem.Decode(certPEM)
			if block != nil {
				caCert, err := x509.ParseCertificate(block.Bytes)
				if err == nil && time.Now().Before(caCert.NotAfter) {
					keyBlock, _ := pem.Decode(keyPEM)
					if keyBlock != nil {
						caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
						if err == nil {
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
		SerialNumber:          serial,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
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

func loadOrMintLeaf(certPath, keyPath string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (tls.Certificate, error) {
	if existing, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, parseErr := x509.ParseCertificate(existing.Certificate[0]); parseErr == nil {
			if time.Now().Before(leaf.NotAfter.Add(-24*time.Hour)) {
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
		NotBefore:             time.Now(),
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
	switch runtime.GOOS {
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
		cmd := exec.Command("security", "verify-cert", "-c", tmp, "-p", "basic")
		if err := cmd.Run(); err != nil {
			logger.Debug("system CA trust check failed, will reinstall", "error", err)
			return false
		}
		return true
	case "linux":
		// Presence in the system cert store is sufficient: update-ca-certificates
		// builds the pool from there on each run, and Chrome/Firefox on Linux
		// consult it (plus their own NSS stores, which we don't touch).
		_, err := os.Stat("/usr/local/share/ca-certificates/ob-local-ca.crt")
		return err == nil
	case "windows":
		// certutil -verifystore Root <thumbprint> would be ideal; settle for
		// presence since Windows trust semantics are well-defined once a cert
		// is in the Root store.
		return true
	default:
		return true
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
			return fmt.Errorf("sudo security add-trusted-cert failed: %w (HTTP still works; re-run `ob serve` to retry)", err)
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
