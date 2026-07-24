package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/spf13/cobra"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/mcpbridge"
)

func newMCPCmd() *cobra.Command {
	var (
		transport       string
		port            int
		serverName      string
		tokenFlag       string
		tokenFile       string
		toolTimeout     time.Duration
		contextArg      string
		configArg       string
		selection       []string
		requireCoverage bool
		coverageReport  string
	)

	cmd := &cobra.Command{
		Use:   "mcp <interface-or-artifact>",
		Short: "Serve an interface or raw artifact as an MCP server",
		Long: `Start an MCP (Model Context Protocol) server that exposes one interface's
operations as MCP tools, resources, and prompts. Agents (Cursor, Claude
Desktop, etc.) connect and interact with the underlying service through
OpenBindings. Ordinary operation tool names are full operation keys sanitized
to the MCP charset (openbindings.ob.describe → openbindings_ob_describe).
Untransformed openbindings.mcp@1 bindings preserve their original MCP
primitive family, identifier, descriptor when pinned, and complete result.
Generic operations return the complete OpenBindings output sequence as
structured content {"outputs":[...]}; inputs without an explicit object schema
use the optional, reversible tool argument envelope {"input":...}.

The URL may point at an OBI, a well-known discovery base, or a raw binding
spec (e.g. an OpenAPI document) — non-OBI specs are synthesized into an
interface on the fly.

When a raw artifact is synthesized, ob reports its durable synthesis coverage.
--require-complete-coverage refuses startup unless the inventory is exhaustive
and fully represented. --coverage-report writes the complete evidence to a
file; stdout remains reserved for the stdio MCP transport.

Resolving the interface itself is unauthenticated (discovery is public by
design). Authentication to the target server for EXECUTING operations can
be provided via --token, --token-file, or the OB_TOKEN environment
variable; the token is supplied as a Bearer credential in every bridged
operation's invocation context.

Use --context with a JSON object or @file for the same context vocabulary
ordinary OpenBindings invocation accepts (headers, cookies, apiKey/apiKeys,
basic, bearerToken, and implementation extensions). --configuration supplies
binding-spec interpretation points, and repeatable --select-binding supplies
the ordered caller choice used by this and nested operations. Conflicting
values across these explicit inputs are refused rather than silently
overwritten.

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
			if err := refuseUnhonoredOutputFlags(cmd, "mcp", "output", "format"); err != nil {
				return err
			}
			if args[0] == app.StdinLocator && (transport == "" || transport == "stdio") {
				return app.ExitResult{
					Code:     2,
					Message:  "ob mcp cannot read its interface from stdin while serving MCP over stdio; use a file/URL locator or --transport http",
					ToStderr: true,
				}
			}
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "ob-mcp")
			invoker := app.DefaultInvoker()

			// Resolve token from flag, file, or environment. When present it is
			// supplied as a bearer credential in every bridged operation's
			// invocation context (bindings read credentials from context).
			token, err := resolveToken(tokenFlag, tokenFile)
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			if contextArg == "-" || configArg == "-" {
				return app.ExitResult{Code: 2, Message: "ob mcp reserves stdin for the MCP transport; use @file for --context and --configuration", ToStderr: true}
			}
			contextValue, err := readJSONObjectArg(contextArg, "--context")
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			configuration, err := readJSONObjectArg(configArg, "--configuration")
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			baseContext, err := buildMCPContext(contextValue, token, configuration, selection)
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
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

			// A locator is a local file path, an http(s) URL, or an exec: ref;
			// each resolves to an OBI, synthesizing one from a raw source
			// (local or remote) when needed. This is why `ob mcp ./api.obi.json`
			// and `ob mcp https://…/openapi.yaml` both work.
			resolved, err := app.ResolveInterfaceDetailed(args[0])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to resolve interface %s: %v", args[0], err), ToStderr: true}
			}
			iface := resolved.Interface
			if iface == nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("no interface resolved from %s", args[0]), ToStderr: true}
			}
			if coverageReport == "-" {
				return app.ExitResult{Code: 2, Message: "--coverage-report cannot write to stdout because the stdio MCP transport owns stdout; provide a file path", ToStderr: true}
			}
			if coverageReport != "" {
				if resolved.Coverage == nil {
					return app.ExitResult{Code: 2, Message: "--coverage-report requested, but resolving this locator did not synthesize a raw artifact", ToStderr: true}
				}
				data, marshalErr := json.MarshalIndent(resolved.Coverage, "", "  ")
				if marshalErr != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("marshal synthesis coverage: %v", marshalErr), ToStderr: true}
				}
				data = append(data, '\n')
				if writeErr := os.WriteFile(coverageReport, data, 0o644); writeErr != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("write synthesis coverage: %v", writeErr), ToStderr: true}
				}
			}
			if resolved.Synthesized {
				if resolved.Coverage == nil {
					logger.Warn("synthesized raw artifact without coverage evidence", "bindingSpec", resolved.SourceBindingSpec)
				} else {
					logger.Info("synthesis coverage",
						"bindingSpec", resolved.SourceBindingSpec,
						"entries", len(resolved.Coverage.Entries),
						"exhaustive", resolved.Coverage.Exhaustive,
						"fullyRepresented", resolved.Coverage.FullyRepresented)
					for _, entry := range resolved.Coverage.Entries {
						if entry.Status != openbindings.SynthesisRepresented {
							logger.Warn("synthesis disposition",
								"sourceRef", entry.SourceRef,
								"scope", entry.Scope,
								"status", entry.Status,
								"reason", entry.Message)
						}
					}
				}
			}
			if coverageErr := requireCompleteMCPCoverage(resolved, requireCoverage); coverageErr != nil {
				return app.ExitResult{Code: 1, Message: coverageErr.Error(), ToStderr: true}
			}

			report := mcpbridge.RegisterInterfaceWithReport(mcpServer, iface, invoker, baseContext,
				mcpbridge.RegisterOptions{ToolDeadline: toolTimeout})
			for _, entry := range report.Entries {
				if entry.Status == "excluded" {
					logger.Warn("operation not advertised", "operation", entry.Operation, "reason", entry.Reason)
				}
			}
			if report.Registered == 0 {
				return app.ExitResult{Code: 1, Message: "the interface has no operations that can be honestly advertised through the installed OpenBindings invokers", ToStderr: true}
			}
			logger.Info("resolved interface", "locator", args[0], "primitives", report.Registered, "excluded", report.Excluded)

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
	cmd.Flags().DurationVar(&toolTimeout, "tool-timeout", mcpbridge.DefaultToolDeadline, "bound on a single bridged tool call (MCP tools are request-scoped; a subscription-style operation cannot complete as a tool)")
	cmd.Flags().StringVar(&contextArg, "context", "", "invocation context JSON object or @file (credentials, headers, cookies, extensions)")
	cmd.Flags().StringVar(&configArg, "configuration", "", "binding-spec configuration JSON object or @file")
	cmd.Flags().StringArrayVar(&selection, "select-binding", nil, "ordered binding choice for this and nested operations (repeatable)")
	cmd.Flags().BoolVar(&requireCoverage, "require-complete-coverage", false, "refuse synthesized raw artifacts unless coverage is exhaustive and fully represented")
	cmd.Flags().StringVar(&coverageReport, "coverage-report", "", "write synthesis coverage evidence to a JSON file")

	return cmd
}

