package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/spf13/cobra"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/server"
)

const maxRequestBodyBytes = 2 << 20 // 2 MiB

// DefaultServePort is the default TCP port for `ob serve`.
// It equals 0x4F42 (decimal 20290): the big-endian pair of ASCII 'O' (0x4F) and 'B' (0x42),
// a mnemonic for OpenBindings. High enough to avoid common dev-server collisions.
const DefaultServePort = 0x4F42

func newServeCmd() *cobra.Command {
	var (
		port           int
		allowedOrigins []string
		tokenFlag      string
		tokenFile      string
		noTLS          bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start a local HTTP server exposing ob operations",
		Long: `Start a local HTTP/REST server that exposes ob's full capability surface.
Authorized clients can execute operations, browse interfaces,
and manage contexts through the same operations available via the CLI.

A session token is generated on startup and printed to the terminal.
Clients must present it as "Authorization: Bearer <token>" on every request.
The server binds to 127.0.0.1 only — never exposed to the network.

The token can be provided via --token flag or OB_SERVE_TOKEN environment variable
to enable stable tokens for CI/CD and automation. When provided, the token is not
printed to stderr (the caller already knows it).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "ob-serve")
			slog.SetDefault(logger)

			tokenProvided := false
			resolvedToken := tokenFlag
			if resolvedToken == "" {
				resolvedToken = os.Getenv("OB_SERVE_TOKEN")
			}
			if resolvedToken != "" {
				tokenProvided = true
			}

			if !cmd.Flags().Changed("port") {
				if envPort := os.Getenv("OB_SERVE_PORT"); envPort != "" {
					p, err := parsePort(envPort)
					if err != nil {
						return app.ExitResult{Code: 2, Message: fmt.Sprintf("invalid OB_SERVE_PORT=%q: %v", envPort, err), ToStderr: true}
					}
					port = p
				}
			}

			if len(allowedOrigins) == 0 {
				if envOrigins := os.Getenv("OB_SERVE_ORIGINS"); envOrigins != "" {
					allowedOrigins = strings.Split(envOrigins, ",")
					for i := range allowedOrigins {
						allowedOrigins[i] = strings.TrimSpace(allowedOrigins[i])
					}
				}
			}

			srv, err := server.New(server.Config{
				Port:           port,
				AllowedOrigins: allowedOrigins,
				Logger:         logger,
				Token:          resolvedToken,
				TLS:            !noTLS,
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			actualToken := srv.Token()
			if tokenFile != "" {
				if err := os.WriteFile(tokenFile, []byte(actualToken+"\n"), 0600); err != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("writing token file: %v", err), ToStderr: true}
				}
				fmt.Fprintf(os.Stderr, "session token written to %s\n", tokenFile)
			} else if !tokenProvided {
				if term.IsTerminal(int(os.Stderr.Fd())) {
					fmt.Fprintf(os.Stderr, "session token: %s\n", actualToken)
				} else {
					fmt.Fprintf(os.Stderr, "session token: %s...\n", actualToken[:8])
				}
			}

			oauthSt := newOAuthStore()
			registerRoutes(srv, logger, port, oauthSt)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			go func() {
				ticker := time.NewTicker(5 * time.Minute)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						oauthSt.cleanup()
					case <-ctx.Done():
						return
					}
				}
			}()

			if err := srv.ListenAndServe(ctx); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", DefaultServePort, `port to listen on (default 20290 = 0x4F42, ASCII "OB")`)
	cmd.Flags().StringArrayVar(&allowedOrigins, "allow-origin", nil, "allowed CORS origin (repeatable)")
	cmd.Flags().StringVar(&tokenFlag, "token", "", "pre-shared session token (also: OB_SERVE_TOKEN env var)")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "write session token to file instead of stderr")
	cmd.Flags().BoolVar(&noTLS, "no-tls", false, "skip HTTPS listener and CA trust setup (HTTP only, no sudo prompt)")

	return cmd
}

func parsePort(s string) (int, error) {
	var p int
	_, err := fmt.Sscanf(s, "%d", &p)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("invalid port: %s", s)
	}
	return p, nil
}

func registerRoutes(srv *server.Server, logger *slog.Logger, port int, oauthSt *oauthStore) {
	mux := srv.Mux()

	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /{$}", handleOBI(port))
	mux.HandleFunc("GET /.well-known/openbindings", handleOBI(port))
	mux.HandleFunc("GET /openapi.yaml", handleOpenAPISpec(port))
	mux.HandleFunc("GET /asyncapi.yaml", handleAsyncAPISpec(port))
	mux.HandleFunc("GET /info", handleInfo)
	mux.HandleFunc("GET /formats", handleFormats)
	mux.HandleFunc("GET /delegates", handleDelegates)

	mux.HandleFunc("GET /status", handleStatus)

	mux.HandleFunc("GET /contexts", handleContextList)
	mux.HandleFunc("GET /contexts/{url...}", handleContextGet)
	mux.HandleFunc("PUT /contexts/{url...}", handleContextSet)
	mux.HandleFunc("DELETE /contexts/{url...}", handleContextDelete)

	mux.HandleFunc("POST /resolve", handleResolve)

	mux.HandleFunc("GET /spec/{name...}", handleSpecResource)

	registerOAuthRoutes(srv, oauthSt, logger)
	registerBindingRoutes(srv, logger)
	registerAuthoringRoutes(srv)
	registerMCPEndpoint(srv, logger)
}

// --- Health ---

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- OBI / Info / Formats / Delegates ---

func handleOBI(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Serve the host OBI (not the CLI OBI). The host OBI conforms to
		// openbindings.host role.
		raw := server.HostOBI()
		var iface map[string]any
		if err := json.Unmarshal(raw, &iface); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		baseURL := deriveBaseURL(r, port)

		// Rewrite source locations to point at this server.
		if sources, ok := iface["sources"].(map[string]any); ok {
			if openapi, ok := sources["openapi"].(map[string]any); ok {
				openapi["location"] = baseURL + "/openapi.yaml"
			}
			if asyncapi, ok := sources["asyncapi"].(map[string]any); ok {
				asyncapi["location"] = baseURL + "/asyncapi.yaml"
			}
			if mcpSource, ok := sources["mcp"].(map[string]any); ok {
				mcpSource["location"] = baseURL + "/mcp"
			}
		}

		// Resolve relative URLs in security methods against the server's base URL.
		if security, ok := iface["security"].(map[string]any); ok {
			for _, entry := range security {
				methods, ok := entry.([]any)
				if !ok {
					continue
				}
				for _, m := range methods {
					method, ok := m.(map[string]any)
					if !ok {
						continue
					}
					for _, field := range []string{"authorizeUrl", "tokenUrl"} {
						if v, ok := method[field].(string); ok && len(v) > 0 && v[0] == '/' {
							method[field] = baseURL + v
						}
					}
				}
			}
		}

		writeJSON(w, http.StatusOK, iface)
	}
}

func handleOpenAPISpec(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		spec := rewriteSpecPlaceholders(server.OpenAPISpec(), deriveBaseURL(r, port))
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(spec)
	}
}

func handleAsyncAPISpec(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		spec := rewriteSpecPlaceholders(server.AsyncAPISpec(), deriveBaseURL(r, port))
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(spec)
	}
}

// rewriteSpecPlaceholders replaces server placeholders in embedded spec files.
// Specs use ${OB_SERVER_URL}, ${OB_SERVER_HOST}, and ${OB_SERVER_PROTOCOL}
// instead of hardcoded addresses so they reflect the actual runtime config.
func rewriteSpecPlaceholders(spec []byte, baseURL string) []byte {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		// baseURL comes from deriveBaseURL which always produces a valid URL,
		// but return the spec with placeholders stripped rather than serving
		// broken placeholder syntax.
		spec = bytes.ReplaceAll(spec, []byte("${OB_SERVER_URL}"), []byte(baseURL))
		spec = bytes.ReplaceAll(spec, []byte("${OB_SERVER_HOST}"), nil)
		spec = bytes.ReplaceAll(spec, []byte("${OB_SERVER_PROTOCOL}"), []byte("ws"))
		return spec
	}
	host := u.Host
	wsProto := "ws"
	if u.Scheme == "https" {
		wsProto = "wss"
	}
	spec = bytes.ReplaceAll(spec, []byte("${OB_SERVER_URL}"), []byte(baseURL))
	spec = bytes.ReplaceAll(spec, []byte("${OB_SERVER_HOST}"), []byte(host))
	spec = bytes.ReplaceAll(spec, []byte("${OB_SERVER_PROTOCOL}"), []byte(wsProto))
	return spec
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, app.Info())
}

func handleFormats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, app.ListFormats())
}

func handleDelegates(w http.ResponseWriter, r *http.Request) {
	output := app.BuildDelegateListOutput(app.DelegateListParams{})
	if output.Error != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: output.Error.Message})
		return
	}
	writeJSON(w, http.StatusOK, output.Delegates)
}

// --- Spec Resources ---

func handleSpecResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name path parameter required"})
		return
	}

	content, err := server.SpecResource(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "spec resource not found"})
		return
	}

	mime := "text/markdown; charset=utf-8"
	if strings.HasSuffix(name, ".json") {
		mime = "application/json"
	}
	w.Header().Set("Content-Type", mime)
	w.WriteHeader(http.StatusOK)
	w.Write(content)
}

// --- Status ---

func handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := app.GetEnvironmentStatus()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// --- Context ---

func handleContextList(w http.ResponseWriter, r *http.Request) {
	summaries, err := app.ListContexts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

func handleContextGet(w http.ResponseWriter, r *http.Request) {
	targetURL := r.PathValue("url")
	if targetURL == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url path parameter required"})
		return
	}

	summary, err := app.GetContextSummary(targetURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func handleContextSet(w http.ResponseWriter, r *http.Request) {
	targetURL := r.PathValue("url")
	if targetURL == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url path parameter required"})
		return
	}

	var body struct {
		Headers     map[string]string `json:"headers,omitempty"`
		Cookies     map[string]string `json:"cookies,omitempty"`
		Environment map[string]string `json:"environment,omitempty"`
		Metadata    map[string]any    `json:"metadata,omitempty"`
		BearerToken string            `json:"bearerToken,omitempty"`
		APIKey      string            `json:"apiKey,omitempty"`
	}
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}
	}

	cfg := app.ContextConfig{
		Headers:     body.Headers,
		Cookies:     body.Cookies,
		Environment: body.Environment,
		Metadata:    body.Metadata,
	}
	if err := app.SaveContextConfig(targetURL, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	if body.BearerToken != "" || body.APIKey != "" {
		cred := map[string]any{}
		if body.BearerToken != "" {
			cred["bearerToken"] = body.BearerToken
		}
		if body.APIKey != "" {
			cred["apiKey"] = body.APIKey
		}
		if err := app.SaveContextCredentials(targetURL, cred); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": targetURL, "status": "updated"})
}

func handleContextDelete(w http.ResponseWriter, r *http.Request) {
	targetURL := r.PathValue("url")
	if targetURL == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url path parameter required"})
		return
	}

	if err := app.DeleteContext(targetURL); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": targetURL, "status": "deleted"})
}

// --- Resolve (with SSRF protection) ---

func handleResolve(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if body.URL == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url is required"})
		return
	}

	if err := validateResolveURL(body.URL); err != nil {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: err.Error()})
		return
	}

	result := app.ProbeOBI(body.URL, 15*time.Second)
	if result.Status != "ok" {
		writeErrorJSON(w, http.StatusBadGateway, "failed to resolve interface", result.Detail)
		return
	}

	var iface any
	if err := json.Unmarshal([]byte(result.OBI), &iface); err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "remote interface returned invalid JSON"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"interface":   iface,
		"url":         result.OBIURL,
		"finalUrl":    result.FinalURL,
		"synthesized": result.Synthesized,
	})
}

// validateResolveURL enforces SSRF protection on arbitrary URL resolution.
func validateResolveURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return &url.Error{Op: "resolve", URL: rawURL, Err: errNonHTTPScheme}
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return &url.Error{Op: "resolve", URL: rawURL, Err: errEmptyHost}
	}

	// Allow localhost and loopback — ob serve is a local dev tool and
	// resolving locally-running services is the primary use case.
	// Block other private ranges to prevent LAN scanning.
	ip := net.ParseIP(hostname)
	if ip != nil {
		if !ip.IsLoopback() && isPrivateIP(ip) {
			return &url.Error{Op: "resolve", URL: rawURL, Err: errPrivateIP}
		}
	} else if !strings.EqualFold(hostname, "localhost") {
		addrs, err := net.LookupHost(hostname)
		if err == nil {
			for _, a := range addrs {
				if resolved := net.ParseIP(a); resolved != nil && !resolved.IsLoopback() && isPrivateIP(resolved) {
					return &url.Error{Op: "resolve", URL: rawURL, Err: errPrivateIP}
				}
			}
		}
	}

	return nil
}

var privateRanges = []*net.IPNet{
	parseCIDR("0.0.0.0/8"),
	parseCIDR("10.0.0.0/8"),
	parseCIDR("172.16.0.0/12"),
	parseCIDR("192.168.0.0/16"),
	parseCIDR("127.0.0.0/8"),
	parseCIDR("169.254.0.0/16"),
	parseCIDR("::1/128"),
	parseCIDR("fc00::/7"),
	parseCIDR("fe80::/10"),
}

func isPrivateIP(ip net.IP) bool {
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDR(s string) *net.IPNet {
	_, n, _ := net.ParseCIDR(s)
	return n
}

type ssrfError string

func (e ssrfError) Error() string { return string(e) }

const (
	errNonHTTPScheme ssrfError = "only http and https schemes are allowed"
	errEmptyHost     ssrfError = "empty hostname"
	errPrivateIP     ssrfError = "private/internal IP addresses are not allowed"
)

// --- Helpers ---

// ErrorResponse is the standard error body returned by all ob serve endpoints.
type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode failed", "error", err)
	}
}

func writeErrorJSON(w http.ResponseWriter, status int, msg string, detail string) {
	writeJSON(w, status, ErrorResponse{Error: msg, Detail: detail})
}
