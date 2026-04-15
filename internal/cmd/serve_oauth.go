package cmd

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/openbindings/ob/internal/server"
)

// oauthStore manages pending authorization requests, issued codes, and access tokens.
type oauthStore struct {
	mu      sync.Mutex
	pending map[string]*authRequest // keyed by a request nonce
	codes   map[string]*authCode    // keyed by authorization code
	tokens  map[string]time.Time    // access tokens → expiry
}

type authRequest struct {
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Nonce               string
	CreatedAt           time.Time
}

type authCode struct {
	Code          string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	CreatedAt     time.Time
}

func newOAuthStore() *oauthStore {
	return &oauthStore{
		pending: make(map[string]*authRequest),
		codes:   make(map[string]*authCode),
		tokens:  make(map[string]time.Time),
	}
}

func (s *oauthStore) addPending(req *authRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[req.Nonce] = req
}

func (s *oauthStore) consumePending(nonce string) *authRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.pending[nonce]
	if !ok {
		return nil
	}
	delete(s.pending, nonce)
	return req
}

func (s *oauthStore) addCode(code *authCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[code.Code] = code
}

// consumeCode atomically retrieves and deletes an authorization code, ensuring
// each code can be exchanged for a token at most once (single-use guarantee).
func (s *oauthStore) consumeCode(code string) *authCode {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, ok := s.codes[code]
	if !ok {
		return nil
	}
	delete(s.codes, code)
	return ac
}

func (s *oauthStore) addToken(token string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = time.Now().Add(ttl)
}

func (s *oauthStore) validToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tokens, token)
		return false
	}
	return true
}

// cleanup removes expired entries. Called periodically.
func (s *oauthStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()

	for k, req := range s.pending {
		if now.Sub(req.CreatedAt) > 10*time.Minute {
			delete(s.pending, k)
		}
	}
	for k, code := range s.codes {
		if now.Sub(code.CreatedAt) > 10*time.Minute {
			delete(s.codes, k)
		}
	}
	for k, exp := range s.tokens {
		if now.After(exp) {
			delete(s.tokens, k)
		}
	}
}

const oauthTokenTTL = 24 * time.Hour

// registerOAuthRoutes adds OAuth2 Authorization Code + PKCE endpoints and
// wires the store's token validator into the server's auth middleware.
func registerOAuthRoutes(srv *server.Server, store *oauthStore, logger *slog.Logger) {
	srv.SetOAuthValidator(store.validToken)

	mux := srv.Mux()
	mux.HandleFunc("GET /oauth/authorize", handleOAuthAuthorize(store, logger))
	mux.HandleFunc("POST /oauth/authorize", handleOAuthApprove(store, logger))
	mux.HandleFunc("POST /oauth/token", handleOAuthToken(store, logger))
}

func handleOAuthAuthorize(store *oauthStore, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		responseType := q.Get("response_type")
		clientID := q.Get("client_id")
		redirectURI := q.Get("redirect_uri")
		state := q.Get("state")
		codeChallenge := q.Get("code_challenge")
		challengeMethod := q.Get("code_challenge_method")

		if responseType != "code" {
			oauthAuthorizeHTMLFail(w, logger, http.StatusBadRequest, "Unsupported response type",
				"Only the authorization code flow (response_type=code) is supported.", "unsupported_response_type")
			return
		}
		if clientID == "" || redirectURI == "" {
			oauthAuthorizeHTMLFail(w, logger, http.StatusBadRequest, "Missing parameters",
				"The application did not send client_id and redirect_uri. Try starting the authorization flow again from your app.", "invalid_request")
			return
		}
		if !isAllowedRedirectURI(redirectURI) {
			oauthAuthorizeHTMLFail(w, logger, http.StatusBadRequest, "Invalid redirect URI",
				"The redirect_uri must use HTTPS or target a loopback address (localhost, 127.0.0.1, or [::1]).", "invalid_request")
			return
		}
		if codeChallenge == "" || challengeMethod != "S256" {
			oauthAuthorizeHTMLFail(w, logger, http.StatusBadRequest, "PKCE required",
				"This server requires PKCE with S256 (code_challenge and code_challenge_method=S256). Update the client to send a valid PKCE challenge.", "invalid_request")
			return
		}

		nonce, err := randomHex(16)
		if err != nil {
			oauthAuthorizeHTMLFail(w, logger, http.StatusInternalServerError, "Something went wrong",
				"The server could not start the authorization request. Try again in a moment.", "server_error")
			return
		}

		req := &authRequest{
			ClientID:            clientID,
			RedirectURI:         redirectURI,
			State:               state,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: challengeMethod,
			Nonce:               nonce,
			CreatedAt:           time.Now(),
		}
		store.addPending(req)

		logger.Info("oauth authorize", "client_id", clientID, "redirect_uri", redirectURI)

		writeOAuthHTML(w, http.StatusOK, "oauth-consent", oauthConsentView{
			ClientID: clientID,
			Nonce:    nonce,
			Glyph:    template.HTML(oauthOpenBindingsGlyph),
		}, logger)
	}
}

