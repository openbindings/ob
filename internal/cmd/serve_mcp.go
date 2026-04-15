package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/server"
)

// registerMCPEndpoint creates a native MCP server for ob serve's own interface
// and mounts it at /mcp. The handlers call app functions directly, same pattern
// as the HTTP handlers. No executor, no binding resolution.
func registerMCPEndpoint(srv *server.Server, logger *slog.Logger) {
	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "ob",
		Version: app.Info().Version,
	}, nil)

	registerMCPTools(mcpSrv, logger)
	registerMCPResources(mcpSrv)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return mcpSrv }, nil,
	)

	srv.Mux().Handle("/mcp", handler)
	srv.Mux().Handle("/mcp/", handler)

	logger.Info("MCP endpoint registered", "path", "/mcp")
}

func registerMCPTools(srv *mcp.Server, logger *slog.Logger) {
	srv.AddTool(&mcp.Tool{
		Name:        "getInfo",
		Description: "Return identity and metadata about this host.",
		InputSchema: emptyObject(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(app.Info())
	})

	srv.AddTool(&mcp.Tool{
		Name:        "listFormats",
		Description: "List binding formats supported by this host.",
		InputSchema: emptyObject(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(app.ListFormats())
	})

	srv.AddTool(&mcp.Tool{
		Name:        "createInterface",
		Description: "Create an OpenBindings interface from binding source artifacts.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"openbindingsVersion": map[string]any{"type": "string"},
				"sources": map[string]any{
					"type": "array",
					"items": map[string]any{"type": "object"},
				},
				"name":        map[string]any{"type": "string"},
				"version":     map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input app.CreateInterfaceInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		iface, err := app.CreateInterface(input)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(iface)
	})

	srv.AddTool(&mcp.Tool{
		Name:        "executeBinding",
		Description: "Execute a resolved binding.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{"type": "object"},
				"ref":    map[string]any{"type": "string"},
				"input":  map[string]any{},
				"context": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
				"options": map[string]any{"type": "object"},
			},
			"required": []string{"source", "ref"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input app.ExecuteOperationInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		result := app.ExecuteOperationWithContext(ctx, input)
		if result.Error != nil {
			return errorResult(result.Error.Message), nil
		}
		return jsonResult(result)
	})

	srv.AddTool(&mcp.Tool{
		Name:        "listContexts",
		Description: "List all stored context entries.",
		InputSchema: emptyObject(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		contexts, err := app.ListContexts()
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(contexts)
	})

	srv.AddTool(&mcp.Tool{
		Name:        "getContext",
		Description: "Get a stored context entry by key.",
		InputSchema: map[string]any{
			"type":     "object",
			"properties": map[string]any{"key": map[string]any{"type": "string"}},
			"required": []string{"key"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		summary, err := app.GetContextSummary(input.Key)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(summary)
	})

	srv.AddTool(&mcp.Tool{
		Name:        "setContext",
		Description: "Set or update a context entry.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":     map[string]any{"type": "string"},
				"context": map[string]any{"type": "object", "additionalProperties": true},
			},
			"required": []string{"key", "context"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Key     string         `json:"key"`
			Context map[string]any `json:"context"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		if input.Key == "" {
			return errorResult("key is required"), nil
		}

		cfg := app.ContextConfig{}
		if h, ok := input.Context["headers"].(map[string]any); ok {
			cfg.Headers = toStringMap(h)
		}
		if c, ok := input.Context["cookies"].(map[string]any); ok {
			cfg.Cookies = toStringMap(c)
		}
		if e, ok := input.Context["environment"].(map[string]any); ok {
			cfg.Environment = toStringMap(e)
		}
		if m, ok := input.Context["metadata"].(map[string]any); ok {
			cfg.Metadata = m
		}
		if err := app.SaveContextConfig(input.Key, cfg); err != nil {
			return errorResult(err.Error()), nil
		}

		cred := map[string]any{}
		if bt, ok := input.Context["bearerToken"].(string); ok && bt != "" {
			cred["bearerToken"] = bt
		}
		if ak, ok := input.Context["apiKey"].(string); ok && ak != "" {
			cred["apiKey"] = ak
		}
		if len(cred) > 0 {
			if err := app.SaveContextCredentials(input.Key, cred); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		return jsonResult(map[string]string{"key": input.Key, "status": "updated"})
	})

	srv.AddTool(&mcp.Tool{
		Name:        "deleteContext",
		Description: "Delete a stored context entry.",
		InputSchema: map[string]any{
			"type":     "object",
			"properties": map[string]any{"key": map[string]any{"type": "string"}},
			"required": []string{"key"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		if err := app.DeleteContext(input.Key); err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(map[string]string{"key": input.Key, "status": "deleted"})
	})

	srv.AddTool(&mcp.Tool{
		Name:        "resolveInterface",
		Description: "Resolve an OpenBindings interface from a URL.",
		InputSchema: map[string]any{
			"type":     "object",
			"properties": map[string]any{"url": map[string]any{"type": "string"}},
			"required": []string{"url"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		if input.URL == "" {
			return errorResult("url is required"), nil
		}
		result := app.ProbeOBI(input.URL, 15*time.Second)
		if result.Status != "ok" {
			return errorResult("failed to resolve interface: " + result.Detail), nil
		}
		var iface any
		if err := json.Unmarshal([]byte(result.OBI), &iface); err != nil {
			return errorResult("remote interface returned invalid JSON"), nil
		}
		return jsonResult(map[string]any{
			"interface":   iface,
			"url":         result.OBIURL,
			"finalUrl":    result.FinalURL,
			"synthesized": result.Synthesized,
		})
	})

	srv.AddTool(&mcp.Tool{
		Name:        "request",
		Description: "Make an HTTP request and return the complete response.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":              map[string]any{"type": "string"},
				"method":           map[string]any{"type": "string"},
				"headers":          map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				"body":             map[string]any{"type": "string"},
				"timeoutMs":        map[string]any{"type": "integer"},
				"maxResponseBytes": map[string]any{"type": "integer"},
				"followRedirects":  map[string]any{"type": "boolean"},
			},
			"required": []string{"url"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input httpRequestInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		if input.URL == "" {
			return errorResult("url is required"), nil
		}
		if !strings.HasPrefix(input.URL, "http://") && !strings.HasPrefix(input.URL, "https://") {
			return errorResult("url must use http or https scheme"), nil
		}

		method := input.Method
		if method == "" {
			method = http.MethodGet
		}
		timeout := defaultHTTPClientTimeout
		if input.TimeoutMs > 0 {
			timeout = time.Duration(input.TimeoutMs) * time.Millisecond
			if timeout > maxHTTPClientTimeout {
				timeout = maxHTTPClientTimeout
			}
		}
		maxBytes := defaultMaxHTTPResponseBytes
		if input.MaxResponseBytes > 0 && input.MaxResponseBytes < defaultMaxHTTPResponseBytes {
			maxBytes = input.MaxResponseBytes
		}
		followRedirects := true
		if input.FollowRedirects != nil {
			followRedirects = *input.FollowRedirects
		}

		var bodyReader io.Reader
		if input.Body != "" {
			bodyReader = strings.NewReader(input.Body)
		}
		httpReq, err := http.NewRequestWithContext(ctx, method, input.URL, bodyReader)
		if err != nil {
			return errorResult(fmt.Sprintf("invalid request: %v", err)), nil
		}
		for k, v := range input.Headers {
			httpReq.Header.Set(k, v)
		}

		client := &http.Client{Timeout: timeout}
		if !followRedirects {
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}

		logger.Info("mcp http/request", "method", method, "url", input.URL)
		resp, err := client.Do(httpReq)
		if err != nil {
			return errorResult("request failed: " + err.Error()), nil
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
		if err != nil {
			return errorResult("failed to read response: " + err.Error()), nil
		}
		if len(respBody) > maxBytes {
			return errorResult(fmt.Sprintf("response exceeds %d byte limit", maxBytes)), nil
		}

		respHeaders := make(map[string]string)
		for k := range resp.Header {
			respHeaders[strings.ToLower(k)] = resp.Header.Get(k)
		}
		finalURL := ""
		if resp.Request != nil && resp.Request.URL.String() != input.URL {
			finalURL = resp.Request.URL.String()
		}

		return jsonResult(httpResponse{
			Status:  resp.StatusCode,
			Headers: respHeaders,
			Body:    string(respBody),
			URL:     finalURL,
		})
	})
}

func registerMCPResources(srv *mcp.Server) {
	specFiles := []struct {
		filename string
		uri      string
		name     string
		desc     string
		mime     string
	}{
		{
			filename: "quick-reference.md",
			uri:      "openbindings://spec/quick-reference.md",
			name:     "Quick reference",
			desc:     "Concise OBI cheat sheet: operations, bindings, sources, common ref formats, transform syntax.",
			mime:     "text/markdown",
		},
	}

	for _, sf := range specFiles {
		captured := sf
		srv.AddResource(&mcp.Resource{
			URI:         captured.uri,
			Name:        captured.name,
			Description: captured.desc,
			MIMEType:    captured.mime,
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			content, err := server.SpecResource(captured.filename)
			if err != nil {
				return nil, fmt.Errorf("spec resource not found: %s", captured.filename)
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      captured.uri,
					MIMEType: captured.mime,
					Text:     string(content),
				}},
			}, nil
		})
	}
}

// --- helpers ---

func emptyObject() any {
	return map[string]any{"type": "object"}
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func toStringMap(m map[string]any) map[string]string {
	result := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}
