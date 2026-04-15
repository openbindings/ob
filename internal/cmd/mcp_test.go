package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/mcpbridge"
)

// TestMCPCommand_BridgesInterfaceToTools is the end-to-end test for the
// dual-surface keystone command. It exercises the same wiring `ob mcp <url>`
// uses internally — interface resolution → mcpbridge.MapInterfaceToTools →
// gomcp server registration → real MCP tool call — without subprocess
// management. Connecting to the gomcp server through the StreamableHTTP
// transport (the same transport `ob mcp --transport http` exposes) verifies
// the full flow: tool listing, schema marshalling, argument unmarshalling,
// handler invocation, and result serialization.
//
// This is the test the audit asked for: it pins down that an OBI consumed by
// `ob mcp` actually produces a working MCP server that an MCP client can
// call. Without it, regressions in the bridge (operation name munging,
// schema mapping, executor wiring) would only surface to end users.
func TestMCPCommand_BridgesInterfaceToTools(t *testing.T) {
	// Build a small in-memory interface with one operation.
	iface := &openbindings.Interface{
		OpenBindings: "0.1.0",
		Name:         "test-svc",
		Operations: map[string]openbindings.Operation{
			"echo": {
				Description: "Echoes a message",
				Input: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"message": map[string]any{"type": "string"},
					},
					"required": []string{"message"},
				},
				Output: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"echo": map[string]any{"type": "string"},
					},
				},
			},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"echo.mock": {Operation: "echo", Source: "mock", Ref: "test"},
		},
		Sources: map[string]openbindings.Source{
			"mock": {Format: "x-mock"},
		},
	}

	// Use a mock executor that just echoes the input back as output.
	exec := openbindings.NewOperationExecutor(&echoMockExecutor{})

	// Build the MCP server the same way internal/cmd/mcp.go does.
	mcpServer := gomcp.NewServer(&gomcp.Implementation{
		Name:    "ob-mcp-test",
		Version: "1.0.0",
	}, nil)

	namespace := mcpbridge.DeriveNamespace(iface, "test", "fallback")
	count := mcpbridge.RegisterInterface(mcpServer, iface, namespace, exec)
	if count != 1 {
		t.Fatalf("expected 1 registered primitive, got %d", count)
	}

	// Host the MCP server over StreamableHTTP — same transport as
	// `ob mcp --transport http`.
	handler := gomcp.NewStreamableHTTPHandler(
		func(r *http.Request) *gomcp.Server { return mcpServer }, nil,
	)
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()

	// Connect a real MCP client to the server.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "ob-mcp-test-client",
		Version: "1.0.0",
	}, nil)
	transport := &gomcp.StreamableClientTransport{Endpoint: httpSrv.URL}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	defer session.Close()

	// List tools — must include the bridged operation.
	listResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	wantToolName := namespace + ".echo"
	foundTool := false
	for _, tool := range listResult.Tools {
		if tool.Name == wantToolName {
			foundTool = true
			break
		}
	}
	if !foundTool {
		var names []string
		for _, tool := range listResult.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("tool %q not in ListTools result; got %v", wantToolName, names)
	}

	// Call the tool — verifies argument marshalling, handler invocation,
	// executor dispatch, and result serialization.
	callResult, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      wantToolName,
		Arguments: map[string]any{"message": "hello world"},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if callResult.IsError {
		// Surface the error content for debugging.
		var msgs []string
		for _, c := range callResult.Content {
			if tc, ok := c.(*gomcp.TextContent); ok {
				msgs = append(msgs, tc.Text)
			}
		}
		t.Fatalf("tool returned IsError=true: %v", msgs)
	}

	// The mock executor echoes the input as the output. Verify the bridge
	// surfaces it back through the MCP content channel.
	if len(callResult.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := callResult.Content[0].(*gomcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", callResult.Content[0])
	}
	// The text content is JSON-encoded output from the executor; just verify
	// the message round-tripped through.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\ntext: %s", err, tc.Text)
	}
	if decoded["message"] != "hello world" {
		t.Errorf("output message = %v, want \"hello world\"", decoded["message"])
	}
}

// echoMockExecutor is a BindingExecutor stub that echoes its input back as
// the output. Used by TestMCPCommand_BridgesInterfaceToTools to drive the
// real mcpbridge → executor flow without depending on a network protocol.
type echoMockExecutor struct{}

func (e *echoMockExecutor) Formats() []openbindings.FormatInfo {
	return []openbindings.FormatInfo{{Token: "x-mock", Description: "echo mock"}}
}

func (e *echoMockExecutor) ExecuteBinding(_ context.Context, in *openbindings.BindingExecutionInput) (<-chan openbindings.StreamEvent, error) {
	ch := make(chan openbindings.StreamEvent, 1)
	ch <- openbindings.StreamEvent{
		Data:   in.Input,
		Status: 200,
	}
	close(ch)
	return ch, nil
}