func handleOAuthApprove(store *oauthStore, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			oauthAuthorizeHTMLFail(w, logger, http.StatusBadRequest, "Invalid request",
				"The form could not be read. Go back and try approving access again.", "invalid_request")
			return
		}

		nonce := r.FormValue("nonce")
		action := r.FormValue("action")

		req := store.consumePending(nonce)
		if req == nil {
			oauthAuthorizeHTMLFail(w, logger, http.StatusBadRequest, "This link expired",
				"This authorization session is no longer valid. It may have expired or already been used. Start again from your application.", "invalid_request")
			return
		}

		if action != "approve" {
			redirectWithError(w, r, req.RedirectURI, req.State, "access_denied", "user denied the request")
			return
		}

		code, err := randomHex(20)
		if err != nil {
			oauthAuthorizeHTMLFail(w, logger, http.StatusInternalServerError, "Something went wrong",
				"The server could not complete authorization. Try again in a moment.", "server_error")
			return
		}

		store.addCode(&authCode{
			Code:          code,
			ClientID:      req.ClientID,
			RedirectURI:   req.RedirectURI,
			CodeChallenge: req.CodeChallenge,
			CreatedAt:     time.Now(),
		})

		logger.Info("oauth code issued", "client_id", req.ClientID)

		sep := "?"
		if strings.Contains(req.RedirectURI, "?") {
			sep = "&"
		}
		location := fmt.Sprintf("%s%scode=%s", req.RedirectURI, sep, url.QueryEscape(code))
		if req.State != "" {
			location += "&state=" + url.QueryEscape(req.State)
		}
		http.Redirect(w, r, location, http.StatusFound)
	}
}

func handleOAuthToken(store *oauthStore, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		if err := r.ParseForm(); err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
			return
		}

		grantType := r.FormValue("grant_type")
		code := r.FormValue("code")
		codeVerifier := r.FormValue("code_verifier")
		redirectURI := r.FormValue("redirect_uri")

		if grantType != "authorization_code" {
			oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "only 'authorization_code' is supported")
			return
		}
		if code == "" || codeVerifier == "" {
			oauthError(w, http.StatusBadRequest, "invalid_request", "code and code_verifier are required")
			return
		}

		ac := store.consumeCode(code)
		if ac == nil {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "unknown, expired, or already-used authorization code")
			return
		}

		if ac.RedirectURI != redirectURI {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
			return
		}

		if !verifyPKCE(ac.CodeChallenge, codeVerifier) {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
			return
		}

		token, err := randomHex(32)
		if err != nil {
			oauthError(w, http.StatusInternalServerError, "server_error", "failed to generate token")
			return
		}

		store.addToken(token, oauthTokenTTL)

		logger.Info("oauth token issued", "client_id", ac.ClientID)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   int(oauthTokenTTL.Seconds()),
		})
	}
}

// verifyPKCE checks the PKCE code_verifier against the stored code_challenge
// using a constant-time comparison. RFC 7636 specifies S256:
// challenge = base64url(sha256(verifier)).
func verifyPKCE(codeChallenge, codeVerifier string) bool {
	h := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(codeChallenge)) == 1
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func oauthError(w http.ResponseWriter, status int, errCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":             errCode,
		"error_description": description,
	})
}

// isAllowedRedirectURI validates that a redirect URI is safe to receive an
// authorization code. Loopback addresses (localhost, 127.0.0.1, [::1]) are
// allowed on any scheme. Non-loopback addresses require HTTPS so the code
// is protected in transit. PKCE (required by this server) cryptographically
// binds the code to the client that initiated the flow, so an intercepted
// code is useless without the verifier.
func isAllowedRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if server.IsLoopbackHost(host) {
		return true
	}
	return u.Scheme == "https"
}

func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, state, errCode, description string) {
	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	location := fmt.Sprintf("%s%serror=%s&error_description=%s", redirectURI, sep, url.QueryEscape(errCode), url.QueryEscape(description))
	if state != "" {
		location += "&state=" + url.QueryEscape(state)
	}
	http.Redirect(w, r, location, http.StatusFound)
}

