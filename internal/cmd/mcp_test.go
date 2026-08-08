package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
	mcpbinding "github.com/openbindings/openbindings-go/formats/mcp"
	openapibinding "github.com/openbindings/openbindings-go/formats/openapi"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/mcpbridge"
)

// TestMCPRoundTrip_Differential is the keystone fidelity test:
//
//	real MCP server -> synthesized OBI -> ob mcp bridge -> real MCP client
//
// It compares the original and bridged capability families and representative
// results. Failures belong to synthesis, the binding implementation, or the
// bridge—not to a server-specific compatibility shim.
func TestMCPRoundTrip_Differential(t *testing.T) {
	upstreamProgressSolicitations := make(chan bool, 2)
	upstreamCancellationStarted := make(chan struct{}, 1)
	upstreamCancellationObserved := make(chan struct{}, 1)
	origin := gomcp.NewServer(&gomcp.Implementation{
		Name: "round-trip-origin", Version: "1.2.3",
	}, nil)

	origin.AddTool(&gomcp.Tool{
		Name:        "weather.lookup",
		Title:       "Weather lookup",
		Description: "Looks up weather",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"city": map[string]any{"type": "string"}},
			"required":   []string{"city"},
		},
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"temperature": map[string]any{"type": "number"}},
			"required":   []string{"temperature"},
		},
	}, func(_ context.Context, req *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var args map[string]any
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &gomcp.CallToolResult{
			Meta: gomcp.Meta{"trace": "tool-1"},
			Content: []gomcp.Content{
				&gomcp.TextContent{Text: "weather for " + args["city"].(string)},
				&gomcp.ImageContent{Data: []byte{0x89, 0x50, 0x4e, 0x47}, MIMEType: "image/png"},
				&gomcp.ResourceLink{URI: "app://forecast/1", Name: "forecast", MIMEType: "application/json"},
			},
			StructuredContent: map[string]any{"temperature": 21.5},
		}, nil
	})

	origin.AddTool(&gomcp.Tool{
		Name: "always.fails", Description: "Returns a protocol-native application error",
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, _ *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		return &gomcp.CallToolResult{
			Meta:    gomcp.Meta{"trace": "error-1"},
			IsError: true,
			Content: []gomcp.Content{
				&gomcp.TextContent{Text: "first diagnostic"},
				&gomcp.TextContent{Text: "second diagnostic"},
			},
		}, nil
	})

	origin.AddTool(&gomcp.Tool{
		Name: "wait.cancel", Description: "Waits until its invocation is cancelled",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, _ *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		upstreamCancellationStarted <- struct{}{}
		select {
		case <-ctx.Done():
			upstreamCancellationObserved <- struct{}{}
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, context.DeadlineExceeded
		}
	})

	origin.AddTool(&gomcp.Tool{
		Name: "with.progress", Description: "Reports progress before its result",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		token := req.Params.GetProgressToken()
		upstreamProgressSolicitations <- token != nil
		if token != nil && req.Session != nil {
			if err := req.Session.NotifyProgress(ctx, &gomcp.ProgressNotificationParams{
				ProgressToken: token, Progress: 1, Total: 2, Message: "halfway",
			}); err != nil {
				return nil, err
			}
			// Keep the result behind the notification long enough for the two
			// Streamable HTTP streams to preserve their semantic order. The
			// binding intentionally discards progress observed after result.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		return &gomcp.CallToolResult{Content: []gomcp.Content{
			&gomcp.TextContent{Text: "complete"},
		}}, nil
	})

	origin.AddResource(&gomcp.Resource{
		Name: "status", Title: "System status", URI: "app://status",
		Description: "Current system status", MIMEType: "application/json",
	}, func(_ context.Context, _ *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		return &gomcp.ReadResourceResult{
			Meta: gomcp.Meta{"trace": "resource-1"},
			Contents: []*gomcp.ResourceContents{
				{URI: "app://status", MIMEType: "application/json", Text: `{"ok":true}`},
			},
		}, nil
	})

	origin.AddResourceTemplate(&gomcp.ResourceTemplate{
		Name: "user", Title: "User profile", URITemplate: "app://users/{id}",
		Description: "A user profile", MIMEType: "application/json",
	}, func(_ context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		return &gomcp.ReadResourceResult{Contents: []*gomcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "application/json", Text: `{"id":"42"}`},
		}}, nil
	})

	origin.AddPrompt(&gomcp.Prompt{
		Name: "greet", Title: "Greeting", Description: "Builds a greeting",
		Arguments: []*gomcp.PromptArgument{
			{Name: "name", Title: "Name", Description: "Who to greet", Required: true},
		},
	}, func(_ context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		return &gomcp.GetPromptResult{
			Meta:        gomcp.Meta{"trace": "prompt-1"},
			Description: "A generated greeting",
			Messages: []*gomcp.PromptMessage{{
				Role: "user", Content: &gomcp.TextContent{Text: "Hello, " + req.Params.Arguments["name"]},
			}},
		}, nil
	})

	originHTTP := httptest.NewServer(gomcp.NewStreamableHTTPHandler(
		func(*http.Request) *gomcp.Server { return origin },
		nil,
	))
	defer originHTTP.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	iface, err := mcpbinding.NewSynthesizer().SynthesizeInterface(ctx, &openbindings.SynthesizeInput{
		Sources: []openbindings.SynthesizeSource{{
			BindingSpec: mcpbinding.LegacyBindingSpec,
			Location:    originHTTP.URL,
			Embed:       true,
		}},
	})
	if err != nil {
		t.Fatalf("synthesize origin: %v", err)
	}
	if got := len(iface.Operations); got != 7 {
		t.Fatalf("synthesized operations = %d, want 7", got)
	}

	mcpInvoker := mcpbinding.NewInvoker(
		mcpbinding.WithClientVersion("round-trip-test"),
		mcpbinding.WithIdleTimeout(10*time.Millisecond),
	)
	defer mcpInvoker.Close()
	roundTripInvoker := openbindings.NewOperationInvoker(mcpInvoker)
	bridged := gomcp.NewServer(&gomcp.Implementation{Name: "round-trip-bridge", Version: "test"}, nil)
	if got := mcpbridge.RegisterInterface(bridged, iface, roundTripInvoker, nil, mcpbridge.RegisterOptions{}); got != 7 {
		t.Fatalf("registered primitives = %d, want 7", got)
	}
	bridgeHTTP := httptest.NewServer(gomcp.NewStreamableHTTPHandler(
		func(*http.Request) *gomcp.Server { return bridged },
		nil,
	))
	defer bridgeHTTP.Close()

	originalProgress := make(chan *gomcp.ProgressNotificationParams, 1)
	bridgedProgress := make(chan *gomcp.ProgressNotificationParams, 1)
	originalSession := connectMCPTestClientWithOptions(t, ctx, originHTTP.URL, &gomcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *gomcp.ProgressNotificationClientRequest) {
			originalProgress <- req.Params
		},
	})
	defer originalSession.Close()
	bridgedSession := connectMCPTestClientWithOptions(t, ctx, bridgeHTTP.URL, &gomcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *gomcp.ProgressNotificationClientRequest) {
			bridgedProgress <- req.Params
		},
	})
	defer bridgedSession.Close()

	originalTools, err := originalSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("origin list tools: %v", err)
	}
	bridgedTools, err := bridgedSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("bridge list tools: %v", err)
	}
	if len(originalTools.Tools) != 4 || len(bridgedTools.Tools) != 4 {
		t.Fatalf("tool counts origin=%d bridge=%d", len(originalTools.Tools), len(bridgedTools.Tools))
	}
	originalWeather := toolByName(t, originalTools.Tools, "weather.lookup")
	bridgedWeather := toolByName(t, bridgedTools.Tools, "weather.lookup")
	if bridgedWeather.Name != originalWeather.Name {
		t.Fatalf("tool identity changed: origin=%q bridge=%q", originalWeather.Name, bridgedWeather.Name)
	}
	if !jsonEquivalent(originalWeather.InputSchema, bridgedWeather.InputSchema) {
		t.Fatalf("tool inputSchema changed:\norigin=%v\nbridge=%v", originalWeather.InputSchema, bridgedWeather.InputSchema)
	}
	if !jsonEquivalent(originalWeather.OutputSchema, bridgedWeather.OutputSchema) {
		t.Fatalf("tool outputSchema changed:\norigin=%v\nbridge=%v", originalWeather.OutputSchema, bridgedWeather.OutputSchema)
	}
	assertMCPJSONEqual(t, "tool descriptor", originalWeather, bridgedWeather)

	originalResources, _ := originalSession.ListResources(ctx, nil)
	bridgedResources, _ := bridgedSession.ListResources(ctx, nil)
	originalTemplates, _ := originalSession.ListResourceTemplates(ctx, nil)
	bridgedTemplates, _ := bridgedSession.ListResourceTemplates(ctx, nil)
	originalPrompts, _ := originalSession.ListPrompts(ctx, nil)
	bridgedPrompts, _ := bridgedSession.ListPrompts(ctx, nil)
	if len(originalResources.Resources) != len(bridgedResources.Resources) ||
		len(originalTemplates.ResourceTemplates) != len(bridgedTemplates.ResourceTemplates) ||
		len(originalPrompts.Prompts) != len(bridgedPrompts.Prompts) {
		t.Fatalf("primitive-family counts changed: resources %d/%d templates %d/%d prompts %d/%d",
			len(originalResources.Resources), len(bridgedResources.Resources),
			len(originalTemplates.ResourceTemplates), len(bridgedTemplates.ResourceTemplates),
			len(originalPrompts.Prompts), len(bridgedPrompts.Prompts))
	}
	assertMCPJSONEqual(t, "resource descriptors", originalResources.Resources, bridgedResources.Resources)
	assertMCPJSONEqual(t, "resource-template descriptors", originalTemplates.ResourceTemplates, bridgedTemplates.ResourceTemplates)
	assertMCPJSONEqual(t, "prompt descriptors", originalPrompts.Prompts, bridgedPrompts.Prompts)

	assertMCPJSONEqual(t, "tool result",
		mustCallTool(t, ctx, originalSession, "weather.lookup", map[string]any{"city": "Paris"}),
		mustCallTool(t, ctx, bridgedSession, "weather.lookup", map[string]any{"city": "Paris"}))
	assertMCPJSONEqual(t, "tool application error",
		mustCallTool(t, ctx, originalSession, "always.fails", map[string]any{}),
		mustCallTool(t, ctx, bridgedSession, "always.fails", map[string]any{}))

	originalProgressResult := mustCallToolWithProgress(t, ctx, originalSession, "with.progress", "origin-progress")
	bridgedProgressResult := mustCallToolWithProgress(t, ctx, bridgedSession, "with.progress", "bridge-progress")
	assertMCPJSONEqual(t, "progress tool result", originalProgressResult, bridgedProgressResult)
	if !<-upstreamProgressSolicitations {
		t.Fatal("origin call did not solicit progress")
	}
	if !<-upstreamProgressSolicitations {
		t.Fatal("bridged call did not solicit progress upstream")
	}
	assertMCPJSONEqual(t, "progress notification",
		mustReceiveProgress(t, originalProgress),
		mustReceiveProgress(t, bridgedProgress))
	assertMCPJSONEqual(t, "static resource result",
		mustReadResource(t, ctx, originalSession, "app://status"),
		mustReadResource(t, ctx, bridgedSession, "app://status"))
	assertMCPJSONEqual(t, "template resource result",
		mustReadResource(t, ctx, originalSession, "app://users/42"),
		mustReadResource(t, ctx, bridgedSession, "app://users/42"))
	assertMCPJSONEqual(t, "prompt result",
		mustGetPrompt(t, ctx, originalSession, "greet", map[string]string{"name": "Ada"}),
		mustGetPrompt(t, ctx, bridgedSession, "greet", map[string]string{"name": "Ada"}))

	cancelCtx, cancelCall := context.WithCancel(ctx)
	cancelled := make(chan error, 1)
	go func() {
		_, err := bridgedSession.CallTool(cancelCtx, &gomcp.CallToolParams{
			Name: "wait.cancel", Arguments: map[string]any{},
		})
		cancelled <- err
	}()
	select {
	case <-upstreamCancellationStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("bridged cancellation tool never reached the upstream server")
	}
	cancelCall()
	select {
	case <-upstreamCancellationObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("downstream cancellation did not reach the upstream MCP tool")
	}
	select {
	case err := <-cancelled:
		if err == nil {
			t.Fatal("cancelled bridged tool call returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled bridged tool call did not terminate")
	}
}

