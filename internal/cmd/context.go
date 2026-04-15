package cmd

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage binding context (credentials, headers, environment)",
		Long: `Manage URL-keyed contexts for operation execution.

A context is scoped to a target URL and contains credentials, headers,
cookies, environment variables, and metadata. When executing an operation,
context is automatically resolved from the target URL — no manual flag needed.

Credentials are stored securely in the OS keychain. Non-secret
fields (headers, environment, metadata) are stored in config files.

Examples:
  ob context set https://api.stripe.com/openapi.json --bearer-token
  ob context set exec:kubectl --env KUBECONFIG=/home/me/.kube/prod
  ob context set https://api.github.com --from-curl 'curl -H "Authorization: Bearer ghp_..."'`,
	}

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
		Short: "Get context details for a target URL (secrets masked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetURL := args[0]
			bindCtx, opts, err := app.GetContext(targetURL)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			combined := map[string]any{"context": bindCtx, "options": opts}
			return app.OutputResultText(combined, format, outputPath, func() string {
				return app.RenderBindingContext(bindCtx, opts)
			})
		},
	}
}

func newContextSetCmd() *cobra.Command {
	var (
		bearerToken string
		apiKey      string
		basic       bool
		headers     []string
		cookies     []string
		envVars     []string
		metaEntries []string
		source      string
		fromCurl    string
	)

	cmd := &cobra.Command{
		Use:   "set <url>",
		Short: "Set context fields for a target URL",
		Long: `Set fields on a URL-keyed context. Creates the context if it doesn't exist.

The URL is the target that this context applies to (e.g., an OpenAPI spec
URL, an exec: reference, or any binding source URL). Context is automatically
matched when executing operations against this target.

Credential flags (--bearer-token, --api-key, --basic) store values
securely in the OS keychain. Pass "-" to read from stdin without
echoing (keeps secrets out of shell history).

Non-secret flags (--header, --cookie, --env, --meta) are stored in
a config file and can be specified multiple times.

Use --source to scope context to a specific source within the target.
Use --from-curl to import credentials from a curl command.

Examples:
  ob context set https://api.github.com --bearer-token ghp_xxx
  ob context set https://api.github.com --bearer-token -
  ob context set https://api.stripe.com/openapi.json --api-key sk_live_xxx
  ob context set https://api.example.com --basic
  ob context set https://api.example.com --header "Accept: application/json"
  ob context set exec:kubectl --env KUBECONFIG=/home/me/.kube/prod
  ob context set https://api.stripe.com/openapi.json --source payments-v2 --bearer-token TOKEN
  ob context set https://api.github.com --from-curl 'curl -H "Authorization: Bearer ghp_xxx" https://api.github.com'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetURL := args[0]

			if fromCurl != "" {
				return handleFromCurl(targetURL, source, fromCurl)
			}

			cfg, err := app.LoadContextConfig(targetURL)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			var cred map[string]any
			if source != "" {
				cred, err = app.LoadSourceContextCredentials(targetURL, source)
			} else {
				cred, err = app.LoadContextCredentials(targetURL)
			}
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			credChanged := false

			if cmd.Flags().Changed("bearer-token") {
				val, err := resolveSecretValue(bearerToken, "Bearer token")
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				if cred == nil {
					cred = map[string]any{}
				}
				cred["bearerToken"] = val
				credChanged = true
			}

			if cmd.Flags().Changed("api-key") {
				val, err := resolveSecretValue(apiKey, "API key")
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				if cred == nil {
					cred = map[string]any{}
				}
				cred["apiKey"] = val
				credChanged = true
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
				if cred == nil {
					cred = map[string]any{}
				}
				cred["basic"] = map[string]any{
					"username": username,
					"password": password,
				}
				credChanged = true
			}

			// Resolve which target maps the KV fields should write into.
			// Source-scoped context writes to SourceOverrides[source];
			// target-level context writes to cfg directly.
			var targetHeaders, targetCookies, targetEnv *map[string]string
			var targetMeta *map[string]any
			if source != "" {
				if cfg.SourceOverrides == nil {
					cfg.SourceOverrides = make(map[string]*app.ContextOverride)
				}
				ov := cfg.SourceOverrides[source]
				if ov == nil {
					ov = &app.ContextOverride{}
					cfg.SourceOverrides[source] = ov
				}
				targetHeaders = &ov.Headers
				targetCookies = &ov.Cookies
				targetEnv = &ov.Environment
				targetMeta = &ov.Metadata
			} else {
				targetHeaders = &cfg.Headers
				targetCookies = &cfg.Cookies
				targetEnv = &cfg.Environment
				targetMeta = &cfg.Metadata
			}

			cfgChanged, err := applyContextKVFields(
				headers, cookies, envVars, metaEntries,
				targetHeaders, targetCookies, targetEnv, targetMeta,
			)
			if err != nil {
				return err
			}

			if !credChanged && !cfgChanged {
				return app.ExitResult{Code: 1, Message: "no fields specified; use --bearer-token, --api-key, --basic, --header, --cookie, --env, --meta, or --from-curl", ToStderr: true}
			}

			if credChanged {
				if source != "" {
					if err := app.SaveSourceContextCredentials(targetURL, source, cred); err != nil {
						return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
					}
				} else {
					if err := app.SaveContextCredentials(targetURL, cred); err != nil {
						return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
					}
				}
			}

			if cfgChanged || credChanged {
				if err := app.SaveContextConfig(targetURL, cfg); err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
			}

			if source != "" {
				fmt.Fprintf(os.Stderr, "Context for %q (source %q) updated.\n", targetURL, source)
			} else {
				fmt.Fprintf(os.Stderr, "Context for %q updated.\n", targetURL)
			}
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
	cmd.Flags().StringVar(&source, "source", "", "scope context to a specific source within the target")
	cmd.Flags().StringVar(&fromCurl, "from-curl", "", "import context from a curl command string")

	return cmd
}

func newContextRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <url>",
		Aliases: []string{"rm"},
		Short:   "Remove context for a target URL",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetURL := args[0]
			if !app.ContextExists(targetURL) {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("no context found for %q", targetURL), ToStderr: true}
			}
			if err := app.DeleteContext(targetURL); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			fmt.Fprintf(os.Stderr, "Context for %q removed.\n", targetURL)
			return nil
		},
	}
}

func handleFromCurl(targetURL, source, curlCmd string) error {
	parsed := parseCurlCommand(curlCmd)

	cfg, err := app.LoadContextConfig(targetURL)
	if err != nil {
		return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}

	var cred map[string]any
	if source != "" {
		cred, err = app.LoadSourceContextCredentials(targetURL, source)
	} else {
		cred, err = app.LoadContextCredentials(targetURL)
	}
	if err != nil {
		return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}

	if parsed.bearerToken != "" {
		if cred == nil {
			cred = map[string]any{}
		}
		cred["bearerToken"] = parsed.bearerToken
	}
	if parsed.basic != nil {
		if cred == nil {
			cred = map[string]any{}
		}
		cred["basic"] = parsed.basic
	}

	setHeaders := func(h map[string]string, target *map[string]string) {
		if len(h) == 0 {
			return
		}
		if *target == nil {
			*target = make(map[string]string)
		}
		for k, v := range h {
			(*target)[k] = v
		}
	}
	setCookies := func(c map[string]string, target *map[string]string) {
		if len(c) == 0 {
			return
		}
		if *target == nil {
			*target = make(map[string]string)
		}
		for k, v := range c {
			(*target)[k] = v
		}
	}

	if source != "" {
		if cfg.SourceOverrides == nil {
			cfg.SourceOverrides = make(map[string]*app.ContextOverride)
		}
		ov := cfg.SourceOverrides[source]
		if ov == nil {
			ov = &app.ContextOverride{}
			cfg.SourceOverrides[source] = ov
		}
		setHeaders(parsed.headers, &ov.Headers)
		setCookies(parsed.cookies, &ov.Cookies)
	} else {
		setHeaders(parsed.headers, &cfg.Headers)
		setCookies(parsed.cookies, &cfg.Cookies)
	}

	if len(cred) > 0 {
		if source != "" {
			if err := app.SaveSourceContextCredentials(targetURL, source, cred); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
		} else {
			if err := app.SaveContextCredentials(targetURL, cred); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
		}
	}

	if err := app.SaveContextConfig(targetURL, cfg); err != nil {
		return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}

	count := len(parsed.headers) + len(parsed.cookies)
	if parsed.bearerToken != "" {
		count++
	}
	if parsed.basic != nil {
		count++
	}
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

// applyContextKVFields parses and writes header/cookie/env/metadata entries
// into the supplied target maps. Returns (changed, error).
func applyContextKVFields(
	headers, cookies, envVars, metaEntries []string,
	targetHeaders, targetCookies, targetEnv *map[string]string,
	targetMeta *map[string]any,
) (bool, error) {
	changed := false
	setStr := func(entries []string, sep, kind, example string, target *map[string]string) error {
		for _, raw := range entries {
			k, v, ok := parseKV(raw, sep)
			if !ok {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("invalid %s %q (expected %q)", kind, raw, example), ToStderr: true}
			}
			if *target == nil {
				*target = make(map[string]string)
			}
			(*target)[k] = v
			changed = true
		}
		return nil
	}
	if err := setStr(headers, ":", "header", "Key: Value", targetHeaders); err != nil {
		return false, err
	}
	if err := setStr(cookies, "=", "cookie", "Key=Value", targetCookies); err != nil {
		return false, err
	}
	if err := setStr(envVars, "=", "env", "VAR=value", targetEnv); err != nil {
		return false, err
	}
	for _, m := range metaEntries {
		k, v, ok := parseKV(m, "=")
		if !ok {
			return false, app.ExitResult{Code: 1, Message: fmt.Sprintf("invalid meta %q (expected \"key=value\")", m), ToStderr: true}
		}
		if *targetMeta == nil {
			*targetMeta = make(map[string]any)
		}
		(*targetMeta)[k] = v
		changed = true
	}
	return changed, nil
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
