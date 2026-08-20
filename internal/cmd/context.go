package cmd

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "context",
		Aliases: []string{"ctx", "contexts"},
		Short:   "Manage binding context (credentials, headers, environment)",
		Long: `Manage URL-keyed contexts for operation invocation.

A context is scoped to a target URL and contains credentials, headers,
cookies, environment variables, and metadata. When invoking an operation,
context is automatically resolved from the target URL — no manual flag needed.

Credentials are stored in the OS keychain by default. Set
OB_CREDENTIALS_FILE=<path> to store them in a JSON file instead, for headless
environments with no keychain (CI, containers, sandboxes) — see the README's
"Headless / CI" section. Non-secret fields (headers, environment, metadata)
are stored in config files.

Examples:
  ob context set https://api.stripe.com/openapi.json --bearer-token
  ob context set exec:kubectl --env KUBECONFIG=/home/me/.kube/prod
  ob context set https://api.github.com --from-curl 'curl -H "Authorization: Bearer ghp_..."'`,
	}
	markCommandGroup(cmd)

	cmd.AddCommand(
		newContextListCmd(),
		newContextGetCmd(),
		newContextSetCmd(),
		newContextRemoveCmd(),
	)

	return cmd
}

func newContextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			if legacy := app.DetectLegacyContexts(); len(legacy) > 0 {
				fmt.Fprintf(os.Stderr, "Warning: %d named context(s) from a previous version found (%s).\n",
					len(legacy), strings.Join(legacy, ", "))
				fmt.Fprintf(os.Stderr, "These will not auto-match targets. Use 'ob context set <url>' to reconfigure.\n\n")
			}

			summaries, err := app.ListContexts()
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(summaries, format, outputPath, func() string {
				return app.RenderContextList(summaries)
			})
		},
	}
}

func newContextGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <url>",
		Short: "Get context for a target URL (text masks secrets; JSON is the raw payload)",
		Long: `Get the stored context for a target URL.

Resolution is hierarchical for URL keys, like invocation-time matching: the
exact URL wins, then the store walks up the URL path to the most specific
stored prefix (e.g. https://api.example.com/v1/spec.json falls back to
https://api.example.com). Exact keys behave as a plain document get.

Text output masks secret values. JSON output (-F json) returns the raw
payload, credentials included.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetURL := args[0]
			ctx, err := app.GetContext(targetURL)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(ctx, format, outputPath, func() string {
				return app.RenderBindingContext(ctx)
			})
		},
	}
}

func newContextSetCmd() *cobra.Command {
	var (
		bearerToken     string
		apiKey          string
		tokenProvider   string
		tokenCredential string
		basic           bool
		headers         []string
		cookies         []string
		envVars         []string
		metaEntries     []string
		fromCurl        string
		valueJSON       string
	)

	cmd := &cobra.Command{
		Use:   "set [url]",
		Short: "Set context fields for a target URL",
		Long: `Set fields on a URL-keyed context. Creates the context if it doesn't exist.

The URL is the target that this context applies to (e.g., an OpenAPI spec
URL, an exec: reference, or any binding source URL). Context is automatically
matched when invoking operations against this target.

Credential flags (--bearer-token, --api-key, --basic) store values in the OS
keychain by default, or in the file named by OB_CREDENTIALS_FILE when it is
set (headless / CI). Pass "-" to read from stdin without echoing (keeps
secrets out of shell history).

Non-secret flags (--header, --cookie, --env, --meta) are stored in
a config file and can be specified multiple times.

Use --from-curl to import credentials from a curl command.

Pin a token provider (--token-provider, with --token-credential) when the
target's token service corresponds to the shared token-provider contract:
ob then mints and renews this context's bearer token automatically from the
PINNED provider — and only the pinned provider; nothing else carrying the
mint key is ever contacted. A bearer token you set yourself is never
overwritten by the pin.

Machine callers pass the full Context value with --value instead: the <url>
is the key, --value takes the Context ({...}) as JSON and REPLACES the whole
context (the document-store set contract), exclusive with the field flags.
Pass "-" to read the value from stdin, so credentials never ride argv.

