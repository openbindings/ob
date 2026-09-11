// Package app - probe.go contains URL probing logic for discovering OpenBindings interfaces.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"

	"github.com/openbindings/openbindings-go/synthesize"

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
	// SourceBindingSpec is the detected binding specification identifier
	// when the interface was synthesized. Empty for native OBIs.
	SourceBindingSpec string
	// Coverage is present when this resolution synthesized a raw artifact
	// through a coverage-capable synthesizer.
	Coverage *synthesize.SynthesisCoverage
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
	if strings.HasPrefix(strings.ToLower(s), "file:") {
		return s, true
	}
	if !isFilePath(s) {
		return "", false
	}
	uri, err := localPathFileURL(s)
	if err != nil {
		return s, true
	} // never guess an HTTP hostname for a local path
	return uri, true
}

// isFilePath returns true if s looks like a local file path rather than a hostname.
// Matches: /absolute, ./relative, ../parent, ~/home, or any path containing
// a slash that also has a file extension typical of OBI documents.
func isFilePath(s string) bool {
	if strings.HasPrefix(s, `\`) || (len(s) >= 2 && s[1] == ':' &&
		((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z'))) {
		return true
	}
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
	if strings.HasPrefix(strings.ToLower(u), "file:") {
		absPath, pathErr := fileURLLocalPath(u)
		if pathErr != nil {
			return ProbeResult{Status: ProbeStatusBad, Detail: pathErr.Error()}
		}
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
	if isFilePath(u) {
		return ProbeResult{Status: ProbeStatusBad, Detail: "unsupported local path; use an absolute local path on this platform"}
	}

	return probeHTTP(u, timeout)
}

// probeHTTP uses FetchInterface to resolve an HTTP URL, then maps the
// result into a ProbeResult.
func probeHTTP(u string, timeout time.Duration) ProbeResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fetched, err := synthesize.FetchInterface(ctx, u, synthesize.WithSynthesizers(DefaultSynthesizer()))
	if err != nil {
		return ProbeResult{Status: ProbeStatusBad, Detail: err.Error()}
	}
	if fetched == nil || fetched.Interface == nil {
		return ProbeResult{Status: ProbeStatusBad, Detail: "openbindings not found"}
	}

	data, err := json.MarshalIndent(fetched.Interface, "", "  ")
	if err != nil {
		return ProbeResult{Status: ProbeStatusBad, Detail: err.Error()}
	}

	result := ProbeResult{
		Status:      ProbeStatusOK,
		OBI:         string(data),
		OBIURL:      u,
		FinalURL:    u,
		Synthesized: fetched.Synthesized,
		Coverage:    fetched.Coverage,
	}

	if fetched.Synthesized {
		srcFormat := firstSourceFormat(fetched.Interface)
		result.SourceBindingSpec = srcFormat
		result.Detail = "synthesized"
		if srcFormat != "" {
			result.Detail = "synthesized:" + srcFormat
		}
	} else {
		result.Detail = "native"
	}

	return result
}

// ResolveOBI resolves an OpenBindings interface from a URL or host.
// The URL is normalized (e.g. localhost:8080 becomes http://localhost:8080);
// if the direct GET does not return an OBI, /.well-known/openbindings is tried,
// and failing that, an interface is synthesized from the raw spec found there.
// Returns the OBI document bytes (validated JSON) and, when the interface was
// synthesized from a raw spec, the format token it was synthesized from
// (e.g. "openapi@3.1"); synthesizedFrom is empty for native OBIs.
// ResolveInterfaceOutput is resolveInterface's wire output: the resolved
// document plus the binding-format token it was synthesized from (empty for
// native OBIs), realizing the contract's ResolveInterfaceOutput schema.
type ResolveInterfaceOutput struct {
	Interface       *openbindings.Interface `json:"interface"`
	SynthesizedFrom string                  `json:"synthesizedFrom,omitempty"`
}

func ResolveOBI(urlOrHost string) (doc []byte, synthesizedFrom string, err error) {
	u := NormalizeURL(urlOrHost)
	if u == "" {
		return nil, "", fmt.Errorf("empty URL or host")
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return nil, "", fmt.Errorf("resolve requires an HTTP(S) URL or host (got %q)", urlOrHost)
	}

	ctx, cancel := context.WithTimeout(context.Background(), delegates.DefaultProbeTimeout)
	defer cancel()

	fetched, err := synthesize.FetchInterface(ctx, u, synthesize.WithSynthesizers(DefaultSynthesizer()))
	if err != nil {
		return nil, "", err
	}
	if fetched == nil || fetched.Interface == nil {
		return nil, "", fmt.Errorf("no OpenBindings interface at %s (try %s%s)", u, strings.TrimSuffix(u, "/"), openbindings.WellKnownPath)
	}
	if fetched.Synthesized {
		synthesizedFrom = firstSourceFormat(fetched.Interface)
	}
	doc, err = json.MarshalIndent(fetched.Interface, "", "  ")
	return doc, synthesizedFrom, err
}

func normalizeOBIJSON(body []byte) (string, bool) {
	var raw map[string]any
	if err := jsonvalue.Unmarshal(body, &raw); err != nil {
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
// registered synthesizer against the given location. Returns (result, true) on
// the first successful synthesis.
func trySynthesizeInterface(location, originalURL, obiDir string) (ProbeResult, bool) {
	synthesizer := DefaultSynthesizer()
	for _, fi := range synthesizer.BindingSpecs() {
		input := &synthesize.SynthesizeInput{
			Sources: []synthesize.SynthesizeSource{{BindingSpec: fi.BindingSpec, Location: location}},
		}
		var coverage *synthesize.SynthesisCoverage
		var iface *openbindings.Interface
		if coverageSynthesizer, ok := synthesizer.(synthesize.CoverageSynthesizer); ok {
			result, err := coverageSynthesizer.SynthesizeInterfaceWithCoverage(context.Background(), input)
			if errors.Is(err, synthesize.ErrSynthesisCoverageUnsupported) {
				iface, err = synthesizer.SynthesizeInterface(context.Background(), input)
			} else if err != nil {
				continue
			} else if result != nil {
				iface = result.Interface
				value := result.Coverage
				coverage = &value
			}
		} else {
			var err error
			iface, err = synthesizer.SynthesizeInterface(context.Background(), input)
			if err != nil {
				continue
			}
		}
		if iface == nil {
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
			Status:            ProbeStatusOK,
			Detail:            detail,
			OBI:               string(data),
			OBIURL:            originalURL,
			OBIDir:            obiDir,
			Synthesized:       true,
			SourceBindingSpec: srcFormat,
			Coverage:          coverage,
		}, true
	}
	return ProbeResult{}, false
}

func firstSourceFormat(iface *openbindings.Interface) string {
	for _, src := range iface.Sources {
		if src.BindingSpec != "" {
			return src.BindingSpec
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
