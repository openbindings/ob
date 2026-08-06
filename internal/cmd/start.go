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
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/spf13/cobra"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/servecontract"
	"github.com/openbindings/ob/internal/server"
)

// Align the HTTP and WebSocket carrier bound with the SDK's delivery-unit
// policy so the same operation is not accepted in-process and refused by ob
// start solely because it crossed a transport boundary.
const maxRequestBodyBytes = openbindings.DefaultMaxDeliveryUnitBytes

// DefaultServePort is the default TCP port for `ob start`.
// It equals 0x4F42 (decimal 20290): the big-endian pair of ASCII 'O' (0x4F) and 'B' (0x42),
// a mnemonic for OpenBindings. High enough to avoid common dev-server collisions.
const DefaultServePort = 0x4F42

func newStartCmd() *cobra.Command {
	var (
		port           int
		strictPort     bool
		allowedOrigins []string
		tokenFlag      string
		tokenFile      string
		tlsEnabled     bool
		trustLocalCA   bool
		openBrowser    bool
		verbose        bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a local server exposing ob operations (HTTP, WebSocket)",
		Long: `Start a local HTTP/WebSocket server that exposes ob's remotely meaningful capability surface.
Authorized clients can invoke operations, transform interface documents,
and manage contexts and delegates without shelling out to the CLI. Foreground
process commands (start, mcp, and demo) remain local-only.

A session token is generated on startup. In an interactive terminal, ob prints
a clickable workbench URL that transfers the token in a URL fragment; fragments
are not sent to the server and the workbench removes it immediately after load.
Clients must present it as "Authorization: Bearer <token>" on every request.
The server binds to 127.0.0.1 only — never exposed to the network.

The token can be provided via --token flag or OB_START_TOKEN environment variable
to enable stable tokens for CI/CD and automation. No separate token line is
printed; an interactive authenticated workbench URL carries the active token
unless --open or --token-file keeps it out of terminal output. Non-interactive
output never prints a generated secret; automation should supply --token or
--token-file.

By default the server listens over HTTP on the loopback interface. Use --tls
to add an HTTPS listener backed by a locally-generated CA. ob does not modify
the system trust store unless --trust-local-ca is also supplied; that explicit
option may prompt for administrator credentials and implies --tls.

Environment variables: OB_START_TOKEN (pre-shared token), OB_START_PORT
(default port), OB_START_ORIGINS (comma-separated CORS origins).

To expose this server's operations to an MCP agent, bridge it with
'ob mcp <this-url>'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := refuseUnhonoredOutputFlags(cmd, "start", "output", "format"); err != nil {
				return err
			}
			stderr := cmd.ErrOrStderr()
			logLevel := slog.LevelWarn
			if verbose {
				logLevel = slog.LevelInfo
			}
			logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{
				Level: logLevel,
			})).With("component", "ob-start")
			slog.SetDefault(logger)

			resolvedToken := tokenFlag
			if resolvedToken == "" {
				resolvedToken = os.Getenv("OB_START_TOKEN")
			}

			if cmd.Flags().Changed("port") {
				// The flag gets the same validation as OB_START_PORT: an
				// out-of-range value is a usage error up front, not ten
				// futile bind attempts starting at a nonsense port.
				if _, err := parsePort(strconv.Itoa(port)); err != nil {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("invalid --port=%d: %v", port, err), ToStderr: true}
				}
			} else if envPort := os.Getenv("OB_START_PORT"); envPort != "" {
				p, err := parsePort(envPort)
				if err != nil {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("invalid OB_START_PORT=%q: %v", envPort, err), ToStderr: true}
				}
				port = p
			}

			if len(allowedOrigins) == 0 {
				if envOrigins := os.Getenv("OB_START_ORIGINS"); envOrigins != "" {
					allowedOrigins = strings.Split(envOrigins, ",")
					for i := range allowedOrigins {
						allowedOrigins[i] = strings.TrimSpace(allowedOrigins[i])
					}
				}
			}

			if trustLocalCA {
				tlsEnabled = true
			}

			var actualToken string
			srv, err := server.New(server.Config{
				Port:           port,
				StrictPort:     strictPort,
				AllowedOrigins: allowedOrigins,
				Logger:         logger,
				Token:          resolvedToken,
				TLS:            tlsEnabled,
				TrustLocalCA:   trustLocalCA,
				OnReady: func(ready server.ReadyInfo) {
					workbenchBase := ready.HTTPURL
					if trustLocalCA && ready.HTTPSURL != "" {
						workbenchBase = ready.HTTPSURL
					}
					authenticatedURL := workbenchURL(workbenchBase, actualToken)
					interactive := writerIsTerminal(stderr)
					displayURL := authenticatedURL
					if !interactive || tokenFile != "" || openBrowser {
						displayURL = workbenchBase + "/"
					}
					var openErr error
					if openBrowser {
						openErr = openURL(authenticatedURL)
					}
					printStartSummary(stderr, startSummary{
						WorkbenchURL:  displayURL,
						InterfaceURL:  workbenchBase + "/.well-known/openbindings",
						HTTPURL:       ready.HTTPURL,
						HTTPSURL:      ready.HTTPSURL,
						TokenFile:     tokenFile,
						Interactive:   interactive,
						Verbose:       verbose,
						Opened:        openBrowser && openErr == nil,
						RequestedPort: ready.RequestedPort,
						ActualPort:    ready.HTTPPort,
					})
					if openErr != nil {
						fmt.Fprintf(stderr, "\nCould not open the browser: %v\nOpen %s instead.\n", openErr, authenticatedURL)
					}
				},
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			actualToken = srv.Token()
			if tokenFile != "" {
				if err := os.WriteFile(tokenFile, []byte(actualToken+"\n"), 0600); err != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("writing token file: %v", err), ToStderr: true}
				}
			}

			oauthSt := newOAuthStore()
			srv.RegisterSessionRoutes()
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
	cmd.Flags().BoolVar(&strictPort, "strict-port", false, "fail instead of falling back to a nearby port when --port is busy")
	cmd.Flags().StringArrayVar(&allowedOrigins, "allow-origin", nil, "allowed CORS origin (repeatable)")
	cmd.Flags().StringVar(&tokenFlag, "token", "", "pre-shared session token (also: OB_START_TOKEN env var)")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "write session token to file instead of stderr")
	cmd.Flags().BoolVar(&tlsEnabled, "tls", false, "also serve HTTPS using a local CA (does not modify system trust)")
	cmd.Flags().BoolVar(&trustLocalCA, "trust-local-ca", false, "install the local HTTPS CA into system trust (implies --tls; may prompt)")
	cmd.Flags().BoolVar(&openBrowser, "open", false, "open the authenticated workbench in the default browser")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show request and invocation logs")

	return cmd
}

type startSummary struct {
	WorkbenchURL string
	InterfaceURL string
	HTTPURL      string
	HTTPSURL     string
	TokenFile    string
	Interactive  bool
	Verbose      bool
	Opened       bool
	// RequestedPort/ActualPort announce a port fallback: when they differ,
	// the requested port was busy and the server bound ActualPort instead.
	RequestedPort int
	ActualPort    int
}

// portFellBack reports whether the server bound a different port than the one
// requested (both must be known; a zero RequestedPort means "any port").
func (s startSummary) portFellBack() bool {
	return s.RequestedPort != 0 && s.ActualPort != 0 && s.ActualPort != s.RequestedPort
}

func printStartSummary(w interface{ Write([]byte) (int, error) }, summary startSummary) {
	if !summary.Interactive {
		if summary.portFellBack() {
			fmt.Fprintf(w, "Port %d was in use — serving on %d.\n", summary.RequestedPort, summary.ActualPort)
		}
		fmt.Fprintf(w, "ob start listening on %s\n", summary.HTTPURL)
		if summary.HTTPSURL != "" {
			fmt.Fprintf(w, "ob start TLS listening on %s\n", summary.HTTPSURL)
		}
		fmt.Fprintf(w, "OpenBindings interface: %s\n", summary.InterfaceURL)
		if summary.TokenFile != "" {
			fmt.Fprintf(w, "Session token written to %s\n", summary.TokenFile)
		}
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "OpenBindings is ready")
	fmt.Fprintln(w)
	if summary.portFellBack() {
		fmt.Fprintf(w, "  Port %d was in use — serving on %d.\n", summary.RequestedPort, summary.ActualPort)
		fmt.Fprintln(w)
	}
	if summary.Opened {
		fmt.Fprintf(w, "  Workbench  %s  (opened in your browser)\n", summary.WorkbenchURL)
	} else {
		fmt.Fprintf(w, "  Workbench  %s\n", summary.WorkbenchURL)
	}
	fmt.Fprintf(w, "  Interface  %s\n", summary.InterfaceURL)
	if summary.HTTPSURL != "" {
		fmt.Fprintf(w, "  HTTPS      %s\n", summary.HTTPSURL)
	}
	if summary.TokenFile != "" {
		fmt.Fprintf(w, "  Token      written to %s\n", summary.TokenFile)
	}
	fmt.Fprintln(w)
	if summary.Verbose {
		fmt.Fprintln(w, "Request logs are enabled. Press Ctrl+C to stop.")
	} else {
		fmt.Fprintln(w, "Press Ctrl+C to stop. Add --verbose to show request logs.")
	}
}

func workbenchURL(baseURL, token string) string {
	values := url.Values{}
	values.Set("token", token)
	return strings.TrimRight(baseURL, "/") + "/#" + values.Encode()
}

func writerIsTerminal(w any) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

var openURL = openURLWithPlatform

func openURLWithPlatform(target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		command, args = "xdg-open", []string{target}
	}
	process := exec.Command(command, args...)
	if err := process.Start(); err != nil {
		return fmt.Errorf("%s: %w", command, err)
	}
	_ = process.Process.Release()
	return nil
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("invalid port: %s", s)
	}
	return p, nil
}

func registerRoutes(srv *server.Server, logger *slog.Logger, port int, oauthSt *oauthStore) {
	mux := srv.Mux()

	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /{$}", handleRoot)
	mux.Handle("GET /assets/", server.WorkbenchAssets())
	mux.HandleFunc("GET /.well-known/openbindings", handleOBI(port))
	mux.HandleFunc("GET /openapi.yaml", handleOpenAPISpec(port))
	mux.HandleFunc("GET /asyncapi.yaml", handleAsyncAPISpec(port))

	mux.HandleFunc("POST /resolve", handleResolve)

	mux.HandleFunc("GET /spec/{name...}", handleSpecResource)

	registerOAuthRoutes(srv, oauthSt, logger)
	registerCanonicalOperationRoutes(srv, logger)
	registerLegacyAuthoringRoutes(mux)
	// MCP is not a built-in endpoint: ob's served interface is exposed as an MCP
	// server by pointing the generic bridge at this running server —
	// `ob mcp <this-url>`. That dogfoods the same OBI→MCP path ob offers for any
	// interface, so there is no bespoke MCP surface to keep in sync here.
}

// --- Health ---

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Root ---

// handleRoot serves the embedded OpenBindings workbench. The machine-readable
// interface still lives only at /.well-known/openbindings (http-discovery
// companion, DISC-S-01): root remains HTML, so a bare base URL correctly falls
// through direct OBI parsing to well-known discovery.
func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Shadow-root component styles are emitted as <style> elements, so the
	// style policy permits inline CSS. Script remains external/self-only and
	// all document values are assigned with textContent.
	wsScheme := "ws"
	if r.TLS != nil {
		wsScheme = "wss"
	}
	w.Header().Set(
		"Content-Security-Policy",
		fmt.Sprintf(
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' %s://%s; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
			wsScheme,
			safeWebSocketAuthority(r.Host),
		),
	)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(server.WorkbenchIndex())
}

func safeWebSocketAuthority(hostport string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err == nil && server.IsLoopbackHost(host) {
		if _, portErr := parsePort(port); portErr == nil {
			return net.JoinHostPort(host, port)
		}
		return formatHostForAuthority(host)
	}
	host = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
	if server.IsLoopbackHost(host) {
		return formatHostForAuthority(host)
	}
	return "127.0.0.1"
}

func formatHostForAuthority(host string) string {
	if strings.Contains(host, ":") {
		return "[" + strings.TrimSuffix(strings.TrimPrefix(host, "["), "]") + "]"
	}
	return host
}

// --- OBI / Info / Formats / Delegates ---

func handleOBI(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Serve the OBI describing what this `ob start` instance
		// publishes (not the CLI OBI).
		raw := server.ServeOBI()
		var iface map[string]any
		if err := json.Unmarshal(raw, &iface); err != nil {
			writeErrorJSON(w, http.StatusInternalServerError, "internal_error", err.Error())
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
		}

		// Auth requirements are NOT carried in the OBI document (OBI 0.2.0 has no
		// `security` field). A consumer discovers this server's own bearer/oauth2
		// requirement at invocation time via the openapi source: the served
		// /openapi.yaml carries the securitySchemes (with /oauth endpoints already
		// absolutized by rewriteSpecPlaceholders), which the SDK's openapi invoker
		// surfaces as a CONTEXT_REQUIRED challenge. There is nothing OBI-level to
		// rewrite here.

		writeOBI(w, http.StatusOK, iface)
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

func handleDescribe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, app.Info())
}

func handleBindingSpecs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, app.ListBindingSpecs())
}

func handleDelegates(w http.ResponseWriter, r *http.Request) {
	// The contract's listDelegates output: {"delegates": [...]}.
	writeJSON(w, http.StatusOK, app.ListDelegates())
}

// handleResolveDelegate serves the read-only counterpart of `ob delegate
// resolve`: which registered delegates carry an operation identifier,
// ordered by effective preference. Resolves candidates only — it does not
// invoke anything — so an operation nothing carries is a literal 200 with an
// empty candidate list, not an error.
func handleResolveDelegate(w http.ResponseWriter, r *http.Request) {
	result, err := app.ResolveDelegate(r.PathValue("operation"))
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "delegate_resolution_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Spec Resources ---

func handleSpecResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "name path parameter is required")
		return
	}

	content, err := server.SpecResource(name)
	if err != nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "spec resource not found")
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

