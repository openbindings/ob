package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/server"
)

// mockStreamExecutor is a test-only executor that streams canned events.
type mockStreamExecutor struct {
	formats []openbindings.FormatInfo
	events  []any
}

func (m *mockStreamExecutor) Formats() []openbindings.FormatInfo { return m.formats }
func (m *mockStreamExecutor) ExecuteBinding(_ context.Context, _ *openbindings.BindingExecutionInput) (<-chan openbindings.StreamEvent, error) {
	ch := make(chan openbindings.StreamEvent, len(m.events))
	for _, ev := range m.events {
		ch <- openbindings.StreamEvent{Data: ev}
	}
	close(ch)
	return ch, nil
}

func testEnv(t *testing.T) *httptest.Server {
	t.Helper()
	srv, err := server.New(server.Config{
		Port:           0,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Token:          "test-token",
		AllowedOrigins: []string{"http://localhost:3000"},
	})
	if err != nil {
		t.Fatal(err)
	}

	registerRoutes(srv, slog.New(slog.NewTextHandler(io.Discard, nil)), 0, newOAuthStore())

	return httptest.NewServer(srv.Handler())
}

func authedGet(url, token string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func authedPost(url, token, body string) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func mustJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return m
}

// --- /healthz ---

func TestServeHealthz(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := mustJSON(t, resp)
	if body["status"] != "ok" {
		t.Errorf("body = %v, want status=ok", body)
	}
}

func TestServeHealthz_NoAuthRequired(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("healthz should not require auth, got status %d", resp.StatusCode)
	}
}

// --- /info ---

func TestServeInfo(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/info", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := mustJSON(t, resp)
	if _, ok := body["version"]; !ok {
		t.Error("/info response missing 'version' field")
	}
}

// --- /formats ---

func TestServeFormats(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/formats", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// --- Auth enforcement ---

func TestServeAuthRequired(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	paths := []string{"/info", "/formats", "/status"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Errorf("%s without auth: status = %d, want 401", path, resp.StatusCode)
			}
		})
	}
}

func TestServeAuthWrongToken(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/info", "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// --- SSRF validation (unit) ---

func TestValidateResolveURL_PublicHTTPS(t *testing.T) {
	if err := validateResolveURL("https://api.example.com/v1"); err != nil {
		t.Errorf("public HTTPS should pass: %v", err)
	}
}

func TestValidateResolveURL_Localhost(t *testing.T) {
	if err := validateResolveURL("http://localhost:8080/api"); err != nil {
		t.Errorf("localhost should be allowed: %v", err)
	}
}

func TestValidateResolveURL_Loopback(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:9090/api",
		"http://[::1]:8080/api",
	}
	for _, u := range cases {
		if err := validateResolveURL(u); err != nil {
			t.Errorf("loopback %q should be allowed: %v", u, err)
		}
	}
}

func TestValidateResolveURL_PrivateIP(t *testing.T) {
	cases := []string{
		"http://10.0.0.1/api",
		"http://192.168.1.1/api",
		"http://172.16.0.1/api",
	}
	for _, u := range cases {
		if err := validateResolveURL(u); err == nil {
			t.Errorf("private IP %q should be rejected", u)
		}
	}
}

func TestValidateResolveURL_NonHTTPScheme(t *testing.T) {
	cases := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://evil.com",
	}
	for _, u := range cases {
		if err := validateResolveURL(u); err == nil {
			t.Errorf("non-HTTP scheme %q should be rejected", u)
		}
	}
}

// --- /resolve endpoint ---

func TestServeResolve_MissingURL(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/resolve", "test-token", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	body := mustJSON(t, resp)
	if body["error"] != "url is required" {
		t.Errorf("error = %q, want 'url is required'", body["error"])
	}
}

func TestServeResolve_SSRFBlocked(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/resolve", "test-token", `{"url":"http://10.0.0.1/api"}`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 403 {
		t.Errorf("status = %d, want 403 for SSRF", resp.StatusCode)
	}
}

func TestServeResolve_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/resolve", "test-token", `not json`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Content-Type ---

