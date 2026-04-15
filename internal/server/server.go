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

// ListenAndServe binds to localhost on the configured port (with fallback)
// and serves until the context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	handler := s.buildMiddlewareChain(s.mux)

	port := s.config.Port
	var listener net.Listener
	var err error

	for attempts := 0; attempts < 10; attempts++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		port++
	}
	if err != nil {
		return fmt.Errorf("failed to bind (tried ports %d–%d): %w", s.config.Port, port, err)
	}

	httpSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	scheme := "http"
	if s.config.TLS {
		tlsCfg, err := localhostTLSConfig(s.logger)
		if err != nil {
			return fmt.Errorf("TLS setup: %w", err)
		}
		httpSrv.TLSConfig = tlsCfg
		listener = tls.NewListener(listener, tlsCfg)
		scheme = "https"
	}

	s.httpSrv.Store(httpSrv)

	s.logger.Info("server starting",
		"addr", fmt.Sprintf("%s://localhost:%d", scheme, port),
	)
	if len(s.config.AllowedOrigins) > 0 {
		s.logger.Info("CORS configured", "origins", s.config.AllowedOrigins)
	} else {
		s.logger.Info("CORS allowing localhost origins (use --allow-origin to add others)")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.logger.Info("shutting down")
		return httpSrv.Shutdown(shutCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
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

// localhostTLSConfig returns a tls.Config using a locally-trusted CA.
//
// On first run, ob creates a root CA at ~/.ob/tls/ob-ca.crt and installs
// it in the system trust store (prompts for password once). All subsequent
// runs reuse the CA to sign ephemeral localhost certs that every browser
// trusts without warnings or manual setup.
func localhostTLSConfig(logger *slog.Logger) (*tls.Config, error) {
	dir, err := obTLSDir()
	if err != nil {
		return nil, err
	}

	caKeyPath := filepath.Join(dir, "ob-ca.key")
	caCertPath := filepath.Join(dir, "ob-ca.crt")
	certPath := filepath.Join(dir, "localhost.crt")
	keyPath := filepath.Join(dir, "localhost.key")

	// Load or create the local CA.
	caCert, caKey, err := loadOrCreateCA(caCertPath, caKeyPath, logger)
	if err != nil {
		return nil, fmt.Errorf("CA setup: %w", err)
	}

	// Try loading existing leaf cert.
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err == nil {
		leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
		if parseErr == nil && time.Now().Before(leaf.NotAfter.Add(-24*time.Hour)) {
			// Still valid with at least a day of margin.
			return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
		}
	}

	// Generate a new leaf cert signed by the CA.
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating leaf key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial: %w", err)
	}

	leafTmpl := &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(90 * 24 * time.Hour), // 90 days
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("signing leaf cert: %w", err)
	}

	leafCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	if err := os.WriteFile(certPath, leafCertPEM, 0644); err != nil {
		return nil, fmt.Errorf("writing leaf cert: %w", err)
	}

	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling leaf key: %w", err)
	}
	leafKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER})
	if err := os.WriteFile(keyPath, leafKeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("writing leaf key: %w", err)
	}

	tlsCert, err := tls.X509KeyPair(leafCertPEM, leafKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("loading leaf keypair: %w", err)
	}

	return &tls.Config{Certificates: []tls.Certificate{tlsCert}}, nil
}

func loadOrCreateCA(certPath, keyPath string, logger *slog.Logger) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	// Try loading existing CA.
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
							return caCert, caKey, nil
						}
					}
				}
			}
		}
	}

	logger.Info("creating local CA for HTTPS (one-time setup)")

	// Generate CA key.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generating CA serial: %w", err)
	}

	caTmpl := &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
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
		return nil, nil, fmt.Errorf("creating CA cert: %w", err)
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA cert: %w", err)
	}

	// Write CA cert.
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	if err := os.WriteFile(certPath, caCertPEM, 0644); err != nil {
		return nil, nil, fmt.Errorf("writing CA cert: %w", err)
	}

	// Write CA key.
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling CA key: %w", err)
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})
	if err := os.WriteFile(keyPath, caKeyPEM, 0600); err != nil {
		return nil, nil, fmt.Errorf("writing CA key: %w", err)
	}

	// Install CA in system trust store.
	if err := installCA(certPath, logger); err != nil {
		return nil, nil, fmt.Errorf("installing CA: %w", err)
	}

	return caCert, caKey, nil
}

// installCA adds the CA certificate to the platform's trust store.
func installCA(certPath string, logger *slog.Logger) error {
	switch runtime.GOOS {
	case "darwin":
		logger.Info("installing CA in macOS Keychain (you may be prompted for your password)")
		cmd := exec.Command("security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", filepath.Join(os.Getenv("HOME"), "Library", "Keychains", "login.keychain-db"),
			certPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "linux":
		logger.Info("installing CA in system trust store")
		// Try common Linux trust store paths.
		dest := "/usr/local/share/ca-certificates/ob-local-ca.crt"
		if err := exec.Command("cp", certPath, dest).Run(); err != nil {
			return fmt.Errorf("copying cert to %s: %w (try running with sudo)", dest, err)
		}
		return exec.Command("update-ca-certificates").Run()
	case "windows":
		logger.Info("installing CA in Windows certificate store")
		return exec.Command("certutil", "-addstore", "Root", certPath).Run()
	default:
		return fmt.Errorf("unsupported platform %s: manually trust %s", runtime.GOOS, certPath)
	}
}

func obTLSDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ob", "tls")
	return dir, os.MkdirAll(dir, 0700)
}
