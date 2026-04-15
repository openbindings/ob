package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/mcpbridge"
)

func newMCPCmd() *cobra.Command {
	var (
		transport  string
		port       int
		serverName string
		tokenFlag  string
		tokenFile  string
	)

	cmd := &cobra.Command{
		Use:   "mcp <url> [url...]",
		Short: "Serve interface URLs as an MCP server",
		Long: `Start an MCP (Model Context Protocol) server that exposes the given interface
URLs as tools, resources, and prompts. Agents (Cursor, Claude Desktop, etc.)
can connect and interact with any API through OpenBindings.

Authentication to the target server can be provided via --token, --token-file,
or the OB_TOKEN environment variable. The token is used as a Bearer credential
when resolving interfaces and executing operations.

Examples:
  ob mcp https://api.example.com/.well-known/openbindings
  ob mcp --token-file ~/.ob/session-token http://127.0.0.1:20290
  ob mcp https://api.stripe.com/openapi.yaml https://xkcd.com/info.0.json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "ob-mcp")
			executor := app.DefaultExecutor()

			// Resolve token from flag, file, or environment.
			token := resolveToken(tokenFlag, tokenFile)
			if token != "" {
				executor = withBearerToken(executor, token, args)
			}

			if serverName == "" {
				serverName = "ob-mcp"
			}

			mcpServer := mcp.NewServer(&mcp.Implementation{
				Name:    serverName,
				Version: "1.0.0",
			}, nil)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			logger.Info("resolving interfaces", "count", len(args))

			for i, rawURL := range args {
				normalized := app.NormalizeURL(rawURL)
				if normalized == "" {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("invalid URL: %s", rawURL), ToStderr: true}
				}

				ic := openbindings.NewUnboundClient(executor)
				if err := ic.Resolve(ctx, normalized); err != nil {
					logger.Warn("failed to resolve interface", "url", normalized, "error", err)
					continue
				}
				iface := ic.Resolved()
				if iface == nil {
					logger.Warn("no interface resolved", "url", normalized)
					continue
				}

				label := labelFromURL(normalized)
				namespace := mcpbridge.DeriveNamespace(iface, label, fmt.Sprintf("arg-%d", i))
				count := mcpbridge.RegisterInterface(mcpServer, iface, namespace, executor)
				logger.Info("resolved interface", "url", normalized, "namespace", namespace, "primitives", count)
			}

			switch transport {
			case "stdio", "":
				logger.Info("serving MCP via stdio")
				return mcpServer.Run(ctx, &mcp.StdioTransport{})
			case "http":
				addr := fmt.Sprintf(":%d", port)
				handler := mcp.NewStreamableHTTPHandler(
					func(r *http.Request) *mcp.Server { return mcpServer }, nil,
				)
				srv := &http.Server{Addr: addr, Handler: handler}
				go func() {
					<-ctx.Done()
					shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer shutdownCancel()
					srv.Shutdown(shutdownCtx)
				}()
				logger.Info("serving MCP via HTTP", "addr", addr)
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				return nil
			default:
				return app.ExitResult{Code: 2, Message: fmt.Sprintf("unknown transport %q (valid: stdio, http)", transport), ToStderr: true}
			}
		},
	}

	cmd.Flags().StringVar(&transport, "transport", "stdio", "transport: stdio or http")
	cmd.Flags().IntVar(&port, "port", 8080, "HTTP port (when transport is http)")
	cmd.Flags().StringVar(&serverName, "name", "", "MCP server name (default: ob-mcp)")
	cmd.Flags().StringVar(&tokenFlag, "token", "", "Bearer token for authenticating to target servers (also: OB_TOKEN)")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "read Bearer token from file")

	return cmd
}

// resolveToken returns a token from flag, file, or OB_TOKEN env var.
func resolveToken(flag, file string) string {
	if flag != "" {
		return flag
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return os.Getenv("OB_TOKEN")
}

// withBearerToken wraps the executor's context store to overlay Bearer
// credentials for the given URLs. This avoids writing to the persistent
// keychain while making auth available for resolution and execution.
func withBearerToken(executor *openbindings.OperationExecutor, token string, rawURLs []string) *openbindings.OperationExecutor {
	overlay := &tokenOverlayStore{
		inner: executor.ContextStore,
		creds: make(map[string]map[string]any),
	}
	for _, rawURL := range rawURLs {
		normalized := app.NormalizeURL(rawURL)
		if normalized == "" {
			continue
		}
		// The MCP format executor normalizes store keys to host[:port]
		// (scheme and path stripped) via NormalizeContextKey. Match that.
		if parsed, err := url.Parse(normalized); err == nil && parsed.Host != "" {
			overlay.creds[parsed.Host] = map[string]any{"bearerToken": token}
		}
		// Also key by the full and origin URLs for other executors.
		overlay.creds[normalized] = map[string]any{"bearerToken": token}
	}
	return executor.WithRuntime(overlay, executor.PlatformCallbacks)
}

// tokenOverlayStore wraps a ContextStore, overlaying in-memory credentials
// on top of the inner store. Overlay entries take precedence on Get.
// Set and Delete pass through to the inner store.
type tokenOverlayStore struct {
	inner openbindings.ContextStore
	creds map[string]map[string]any
}

func (s *tokenOverlayStore) Get(ctx context.Context, key string) (map[string]any, error) {
	if cred, ok := s.creds[key]; ok {
		return cred, nil
	}
	// Try host-only matching: the MCP executor normalizes keys to
	// host[:port] (scheme stripped). Try extracting just the host.
	if parsed, err := url.Parse(key); err == nil && parsed.Host != "" {
		if cred, ok := s.creds[parsed.Host]; ok {
			return cred, nil
		}
	}
	if s.inner != nil {
		return s.inner.Get(ctx, key)
	}
	return nil, nil
}

func (s *tokenOverlayStore) Set(ctx context.Context, key string, value map[string]any) error {
	if s.inner != nil {
		return s.inner.Set(ctx, key, value)
	}
	return nil
}

func (s *tokenOverlayStore) Delete(ctx context.Context, key string) error {
	if s.inner != nil {
		return s.inner.Delete(ctx, key)
	}
	return nil
}

func labelFromURL(u string) string {
	u = strings.TrimRight(u, "/")
	if idx := strings.LastIndex(u, "/"); idx >= 0 && idx < len(u)-1 {
		seg := u[idx+1:]
		if seg != "openbindings" && seg != "openapi.yaml" && seg != "openapi.json" {
			return seg
		}
	}
	if parsed, err := url.Parse(u); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return u
}
