package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"gopkg.in/yaml.v3"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/invoke"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/frames"
	"github.com/openbindings/ob/internal/server"
)

// mockBlockingInvoker emits one output then streams forever, closing tornDown
// when the invocation finally terminates (its EmitOutput returns the terminal
// error). It models an infinite server-stream so a client disconnect must be
// what tears it down.
type mockBlockingInvoker struct {
	formats  []openbindings.BindingSpecInfo
	tornDown chan struct{}
}

func (m *mockBlockingInvoker) BindingSpecs() []openbindings.BindingSpecInfo { return m.formats }
func (m *mockBlockingInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, m.BindingSpecs())
}
func (m *mockBlockingInvoker) InvokeBinding(ctx context.Context, _ *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		_ = inv.CloseInput()
		for {
			if err := inv.EmitOutput("tick"); err != nil {
				close(m.tornDown)
				return
			}
		}
	}()
	return inv
}

// mockStreamInvoker is a test-only invoker that emits canned output values as
// a stream, then closes cleanly.
type mockStreamInvoker struct {
	formats []openbindings.BindingSpecInfo
	events  []any
}

func (m *mockStreamInvoker) BindingSpecs() []openbindings.BindingSpecInfo { return m.formats }
func (m *mockStreamInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, m.BindingSpecs())
}
func (m *mockStreamInvoker) InvokeBinding(ctx context.Context, _ *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		_ = inv.CloseInput()
		for _, ev := range m.events {
			if err := inv.EmitOutput(ev); err != nil {
				return
			}
		}
		inv.CloseOutput()
	}()
	return inv
}

// errorStreamInvoker emits zero or more outputs and then terminates with a
// terminal error, letting tests exercise error frames.
type errorStreamInvoker struct {
	formats []openbindings.BindingSpecInfo
	outputs []any
	err     *invoke.InvocationError
}

func (m *errorStreamInvoker) BindingSpecs() []openbindings.BindingSpecInfo { return m.formats }
func (m *errorStreamInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, m.BindingSpecs())
}
func (m *errorStreamInvoker) InvokeBinding(ctx context.Context, _ *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		_ = inv.CloseInput()
		for _, ev := range m.outputs {
			if err := inv.EmitOutput(ev); err != nil {
				return
			}
		}
		inv.FireError(m.err)
	}()
	return inv
}

// mockEchoInvoker models a unary binding through the handle: it reads one
// input, closes the input side from below, emits one derived output, and
// completes.
type mockEchoInvoker struct {
	formats []openbindings.BindingSpecInfo
}

func (m *mockEchoInvoker) BindingSpecs() []openbindings.BindingSpecInfo { return m.formats }
func (m *mockEchoInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, m.BindingSpecs())
}
func (m *mockEchoInvoker) InvokeBinding(ctx context.Context, _ *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		v, err := inv.ReadInput(ctx)
		if err != nil {
			inv.FireError(invoke.AsInvocationError(err))
			return
		}
		_ = inv.CloseInput()
		if inv.EmitOutput(map[string]any{"echo": v}) != nil {
			return
		}
		inv.CloseOutput()
	}()
	return inv
}

type contextCaptureInvoker struct {
	formats []openbindings.BindingSpecInfo
	seen    chan map[string]any
}

func (m *contextCaptureInvoker) BindingSpecs() []openbindings.BindingSpecInfo { return m.formats }
func (m *contextCaptureInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, m.BindingSpecs())
}
func (m *contextCaptureInvoker) InvokeBinding(ctx context.Context, args *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	m.seen <- args.Context
	inv := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		for {
			if _, err := inv.ReadInput(ctx); err != nil {
				break
			}
		}
		inv.CloseOutput()
	}()
	return inv
}

// gatedUnaryInvoker reads one input, closes the input side, emits one output,
// then parks until release closes before emitting a second output and
// completing — letting a test interleave a late input frame deterministically.
type gatedUnaryInvoker struct {
	formats []openbindings.BindingSpecInfo
	release chan struct{}
}

func (m *gatedUnaryInvoker) BindingSpecs() []openbindings.BindingSpecInfo { return m.formats }
func (m *gatedUnaryInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, m.BindingSpecs())
}
func (m *gatedUnaryInvoker) InvokeBinding(ctx context.Context, _ *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		if _, err := inv.ReadInput(ctx); err != nil {
			inv.FireError(invoke.AsInvocationError(err))
			return
		}
		_ = inv.CloseInput()
		if inv.EmitOutput("first") != nil {
			return
		}
		select {
		case <-m.release:
		case <-ctx.Done():
			return
		}
		if inv.EmitOutput("second") != nil {
			return
		}
		inv.CloseOutput()
	}()
	return inv
}

// contextRequiredInvoker terminates immediately with a CONTEXT_REQUIRED
// challenge, before any output (binding-invoker rule 8).
type contextRequiredInvoker struct {
	formats []openbindings.BindingSpecInfo
	details *invoke.ContextRequiredDetails
}

