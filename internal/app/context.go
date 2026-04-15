package app

import (
	"fmt"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// GetContext loads context and execution options for a target URL from the store.
// Returns nil, nil if the URL is empty or no context exists.
func GetContext(targetURL string) (map[string]any, *openbindings.ExecutionOptions, error) {
	if targetURL == "" {
		return nil, nil, nil
	}
	ctx, opts, err := LoadContext(targetURL)
	if err != nil {
		return nil, nil, fmt.Errorf("loading context for %q: %w", targetURL, err)
	}
	return ctx, opts, nil
}

// GetContextForSource loads context and execution options for a specific source
// within a target URL. Source-level overrides are merged on top of target-level.
func GetContextForSource(targetURL, sourceName string) (map[string]any, *openbindings.ExecutionOptions, error) {
	if targetURL == "" {
		return nil, nil, nil
	}
	ctx, opts, err := LoadContextForSource(targetURL, sourceName)
	if err != nil {
		return nil, nil, fmt.Errorf("loading context for %q (source %q): %w", targetURL, sourceName, err)
	}
	return ctx, opts, nil
}

// RenderBindingContext returns a human-friendly representation of binding context
// and execution options.
func RenderBindingContext(bindCtx map[string]any, opts *openbindings.ExecutionOptions) string {
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
		if cs.SourceCount > 0 {
			parts = append(parts, fmt.Sprintf("%d source overrides", cs.SourceCount))
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
