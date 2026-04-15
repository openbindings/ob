// Package mcpbridge maps OBI interfaces to MCP servers.
package mcpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
)

// RegisterInterface maps an OBI's operations to MCP primitives on the given
// server. Operations with MCP bindings are registered as the correct primitive
// type based on binding ref prefix (tools/, resources/, prompts/). Operations
// without MCP bindings are registered as tools.
//
// Returns the number of primitives registered.
func RegisterInterface(
	srv *mcp.Server,
	iface *openbindings.Interface,
	namespace string,
	executor *openbindings.OperationExecutor,
) int {
	count := 0
	for opKey, op := range iface.Operations {
		ref, kind := findMCPBinding(iface, opKey)
		toolName := namespace + "." + opKey

		switch kind {
		case "resources":
			registerResource(srv, toolName, op, iface, opKey, ref, executor)
		case "prompts":
			registerPrompt(srv, toolName, op, iface, opKey, ref, executor)
		default:
			registerTool(srv, toolName, op, iface, opKey, executor)
		}
		count++
	}
	return count
}

// findMCPBinding looks for an MCP binding for the given operation and returns
// the ref value and the entity kind (tools, resources, prompts). If no MCP
// binding exists, returns ("", "tools").
func findMCPBinding(iface *openbindings.Interface, opKey string) (ref string, kind string) {
	for _, be := range iface.Bindings {
		if be.Operation != opKey {
			continue
		}
		src, ok := iface.Sources[be.Source]
		if !ok {
			continue
		}
		if !strings.HasPrefix(src.Format, "mcp") {
			continue
		}
		// Found an MCP binding. Parse the ref prefix.
		for _, prefix := range []string{"resources/", "prompts/", "tools/"} {
			if strings.HasPrefix(be.Ref, prefix) {
				return be.Ref, strings.TrimSuffix(prefix, "/")
			}
		}
		return be.Ref, "tools"
	}
	return "", "tools"
}

func registerTool(
	srv *mcp.Server,
	toolName string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	executor *openbindings.OperationExecutor,
) {
	srv.AddTool(&mcp.Tool{
		Name:        toolName,
		Description: op.Description,
		InputSchema: buildInputSchema(op.Input),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "invalid arguments: " + err.Error()}},
				}, nil
			}
		}

		ch, err := executor.ExecuteOperation(ctx, &openbindings.OperationExecutionInput{
			Interface: iface,
			Operation: opKey,
			Input:     input,
		})
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil
		}

		var lastData any
		for ev := range ch {
			if ev.Error != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: ev.Error.Message}},
				}, nil
			}
			lastData = ev.Data
		}

		data, err := json.Marshal(lastData)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to marshal output: %v", err)}},
			}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil
	})
}

func registerResource(
	srv *mcp.Server,
	name string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	ref string,
	executor *openbindings.OperationExecutor,
) {
	uri := strings.TrimPrefix(ref, "resources/")

	srv.AddResource(&mcp.Resource{
		URI:         uri,
		Name:        name,
		Description: op.Description,
		MIMEType:    guessMIME(uri),
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		ch, err := executor.ExecuteOperation(ctx, &openbindings.OperationExecutionInput{
			Interface: iface,
			Operation: opKey,
			Input:     map[string]any{"uri": req.Params.URI},
		})
		if err != nil {
			return nil, err
		}

		var lastData any
		for ev := range ch {
			if ev.Error != nil {
				return nil, fmt.Errorf("%s: %s", ev.Error.Code, ev.Error.Message)
			}
			lastData = ev.Data
		}

		text := ""
		switch v := lastData.(type) {
		case string:
			text = v
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			text = string(b)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: guessMIME(req.Params.URI),
				Text:     text,
			}},
		}, nil
	})
}

func registerPrompt(
	srv *mcp.Server,
	name string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	ref string,
	executor *openbindings.OperationExecutor,
) {
	promptName := strings.TrimPrefix(ref, "prompts/")

	var args []*mcp.PromptArgument
	if props, ok := op.Input["properties"].(map[string]any); ok {
		for k := range props {
			args = append(args, &mcp.PromptArgument{Name: k})
		}
	}

	srv.AddPrompt(&mcp.Prompt{
		Name:        promptName,
		Description: op.Description,
		Arguments:   args,
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		var input any
		if len(req.Params.Arguments) > 0 {
			m := make(map[string]any, len(req.Params.Arguments))
			for k, v := range req.Params.Arguments {
				m[k] = v
			}
			input = m
		}

		ch, err := executor.ExecuteOperation(ctx, &openbindings.OperationExecutionInput{
			Interface: iface,
			Operation: opKey,
			Input:     input,
		})
		if err != nil {
			return nil, err
		}

		var lastData any
		for ev := range ch {
			if ev.Error != nil {
				return nil, fmt.Errorf("%s: %s", ev.Error.Code, ev.Error.Message)
			}
			lastData = ev.Data
		}

		// The executor returns the prompt result as an object with
		// "messages" and optional "description".
		result := &mcp.GetPromptResult{}
		b, _ := json.Marshal(lastData)
		json.Unmarshal(b, result)
		return result, nil
	})
}

func guessMIME(uri string) string {
	switch {
	case strings.HasSuffix(uri, ".json"):
		return "application/json"
	case strings.HasSuffix(uri, ".md"):
		return "text/markdown"
	case strings.HasSuffix(uri, ".yaml"), strings.HasSuffix(uri, ".yml"):
		return "text/yaml"
	default:
		return "text/plain"
	}
}

// DeriveNamespace determines the namespace for primitives from an interface and
// optional label. Priority: interface Name > label > fallback.
func DeriveNamespace(iface *openbindings.Interface, label, fallback string) string {
	if iface.Name != "" {
		return iface.Name
	}
	if label != "" {
		return label
	}
	return fallback
}

func buildInputSchema(input openbindings.JSONSchema) any {
	if len(input) == 0 {
		return map[string]any{"type": "object"}
	}
	if _, ok := input["type"]; ok {
		return map[string]any(input)
	}
	schema := make(map[string]any, len(input)+1)
	for k, v := range input {
		schema[k] = v
	}
	schema["type"] = "object"
	return schema
}