func (m *contextRequiredInvoker) BindingSpecs() []openbindings.BindingSpecInfo { return m.formats }
func (m *contextRequiredInvoker) CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(bindingSpecs, m.BindingSpecs())
}
func (m *contextRequiredInvoker) InvokeBinding(ctx context.Context, _ *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	inv.FireError(invoke.NewContextRequiredError(m.details))
	return inv
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

	resp, err := authedGet(ts.URL+"/describe", "test-token")
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

	resp, err := authedGet(ts.URL+"/binding-specs", "test-token")
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

	paths := []string{"/describe", "/binding-specs", "/environment"}
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

	resp, err := authedGet(ts.URL+"/describe", "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// --- SSRF validation (unit) ---

func TestValidateOutboundURL_PublicHTTPS(t *testing.T) {
	if err := validateOutboundURL("https://api.example.com/v1"); err != nil {
		t.Errorf("public HTTPS should pass: %v", err)
	}
}

func TestValidateOutboundURL_Localhost(t *testing.T) {
	if err := validateOutboundURL("http://localhost:8080/api"); err != nil {
		t.Errorf("localhost should be allowed: %v", err)
	}
}

func TestValidateOutboundURL_Loopback(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:9090/api",
		"http://[::1]:8080/api",
	}
	for _, u := range cases {
		if err := validateOutboundURL(u); err != nil {
			t.Errorf("loopback %q should be allowed: %v", u, err)
		}
	}
}

func TestValidateOutboundURL_PrivateIP(t *testing.T) {
	cases := []string{
		"http://10.0.0.1/api",
		"http://192.168.1.1/api",
		"http://172.16.0.1/api",
	}
	for _, u := range cases {
		if err := validateOutboundURL(u); err == nil {
			t.Errorf("private IP %q should be rejected", u)
		}
	}
}

func TestValidateOutboundURL_NonHTTPScheme(t *testing.T) {
	cases := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://evil.com",
	}
	for _, u := range cases {
		if err := validateOutboundURL(u); err == nil {
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
	if body["error"] != "address is required" {
		t.Errorf("error = %q, want 'address is required'", body["error"])
	}
}

func TestServeResolve_SSRFBlocked(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/resolve", "test-token", `{"address":"http://10.0.0.1/api"}`)
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

	resp, err := authedGet(ts.URL+"/describe", "test-token")
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

// --- expansion routes (conform, codegen, merge, interface-status) ---

// TestServeExpansionRoutes_Registered confirms the whole-interface authoring
// routes are wired: a malformed body must reach the handler and yield a 400
// (decode error), never a 404 (route missing) or a 500 (panic).
func TestServeExpansionRoutes_Registered(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	for _, path := range []string{"/conform", "/codegen", "/merge", "/interfaces/status"} {
		resp, err := authedPost(ts.URL+path, "test-token", `not json`)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("%s: status = %d, want 400 (route registered, decode rejected)", path, resp.StatusCode)
		}
	}
}

// TestServeCodegen_UnsupportedLanguage confirms the handler dispatches past
// decode: a well-formed body with a bad language is rejected by handler logic.
func TestServeCodegen_UnsupportedLanguage(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/codegen", "test-token", `{"source":"x","language":"cobol"}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Request ID header ---

func TestServeRequestIDHeader(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/describe", "test-token")
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

// Root is deliberately NOT an OBI discovery location: discovery is well-known
// only (spec §7), consistent with the registry. Root serves the non-OBI
// workbench, so the SDK direct-fetch branch fails over to well-known instead of
// treating root as canonical. This guards against re-introducing OBI-at-root.
func TestServeRoot_NotOBI(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("root Content-Type = %q, want text/html (a non-OBI workbench)", ct)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("root Content-Security-Policy = %q, want workbench framing protection", csp)
	}
	expectedWebSocketOrigin := "ws://" + strings.TrimPrefix(ts.URL, "http://")
	if !strings.Contains(csp, "connect-src 'self' "+expectedWebSocketOrigin) {
		t.Errorf("root Content-Security-Policy = %q, want exact WebSocket origin %q", csp, expectedWebSocketOrigin)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "\"openbindings\"") || strings.Contains(body, "\"operations\"") {
		t.Error("root looks like an OBI document; it must be a non-OBI page")
	}
	if !strings.Contains(body, "/.well-known/openbindings") {
		t.Error("root page should point at /.well-known/openbindings")
	}
}

func TestServeRoot_DoesNotReflectMalformedHostIntoCSP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	req.Host = "localhost:20402;sandbox"
	rec := httptest.NewRecorder()

	handleRoot(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, ";sandbox") {
		t.Fatalf("root CSP reflected a Host-header directive: %q", csp)
	}
	if !strings.Contains(csp, "connect-src 'self' ws://localhost") {
		t.Fatalf("root CSP did not retain a safe loopback WebSocket origin: %q", csp)
	}
}

// The served /openapi.yaml is how a consumer discovers this server's own
// transport auth: the SDK's openapi invoker reads these securitySchemes and
// surfaces a CONTEXT_REQUIRED challenge (bearer or oauth2). The /oauth URLs
// MUST be absolutized against the request origin (placeholders fully
// substituted) or a consumer's PKCE flow has nowhere to go. This is the
// mechanism Panjir web's connect flow relies on; guard it against regression.
func TestServeOpenAPISpec_CarriesAbsoluteOAuthEndpoints(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(raw)

	for _, scheme := range []string{"oauth2Auth", "bearerAuth"} {
		if !strings.Contains(spec, scheme) {
			t.Errorf("served openapi.yaml missing security scheme %q", scheme)
		}
	}
	if strings.Contains(spec, "${OB_SERVER_URL}") {
		t.Error("served openapi.yaml still contains an unsubstituted ${OB_SERVER_URL} placeholder")
	}
	for _, want := range []string{ts.URL + "/oauth/authorize", ts.URL + "/oauth/token"} {
		if !strings.Contains(spec, want) {
			t.Errorf("served openapi.yaml missing absolute oauth endpoint %q", want)
		}
	}
}

func TestServeAsyncAPISpec_CarriesBothWebSocketAuthLanes(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/asyncapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(raw)
	for _, want := range []string{"protocol: ws", "bearer:", "tokenQuery:", "name: token", "invokeBinding:", "invokeOperation:"} {
		if !strings.Contains(spec, want) {
			t.Errorf("served asyncapi.yaml missing %q", want)
		}
	}
	if strings.Contains(spec, "${OB_SERVER_") {
		t.Fatal("served asyncapi.yaml contains an unsubstituted server placeholder")
	}
}

// OBI 0.2.0 carries no `security` field; auth is a runtime CONTEXT_REQUIRED
// concern discovered via the openapi source (see the test above). This locks
// both invariants: the served OBI stays security-field-free, and its openapi
// source location is absolutized so a consumer can fetch the spec that carries
// the auth metadata.
func TestServeWellKnown_NoSecurityField_OpenAPISourceAbsolutized(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/openbindings")
	if err != nil {
		t.Fatal(err)
	}
	body := mustJSON(t, resp)

	if _, ok := body["security"]; ok {
		t.Error("served OBI must not carry a top-level 'security' field (OBI 0.2.0 has none)")
	}

	sources, ok := body["sources"].(map[string]any)
	if !ok {
		t.Fatal("served OBI missing 'sources' map")
	}
	for _, name := range []string{"openapi", "asyncapi"} {
		source, ok := sources[name].(map[string]any)
		if !ok {
			t.Fatalf("served OBI missing %q source", name)
		}
		loc, _ := source["location"].(string)
		if want := ts.URL + "/" + name + ".yaml"; loc != want {
			t.Errorf("%s source location = %q, want absolutized %q", name, loc, want)
		}
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

	resp, err := authedGet(ts.URL+"/environment", "test-token")
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

// --- /bindings/invoke (binding-invoker frame protocol) ---

// dialFrameWS opens the frame-protocol WebSocket, authenticating via the
// bearer WebSocket subprotocol (the browser path; the Authorization-header path is
// covered by TestServeBindingInvoke_FrameRoundTripViaClient).
func dialFrameWS(t *testing.T, ctx context.Context, ts *httptest.Server, token string) *websocket.Conn {
	return dialFrameWSAt(t, ctx, ts, "/bindings/invoke", token)
}

func dialFrameWSAt(t *testing.T, ctx context.Context, ts *httptest.Server, path, token string) *websocket.Conn {
	t.Helper()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + path
	options := &websocket.DialOptions{}
	if token != "" {
		options.Subprotocols = []string{
			wsFrameProtocol,
			wsBearerProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(token)),
		}
	}
	conn, _, err := websocket.Dial(ctx, wsURL, options)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	if token != "" && conn.Subprotocol() != wsFrameProtocol {
		t.Fatalf("selected WebSocket protocol = %q, want %q", conn.Subprotocol(), wsFrameProtocol)
	}
	return conn
}

func TestServeOperationInvoke_WS_UnaryRoundTrip(t *testing.T) {
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()
	ctx := t.Context()
	conn := dialFrameWSAt(t, ctx, ts, "/operations/invoke", "test-token")

	iface := echoOperationInterface()
	sendFrame(t, ctx, conn, map[string]any{
		"kind":  "open",
		"input": map[string]any{"interface": iface, "operation": "echo"},
	})
	sendFrame(t, ctx, conn, map[string]any{"kind": "input", "value": "ping"})
	sendFrame(t, ctx, conn, map[string]any{"kind": "close"})

	outputs, terminal := collectUntilTerminal(t, ctx, conn)
	if terminal.Kind != "complete" {
		t.Fatalf("expected terminal complete, got %q (error=%v)", terminal.Kind, terminal.Error)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %#v", outputs)
	}
	if echo, ok := outputs[0].(map[string]any); !ok || echo["echo"] != "ping" {
		t.Errorf("output = %#v, want {echo: ping}", outputs[0])
	}
}

func TestServeOperationInvoke_WS_BindingAddressed(t *testing.T) {
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()
	ctx := t.Context()
	conn := dialFrameWSAt(t, ctx, ts, "/operations/invoke", "test-token")
	sendFrame(t, ctx, conn, map[string]any{
		"kind":  "open",
		"input": map[string]any{"interface": echoOperationInterface(), "binding": "echo.mock"},
	})
	sendFrame(t, ctx, conn, map[string]any{"kind": "input", "value": "bound"})
	sendFrame(t, ctx, conn, map[string]any{"kind": "close"})
	outputs, terminal := collectUntilTerminal(t, ctx, conn)
	if terminal.Kind != "complete" || len(outputs) != 1 {
		t.Fatalf("binding-addressed invocation = outputs %#v, terminal %#v", outputs, terminal)
	}
	if outputs[0].(map[string]any)["echo"] != "bound" {
		t.Fatalf("binding-addressed output = %#v", outputs[0])
	}
}

func TestServeOperationInvoke_WS_CarriesSameConfigurationAsCLI(t *testing.T) {
	mock := &contextCaptureInvoker{
		formats: []openbindings.BindingSpecInfo{{BindingSpec: "openbindings.graphql@1"}},
		seen:    make(chan map[string]any, 1),
	}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	configuration := map[string]any{
		"document": map[string]any{
			"source":        "query Viewer { viewer { id } }",
			"operationName": "Viewer",
		},
		"protocolFields": map[string]any{
			"httpHeaders": map[string]any{"X-Tenant": "acme"},
		},
	}
	iface := echoOperationInterface()
	iface.Sources["mock"] = openbindings.Source{
		BindingSpec: "openbindings.graphql@1",
		Location:    "https://example.test/graphql",
	}

	ts := testEnv(t)
	defer ts.Close()
	ctx := t.Context()
	conn := dialFrameWSAt(t, ctx, ts, "/operations/invoke", "test-token")
	sendFrame(t, ctx, conn, map[string]any{
		"kind": "open",
		"input": map[string]any{
			"interface": iface,
			"binding":   "echo.mock",
			"context":   map[string]any{"configuration": configuration},
		},
	})
	sendFrame(t, ctx, conn, map[string]any{"kind": "close"})
	_, terminal := collectUntilTerminal(t, ctx, conn)
	if terminal.Kind != "complete" {
		t.Fatalf("terminal = %#v", terminal)
	}

	servedContext := <-mock.seen
	cliContext := (&app.InvokeConfig{Configuration: configuration}).Context()
	servedJSON, _ := json.Marshal(servedContext)
	cliJSON, _ := json.Marshal(cliContext)
	if !bytes.Equal(servedJSON, cliJSON) {
		t.Fatalf("configuration differs across surfaces\nCLI: %s\nAPI: %s", cliJSON, servedJSON)
	}
}

func echoOperationInterface() *openbindings.Interface {
	return &openbindings.Interface{
		OpenBindings: openbindings.MaxTestedVersion,
		Operations: map[string]openbindings.Operation{
			"echo": {
				Input:  map[string]any{"type": "string"},
				Output: map[string]any{"type": "object"},
			},
		},
		Sources: map[string]openbindings.Source{
			"mock": {BindingSpec: "mock-echo@1.0", Location: "mock://test"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"echo.mock": {Operation: "echo", Source: "mock", Selector: "#/echo"},
		},
	}
}

func sendFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, frame map[string]any) {
	t.Helper()
	if err := wsjson.Write(ctx, conn, frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func openFrame(format, location, selector string) map[string]any {
	return map[string]any{
		"kind": "open",
		"input": map[string]any{
			"source":   map[string]any{"bindingSpec": format, "location": location},
			"selector": selector,
		},
	}
}

// wireOutputFrame is the test-side view of a BindingInvokerOutputFrame.
type wireOutputFrame struct {
	Kind  string         `json:"kind"`
	Value any            `json:"value"`
	Error map[string]any `json:"error"`
}

func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) wireOutputFrame {
	t.Helper()
	var frame wireOutputFrame
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return frame
}

// collectUntilTerminal reads frames until the terminal one, returning the
// output values and the terminal frame. Non-terminal input_closed frames are
// tolerated anywhere before the terminal (their timing is inherently racy
// relative to outputs).
func collectUntilTerminal(t *testing.T, ctx context.Context, conn *websocket.Conn) (outputs []any, terminal wireOutputFrame) {
	t.Helper()
	for {
		frame := readFrame(t, ctx, conn)
		switch frame.Kind {
		case "output":
			outputs = append(outputs, frame.Value)
		case "input_closed":
		case "complete", "error":
			return outputs, frame
		default:
			t.Fatalf("unexpected frame kind %q", frame.Kind)
		}
	}
}

func TestServeBindingInvoke_POSTRemoved(t *testing.T) {
	// The legacy unary POST route is gone: the frame endpoint is GET-only.
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/bindings/invoke", "test-token", `{"source":{"bindingSpec":"openbindings.openapi-3.1@1","location":"x"},"selector":"#/paths/~1health/get"}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("POST /bindings/invoke: status = %d, want 405", resp.StatusCode)
	}
}

func TestServeBindingInvoke_NonUpgradeGETRejected(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedGet(ts.URL+"/bindings/invoke", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("plain GET /bindings/invoke: status = %d, want %d", resp.StatusCode, http.StatusUpgradeRequired)
	}
}

func TestServeBindingInvoke_WS_FirstFrameNotOpen(t *testing.T) {
	// Rule 1: any first frame other than `open` is a terminal ERR_FRAME_PROTOCOL.
	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, map[string]any{"kind": "input", "value": 1})

	frame := readFrame(t, ctx, conn)
	if frame.Kind != "error" {
		t.Fatalf("expected terminal error frame, got kind=%q", frame.Kind)
	}
	if frame.Error["code"] != "ERR_FRAME_PROTOCOL" {
		t.Errorf("error code = %v, want ERR_FRAME_PROTOCOL", frame.Error["code"])
	}
}

func TestServeBindingInvoke_WS_SecondOpenRejected(t *testing.T) {
	// Rule 2: exactly one open frame per invocation.
	release := make(chan struct{})
	defer close(release)
	mock := &gatedUnaryInvoker{
		formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-gated@1.0"}},
		release: release,
	}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-gated@1.0", "mock://test", "#/test"))
	sendFrame(t, ctx, conn, openFrame("mock-gated@1.0", "mock://test", "#/test"))

	_, terminal := collectUntilTerminal(t, ctx, conn)
	if terminal.Kind != "error" {
		t.Fatalf("expected terminal error frame, got %q", terminal.Kind)
	}
	if terminal.Error["code"] != "ERR_FRAME_PROTOCOL" {
		t.Errorf("error code = %v, want ERR_FRAME_PROTOCOL", terminal.Error["code"])
	}
}

func TestServeBindingInvoke_WS_UnaryRoundTrip(t *testing.T) {
	// open, input, close -> output, complete: the unary cardinality under
	// the frame protocol.
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-echo@1.0", "mock://test", "#/test"))
	sendFrame(t, ctx, conn, map[string]any{"kind": "input", "value": "ping"})
	sendFrame(t, ctx, conn, map[string]any{"kind": "close"})

	outputs, terminal := collectUntilTerminal(t, ctx, conn)
	if terminal.Kind != "complete" {
		t.Fatalf("expected terminal complete, got %q (error=%v)", terminal.Kind, terminal.Error)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d: %#v", len(outputs), outputs)
	}
	if echo, ok := outputs[0].(map[string]any); !ok || echo["echo"] != "ping" {
		t.Errorf("output = %#v, want {echo: ping}", outputs[0])
	}
}

func TestServeBindingInvoke_WS_LateInputAfterInputClosedIgnored(t *testing.T) {
	// Rule 3: an input frame after the service emitted input_closed is
	// ignored — never written, never a protocol violation — and the
	// invocation continues to completion.
	release := make(chan struct{})
	mock := &gatedUnaryInvoker{
		formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-gated@1.0"}},
		release: release,
	}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-gated@1.0", "mock://test", "#/test"))
	sendFrame(t, ctx, conn, map[string]any{"kind": "input", "value": "wanted"})

	// The binding closes input after its first read: wait for input_closed
	// (the first output may arrive first; both orders are legal).
	var outputs []any
	sawInputClosed := false
	for !sawInputClosed {
		frame := readFrame(t, ctx, conn)
		switch frame.Kind {
		case "input_closed":
			sawInputClosed = true
		case "output":
			outputs = append(outputs, frame.Value)
		default:
			t.Fatalf("unexpected frame kind %q before input_closed", frame.Kind)
		}
	}

	// Late input after input closure: must be ignored, invocation continues.
	sendFrame(t, ctx, conn, map[string]any{"kind": "input", "value": "late"})
	close(release)

	rest, terminal := collectUntilTerminal(t, ctx, conn)
	outputs = append(outputs, rest...)
	if terminal.Kind != "complete" {
		t.Fatalf("expected terminal complete after late input, got %q (error=%v)", terminal.Kind, terminal.Error)
	}
	if len(outputs) != 2 || outputs[0] != "first" || outputs[1] != "second" {
		t.Errorf("outputs = %#v, want [first second]", outputs)
	}
}

func TestServeBindingInvoke_WS_UnknownFramePropertyRejected(t *testing.T) {
	// Rule 7: frame variants declare additionalProperties: false; unknown
	// properties are a terminal ERR_FRAME_PROTOCOL.
	release := make(chan struct{})
	defer close(release)
	mock := &gatedUnaryInvoker{
		formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-gated@1.0"}},
		release: release,
	}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-gated@1.0", "mock://test", "#/test"))
	sendFrame(t, ctx, conn, map[string]any{"kind": "input", "value": 1, "extra": true})

	_, terminal := collectUntilTerminal(t, ctx, conn)
	if terminal.Kind != "error" {
		t.Fatalf("expected terminal error frame, got %q", terminal.Kind)
	}
	if terminal.Error["code"] != "ERR_FRAME_PROTOCOL" {
		t.Errorf("error code = %v, want ERR_FRAME_PROTOCOL", terminal.Error["code"])
	}
}

func TestServeBindingInvoke_WS_UnknownOpenPropertyRejected(t *testing.T) {
	// The open frame's payload is equally strict: the legacy envelope's
	// `input` sibling (operation input on the open message) is gone.
	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, map[string]any{
		"kind": "open",
		"input": map[string]any{
			"source":   map[string]any{"bindingSpec": "openbindings.openapi-3.1@1", "location": "x"},
			"selector": "#/paths/~1test/get",
			"input":    map[string]any{"limit": 10},
		},
	})

	frame := readFrame(t, ctx, conn)
	if frame.Kind != "error" || frame.Error["code"] != "ERR_FRAME_PROTOCOL" {
		t.Fatalf("expected terminal ERR_FRAME_PROTOCOL, got kind=%q error=%v", frame.Kind, frame.Error)
	}
}

func TestServeBindingInvoke_WS_ContextRequiredPassthrough(t *testing.T) {
	// Rule 8: a CONTEXT_REQUIRED terminal passes through as the error frame
	// with its ContextRequiredDetails intact, before any output.
	mock := &contextRequiredInvoker{
		formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-ctx@1.0"}},
		details: &invoke.ContextRequiredDetails{
			Target: "api.example.com",
			Alternatives: []invoke.ContextAlternative{
				{Requirements: []invoke.ContextRequirement{{Type: "auth.bearer"}}},
			},
		},
	}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-ctx@1.0", "mock://test", "#/test"))

	outputs, terminal := collectUntilTerminal(t, ctx, conn)
	if len(outputs) != 0 {
		t.Errorf("expected no outputs before CONTEXT_REQUIRED, got %#v", outputs)
	}
	if terminal.Kind != "error" || terminal.Error["code"] != "CONTEXT_REQUIRED" {
		t.Fatalf("expected terminal CONTEXT_REQUIRED, got kind=%q error=%v", terminal.Kind, terminal.Error)
	}
	details, ok := terminal.Error["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", terminal.Error["data"])
	}
	if details["target"] != "api.example.com" {
		t.Errorf("details.target = %v, want api.example.com", details["target"])
	}
	alts, ok := details["alternatives"].([]any)
	if !ok || len(alts) != 1 {
		t.Fatalf("expected one alternative, got %#v", details["alternatives"])
	}
	reqs := alts[0].(map[string]any)["requirements"].([]any)
	if len(reqs) != 1 || reqs[0].(map[string]any)["type"] != "auth.bearer" {
		t.Errorf("requirements = %#v, want one auth.bearer", reqs)
	}
}

