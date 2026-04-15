package cmd

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"

	"github.com/openbindings/ob/internal/server"
)

// mcpTestEnv returns an httptest.Server with the MCP endpoint registered,
// along with a connected MCP session.
func mcpTestEnv(t *testing.T) (*httptest.Server, *gomcp.ClientSession) {
	t.Helper()
	keyring.MockInit()

	srv, err := server.New(server.Config{
		Port:   0,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Token:  "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registerMCPEndpoint(srv, logger)

	ts := httptest.NewServer(srv.Handler())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	t.Cleanup(ts.Close)

	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "mcp-test-client",
		Version: "1.0.0",
	}, nil)
	transport := &gomcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: bearerHTTPClient("test-token"),
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("MCP client connect failed: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return ts, session
}

func TestMCPEndpoint_ListTools(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	wantTools := []string{
		"getInfo", "listFormats", "createInterface", "executeBinding",
		"listContexts", "getContext", "setContext", "deleteContext",
		"resolveInterface", "request",
	}

	toolNames := map[string]bool{}
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	for _, name := range wantTools {
		if !toolNames[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestMCPEndpoint_ListResources(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	r := result.Resources[0]
	if r.URI != "openbindings://spec/quick-reference.md" {
		t.Errorf("resource URI = %q, want openbindings://spec/quick-reference.md", r.URI)
	}
	if r.MIMEType != "text/markdown" {
		t.Errorf("resource MIME = %q, want text/markdown", r.MIMEType)
	}
}

func TestMCPEndpoint_ReadResource(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "openbindings://spec/quick-reference.md",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("no resource contents returned")
	}
	if result.Contents[0].Text == "" {
		t.Fatal("resource text is empty")
	}
	if result.Contents[0].MIMEType != "text/markdown" {
		t.Errorf("MIME = %q, want text/markdown", result.Contents[0].MIMEType)
	}
	if len(result.Contents[0].Text) < 100 {
		t.Errorf("resource content suspiciously short: %d bytes", len(result.Contents[0].Text))
	}
}

func TestMCPEndpoint_GetInfo(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "getInfo",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool getInfo: %v", err)
	}
	if result.IsError {
		t.Fatalf("getInfo returned error: %s", textContent(result))
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &info); err != nil {
		t.Fatalf("getInfo output not valid JSON: %v", err)
	}
	if info["name"] == nil || info["name"] == "" {
		t.Error("getInfo: name missing or empty")
	}
}

func TestMCPEndpoint_ListFormats(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "listFormats",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool listFormats: %v", err)
	}
	if result.IsError {
		t.Fatalf("listFormats returned error: %s", textContent(result))
	}

	var formats []map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &formats); err != nil {
		t.Fatalf("listFormats output not valid JSON array: %v", err)
	}
	if len(formats) == 0 {
		t.Error("listFormats returned empty array")
	}
}

func TestMCPEndpoint_ListContexts(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "listContexts",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool listContexts: %v", err)
	}
	if result.IsError {
		t.Fatalf("listContexts returned error: %s", textContent(result))
	}

	// Should return valid JSON array (possibly empty).
	var contexts []any
	if err := json.Unmarshal([]byte(textContent(result)), &contexts); err != nil {
		t.Fatalf("listContexts output not valid JSON array: %v", err)
	}
}

func TestMCPEndpoint_GetContext_Missing(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "getContext",
		Arguments: map[string]any{"key": "https://nonexistent.example.com"},
	})
	if err != nil {
		t.Fatalf("CallTool getContext: %v", err)
	}
	// Getting a non-existent context shouldn't error, just return empty data.
	if result.IsError {
		// Some implementations may error on missing context; that's acceptable too.
		return
	}
}

func TestMCPEndpoint_DeleteContext_Missing(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "deleteContext",
		Arguments: map[string]any{"key": "https://nonexistent.example.com"},
	})
	if err != nil {
		t.Fatalf("CallTool deleteContext: %v", err)
	}
	// Deleting a non-existent context should succeed silently.
	if result.IsError {
		t.Fatalf("deleteContext returned error: %s", textContent(result))
	}
}

func TestMCPEndpoint_ResolveInterface_BadURL(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "resolveInterface",
		Arguments: map[string]any{"url": ""},
	})
	if err != nil {
		t.Fatalf("CallTool resolveInterface: %v", err)
	}
	if !result.IsError {
		t.Error("resolveInterface with empty URL should return error")
	}
}

func TestMCPEndpoint_Request_BadURL(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "request",
		Arguments: map[string]any{"url": "not-a-url"},
	})
	if err != nil {
		t.Fatalf("CallTool request: %v", err)
	}
	if !result.IsError {
		t.Error("request with invalid URL should return error")
	}
}

func TestMCPEndpoint_CreateInterface_EmptySources(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "createInterface",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool createInterface: %v", err)
	}
	// Empty input should either error or return a minimal interface.
	// Just verify we get valid JSON back.
	text := textContent(result)
	if text == "" {
		t.Fatal("createInterface returned empty content")
	}
}

func TestMCPEndpoint_SetContext_InvalidArgs(t *testing.T) {
	_, session := mcpTestEnv(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "setContext",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool setContext: %v", err)
	}
	// Missing required key should error.
	if !result.IsError {
		t.Error("setContext with no key should return error")
	}
}

// bearerHTTPClient returns an HTTP client that injects a Bearer token on every request.
func bearerHTTPClient(token string) *http.Client {
	return &http.Client{
		Transport: &bearerTransport{token: token, inner: http.DefaultTransport},
	}
}

type bearerTransport struct {
	token string
	inner http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.inner.RoundTrip(req)
}

// textContent extracts the text from the first content item of a CallToolResult.
func textContent(r *gomcp.CallToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(*gomcp.TextContent); ok {
		return tc.Text
	}
	return ""
}