// handleDelegateRequirements serves the interface a delegate must satisfy for a
// capability (invoke/synthesize/inspect), so a prospective delegate can be checked
// against a running ob. The requirement interfaces are immutable bundled
// documents, served like spec resources.
func handleDelegateRequirements(w http.ResponseWriter, r *http.Request) {
	data, err := app.RequirementInterfaceJSON(app.DelegateCapability(r.PathValue("capability")))
	if err != nil {
		writeErrorJSON(w, http.StatusNotFound, "unknown_capability", "unknown delegate capability (want invoke, synthesize, or inspect)")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openbindings+json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// --- Status ---

func handleEnvironment(w http.ResponseWriter, r *http.Request) {
	status, err := app.GetEnvironmentStatus()
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "environment_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// --- Context ---

func handleContextList(w http.ResponseWriter, r *http.Request) {
	summaries, err := app.ListContexts()
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "context_store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

func handleContextGet(w http.ResponseWriter, r *http.Request) {
	targetURL := r.PathValue("url")
	if targetURL == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "url path parameter is required")
		return
	}

	payload, err := app.LoadContext(targetURL)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "context_store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func handleContextSet(w http.ResponseWriter, r *http.Request) {
	targetURL := r.PathValue("url")
	if targetURL == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "url path parameter is required")
		return
	}

	var body map[string]any
	if !decodeRequest(w, r, &body) {
		return
	}

	if err := app.SaveUnifiedContext(targetURL, body); err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "context_store_failed", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleContextDelete(w http.ResponseWriter, r *http.Request) {
	targetURL := r.PathValue("url")
	if targetURL == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "url path parameter is required")
		return
	}

	if err := app.DeleteContext(targetURL); err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "context_store_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Resolve (with SSRF protection) ---