func TestServeBindingInvoke_WS_StreamE2E(t *testing.T) {
	mockInvoker := &mockStreamInvoker{
		formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-stream@1.0"}},
		events:  []any{"event-1", "event-2", "event-3"},
	}
	cleanup := app.OverrideInvokerForTest(
		invoke.NewOperationInvoker(mockInvoker),
	)
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-stream@1.0", "mock://test", "#/test"))

	outputs, terminal := collectUntilTerminal(t, ctx, conn)
	if terminal.Kind != "complete" {
		t.Fatalf("expected terminal complete, got %q (error=%v)", terminal.Kind, terminal.Error)
	}
	if len(outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(outputs))
	}
	for i, want := range []string{"event-1", "event-2", "event-3"} {
		if outputs[i] != want {
			t.Errorf("output[%d] = %v, want %q", i, outputs[i], want)
		}
	}
}

func TestServeBindingInvoke_WS_StreamThenError(t *testing.T) {
	// Outputs are outputs, errors are errors: a stream that emits values and
	// then hits a terminal error surfaces the values as `output` frames
	// followed by a single terminal code-only `error` frame.
	mockInvoker := &errorStreamInvoker{
		formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-stream@1.0"}},
		outputs: []any{map[string]any{"count": 2}},
		err:     invoke.NewInvocationError("ERR_VALIDATION_FAILED"),
	}
	cleanup := app.OverrideInvokerForTest(
		invoke.NewOperationInvoker(mockInvoker),
	)
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-stream@1.0", "mock://test", "#/test"))

	outputs, terminal := collectUntilTerminal(t, ctx, conn)
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output before the error, got %#v", outputs)
	}
	if data, ok := outputs[0].(map[string]any); !ok || data["count"].(float64) != 2 {
		t.Errorf("output = %#v, want {count:2}", outputs[0])
	}
	if terminal.Kind != "error" {
		t.Fatalf("expected terminal error frame, got %q", terminal.Kind)
	}
	if terminal.Error["code"] != "ERR_VALIDATION_FAILED" {
		t.Fatalf("error frame missing or wrong code: %#v", terminal.Error)
	}
	if _, present := terminal.Error["data"]; present {
		t.Fatalf("SDK-local validation evidence crossed as abstract data: %#v", terminal.Error)
	}
}