func TestServeJSONContentType(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/info", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// --- /validate ---

func TestServeValidate_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/validate", "test-token", `not json`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- /diff ---

func TestServeDiff_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/diff", "test-token", `not json`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- /compatibility ---

func TestServeCompat_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/compatibility", "test-token", `not json`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Request ID header ---

func TestServeRequestIDHeader(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/info", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rid := resp.Header.Get("X-Request-Id")
	if rid == "" {
		t.Error("X-Request-Id header missing")
	}
	if len(rid) != 16 {
		t.Errorf("X-Request-Id length = %d, want 16", len(rid))
	}
}

// --- /.well-known/openbindings ---

func TestServeWellKnown(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/openbindings")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := mustJSON(t, resp)
	if _, ok := body["openbindings"]; !ok {
		t.Error("/.well-known/openbindings missing 'openbindings' field")
	}
	if _, ok := body["operations"]; !ok {
		t.Error("/.well-known/openbindings missing 'operations' field")
	}
}

// --- /delegates ---

func TestServeDelegates(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/delegates", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// --- /status ---

func TestServeStatus(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/status", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := mustJSON(t, resp)
	if _, ok := body["environmentType"]; !ok {
		t.Error("/status missing 'environmentType' field")
	}
	if _, ok := body["delegateCount"]; !ok {
		t.Error("/status missing 'delegateCount' field")
	}
}

// --- /contexts ---

func TestServeContextList(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/contexts", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// --- /bindings/execute ---

func TestServeBindingExecute_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/bindings/execute", "test-token", `not json`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServeBindingExecute_POST_StillWorks(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/bindings/execute", "test-token", `{"source":{"format":"openapi@3.1","location":"http://example.com/spec.yaml"},"ref":"#/paths/~1health/get"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		t.Errorf("POST should still be accepted, got status %d", resp.StatusCode)
	}
}

func TestServeBindingExecute_MethodNotAllowed(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/bindings/execute", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("DELETE should be rejected, got status %d", resp.StatusCode)
	}
}

func TestServeBindingExecute_WS_Upgrade(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/execute"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer conn.CloseNow()

	// Auth is in the first message (browsers can't set WS headers).
	err = wsjson.Write(ctx, conn, map[string]any{
		"source":      map[string]any{"format": "nonexistent-format"},
		"ref":         "#/some/ref",
		"bearerToken": "test-token",
	})
	if err != nil {
		t.Fatalf("write initial message: %v", err)
	}

	var msg struct {
		Type  string `json:"type"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if msg.Type != "error" {
		t.Errorf("expected error message, got type=%q", msg.Type)
	}
	if msg.Error == nil {
		t.Fatal("expected error field to be present")
	}
}

func TestServeBindingExecute_WS_StreamE2E(t *testing.T) {
	mockExec := &mockStreamExecutor{
		formats: []openbindings.FormatInfo{{Token: "mock-stream@1.0"}},
		events: []any{"event-1", "event-2", "event-3"},
	}
	cleanup := app.OverrideExecutorForTest(
		openbindings.NewOperationExecutor(mockExec),
	)
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/execute"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer conn.CloseNow()

	err = wsjson.Write(ctx, conn, map[string]any{
		"source":      map[string]any{"format": "mock-stream@1.0", "location": "mock://test"},
		"ref":         "#/test",
		"bearerToken": "test-token",
	})
	if err != nil {
		t.Fatalf("write initial message: %v", err)
	}

	var received []any
	for {
		var msg struct {
			Type string `json:"type"`
			Data any    `json:"data,omitempty"`
		}
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			// Normal close frame signals end of stream.
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			t.Fatalf("read stream message: %v", err)
		}
		if msg.Type != "event" {
			t.Fatalf("unexpected message type %q", msg.Type)
		}
		received = append(received, msg.Data)
	}

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
	for i, want := range []string{"event-1", "event-2", "event-3"} {
		if received[i] != want {
			t.Errorf("event[%d] = %v, want %q", i, received[i], want)
		}
	}
}

func TestServeBindingExecute_WS_NoAuth(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/execute"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		// Connection refused is also acceptable (middleware may reject).
		return
	}
	defer conn.CloseNow()

	// Send a message without a bearer token — server should close the connection.
	err = wsjson.Write(ctx, conn, map[string]any{
		"source": map[string]any{"format": "openapi@3.1", "location": "x"},
		"ref":    "#/paths/~1test/get",
	})
	if err != nil {
		return // write failed, connection already closed
	}

	// Try to read — should fail because server closed with policy violation.
	var msg json.RawMessage
	err = wsjson.Read(ctx, conn, &msg)
	if err == nil {
		t.Fatal("expected read to fail after unauthenticated WebSocket message")
	}
}

// --- /interfaces/create ---

func TestServeInterfaceCreate_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/interfaces/create", "test-token", `not json`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Auth enforcement (expanded) ---