Examples:
  ob context set https://api.github.com --bearer-token ghp_xxx
  ob context set https://api.github.com --bearer-token -
  ob context set https://api.stripe.com/openapi.json --api-key sk_live_xxx
  ob context set https://api.example.com --basic
  ob context set https://api.example.com --token-provider https://auth.example.com --token-credential -
  ob context set https://api.example.com --header "Accept: application/json"
  ob context set exec:kubectl --env KUBECONFIG=/home/me/.kube/prod
  ob context set https://api.github.com --from-curl 'curl -H "Authorization: Bearer ghp_xxx" https://api.github.com'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("value") {
				return runContextSetValue(cmd, args, valueJSON)
			}
			if len(args) != 1 {
				return app.ExitResult{Code: 2, Message: "provide a <url> argument", ToStderr: true}
			}
			targetURL := args[0]

			if fromCurl != "" {
				return handleFromCurl(targetURL, fromCurl)
			}

			var update app.ContextUpdate

			if cmd.Flags().Changed("bearer-token") {
				val, err := resolveSecretValue(bearerToken, "Bearer token")
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				update.Credentials = map[string]any{"bearerToken": val}
			}

			if cmd.Flags().Changed("api-key") {
				val, err := resolveSecretValue(apiKey, "API key")
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				if update.Credentials == nil {
					update.Credentials = map[string]any{}
				}
				update.Credentials["apiKey"] = val
			}

			if cmd.Flags().Changed("token-provider") {
				if update.Credentials == nil {
					update.Credentials = map[string]any{}
				}
				update.Credentials["tokenProvider"] = tokenProvider
			}

			if cmd.Flags().Changed("token-credential") {
				val, err := resolveSecretValue(tokenCredential, "Token-provider credential")
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				if update.Credentials == nil {
					update.Credentials = map[string]any{}
				}
				update.Credentials["tokenCredential"] = val
			}

			if basic {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return app.ExitResult{Code: 1, Message: "--basic requires an interactive terminal", ToStderr: true}
				}
				fmt.Print("Username: ")
				var username string
				if _, err := fmt.Scanln(&username); err != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("reading username: %v", err), ToStderr: true}
				}
				password, err := promptSecret("Password: ")
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				if update.Credentials == nil {
					update.Credentials = map[string]any{}
				}
				update.Credentials["basic"] = map[string]any{
					"username": username,
					"password": password,
				}
			}

			var err error
			update.Headers, update.Cookies, update.Environment, update.Metadata, err =
				parseContextKVFlags(headers, cookies, envVars, metaEntries)
			if err != nil {
				return err
			}

			if update.IsEmpty() {
				return app.ExitResult{Code: 1, Message: "no fields specified; use --bearer-token, --api-key, --basic, --header, --cookie, --env, --meta, or --from-curl", ToStderr: true}
			}

			if err := app.ApplyContextUpdate(targetURL, update); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			fmt.Fprintf(os.Stderr, "Context for %q updated.\n", targetURL)
			return nil
		},
	}

	cmd.Flags().StringVar(&bearerToken, "bearer-token", "", "bearer token (use \"-\" to read from stdin)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key (use \"-\" to read from stdin)")
	cmd.Flags().BoolVar(&basic, "basic", false, "set basic auth (prompts for username and password)")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "add header as \"Key: Value\" (repeatable)")
	cmd.Flags().StringArrayVar(&cookies, "cookie", nil, "add cookie as \"Key=Value\" (repeatable)")
	cmd.Flags().StringArrayVar(&envVars, "env", nil, "add env var as \"VAR=value\" (repeatable)")
	cmd.Flags().StringArrayVar(&metaEntries, "meta", nil, "add metadata as \"key=value\" (repeatable)")
	cmd.Flags().StringVar(&tokenProvider, "token-provider", "", "pin a token provider for this target: an interface locator whose openbindings.token-provider.mint keeps this context's bearer token minted automatically")
	cmd.Flags().StringVar(&tokenCredential, "token-credential", "", "durable credential the pinned provider's mint exchanges (use \"-\" to read from stdin)")
	cmd.Flags().StringVar(&fromCurl, "from-curl", "", "import context from a curl command string")
	cmd.Flags().StringVar(&valueJSON, "value", "", "full Context value as JSON, full-replacement machine lane (- reads stdin); keyed by <url>, exclusive with field flags")

	return cmd
}