func connectMCPTestClient(t *testing.T, ctx context.Context, endpoint string) *gomcp.ClientSession {
	return connectMCPTestClientWithOptions(t, ctx, endpoint, nil)
}

func connectMCPTestClientWithOptions(t *testing.T, ctx context.Context, endpoint string, opts *gomcp.ClientOptions) *gomcp.ClientSession {
	t.Helper()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "round-trip-test", Version: "test"}, opts)
	session, err := client.Connect(ctx, &gomcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect MCP client to %s: %v", endpoint, err)
	}
	return session
}

func toolByName(t *testing.T, tools []*gomcp.Tool, name string) *gomcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func mustCallTool(t *testing.T, ctx context.Context, session *gomcp.ClientSession, name string, args any) *gomcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call tool %q: %v", name, err)
	}
	return result
}

func mustCallToolWithProgress(t *testing.T, ctx context.Context, session *gomcp.ClientSession, name string, token any) *gomcp.CallToolResult {
	t.Helper()
	params := &gomcp.CallToolParams{Name: name, Arguments: map[string]any{}}
	params.SetProgressToken(token)
	result, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("call progress tool %q: %v", name, err)
	}
	return result
}

func mustReceiveProgress(t *testing.T, values <-chan *gomcp.ProgressNotificationParams) *gomcp.ProgressNotificationParams {
	t.Helper()
	select {
	case value := <-values:
		// Tokens are correlation-local and intentionally differ between the two
		// calls; compare the represented progress payload.
		value.ProgressToken = nil
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for progress notification")
		return nil
	}
}

