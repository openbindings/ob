package app

import (
	"fmt"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// GetContext loads context and execution options for a target URL from the store.
// Returns nil, nil if the URL is empty or no context exists.
func GetContext(targetURL string) (map[string]any, *openbindings.InvocationOptions, error) {
	if targetURL == "" {
		return nil, nil, nil
	}
	ctx, opts, err := LoadContext(targetURL)
	if err != nil {
		return nil, nil, fmt.Errorf("loading context for %q: %w", targetURL, err)
	}
	return ctx, opts, nil
}

// transportFields are the well-known context fields that map to InvocationOptions
// rather than to the keychain credentials blob. Anything else in a unified context
// payload goes to the keychain.
var transportFields = map[string]bool{
	"headers":     true,
	"cookies":     true,
	"environment": true,
	"metadata":    true,
}

// BuildUnifiedContext returns the unified context payload stored for a URL,
// or nil if nothing is stored. The unified shape carries credential fields
// (bearerToken, apiKey, basic, ...) alongside transport fields (headers,
// cookies, environment, metadata) in a single opaque map — matching the
// openbindings.context-store role's Context schema.
func BuildUnifiedContext(rawURL string) (map[string]any, error) {
	cred, opts, err := LoadContext(rawURL)
	if err != nil {
		return nil, fmt.Errorf("loading context for %q: %w", rawURL, err)
	}
	return UnifyContext(cred, opts), nil
}

// UnifyContext combines a credential map and InvocationOptions into the
// unified Context payload shape (the openbindings.context-store role's
// Context schema). Returns nil if both inputs are empty.
func UnifyContext(cred map[string]any, opts *openbindings.InvocationOptions) map[string]any {
	if len(cred) == 0 && (opts == nil || (len(opts.Headers) == 0 && len(opts.Cookies) == 0 && len(opts.Environment) == 0 && len(opts.Metadata) == 0)) {
		return nil
	}
	result := make(map[string]any, len(cred)+4)
	for k, v := range cred {
		result[k] = v
	}
	if opts != nil {
		if len(opts.Headers) > 0 {
			result["headers"] = stringMapToAny(opts.Headers)
		}
		if len(opts.Cookies) > 0 {
			result["cookies"] = stringMapToAny(opts.Cookies)
		}
		if len(opts.Environment) > 0 {
			result["environment"] = stringMapToAny(opts.Environment)
		}
		if len(opts.Metadata) > 0 {
			result["metadata"] = opts.Metadata
		}
	}
	return result
}

// SaveUnifiedContext stores a unified context payload under a URL, fully
// replacing any prior context for the key (per the context-store role's
// setContext contract). The payload's transport fields go to the on-disk
// config file; everything else goes to the OS keychain.
func SaveUnifiedContext(rawURL string, ctx map[string]any) error {
	cfg := ContextConfig{}
	cred := map[string]any{}
	for k, v := range ctx {
		if !transportFields[k] {
			cred[k] = v
			continue
		}
		switch k {
		case "headers":
			if m, ok := v.(map[string]any); ok {
				cfg.Headers = anyMapToString(m)
			}
		case "cookies":
			if m, ok := v.(map[string]any); ok {
				cfg.Cookies = anyMapToString(m)
			}
		case "environment":
			if m, ok := v.(map[string]any); ok {
				cfg.Environment = anyMapToString(m)
			}
		case "metadata":
			if m, ok := v.(map[string]any); ok {
				cfg.Metadata = m
			}
		}
	}
	if err := SaveContextConfig(rawURL, cfg); err != nil {
		return err
	}
	if len(cred) == 0 {
		return DeleteContextCredentials(rawURL)
	}
	return SaveContextCredentials(rawURL, cred)
}

func stringMapToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func anyMapToString(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// RenderBindingContext returns a human-friendly representation of binding context
// and execution options.
func RenderBindingContext(bindCtx map[string]any, opts *openbindings.InvocationOptions) string {
	s := Styles
	var sb strings.Builder

	empty := len(bindCtx) == 0 && (opts == nil || (len(opts.Headers) == 0 && len(opts.Cookies) == 0 && len(opts.Environment) == 0 && len(opts.Metadata) == 0))
	if empty {
		sb.WriteString(s.Dim.Render("No context configured"))
		return sb.String()
	}

	sb.WriteString(s.Header.Render("Binding Context"))

	if len(bindCtx) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(s.Dim.Render("Credentials:"))
		if token := openbindings.ContextBearerToken(bindCtx); token != "" {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Dim.Render("Bearer: "))
			sb.WriteString(maskSecret(token))
		}
		if key := openbindings.ContextAPIKey(bindCtx); key != "" {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Dim.Render("API Key: "))
			sb.WriteString(maskSecret(key))
		}
		if u, _, ok := openbindings.ContextBasicAuth(bindCtx); ok {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Dim.Render("Basic: "))
			sb.WriteString(u + ":****")
		}
	}

	if opts != nil {
		renderStringMap(&sb, s, "Headers:", opts.Headers, ": ", false)
		renderStringMap(&sb, s, "Cookies:", opts.Cookies, "=", true)
		renderStringMap(&sb, s, "Environment:", opts.Environment, "=", true)

		if len(opts.Metadata) > 0 {
			sb.WriteString("\n\n")
			sb.WriteString(s.Dim.Render("Metadata:"))
			metaKeys := make([]string, 0, len(opts.Metadata))
			for k := range opts.Metadata {
				metaKeys = append(metaKeys, k)
			}
			sort.Strings(metaKeys)
			for _, k := range metaKeys {
				sb.WriteString("\n  ")
				sb.WriteString(s.Bullet.Render("•"))
				sb.WriteString(" ")
				sb.WriteString(s.Key.Render(k))
				sb.WriteString(s.Dim.Render(": "))
				sb.WriteString(fmt.Sprintf("%v", opts.Metadata[k]))
			}
		}
	}

	return sb.String()
}

