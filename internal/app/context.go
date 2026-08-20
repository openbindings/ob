package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go/invoke"
)

// GetContext loads the unified context payload for a target URL.
// Returns nil if the URL is empty or no context exists.
func GetContext(targetURL string) (map[string]any, error) {
	if targetURL == "" {
		return nil, nil
	}
	ctx, err := LoadContext(targetURL)
	if err != nil {
		return nil, fmt.Errorf("loading context for %q: %w", targetURL, err)
	}
	return ctx, nil
}

// transportFields are the well-known context fields that the CLI stores in
// the on-disk config file rather than the keychain credentials blob.
// "configuration" (binding-specification configuration points, the
// config.value answers) rides the file side: configuration is not a
// credential, though the binding-invoker contract notes it may be sensitive
// according to its meaning — the config file already carries
// credential-store permissions, and secret-bearing configuration belongs in
// credentials instead.
var transportFields = map[string]bool{
	"headers":       true,
	"cookies":       true,
	"environment":   true,
	"metadata":      true,
	"configuration": true,
}

// SaveUnifiedContext stores a unified context payload under a URL, fully
// replacing any prior context for the key (per the document-store interface's
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
		case "configuration":
			if m, ok := v.(map[string]any); ok {
				cfg.Configuration = m
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

// ContextUpdate carries field-level updates for a stored context. Nil groups
// leave that group unchanged; provided keys are merged over existing ones.
type ContextUpdate struct {
	Credentials map[string]any
	Headers     map[string]string
	Cookies     map[string]string
	Environment map[string]string
	Metadata    map[string]any
	// Configuration merges point-wise: each provided point's value replaces
	// that point, sibling points and all other fields are preserved.
	Configuration map[string]any
}

// IsEmpty reports whether the update carries no changes.
func (u ContextUpdate) IsEmpty() bool {
	return len(u.Credentials) == 0 && len(u.Headers) == 0 && len(u.Cookies) == 0 &&
		len(u.Environment) == 0 && len(u.Metadata) == 0 && len(u.Configuration) == 0
}

// ApplyContextUpdate merges field updates into the stored context for a URL,
// creating the context when absent. This is the CLI's field-flag lane
// (`ob context set --header ...`); the wire operation setContext replaces the
// whole value instead (SaveUnifiedContext). Credentials go to the OS keychain,
// the other groups to the on-disk config file.
func ApplyContextUpdate(rawURL string, up ContextUpdate) error {
	cfg, err := LoadContextConfig(rawURL)
	if err != nil {
		return err
	}

	if len(up.Credentials) > 0 {
		cred, err := LoadContextCredentials(rawURL)
		if err != nil {
			return err
		}
		if cred == nil {
			cred = map[string]any{}
		}
		for k, v := range up.Credentials {
			cred[k] = v
		}
		if err := SaveContextCredentials(rawURL, cred); err != nil {
			return err
		}
	}

	mergeStr := func(dst *map[string]string, src map[string]string) {
		if len(src) == 0 {
			return
		}
		if *dst == nil {
			*dst = make(map[string]string, len(src))
		}
		for k, v := range src {
			(*dst)[k] = v
		}
	}
	mergeStr(&cfg.Headers, up.Headers)
	mergeStr(&cfg.Cookies, up.Cookies)
	mergeStr(&cfg.Environment, up.Environment)
	if len(up.Metadata) > 0 {
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any, len(up.Metadata))
		}
		for k, v := range up.Metadata {
			cfg.Metadata[k] = v
		}
	}
	if len(up.Configuration) > 0 {
		if cfg.Configuration == nil {
			cfg.Configuration = make(map[string]any, len(up.Configuration))
		}
		for point, v := range up.Configuration {
			cfg.Configuration[point] = v
		}
	}

	// Always write the config file: it doubles as the store's index entry, so
	// a credentials-only context still lists and matches.
	return SaveContextConfig(rawURL, cfg)
}

// mergeDurableConfiguration persists resolved config.value answers under the
// exact asserted target key, merging point-wise: each point's fragment
// deep-merges into any stored value at that point (a `/url` answer lands
// beside a stored `/variables/region` answer), sibling points and all other
// fields are preserved. Configuration is not a credential; it rides the
// on-disk config side (see transportFields).
func mergeDurableConfiguration(rawURL string, points map[string]any) error {
	if len(points) == 0 {
		return nil
	}
	cfg, err := LoadContextConfig(rawURL)
	if err != nil {
		return err
	}
	if cfg.Configuration == nil {
		cfg.Configuration = make(map[string]any, len(points))
	}
	mergeConfigFragment(cfg.Configuration, points)
	return SaveContextConfig(rawURL, cfg)
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

// RenderBindingContext returns a human-friendly representation of a unified
// context payload.
func RenderBindingContext(ctx map[string]any) string {
	s := Styles
	var sb strings.Builder

	if len(ctx) == 0 {
		sb.WriteString(s.Dim.Render("No context configured"))
		return sb.String()
	}

	sb.WriteString(s.Header.Render("Binding Context"))

	hasCred := invoke.ContextBearerToken(ctx) != "" ||
		invoke.ContextAPIKey(ctx) != ""
	if _, _, ok := invoke.ContextBasicAuth(ctx); ok {
		hasCred = true
	}
	if hasCred {
		sb.WriteString("\n\n")
		sb.WriteString(s.Dim.Render("Credentials:"))
		if token := invoke.ContextBearerToken(ctx); token != "" {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Dim.Render("Bearer: "))
			sb.WriteString(maskSecret(token))
		}
		if key := invoke.ContextAPIKey(ctx); key != "" {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Dim.Render("API Key: "))
			sb.WriteString(maskSecret(key))
		}
		if u, _, ok := invoke.ContextBasicAuth(ctx); ok {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Dim.Render("Basic: "))
			sb.WriteString(u + ":****")
		}
	}

	renderStringMap(&sb, s, "Headers:", invoke.ContextHeaders(ctx), ": ", false)
	renderStringMap(&sb, s, "Cookies:", invoke.ContextCookies(ctx), "=", true)
	renderStringMap(&sb, s, "Environment:", invoke.ContextEnvironment(ctx), "=", true)

	meta := invoke.ContextMetadata(ctx)
	if len(meta) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(s.Dim.Render("Metadata:"))
		metaKeys := make([]string, 0, len(meta))
		for k := range meta {
			metaKeys = append(metaKeys, k)
		}
		sort.Strings(metaKeys)
		for _, k := range metaKeys {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Key.Render(k))
			sb.WriteString(s.Dim.Render(": "))
			sb.WriteString(fmt.Sprintf("%v", meta[k]))
		}
	}

	configuration := invoke.ContextConfiguration(ctx)
	if len(configuration) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(s.Dim.Render("Configuration:"))
		points := make([]string, 0, len(configuration))
		for point := range configuration {
			points = append(points, point)
		}
		sort.Strings(points)
		for _, point := range points {
			sb.WriteString("\n  ")
			sb.WriteString(s.Bullet.Render("•"))
			sb.WriteString(" ")
			sb.WriteString(s.Key.Render(point))
			sb.WriteString(s.Dim.Render(": "))
			if compact, err := json.Marshal(configuration[point]); err == nil {
				sb.WriteString(string(compact))
			} else {
				sb.WriteString(fmt.Sprintf("%v", configuration[point]))
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
		if cs.ConfigurationCount > 0 {
			parts = append(parts, fmt.Sprintf("%d configuration", cs.ConfigurationCount))
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