func mustReadResource(t *testing.T, ctx context.Context, session *gomcp.ClientSession, uri string) *gomcp.ReadResourceResult {
	t.Helper()
	result, err := session.ReadResource(ctx, &gomcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read resource %q: %v", uri, err)
	}
	return result
}

func mustGetPrompt(t *testing.T, ctx context.Context, session *gomcp.ClientSession, name string, args map[string]string) *gomcp.GetPromptResult {
	t.Helper()
	result, err := session.GetPrompt(ctx, &gomcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("get prompt %q: %v", name, err)
	}
	return result
}

func assertMCPJSONEqual(t *testing.T, label string, want, got any) {
	t.Helper()
	if !jsonEquivalent(want, got) {
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		t.Fatalf("%s changed:\norigin=%s\nbridge=%s", label, wantJSON, gotJSON)
	}
}

func jsonEquivalent(a, b any) bool {
	normalize := func(value any) any {
		data, _ := json.Marshal(value)
		var decoded any
		_ = json.Unmarshal(data, &decoded)
		return decoded
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}

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
// schema mapping, invoker wiring) would only surface to end users.
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
			"mock": {BindingSpec: "x-mock"},
		},
	}

	// Use a mock invoker that just echoes the input back as output.
	invoker := openbindings.NewOperationInvoker(&echoMockInvoker{})

	// Build the MCP server the same way internal/cmd/mcp.go does.
	mcpServer := gomcp.NewServer(&gomcp.Implementation{
		Name:    "ob-mcp-test",
		Version: "1.0.0",
	}, nil)

	count := mcpbridge.RegisterInterface(mcpServer, iface, invoker, nil, mcpbridge.RegisterOptions{})
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
	wantToolName := "echo"
	foundTool := false
	for _, tool := range listResult.Tools {
		if tool.Name == wantToolName {
			foundTool = true
			outputSchema, ok := tool.OutputSchema.(map[string]any)
			if !ok || outputSchema["type"] != "object" {
				t.Fatalf("generic tool has no object output sequence schema: %#v", tool.OutputSchema)
			}
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
	// invoker dispatch, and result serialization.
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

	// The mock invoker echoes one input as one OpenBindings output. The generic
	// adapter preserves cardinality explicitly in both MCP result lanes.
	if len(callResult.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := callResult.Content[0].(*gomcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", callResult.Content[0])
	}
	// The text content is JSON-encoded output from the invoker; just verify
	// the message round-tripped through.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\ntext: %s", err, tc.Text)
	}
	outputs, ok := decoded["outputs"].([]any)
	if !ok || len(outputs) != 1 {
		t.Fatalf("output sequence = %#v, want one output", decoded["outputs"])
	}
	output := outputs[0].(map[string]any)
	if output["message"] != "hello world" {
		t.Errorf("output message = %v, want \"hello world\"", output["message"])
	}
	structured := callResult.StructuredContent.(map[string]any)
	if got := structured["outputs"].([]any)[0].(map[string]any)["message"]; got != "hello world" {
		t.Errorf("structured output message = %v, want hello world", got)
	}
}

// TestMCPGenericProjection_BindingFamilyNeutral proves that the generic bridge
// adapts the OBI boundary rather than branching on the originating artifact
// family. Family implementations own the concrete hop; once they emit the
// same OpenBindings value, the MCP projection is identical.
func TestMCPGenericProjection_BindingFamilyNeutral(t *testing.T) {
	families := []string{
		"openbindings.openapi@1",
		"openbindings.graphql@2",
		"openbindings.graphql@1",
		"openbindings.grpc@1",
		"openbindings.connect@1",
		"openbindings.asyncapi@1",
		"openbindings.usage@1",
		"openbindings.operation-graph@1",
	}
	bindingInvoker := &familyEchoInvoker{families: families}
	invoker := openbindings.NewOperationInvoker(bindingInvoker)
	input := map[string]any{"message": "same boundary"}

	for _, family := range families {
		t.Run(family, func(t *testing.T) {
			iface := &openbindings.Interface{
				OpenBindings: "0.2.0",
				Name:         "family-neutral",
				Operations: map[string]openbindings.Operation{
					"echo": {
						Input: map[string]any{
							"type":       "object",
							"properties": map[string]any{"message": map[string]any{"type": "string"}},
						},
						Output: map[string]any{
							"type":       "object",
							"properties": map[string]any{"message": map[string]any{"type": "string"}},
						},
					},
				},
				Sources: map[string]openbindings.Source{
					"source": {BindingSpec: family, Location: "https://example.invalid"},
				},
				Bindings: map[string]openbindings.BindingEntry{
					"echo.source": {Operation: "echo", Source: "source", Ref: "echo"},
				},
			}

			directCall := openbindings.Invoke(context.Background(), invoker, iface,
				openbindings.NewOperationSignature[any, any]("echo"))
			if err := directCall.Write(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			_ = directCall.Close()
			directOutput, err := directCall.Outputs().Read(context.Background())
			if err != nil {
				t.Fatalf("direct output: %v", err)
			}

			server := gomcp.NewServer(&gomcp.Implementation{Name: "family-neutral", Version: "test"}, nil)
			report := mcpbridge.RegisterInterfaceWithReport(server, iface, invoker, nil, mcpbridge.RegisterOptions{})
			if report.Registered != 1 {
				t.Fatalf("registration report: %#v", report)
			}
			httpServer := httptest.NewServer(gomcp.NewStreamableHTTPHandler(
				func(*http.Request) *gomcp.Server { return server },
				nil,
			))
			defer httpServer.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session := connectMCPTestClient(t, ctx, httpServer.URL)
			defer session.Close()
			result := mustCallTool(t, ctx, session, "echo", input)
			if result.IsError {
				t.Fatalf("bridge returned error: %#v", result)
			}
			envelope := result.StructuredContent.(map[string]any)
			outputs := envelope["outputs"].([]any)
			if len(outputs) != 1 || !jsonEquivalent(outputs[0], directOutput) {
				t.Fatalf("direct/MCP boundary mismatch: direct=%#v MCP=%#v", directOutput, outputs)
			}
		})
	}
}

// TestMCPGenericProjection_OpenAPISynthesizedDifferential joins the layered
// proof with one real concrete family:
//
//	OpenAPI artifact -> synthesized OBI -> OpenAPI invocation -> generic MCP
//
// The family-neutral table above proves the bridge has no per-family branch;
// this test proves the abstract boundary agrees with an actual synthesized
// and invoked upstream operation.
func TestMCPGenericProjection_OpenAPISynthesizedDifferential(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/pets/42" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42","name":"Rex"}`))
	}))
	defer upstream.Close()

	artifact := `{
	  "openapi": "3.1.0",
	  "info": {"title": "Pets", "version": "1"},
	  "servers": [{"url": "` + upstream.URL + `"}],
	  "paths": {
	    "/pets/{id}": {
	      "get": {
	        "operationId": "getPet",
	        "parameters": [{
	          "name": "id",
	          "in": "path",
	          "required": true,
	          "schema": {"type": "string"}
	        }],
	        "responses": {
	          "200": {
	            "description": "pet",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "id": {"type": "string"},
	                    "name": {"type": "string"}
	                  },
	                  "required": ["id", "name"]
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	synthesized, err := openapibinding.NewSynthesizer().SynthesizeInterfaceWithCoverage(
		context.Background(),
		&openbindings.SynthesizeInput{Sources: []openbindings.SynthesizeSource{{
			BindingSpec: openapibinding.BindingSpec,
			Content:     openbindings.TextContent(artifact),
		}}},
	)
	if err != nil {
		t.Fatalf("synthesize OpenAPI: %v", err)
	}
	if !synthesized.Coverage.Exhaustive || !synthesized.Coverage.FullyRepresented {
		t.Fatalf("unexpected synthesis coverage: %#v", synthesized.Coverage)
	}

	invoker := openbindings.NewOperationInvoker(openapibinding.NewInvoker())
	input := map[string]any{"id": "42"}
	direct := openbindings.Invoke(context.Background(), invoker, synthesized.Interface,
		openbindings.NewOperationSignature[any, any]("getPet"))
	if err := direct.Write(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	_ = direct.Close()
	directOutput, err := openbindings.Single(context.Background(), direct.Outputs())
	if err != nil {
		t.Fatalf("direct OpenAPI invocation: %v", err)
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "openapi-differential", Version: "test"}, nil)
	report := mcpbridge.RegisterInterfaceWithReport(
		server, synthesized.Interface, invoker, nil, mcpbridge.RegisterOptions{},
	)
	if report.Registered != 1 || report.Excluded != 0 {
		t.Fatalf("registration report: %#v", report)
	}
	bridgeHTTP := httptest.NewServer(gomcp.NewStreamableHTTPHandler(
		func(*http.Request) *gomcp.Server { return server },
		nil,
	))
	defer bridgeHTTP.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session := connectMCPTestClient(t, ctx, bridgeHTTP.URL)
	defer session.Close()
	result := mustCallTool(t, ctx, session, "getPet", input)
	if result.IsError {
		t.Fatalf("bridge returned error: %#v", result)
	}
	outputs := result.StructuredContent.(map[string]any)["outputs"].([]any)
	if len(outputs) != 1 || !jsonEquivalent(outputs[0], directOutput) {
		t.Fatalf("direct/MCP mismatch: direct=%#v MCP=%#v", directOutput, outputs)
	}
}

func TestBuildMCPContext_ComposesGenericInvocationContext(t *testing.T) {
	got, err := buildMCPContext(
		map[string]any{
			"headers": map[string]any{"X-Tenant": "acme"},
			"configuration": map[string]any{
				"server": "primary",
			},
		},
		"secret",
		map[string]any{"document": "query { viewer { id } }"},
		[]string{"viewer.graphql"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got["bearerToken"] != "secret" || got["headers"] == nil {
		t.Fatalf("credential context changed: %#v", got)
	}
	configuration := got["configuration"].(map[string]any)
	if configuration["server"] != "primary" || configuration["document"] == nil {
		t.Fatalf("configuration did not merge: %#v", configuration)
	}
	if selection, ok := configuration["selection"].([]string); !ok || len(selection) != 1 || selection[0] != "viewer.graphql" {
		t.Fatalf("selection did not merge: %#v", configuration["selection"])
	}
}

func TestBuildMCPContext_RefusesConflictingIntent(t *testing.T) {
	if _, err := buildMCPContext(map[string]any{"bearerToken": "a"}, "b", nil, nil); err == nil {
		t.Fatal("conflicting bearer tokens must refuse")
	}
	if _, err := buildMCPContext(
		map[string]any{"configuration": map[string]any{"server": "a"}},
		"",
		map[string]any{"server": "b"},
		nil,
	); err == nil {
		t.Fatal("conflicting configuration must refuse")
	}
}

func TestRequireCompleteMCPCoverage(t *testing.T) {
	complete := &openbindings.SynthesisCoverage{Exhaustive: true, FullyRepresented: true}
	incomplete := &openbindings.SynthesisCoverage{Exhaustive: true, FullyRepresented: false}
	tests := []struct {
		name     string
		resolved *app.ResolvedInterface
		required bool
		wantErr  bool
	}{
		{name: "not required", resolved: &app.ResolvedInterface{Synthesized: true}, required: false},
		{name: "native OBI", resolved: &app.ResolvedInterface{}, required: true},
		{name: "synthesized without evidence", resolved: &app.ResolvedInterface{Synthesized: true}, required: true, wantErr: true},
		{name: "incomplete evidence", resolved: &app.ResolvedInterface{Synthesized: true, Coverage: incomplete}, required: true, wantErr: true},
		{name: "complete evidence", resolved: &app.ResolvedInterface{Synthesized: true, Coverage: complete}, required: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := requireCompleteMCPCoverage(test.resolved, test.required)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestMCPCommand_LocalRawArtifactWritesCompleteCoverage(t *testing.T) {
	t.Setenv("OB_TOKEN", "")
	reportPath := t.TempDir() + "/coverage.json"
	cmd := newMCPCmd()
	cmd.SetArgs([]string{
		"--transport", "invalid-after-setup",
		"--require-complete-coverage",
		"--coverage-report", reportPath,
		"../../testdata/petstore-mini.json",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown transport") {
		t.Fatalf("command did not reach post-registration transport selection: %v", err)
	}
	data, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatalf("coverage report was not written: %v", readErr)
	}
	var coverage openbindings.SynthesisCoverage
	if err := json.Unmarshal(data, &coverage); err != nil {
		t.Fatalf("coverage report is not valid evidence JSON: %v", err)
	}
	if !coverage.Exhaustive || !coverage.FullyRepresented || len(coverage.Entries) == 0 {
		t.Fatalf("unexpected coverage report: %#v", coverage)
	}
}

func TestMCPCommand_RefusesStdinLocatorOnStdio(t *testing.T) {
	cmd := newMCPCmd()
	cmd.SetArgs([]string{"-"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "interface from stdin") {
		t.Fatalf("stdio ownership conflict was not refused before reading stdin: %v", err)
	}
}

// TestObStartServedInterfaceBridgesToMCP proves the dogfood that lets ob start
// drop its bespoke MCP endpoint: ob's own served interface, fetched live from a
// running server, maps onto MCP via the same RegisterInterface bridge ob offers
// for any interface. `ob mcp <ob-url>` is therefore the MCP surface — no
// hand-written tools, automatic parity with the REST surface.
func TestObStartServedInterfaceBridgesToMCP(t *testing.T) {
	ts := testEnv(t)
	defer ts.Close()

	fetched, err := openbindings.FetchInterface(context.Background(), ts.URL+"/.well-known/openbindings")
	if err != nil {
		t.Fatalf("fetch ob's served OBI: %v", err)
	}
	if fetched.Interface == nil {
		t.Fatal("no interface resolved from ob start")
	}

	srv := gomcp.NewServer(&gomcp.Implementation{Name: "ob", Version: "test"}, nil)
	count := mcpbridge.RegisterInterface(srv, fetched.Interface, app.DefaultInvoker(),
		map[string]any{"bearerToken": "test-token"}, mcpbridge.RegisterOptions{})

	if count == 0 {
		t.Fatal("bridge registered no MCP primitives from ob's served interface")
	}
	if count != len(fetched.Interface.Operations) {
		t.Errorf("registered %d primitives, want one per served operation (%d)", count, len(fetched.Interface.Operations))
	}
}

// echoMockInvoker is a BindingInvoker stub that echoes its input back as
// the output. Used by TestMCPCommand_BridgesInterfaceToTools to drive the
// real mcpbridge → invoker flow without depending on a network protocol.
type echoMockInvoker struct{}

func (e *echoMockInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: "x-mock", Description: "echo mock"}}
}

type familyEchoInvoker struct {
	families []string
}

func (e *familyEchoInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	out := make([]openbindings.BindingSpecInfo, 0, len(e.families))
	for _, family := range e.families {
		out = append(out, openbindings.BindingSpecInfo{BindingSpec: family})
	}
	return out
}

func (e *familyEchoInvoker) InvokeBinding(ctx context.Context, args *openbindings.BindingInvocationArgs) openbindings.Invocation[any, any] {
	return (&echoMockInvoker{}).InvokeBinding(ctx, args)
}

func (e *echoMockInvoker) InvokeBinding(ctx context.Context, args *openbindings.BindingInvocationArgs) openbindings.Invocation[any, any] {
	inv := openbindings.NewInvocationImpl[any, any](ctx)
	go func() {
		// Echo the first input message back as the single output.
		first, err := inv.ReadInput(ctx)
		_ = inv.CloseInput()
		if err != nil {
			// No input written: echo an empty object.
			first = map[string]any{}
		}
		if err := inv.EmitOutput(first); err != nil {
			return
		}
		inv.CloseOutput()
	}()
	return inv
}