func TestServeBindingInvoke_WS_NoAuth(t *testing.T) {
	// Without a token on the upgrade request (header or bearer subprotocol),
	// the upgrade is rejected before the WebSocket is accepted.
	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/invoke"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		conn.CloseNow()
		t.Fatal("expected unauthenticated WebSocket dial to fail")
	}
}

func TestServeBindingInvoke_WS_RejectsQueryToken(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/invoke?token=test-token"
	conn, _, err := websocket.Dial(t.Context(), wsURL, nil)
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil {
		t.Fatal("expected URL query token to be rejected")
	}
}

func TestWSAuthTokenRejectsAmbiguousOrMalformedCarriers(t *testing.T) {
	encode := func(token string) string {
		return wsBearerProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(token))
	}
	tests := []struct {
		name          string
		authorization []string
		protocols     []string
		wantToken     string
		wantValid     bool
	}{
		{
			name:          "authorization header",
			authorization: []string{"Bearer header-token"},
			wantToken:     "header-token",
			wantValid:     true,
		},
		{
			name:      "browser credential",
			protocols: []string{wsFrameProtocol, encode("browser-token")},
			wantToken: "browser-token",
			wantValid: true,
		},
		{
			name:          "duplicate authorization headers",
			authorization: []string{"Bearer first", "Bearer second"},
		},
		{
			name:          "malformed authorization cannot fall through",
			authorization: []string{"Basic ignored"},
			protocols:     []string{wsFrameProtocol, encode("browser-token")},
		},
		{
			name:          "authorization plus browser credential",
			authorization: []string{"Bearer header-token"},
			protocols:     []string{wsFrameProtocol, encode("browser-token")},
		},
		{
			name:      "browser credential requires frame protocol",
			protocols: []string{encode("browser-token")},
		},
		{
			name:      "padded base64url is not accepted",
			protocols: []string{wsFrameProtocol, wsBearerProtocolPrefix + "dGVzdA=="},
		},
		{
			name:      "multiple browser credentials across header lines",
			protocols: []string{wsFrameProtocol, encode("first"), encode("second")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/bindings/invoke", nil)
			for _, value := range tt.authorization {
				request.Header.Add("Authorization", value)
			}
			for _, value := range tt.protocols {
				request.Header.Add("Sec-WebSocket-Protocol", value)
			}
			token, valid := wsAuthToken(request)
			if token != tt.wantToken || valid != tt.wantValid {
				t.Fatalf("wsAuthToken() = (%q, %t), want (%q, %t)", token, valid, tt.wantToken, tt.wantValid)
			}
		})
	}
}

