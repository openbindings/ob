// Package app - probe.go contains URL probing logic for discovering OpenBindings interfaces.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/execref"
)

// ProbeResult is the shared, presentation-agnostic result of probing a URL for an OpenBindings interface.
type ProbeResult struct {
	Status string // "idle" | "probing" | "ok" | "bad"
	Detail string
	OBI    string
	OBIURL string
	// FinalURL is the resolved URL after redirects (if any).
	FinalURL string
	// OBIDir is the base directory for resolving relative artifact paths.
	// Set for file-path targets (dirname of the file). Empty for exec: targets.
	OBIDir string
	// Synthesized is true when the interface was created from a raw spec
	// (e.g. OpenAPI, AsyncAPI) rather than loaded from a published OBI.
	Synthesized bool
	// SourceFormat is the detected binding format token (e.g. "openapi@3.0.3")
	// when the interface was synthesized. Empty for native OBIs.
	SourceFormat string
}

// NormalizeURL trims input and canonicalises the scheme.
//   - exec: references are preserved as-is.
//   - Local file paths and file:// URLs become file:///absolute/path.
//   - Bare hostnames get an http:// prefix.
func NormalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if execref.IsExec(s) {
		return s
	}
	if p, ok := toFileURL(s); ok {
		return p
	}
	if !strings.Contains(s, "://") {
		s = delegates.HTTPScheme + s
	}
	return s
}

// toFileURL detects local paths and file:// URLs and returns a canonical
// file:///absolute/path form. Returns ("", false) when s is not a file ref.
func toFileURL(s string) (string, bool) {
	var path string
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "file:///"):
		path = s[len("file://"):]
	case strings.HasPrefix(lower, "file://"):
		path = "/" + s[len("file://"):]
	case isFilePath(s):
		path = s
	default:
		return "", false
	}

	if !filepath.IsAbs(path) {
		if cwd, err := os.Getwd(); err == nil {
			path = filepath.Join(cwd, path)
		}
	}
	return "file://" + path, true
}

// isFilePath returns true if s looks like a local file path rather than a hostname.
// Matches: /absolute, ./relative, ../parent, ~/home, or any path containing
// a slash that also has a file extension typical of OBI documents.
func isFilePath(s string) bool {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") || strings.HasPrefix(s, "~") {
		return true
	}
	// Bare name with JSON/YAML extension (e.g., "interface.json")
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		// Only if it doesn't look like a URL (no port, no path segments before extension)
		if !strings.Contains(s, ":") {
			return true
		}
	}
	return false
}

// IsFileURL returns true for file:// URLs.
func IsFileURL(raw string) bool {
	return strings.HasPrefix(strings.ToLower(raw), "file://")
}

// IsHTTPURL returns true for http(s) URLs with explicit scheme.
func IsHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// IsExecURL returns true when the raw string is an exec: reference.
func IsExecURL(raw string) bool {
	return execref.IsExec(raw)
}