func TestServeAuthRequired_AllProtectedEndpoints(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	getPaths := []string{
		"/info", "/formats", "/delegates", "/status", "/contexts",
	}
	for _, path := range getPaths {
		t.Run("GET "+path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Errorf("GET %s without auth: status = %d, want 401", path, resp.StatusCode)
			}
		})
	}

	postPaths := []string{
		"/resolve", "/validate", "/diff", "/compatibility",
		"/bindings/execute", "/interfaces/create",
	}
	for _, path := range postPaths {
		t.Run("POST "+path, func(t *testing.T) {
			req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Errorf("POST %s without auth: status = %d, want 401", path, resp.StatusCode)
			}
		})
	}
}

// --- CORS ---

func TestServeCORS_PreflightAllowed(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/info", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao != "http://localhost:3000" {
		t.Errorf("ACAO = %q, want http://localhost:3000", acao)
	}
}

// --- End-to-end OAuth2 Authorization Code + PKCE flow ---

func TestServeOAuthE2E(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	// Don't follow redirects — we want to inspect Location headers.
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Generate PKCE verifier and challenge.
	verifierBytes := make([]byte, 32)
	rand.Read(verifierBytes)
	codeVerifier := hex.EncodeToString(verifierBytes)
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	redirectURI := "http://localhost:9999/callback"

	// 2. GET /oauth/authorize — should return 200 with HTML containing a nonce.
	authURL := fmt.Sprintf("%s/oauth/authorize?response_type=code&client_id=test-app&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256",
		ts.URL, redirectURI, codeChallenge)
	resp, err := http.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("authorize: status = %d, want 200", resp.StatusCode)
	}

	nonceRe := regexp.MustCompile(`name="nonce"\s+value="([a-f0-9]+)"`)
	matches := nonceRe.FindSubmatch(body)
	if len(matches) < 2 {
		t.Fatal("authorize: could not find nonce in HTML response")
	}
	nonce := string(matches[1])

	// 3. POST /oauth/authorize (approve) — should redirect with code.
	approveBody := fmt.Sprintf("nonce=%s&action=approve", nonce)
	req, _ := http.NewRequest("POST", ts.URL+"/oauth/authorize", strings.NewReader(approveBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("approve: status = %d, want 302", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, redirectURI) {
		t.Fatalf("approve: Location = %q, should start with %q", location, redirectURI)
	}

	codeRe := regexp.MustCompile(`code=([a-f0-9]+)`)
	codeMatches := codeRe.FindStringSubmatch(location)
	if len(codeMatches) < 2 {
		t.Fatalf("approve: could not extract code from Location: %s", location)
	}
	authCode := codeMatches[1]

	// 4. POST /oauth/token — exchange code for access token.
	tokenBody := fmt.Sprintf("grant_type=authorization_code&code=%s&code_verifier=%s&redirect_uri=%s",
		authCode, codeVerifier, redirectURI)
	req, _ = http.NewRequest("POST", ts.URL+"/oauth/token", strings.NewReader(tokenBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tokenResp map[string]any
	json.NewDecoder(resp.Body).Decode(&tokenResp)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("token: status = %d, want 200", resp.StatusCode)
	}
	accessToken, ok := tokenResp["access_token"].(string)
	if !ok || accessToken == "" {
		t.Fatal("token: missing access_token in response")
	}
	if tokenResp["token_type"] != "Bearer" {
		t.Errorf("token: token_type = %v, want Bearer", tokenResp["token_type"])
	}

	// 5. Use OAuth token on an authenticated endpoint.
	resp, err = authedGet(ts.URL+"/info", accessToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("info with OAuth token: status = %d, want 200", resp.StatusCode)
	}

	// 6. Verify invalid token is rejected.
	resp, err = authedGet(ts.URL+"/info", "totally-invalid-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("info with bad token: status = %d, want 401", resp.StatusCode)
	}
}

// --- OAuth redirect URI validation ---

func TestIsAllowedRedirectURI(t *testing.T) {
	allowed := []string{
		"http://localhost:8080/callback",
		"http://localhost/callback",
		"https://localhost:8080/callback",
		"http://127.0.0.1:9999/callback",
		"https://127.0.0.1:9999/callback",
		"http://[::1]:8080/callback",
		"https://[::1]/callback",
		"https://app.example.com/callback",
		"https://staging.panjir.com/auth/host-callback",
		"https://my-tool.internal:3000/oauth/done",
	}
	for _, uri := range allowed {
		if !isAllowedRedirectURI(uri) {
			t.Errorf("should be allowed: %q", uri)
		}
	}

	rejected := []string{
		"http://app.example.com/callback",
		"http://192.168.1.1:8080/callback",
		"http://10.0.0.1/callback",
		"ftp://localhost/callback",
		"not-a-url",
	}
	for _, uri := range rejected {
		if isAllowedRedirectURI(uri) {
			t.Errorf("should be rejected: %q", uri)
		}
	}
}

// --- Spec placeholder rewriting ---

func TestRewriteSpecPlaceholders_HTTP(t *testing.T) {
	spec := []byte("url: ${OB_SERVER_URL}\nhost: ${OB_SERVER_HOST}\nprotocol: ${OB_SERVER_PROTOCOL}")
	result := rewriteSpecPlaceholders(spec, "http://localhost:9876")

	if !strings.Contains(string(result), "url: http://localhost:9876") {
		t.Errorf("expected http URL, got: %s", result)
	}
	if !strings.Contains(string(result), "host: localhost:9876") {
		t.Errorf("expected host localhost:9876, got: %s", result)
	}
	if !strings.Contains(string(result), "protocol: ws") {
		t.Errorf("expected ws protocol, got: %s", result)
	}
}

func TestRewriteSpecPlaceholders_HTTPS(t *testing.T) {
	spec := []byte("url: ${OB_SERVER_URL}\nhost: ${OB_SERVER_HOST}\nprotocol: ${OB_SERVER_PROTOCOL}")
	result := rewriteSpecPlaceholders(spec, "https://localhost:20290")

	if !strings.Contains(string(result), "url: https://localhost:20290") {
		t.Errorf("expected https URL, got: %s", result)
	}
	if !strings.Contains(string(result), "host: localhost:20290") {
		t.Errorf("expected host localhost:20290, got: %s", result)
	}
	if !strings.Contains(string(result), "protocol: wss") {
		t.Errorf("expected wss protocol, got: %s", result)
	}
}

// --- Spec validation ---

func TestSpecsParseCleanly(t *testing.T) {
	t.Run("ob.obi.json", func(t *testing.T) {
		iface, err := app.OpenBindingsInterface()
		if err != nil {
			t.Fatalf("failed to parse ob.obi.json: %v", err)
		}
		if iface.Name == "" {
			t.Error("ob.obi.json: name is empty")
		}
		if len(iface.Operations) == 0 {
			t.Error("ob.obi.json: no operations defined")
		}
	})

	t.Run("openapi.yaml", func(t *testing.T) {
		spec := server.OpenAPISpec()
		if len(spec) == 0 {
			t.Fatal("openapi.yaml is empty")
		}
		if !strings.Contains(string(spec), "openapi:") {
			t.Error("openapi.yaml: missing openapi version field")
		}
		if !strings.Contains(string(spec), "paths:") {
			t.Error("openapi.yaml: missing paths section")
		}
	})
}

// --- Spec-handler conformance ---

func TestSpecHandlerConformance(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	// Endpoints documented in openapi.yaml and the methods they support.
	// Each is tested for "not 404/405" to verify a handler is registered.
	type endpoint struct {
		method string
		path   string
	}
	documented := []endpoint{
		{"GET", "/healthz"},
		{"GET", "/.well-known/openbindings"},
		{"GET", "/openapi.yaml"},
		{"GET", "/info"},
		{"GET", "/formats"},
		{"GET", "/delegates"},
		{"GET", "/status"},
		{"GET", "/contexts"},
		{"GET", "/contexts/https://example.com"},
		{"PUT", "/contexts/https://example.com"},
		{"DELETE", "/contexts/https://example.com"},
		{"POST", "/bindings/execute"},
		{"POST", "/interfaces/create"},
		{"POST", "/resolve"},
		{"POST", "/validate"},
		{"POST", "/diff"},
		{"POST", "/compatibility"},
		{"GET", "/oauth/authorize"},
		{"POST", "/oauth/authorize"},
		{"POST", "/oauth/token"},
	}

	for _, ep := range documented {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			var bodyReader io.Reader
			if ep.method == "POST" || ep.method == "PUT" || ep.method == "PATCH" {
				bodyReader = strings.NewReader("{}")
			}
			req, err := http.NewRequest(ep.method, ts.URL+ep.path, bodyReader)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer test-token")
			if bodyReader != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode == 404 || resp.StatusCode == 405 {
				t.Errorf("documented endpoint returned %d — handler likely missing", resp.StatusCode)
			}
		})
	}
}