func TestServeBindingInvoke_WS_DoesNotEchoCredentialProtocol(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	const token = "test-token"
	credentialProtocol := wsBearerProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(token))
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/invoke"
	conn, resp, err := websocket.Dial(t.Context(), wsURL, &websocket.DialOptions{
		Subprotocols: []string{wsFrameProtocol, credentialProtocol},
	})
	if err != nil {
		t.Fatalf("authenticated WebSocket dial failed: %v", err)
	}
	defer conn.CloseNow()

	if conn.Subprotocol() != wsFrameProtocol {
		t.Fatalf("selected protocol = %q, want %q", conn.Subprotocol(), wsFrameProtocol)
	}
	if resp == nil {
		t.Fatal("authenticated WebSocket dial returned no upgrade response")
	}
	for name, values := range resp.Header {
		for _, value := range values {
			if strings.Contains(value, token) || strings.Contains(value, credentialProtocol) {
				t.Fatalf("response header %s echoed credential material", name)
			}
		}
	}
}

func TestServeBindingInvoke_WS_RejectsMultipleCredentialProtocols(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	encode := func(token string) string {
		return wsBearerProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(token))
	}
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/invoke"
	conn, resp, err := websocket.Dial(t.Context(), wsURL, &websocket.DialOptions{
		Subprotocols: []string{wsFrameProtocol, encode("test-token"), encode("other-token")},
	})
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil {
		t.Fatal("expected ambiguous WebSocket credentials to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("upgrade response = %#v, want 401", resp)
	}
}