// runContextSetValue is the machine lane: full-replacement of a URL-keyed
// context (the document-store set contract). The key rides the <url>
// argument; the Context value rides --value, or its stdin (`-`) so credentials
// never touch argv. The operation declares no output; the lane prints null.
func runContextSetValue(cmd *cobra.Command, args []string, valueJSON string) error {
	if len(args) != 1 {
		return app.ExitResult{Code: 2, Message: "--value requires a <url> argument (the context key)", ToStderr: true}
	}
	if cmd.Flags().Changed("bearer-token") || cmd.Flags().Changed("api-key") || cmd.Flags().Changed("basic") ||
		cmd.Flags().Changed("header") || cmd.Flags().Changed("cookie") || cmd.Flags().Changed("env") ||
		cmd.Flags().Changed("meta") || cmd.Flags().Changed("from-curl") {
		return app.ExitResult{Code: 2, Message: "--value is exclusive with the field flags", ToStderr: true}
	}
	key := args[0]

	raw := valueJSON
	if raw == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return app.ExitResult{Code: 2, Message: fmt.Sprintf("--value: read stdin: %v", err), ToStderr: true}
		}
		raw = string(data)
	}
	var value map[string]any
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return app.ExitResult{Code: 2, Message: fmt.Sprintf("parse --value: %v", err), ToStderr: true}
		}
	}
	if err := app.SaveUnifiedContext(key, value); err != nil {
		return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}
	fmt.Println("null")
	return nil
}

func newContextRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <url>",
		Aliases: []string{"rm"},
		Short:   "Remove context for a target URL",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetURL := args[0]
			if err := app.DeleteContext(targetURL); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			// The contract declares no output; -F json prints null (the wire
			// lane), the text lane prints the confirmation.
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(nil, format, outputPath, func() string {
				return fmt.Sprintf("Context for %q removed.", targetURL)
			})
		},
	}
}

func handleFromCurl(targetURL, curlCmd string) error {
	parsed := parseCurlCommand(curlCmd)

	update := app.ContextUpdate{
		Headers: parsed.headers,
		Cookies: parsed.cookies,
	}
	if parsed.bearerToken != "" {
		update.Credentials = map[string]any{"bearerToken": parsed.bearerToken}
	}
	if parsed.basic != nil {
		if update.Credentials == nil {
			update.Credentials = map[string]any{}
		}
		update.Credentials["basic"] = parsed.basic
	}

	if update.IsEmpty() {
		return app.ExitResult{Code: 1, Message: "no context fields found in curl command", ToStderr: true}
	}

	if err := app.ApplyContextUpdate(targetURL, update); err != nil {
		return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}

	count := len(parsed.headers) + len(parsed.cookies) + len(update.Credentials)
	fmt.Fprintf(os.Stderr, "Imported %d field(s) from curl command into context for %q.\n", count, targetURL)
	return nil
}

type curlParsed struct {
	bearerToken string
	basic       map[string]any
	headers     map[string]string
	cookies     map[string]string
}

func parseCurlCommand(cmd string) curlParsed {
	var result curlParsed
	result.headers = make(map[string]string)
	result.cookies = make(map[string]string)

	tokens := tokenizeCurl(cmd)

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		switch {
		case tok == "-H" || tok == "--header":
			if i+1 < len(tokens) {
				i++
				k, v, ok := parseKV(tokens[i], ":")
				if !ok {
					continue
				}
				kLower := strings.ToLower(k)
				if kLower == "authorization" {
					if strings.HasPrefix(v, "Bearer ") || strings.HasPrefix(v, "bearer ") {
						result.bearerToken = strings.TrimSpace(v[7:])
					} else if strings.HasPrefix(v, "Basic ") || strings.HasPrefix(v, "basic ") {
						decoded := decodeBasicAuth(strings.TrimSpace(v[6:]))
						if decoded != nil {
							result.basic = decoded
						}
					} else {
						result.headers[k] = v
					}
				} else if kLower == "cookie" {
					parseCookieHeader(v, result.cookies)
				} else {
					result.headers[k] = v
				}
			}

		case tok == "-b" || tok == "--cookie":
			if i+1 < len(tokens) {
				i++
				parseCookieHeader(tokens[i], result.cookies)
			}

		case tok == "-u" || tok == "--user":
			if i+1 < len(tokens) {
				i++
				parts := strings.SplitN(tokens[i], ":", 2)
				if len(parts) == 2 {
					result.basic = map[string]any{
						"username": parts[0],
						"password": parts[1],
					}
				}
			}
		}
	}

	if len(result.headers) == 0 {
		result.headers = nil
	}
	if len(result.cookies) == 0 {
		result.cookies = nil
	}

	return result
}