// ProbeOBI attempts to fetch an OpenBindings interface from the given URL (direct or discoverable).
// For exec: references, it runs the command as-is; if stdout is a valid OBI, that is used.
// If not and the command was a single token (e.g. exec:ob), it retries with --openbindings.
func ProbeOBI(rawURL string, timeout time.Duration) ProbeResult {
	u := NormalizeURL(rawURL)
	if u == "" {
		return ProbeResult{Status: ProbeStatusIdle}
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	if execref.IsExec(u) {
		args, err := execref.Parse(u)
		if err != nil {
			return ProbeResult{Status: ProbeStatusBad, Detail: "invalid cli command"}
		}
		// Run as-is first. If stdout is already a valid OBI, use it
		// (e.g. exec:curl file:///path, exec:ob --openbindings, exec:cat iface.json).
		doc, firstErr := fetchOBICLIArgs(args, timeout)
		if firstErr == nil && doc != "" {
			return ProbeResult{Status: ProbeStatusOK, Detail: "cli", OBI: doc, OBIURL: u}
		}
		// If not, and user gave a single bare command name (no path, no args),
		// retry with --openbindings appended (e.g. exec:ob → ob --openbindings).
		if len(args) == 1 && !strings.Contains(args[0], "/") {
			doc, retryErr := fetchOBICLIArgs(append(args, "--openbindings"), timeout)
			if retryErr == nil && doc != "" {
				return ProbeResult{Status: ProbeStatusOK, Detail: "cli", OBI: doc, OBIURL: u}
			}
			// Report the retry error if available, otherwise the first.
			if retryErr != nil {
				return ProbeResult{Status: ProbeStatusBad, Detail: retryErr.Error()}
			}
		}
		// No retry path — report the original error.
		if firstErr != nil {
			return ProbeResult{Status: ProbeStatusBad, Detail: firstErr.Error()}
		}
		return ProbeResult{Status: ProbeStatusBad, Detail: "no openbindings interface in output"}
	}

	// file:// URL — read from local filesystem.
	if strings.HasPrefix(strings.ToLower(u), "file://") {
		absPath := u[len("file://"):]
		data, err := os.ReadFile(absPath)
		if err != nil {
			return ProbeResult{Status: ProbeStatusBad, Detail: err.Error()}
		}
		doc, ok := normalizeOBIJSON(data)
		if ok {
			return ProbeResult{
				Status: ProbeStatusOK,
				Detail: "file",
				OBI:    doc,
				OBIURL: u,
				OBIDir: filepath.Dir(absPath),
			}
		}
		// Not a valid OBI — try synthesizing an interface from the raw spec.
		if result, ok := trySynthesizeInterface(absPath, u, filepath.Dir(absPath)); ok {
			return result
		}
		return ProbeResult{Status: ProbeStatusBad, Detail: "not a valid OpenBindings interface"}
	}

	return probeHTTP(u, timeout)
}

// probeHTTP uses the SDK's InterfaceClient to resolve an HTTP URL, then maps
// the result into a ProbeResult. An empty required interface is used so any
// valid OBI is accepted (compatibility isn't the concern of ProbeOBI).
func probeHTTP(u string, timeout time.Duration) ProbeResult {
	exec := DefaultExecutor()
	ic := openbindings.NewUnboundClient(exec)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := ic.Resolve(ctx, u, DefaultCreator())
	if err != nil {
		return ProbeResult{Status: ProbeStatusBad, Detail: err.Error()}
	}

	resolved := ic.Resolved()
	if resolved == nil {
		return ProbeResult{Status: ProbeStatusBad, Detail: "openbindings not found"}
	}

	data, err := json.MarshalIndent(resolved, "", "  ")
	if err != nil {
		return ProbeResult{Status: ProbeStatusBad, Detail: err.Error()}
	}

	result := ProbeResult{
		Status:      ProbeStatusOK,
		OBI:         string(data),
		OBIURL:      u,
		FinalURL:    ic.ResolvedURL(),
		Synthesized: ic.Synthesized(),
	}

	if ic.Synthesized() {
		srcFormat := firstSourceFormat(resolved)
		result.SourceFormat = srcFormat
		result.Detail = "synthesized"
		if srcFormat != "" {
			result.Detail = "synthesized:" + srcFormat
		}
	} else {
		result.Detail = "native"
	}

	return result
}

// FetchOBI downloads an OpenBindings interface from a URL or host.
// The URL is normalized (e.g. localhost:8080 becomes http://localhost:8080);
// if the direct GET does not return an OBI, /.well-known/openbindings is tried.
// Returns the OBI document bytes (validated JSON) or an error.
func FetchOBI(urlOrHost string) ([]byte, error) {
	u := NormalizeURL(urlOrHost)
	if u == "" {
		return nil, fmt.Errorf("empty URL or host")
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return nil, fmt.Errorf("fetch requires an HTTP(S) URL or host (got %q)", urlOrHost)
	}

	exec := DefaultExecutor()
	ic := openbindings.NewUnboundClient(exec)

	ctx, cancel := context.WithTimeout(context.Background(), delegates.DefaultProbeTimeout)
	defer cancel()

	if err := ic.Resolve(ctx, u, DefaultCreator()); err != nil {
		return nil, err
	}
	resolved := ic.Resolved()
	if resolved == nil {
		return nil, fmt.Errorf("no OpenBindings interface at %s (try %s%s)", u, strings.TrimSuffix(u, "/"), openbindings.WellKnownPath)
	}
	return json.MarshalIndent(resolved, "", "  ")
}

func normalizeOBIJSON(body []byte) (string, bool) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", false
	}
	if !openbindings.IsOBInterface(raw) {
		return "", false
	}
	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", false
	}
	return string(bytes.TrimSpace(pretty)), true
}

// trySynthesizeInterface attempts to create an OBI interface by trying each
// registered creator against the given location. Returns (result, true) on
// the first successful synthesis.
func trySynthesizeInterface(location, originalURL, obiDir string) (ProbeResult, bool) {
	creator := DefaultCreator()
	for _, fi := range creator.Formats() {
		iface, err := creator.CreateInterface(context.Background(), &openbindings.CreateInput{
			Sources: []openbindings.CreateSource{{Format: fi.Token, Location: location}},
		})
		if err != nil || iface == nil {
			continue
		}
		if len(iface.Operations) == 0 {
			continue
		}
		data, err := json.MarshalIndent(iface, "", "  ")
		if err != nil {
			continue
		}
		srcFormat := firstSourceFormat(iface)
		detail := "synthesized"
		if srcFormat != "" {
			detail = "synthesized:" + srcFormat
		}
		return ProbeResult{
			Status:       ProbeStatusOK,
			Detail:       detail,
			OBI:          string(data),
			OBIURL:       originalURL,
			OBIDir:       obiDir,
			Synthesized:  true,
			SourceFormat: srcFormat,
		}, true
	}
	return ProbeResult{}, false
}

func firstSourceFormat(iface *openbindings.Interface) string {
	for _, src := range iface.Sources {
		if src.Format != "" {
			return src.Format
		}
	}
	return ""
}

func fetchOBICLIArgs(args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("openbindings timeout")
	}
	if err != nil {
		return "", fmt.Errorf("openbindings failed")
	}
	doc, ok := normalizeOBIJSON(out)
	if !ok {
		return "", nil
	}
	return doc, nil
}