// RenderContextList returns a human-friendly list of context summaries.
func RenderContextList(summaries []ContextSummary) string {
	s := Styles
	var sb strings.Builder

	if len(summaries) == 0 {
		sb.WriteString(s.Dim.Render("No contexts configured"))
		return sb.String()
	}

	sb.WriteString(s.Header.Render("Contexts"))

	for _, cs := range summaries {
		sb.WriteString("\n  ")
		sb.WriteString(s.Key.Render(cs.URL))

		if cs.LoadError != "" {
			sb.WriteString(s.Dim.Render(" (error: " + cs.LoadError + ")"))
			continue
		}

		var parts []string
		if cs.HasCredentials {
			parts = append(parts, "credentials")
		}
		if cs.HeaderCount > 0 {
			parts = append(parts, fmt.Sprintf("%d headers", cs.HeaderCount))
		}
		if cs.CookieCount > 0 {
			parts = append(parts, fmt.Sprintf("%d cookies", cs.CookieCount))
		}
		if cs.EnvCount > 0 {
			parts = append(parts, fmt.Sprintf("%d env", cs.EnvCount))
		}
		if cs.MetadataCount > 0 {
			parts = append(parts, fmt.Sprintf("%d metadata", cs.MetadataCount))
		}
		if len(parts) > 0 {
			sb.WriteString(s.Dim.Render(" (" + strings.Join(parts, ", ") + ")"))
		}
	}

	return sb.String()
}

func renderStringMap(sb *strings.Builder, s styles, label string, m map[string]string, sep string, mask bool) {
	if len(m) == 0 {
		return
	}
	sb.WriteString("\n\n")
	sb.WriteString(s.Dim.Render(label))
	for _, k := range sortedKeys(m) {
		sb.WriteString("\n  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Key.Render(k))
		sb.WriteString(s.Dim.Render(sep))
		v := m[k]
		if mask {
			v = maskSecret(v)
		}
		sb.WriteString(v)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}