func TestServeBindingInvoke_WS_RejectsDisallowedBrowserOrigin(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/invoke"
	header := http.Header{}
	header.Set("Origin", "http://evil.example")
	conn, resp, err := websocket.Dial(t.Context(), wsURL, &websocket.DialOptions{
		HTTPHeader: header,
		Subprotocols: []string{
			wsFrameProtocol,
			wsBearerProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte("test-token")),
		},
	})
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil {
		t.Fatal("expected disallowed WebSocket origin to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("upgrade response = %#v, want 403", resp)
	}
}

func TestServeBindingInvoke_FrameRoundTripViaClient(t *testing.T) {
	// The delegate-side frame client against the serve-side frame server:
	// the full protocol round trip ob uses when delegating to a remote host.
	// Auth rides the upgrade request's Authorization header.
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := t.Context()
	dial := func(ctx context.Context) (*websocket.Conn, *invoke.InvocationError) {
		wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/bindings/invoke"
		header := http.Header{}
		header.Set("Authorization", "Bearer test-token")
		conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			return nil, invoke.NewInvocationError(invoke.ErrCodeConnectFailed)
		}
		return conn, nil
	}

	inv := frames.Invoke(ctx, dial, &frames.BindingInvocationInput{
		Source:   frames.InvokeSource{BindingSpec: "mock-echo@1.0", Location: "mock://test"},
		Selector: "#/test",
	})
	if err := inv.Write(ctx, "ping"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := inv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	out := inv.Outputs()
	v, err := out.Read(ctx)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if echo, ok := v.(map[string]any); !ok || echo["echo"] != "ping" {
		t.Errorf("output = %#v, want {echo: ping}", v)
	}
	if _, err := out.Read(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF, got %v", err)
	}
}