func handleResolve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"address"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}

	if body.URL == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "address is required")
		return
	}

	if err := validateOutboundURL(body.URL); err != nil {
		writeErrorJSON(w, http.StatusForbidden, "resolution_forbidden", err.Error())
		return
	}

	result := app.ProbeOBI(body.URL, 15*time.Second)
	if result.Status != "ok" {
		writeErrorJSON(w, http.StatusBadGateway, "resolution_failed", result.Detail)
		return
	}

	var iface any
	if err := json.Unmarshal([]byte(result.OBI), &iface); err != nil {
		writeErrorJSON(w, http.StatusBadGateway, "invalid_upstream", "remote interface returned invalid JSON")
		return
	}
	// The ResolveInterfaceOutput shape: the resolved document, the format it
	// was synthesized from (absent for native OBIs), and where it was fetched.
	resp := map[string]any{"interface": iface}
	if result.Synthesized {
		resp["synthesizedFrom"] = result.SourceBindingSpec
	}
	if result.OBIURL != "" {
		resp["resolvedUrl"] = result.OBIURL
	}
	writeJSON(w, http.StatusOK, resp)
}

// validateOutboundURL enforces SSRF protection on any outbound fetch ob makes
// on a caller's behalf (OBI resolution via /resolve). It allows
// loopback/localhost — ob start is a local dev tool and reaching
// locally-running services is a primary use case — but blocks other private,
// link-local, and metadata ranges to prevent LAN scanning and metadata theft.
func validateOutboundURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return &url.Error{Op: "fetch", URL: rawURL, Err: errNonHTTPScheme}
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return &url.Error{Op: "fetch", URL: rawURL, Err: errEmptyHost}
	}

	// Allow localhost and loopback — ob start is a local dev tool and
	// resolving locally-running services is the primary use case.
	// Block other private ranges to prevent LAN scanning.
	ip := net.ParseIP(hostname)
	if ip != nil {
		if !ip.IsLoopback() && isPrivateIP(ip) {
			return &url.Error{Op: "fetch", URL: rawURL, Err: errPrivateIP}
		}
	} else if !strings.EqualFold(hostname, "localhost") {
		addrs, err := net.LookupHost(hostname)
		if err == nil {
			for _, a := range addrs {
				if resolved := net.ParseIP(a); resolved != nil && !resolved.IsLoopback() && isPrivateIP(resolved) {
					return &url.Error{Op: "fetch", URL: rawURL, Err: errPrivateIP}
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode failed", "error", err)
	}
}

// writeOBI writes an OBI document using the vendor-registered media type
// (application/vnd.openbindings+json) per the http-discovery companion's
// DISC-S-02 (response Content-Type) and core §11 (IANA media-type
// registration). Clients that send only Accept: application/json still
// receive the same body; per DISC-S-02 the vendor type is SHOULD-level,
// not a hard requirement.
func writeOBI(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/vnd.openbindings+json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeOBI encode failed", "error", err)
	}
}

func writeErrorJSON(w http.ResponseWriter, status int, code servecontract.ErrorCode, message string) {
	servecontract.WriteError(w, status, code, message)
}
