package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/server"
)

// registerMCPEndpoint creates a native MCP server for ob serve's own interface
// and mounts it at /mcp. The handlers call app functions directly, same pattern
// as the HTTP handlers. No operation invoker, no binding resolution.
func registerMCPEndpoint(srv *server.Server, logger *slog.Logger) {
	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "ob",
		Version: app.Info().Version,
	}, nil)

	registerMCPTools(mcpSrv)
	registerMCPResources(mcpSrv)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return mcpSrv }, nil,
	)

	srv.Mux().Handle("/mcp", handler)
	srv.Mux().Handle("/mcp/", handler)

	logger.Info("MCP endpoint registered", "path", "/mcp")
}

func registerMCPTools(srv *mcp.Server) {
	srv.AddTool(&mcp.Tool{
		Name:        "describe",
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
		Name:        "synthesizeInterface",
		Description: "Synthesize an OpenBindings interface from binding source artifacts.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"openbindingsVersion": map[string]any{"type": "string"},
				"sources": map[string]any{
					"type":  "array",
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
		Name:        "invokeBinding",
		Description: "Invoke a resolved binding.",
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
		var input app.InvokeOperationInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		result := app.InvokeOperationWithContext(ctx, input)
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
		Description: "Get the stored context for a key. Returns the unified context payload (credentials, headers, cookies, env, metadata in one opaque map), or null if no context exists for the key.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"key": map[string]any{"type": "string"}},
			"required":   []string{"key"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		payload, err := app.LoadContext(input.Key)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(payload)
	})

	srv.AddTool(&mcp.Tool{
		Name:        "setContext",
		Description: "Create or replace the context for a key. The supplied value fully replaces any existing context (full replacement, not partial update).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":   map[string]any{"type": "string"},
				"value": map[string]any{"type": "object", "additionalProperties": true},
			},
			"required": []string{"key", "value"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Key   string         `json:"key"`
			Value map[string]any `json:"value"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil
		}
		if input.Key == "" {
			return errorResult("key is required"), nil
		}
		if err := app.SaveUnifiedContext(input.Key, input.Value); err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(map[string]string{"key": input.Key, "status": "updated"})
	})

	srv.AddTool(&mcp.Tool{
		Name:        "deleteContext",
		Description: "Delete a stored context entry.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"key": map[string]any{"type": "string"}},
			"required":   []string{"key"},
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
			"type":       "object",
			"properties": map[string]any{"url": map[string]any{"type": "string"}},
			"required":   []string{"url"},
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
			"interface":    iface,
			"url":          result.OBIURL,
			"finalUrl":     result.FinalURL,
			"synthesized":  result.Synthesized,
			"sourceFormat": result.SourceFormat,
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