func requireCompleteMCPCoverage(resolved *app.ResolvedInterface, required bool) error {
	if !required || resolved == nil || !resolved.Synthesized {
		return nil
	}
	if resolved.Coverage == nil {
		return fmt.Errorf("complete synthesis coverage was required, but the selected synthesizer produced no coverage evidence")
	}
	if !resolved.Coverage.Exhaustive || !resolved.Coverage.FullyRepresented {
		return fmt.Errorf("complete synthesis coverage was required, but the raw artifact was not exhaustively and fully represented")
	}
	return nil
}

func buildMCPContext(
	contextValue map[string]any,
	bearerToken string,
	configuration map[string]any,
	selection []string,
) (map[string]any, error) {
	out := make(map[string]any, len(contextValue)+1)
	for key, value := range contextValue {
		out[key] = value
	}
	if bearerToken != "" {
		if existing, ok := out["bearerToken"]; ok && existing != bearerToken {
			return nil, fmt.Errorf("--token and --context bearerToken conflict")
		}
		out["bearerToken"] = bearerToken
	}

	existingConfiguration := map[string]any{}
	if existing, ok := out["configuration"]; ok {
		var valid bool
		existingConfiguration, valid = existing.(map[string]any)
		if !valid {
			return nil, fmt.Errorf("--context configuration must be a JSON object")
		}
	}
	mergedConfiguration := make(map[string]any, len(existingConfiguration)+len(configuration)+1)
	for key, value := range existingConfiguration {
		mergedConfiguration[key] = value
	}
	for key, value := range configuration {
		if existing, ok := mergedConfiguration[key]; ok && !reflect.DeepEqual(existing, value) {
			return nil, fmt.Errorf("--configuration member %q conflicts with --context configuration", key)
		}
		mergedConfiguration[key] = value
	}
	if len(selection) > 0 {
		if existing, ok := mergedConfiguration["selection"]; ok && !sameStringSequence(existing, selection) {
			return nil, fmt.Errorf("--select-binding conflicts with --context/--configuration selection")
		}
		mergedConfiguration["selection"] = append([]string(nil), selection...)
	}
	if len(mergedConfiguration) > 0 {
		out["configuration"] = mergedConfiguration
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func sameStringSequence(existing any, expected []string) bool {
	switch values := existing.(type) {
	case []string:
		return reflect.DeepEqual(values, expected)
	case []any:
		if len(values) != len(expected) {
			return false
		}
		for index, value := range values {
			if value != expected[index] {
				return false
			}
		}
		return true
	default:
		return false
	}
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
