package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
		Use:   "mcp <url>",
		Short: "Serve an interface URL as an MCP server",
		Long: `Start an MCP (Model Context Protocol) server that exposes one interface's
operations as MCP tools, resources, and prompts. Agents (Cursor, Claude
Desktop, etc.) connect and interact with the underlying service through
OpenBindings. Tool names are the interface's full operation keys, sanitized
to the MCP charset (openbindings.ob.describe → openbindings_ob_describe).

The URL may point at an OBI, a well-known discovery base, or a raw binding
spec (e.g. an OpenAPI document) — non-OBI specs are synthesized into an
interface on the fly.

Resolving the interface itself is unauthenticated (discovery is public by
design). Authentication to the target server for EXECUTING operations can
be provided via --token, --token-file, or the OB_TOKEN environment
variable; the token is supplied as a Bearer credential in every bridged
operation's invocation context.

To expose several services as one MCP server, compose them into a single
aggregate OBI first (e.g. with 'ob merge'), then bridge that one interface —
collisions and naming are resolved deliberately in the composed contract,
not guessed at runtime.

Examples:
  ob mcp https://api.example.com/.well-known/openbindings
  ob mcp --token-file ~/.ob/start.token http://127.0.0.1:20290   # bridge a running 'ob start'
  ob mcp https://api.stripe.com/openapi.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "ob-mcp")
			invoker := app.DefaultInvoker()

			// Resolve token from flag, file, or environment. When present it is
			// supplied as a bearer credential in every bridged operation's
			// invocation context (bindings read credentials from context).
			token, err := resolveToken(tokenFlag, tokenFile)
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			var baseContext map[string]any
			if token != "" {
				baseContext = map[string]any{"bearerToken": token}
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

			normalized := app.NormalizeURL(args[0])
			if normalized == "" {
				return app.ExitResult{Code: 2, Message: fmt.Sprintf("invalid URL: %s", args[0]), ToStderr: true}
			}

			fetched, err := openbindings.FetchInterface(ctx, normalized,
				openbindings.WithSynthesizers(app.DefaultSynthesizer()))
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to resolve interface %s: %v", normalized, err), ToStderr: true}
			}
			iface := fetched.Interface
			if iface == nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("no interface resolved from %s", normalized), ToStderr: true}
			}

			count := mcpbridge.RegisterInterface(mcpServer, iface, invoker, baseContext)
			logger.Info("resolved interface", "url", normalized, "primitives", count)

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

// resolveToken returns a token from flag, file, or OB_TOKEN env var. An
// unreadable --token-file is an error, not a silent fall-through: the caller
// asked for that token, and proceeding without it would fail later with an
// opaque 401.
func resolveToken(flag, file string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading --token-file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return os.Getenv("OB_TOKEN"), nil
}
