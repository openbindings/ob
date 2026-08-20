package mcpbridge

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openbindings/openbindings-go/invoke"
)

// readMCPResource forwards an expanded resources/read URI to the source MCP
// server. A resource-template binding cannot recover the original RFC 6570
// variable object from every expanded URI, so this deliberately narrow
// same-protocol lane avoids inventing a reverse-template convention.
func readMCPResource(ctx context.Context, location, uri string, bindContext map[string]any) (*mcp.ReadResourceResult, error) {
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") {
		return nil, fmt.Errorf("MCP resource-template source must be an HTTP or HTTPS URL, got %q", location)
	}

	headers, err := mcpHTTPHeaders(bindContext)
	if err != nil {
		return nil, err
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint: location,
		HTTPClient: &http.Client{
			Transport: &headerTransport{base: http.DefaultTransport, headers: headers},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ob-mcp-bridge", Version: "0.2.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to MCP resource-template source: %w", err)
	}
	defer func() { _ = session.Close() }()

	init := session.InitializeResult()
	if init == nil || init.ProtocolVersion != "2025-11-25" {
		negotiated := ""
		if init != nil {
			negotiated = init.ProtocolVersion
		}
		return nil, fmt.Errorf("MCP negotiated protocol revision %q; openbindings.mcp@1 accepts only 2025-11-25", negotiated)
	}
	return session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
}

func mcpHTTPHeaders(bindContext map[string]any) (map[string]string, error) {
	if invoke.ContextAPIKey(bindContext) != "" {
		return nil, fmt.Errorf("MCP does not declare a carrier for a generic credential; provide a named HTTP header credential")
	}
	if _, _, ok := invoke.ContextBasicAuth(bindContext); ok {
		return nil, fmt.Errorf("MCP does not declare a carrier for a generic credential; provide a named HTTP header credential")
	}

	headers := map[string]string{}
	if token := invoke.ContextBearerToken(bindContext); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	for name, value := range invoke.ContextHeaders(bindContext) {
		if invoke.ContextBearerToken(bindContext) != "" && strings.EqualFold(name, "Authorization") {
			return nil, fmt.Errorf("bearerToken and an explicit Authorization header target the same MCP credential destination")
		}
		if strings.EqualFold(name, "MCP-Session-Id") || strings.EqualFold(name, "MCP-Protocol-Version") {
			return nil, fmt.Errorf("credential header %q collides with an MCP processor-owned session field", name)
		}
		headers[name] = value
	}
	if cookies := invoke.ContextCookies(bindContext); len(cookies) > 0 {
		names := make([]string, 0, len(cookies))
		for name := range cookies {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, name+"="+cookies[name])
		}
		headers["Cookie"] = strings.Join(parts, "; ")
	}
	return headers, nil
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	for name, value := range t.headers {
		clone.Header.Set(name, value)
	}
	return t.base.RoundTrip(clone)
}