func tokenizeCurl(s string) []string {
	var tokens []string
	s = strings.TrimSpace(s)

	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}

		if s[i] == '\'' {
			i++
			end := strings.IndexByte(s[i:], '\'')
			if end < 0 {
				tokens = append(tokens, s[i:])
				break
			}
			tokens = append(tokens, s[i:i+end])
			i += end + 1
		} else if s[i] == '"' {
			i++
			end := strings.IndexByte(s[i:], '"')
			if end < 0 {
				tokens = append(tokens, s[i:])
				break
			}
			tokens = append(tokens, s[i:i+end])
			i += end + 1
		} else {
			start := i
			for i < len(s) && s[i] != ' ' && s[i] != '\t' {
				i++
			}
			tokens = append(tokens, s[start:i])
		}
	}
	return tokens
}

func parseCookieHeader(header string, out map[string]string) {
	pairs := strings.Split(header, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		k, v, ok := parseKV(pair, "=")
		if ok {
			out[k] = v
		}
	}
}

func decodeBasicAuth(encoded string) map[string]any {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return nil
		}
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return nil
	}
	return map[string]any{
		"username": parts[0],
		"password": parts[1],
	}
}

// resolveSecretValue resolves a secret flag value:
//   - "-" reads from stdin securely (terminal) or as a line (pipe)
//   - any other value is used directly
func resolveSecretValue(flagVal, label string) (string, error) {
	val := strings.TrimSpace(flagVal)
	if val == "-" {
		v, err := promptSecret(label + ": ")
		if err != nil {
			return "", err
		}
		return v, nil
	}
	if val == "" {
		return "", fmt.Errorf("--%s requires a value (use \"-\" to read from stdin)", strings.ToLower(strings.ReplaceAll(label, " ", "-")))
	}
	return val, nil
}

func promptSecret(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			val := strings.TrimSpace(scanner.Text())
			if val == "" {
				return "", fmt.Errorf("empty value from stdin")
			}
			return val, nil
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading from stdin: %w", err)
		}
		return "", fmt.Errorf("empty stdin")
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading secret: %w", err)
	}
	val := strings.TrimSpace(string(b))
	if val == "" {
		return "", fmt.Errorf("empty value")
	}
	return val, nil
}

// parseContextKVFlags parses the repeatable --header/--cookie/--env/--meta
// flag values into update maps. Empty flag groups yield nil maps.
func parseContextKVFlags(headers, cookies, envVars, metaEntries []string) (h, c, e map[string]string, m map[string]any, err error) {
	parseStr := func(entries []string, sep, kind, example string) (map[string]string, error) {
		var out map[string]string
		for _, raw := range entries {
			k, v, ok := parseKV(raw, sep)
			if !ok {
				return nil, app.ExitResult{Code: 1, Message: fmt.Sprintf("invalid %s %q (expected %q)", kind, raw, example), ToStderr: true}
			}
			if out == nil {
				out = make(map[string]string)
			}
			out[k] = v
		}
		return out, nil
	}
	if h, err = parseStr(headers, ":", "header", "Key: Value"); err != nil {
		return nil, nil, nil, nil, err
	}
	if c, err = parseStr(cookies, "=", "cookie", "Key=Value"); err != nil {
		return nil, nil, nil, nil, err
	}
	if e, err = parseStr(envVars, "=", "env", "VAR=value"); err != nil {
		return nil, nil, nil, nil, err
	}
	for _, raw := range metaEntries {
		k, v, ok := parseKV(raw, "=")
		if !ok {
			return nil, nil, nil, nil, app.ExitResult{Code: 1, Message: fmt.Sprintf("invalid meta %q (expected \"key=value\")", raw), ToStderr: true}
		}
		if m == nil {
			m = make(map[string]any)
		}
		m[k] = v
	}
	return h, c, e, m, nil
}

// parseKV splits a string on the first occurrence of sep, trimming whitespace.
func parseKV(s, sep string) (key, value string, ok bool) {
	idx := strings.Index(s, sep)
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:idx])
	value = strings.TrimSpace(s[idx+len(sep):])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}