// --- /bindings/prepare ---

func TestServeBindingPrepare_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/bindings/prepare", "test-token", `not json`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServeBindingPrepare_UnknownPropertyRejected(t *testing.T) {
	// BindingInvocationInput declares additionalProperties: false; the legacy
	// unary body's `input` field is rejected.
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/bindings/prepare", "test-token",
		`{"source":{"bindingSpec":"openbindings.openapi-3.1@1","location":"x"},"selector":"#/paths/~1t/get","input":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServeBindingPrepare_AcceptsExtensibleSource(t *testing.T) {
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()
	resp, err := authedPost(ts.URL+"/bindings/prepare", "test-token",
		`{"source":{"bindingSpec":"mock-echo@1.0","location":"mock://test","x-driver":{"mode":"fast"}},"selector":"#/test"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
}

func TestServeBindingPrepare_RequiresSourceCarrier(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()
	resp, err := authedPost(ts.URL+"/bindings/prepare", "test-token",
		`{"source":{"bindingSpec":"openbindings.openapi-3.1@1"},"selector":"#/test"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServeRejectsNonJSONRequestMediaType(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/interfaces", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := mustJSON(t, resp)
	if resp.StatusCode != http.StatusUnsupportedMediaType || body["code"] != "unsupported_media_type" {
		t.Fatalf("status/body = %d %#v, want 415 unsupported_media_type", resp.StatusCode, body)
	}
}

func TestDecodeRequestRejectsOversizedBody(t *testing.T) {
	payload := `"` + strings.Repeat("x", maxRequestBodyBytes) + `"`
	req := httptest.NewRequest(http.MethodPost, "/interfaces", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	var value any
	if decodeRequest(w, req, &value) {
		t.Fatal("oversized request was accepted")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "request_too_large" {
		t.Fatalf("error body = %#v", body)
	}
}

func TestRelativeTrackedSourceRefs(t *testing.T) {
	relative := openbindings.Source{BindingSpec: "openbindings.openapi-3.1@1"}
	if err := app.SetSourceMeta(&relative, app.SourceMeta{Ref: "specs/openapi.yaml"}); err != nil {
		t.Fatal(err)
	}
	absolute := openbindings.Source{BindingSpec: "openbindings.openapi-3.1@1"}
	if err := app.SetSourceMeta(&absolute, app.SourceMeta{Ref: "https://example.com/openapi.yaml"}); err != nil {
		t.Fatal(err)
	}
	iface := &openbindings.Interface{Sources: map[string]openbindings.Source{"relative": relative, "absolute": absolute}}
	if got := relativeTrackedSourceRefs(iface, nil); len(got) != 1 || got[0] != "relative" {
		t.Fatalf("relative refs = %v, want [relative]", got)
	}
	if got := relativeTrackedSourceRefs(iface, []string{"absolute"}); len(got) != 0 {
		t.Fatalf("selected absolute source was rejected: %v", got)
	}
}

func TestServeBindingPrepare_NullForFormatWithoutPreparer(t *testing.T) {
	// A format whose invoker has no BindingPreparer reports null — the
	// conformant "cannot determine statically" answer.
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/bindings/prepare", "test-token",
		`{"source": {"bindingSpec":"mock-echo@1.0","location":"mock://test"},"selector":"#/test"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(body)); got != "null" {
		t.Errorf("body = %q, want null", got)
	}
}

func TestServeOperationPrepare_InlineInterface(t *testing.T) {
	mock := &mockEchoInvoker{formats: []openbindings.BindingSpecInfo{{BindingSpec: "mock-echo@1.0"}}}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()
	body, err := json.Marshal(map[string]any{
		"interface": echoOperationInterface(),
		"operation": "echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := authedPost(ts.URL+"/operations/prepare", "test-token", string(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, payload)
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(payload)) != "null" {
		t.Fatalf("prepareOperation body = %s, want null", payload)
	}
}

func TestServeOperationPrepare_RejectsMalformedInput(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()
	for _, body := range []string{`null`, `{}`, `{"input":{}}`, `{"operation":"echo","unknown":true}`, `{} {}`} {
		t.Run(body, func(t *testing.T) {
			resp, err := authedPost(ts.URL+"/operations/prepare", "test-token", body)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// --- /interfaces/synthesize ---

func TestServeInterfaceSynthesize_InvalidBody(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/interfaces/synthesize", "test-token", `not json`)
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
		"/describe", "/binding-specs", "/delegates", "/environment", "/contexts",
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
		"/bindings/prepare", "/interfaces/synthesize",
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

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/describe", nil)
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
	resp, err = authedGet(ts.URL+"/describe", accessToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("info with OAuth token: status = %d, want 200", resp.StatusCode)
	}

	// 6. Verify invalid token is rejected.
	resp, err = authedGet(ts.URL+"/describe", "totally-invalid-token")
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
		"https://staging.example.net/auth/host-callback",
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
	t.Run("ob.bound.obi.json", func(t *testing.T) {
		iface, err := app.OpenBindingsInterface()
		if err != nil {
			t.Fatalf("failed to parse ob.bound.obi.json: %v", err)
		}
		if iface.Name == "" {
			t.Error("ob.bound.obi.json: name is empty")
		}
		if len(iface.Operations) == 0 {
			t.Error("ob.bound.obi.json: no operations defined")
		}
	})

	t.Run("openapi.yaml", func(t *testing.T) {
		spec := rewriteSpecPlaceholders(server.OpenAPISpec(), "http://localhost:20290")
		if len(spec) == 0 {
			t.Fatal("openapi.yaml is empty")
		}
		var doc struct {
			OpenAPI    string         `yaml:"openapi"`
			Paths      map[string]any `yaml:"paths"`
			Components map[string]any `yaml:"components"`
		}
		if err := yaml.Unmarshal(spec, &doc); err != nil {
			t.Fatalf("openapi.yaml does not parse: %v", err)
		}
		if doc.OpenAPI != "3.1.0" || len(doc.Paths) == 0 || len(doc.Components) == 0 {
			t.Fatalf("openapi.yaml missing required structure: version=%q paths=%d components=%d", doc.OpenAPI, len(doc.Paths), len(doc.Components))
		}
		if bytes.Contains(spec, []byte("${OB_SERVER_")) {
			t.Fatal("rewritten OpenAPI document still contains server placeholders")
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
		{"GET", "/asyncapi.yaml"},
		{"GET", "/bindings/invoke"},
		{"GET", "/operations/invoke"},
		{"GET", "/oauth/authorize"},
		{"POST", "/oauth/authorize"},
		{"POST", "/oauth/token"},
	}
	for _, route := range app.ServeHTTPRoutes() {
		path := route.Path
		path = strings.ReplaceAll(path, "{url}", "https://example.com")
		path = strings.ReplaceAll(path, "{operation}", "test.operation")
		path = strings.ReplaceAll(path, "{capability}", "invoke")
		documented = append(documented, endpoint{strings.ToUpper(route.Method), path})
	}

	for _, ep := range documented {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			var bodyReader io.Reader
			if ep.method == "POST" || ep.method == "PUT" || ep.method == "PATCH" {
				// Deliberately malformed so the route is exercised without allowing
				// stateful endpoints (environment/delegates/contexts) to mutate disk.
				bodyReader = strings.NewReader("not-json")
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

// TestServeBindingInvoke_WS_ClientDisconnectTearsDown is a regression test for
// the WS-disconnect leak: websocket.Accept hijacks the connection, so a client
// disconnect no longer cancels r.Context(). The handler must derive a
// connection-scoped cancellable ctx (via CloseRead) so that disconnecting an
// in-flight stream tears the invocation — and its upstream transport — down.
func TestServeBindingInvoke_WS_ClientDisconnectTearsDown(t *testing.T) {
	torn := make(chan struct{})
	mock := &mockBlockingInvoker{
		formats:  []openbindings.BindingSpecInfo{{BindingSpec: "mock-block@1.0"}},
		tornDown: torn,
	}
	cleanup := app.OverrideInvokerForTest(invoke.NewOperationInvoker(mock))
	defer cleanup()

	ts := testEnv(t)
	defer ts.Close()

	ctx := context.Background()
	conn := dialFrameWS(t, ctx, ts, "test-token")

	sendFrame(t, ctx, conn, openFrame("mock-block@1.0", "mock://test", "#/test"))

	// Read one frame to confirm the stream is live, then abruptly disconnect.
	readFrame(t, ctx, conn)
	_ = conn.CloseNow()

	select {
	case <-torn:
	case <-time.After(5 * time.Second):
		t.Fatal("invocation was not torn down after client disconnect (goroutine + transport leak)")
	}
}
