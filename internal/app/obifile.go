package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
	"github.com/openbindings/openbindings-go/synthesize"
)

// StdinLocator is the locator/path that means "read the document from stdin".
// It is the CLI-filter convention: a command reading its OBI from `-` is a
// Unix filter, which is how the exec-lane binding delivers a document-valued
// input field (stdin-dash delivery substitutes `-` in the argv slot and pipes
// the bytes to the child's stdin). See ob-pj/wire-conformance.md, batch 3.
const StdinLocator = "-"

// ReadDocumentBytes reads the raw document bytes for a locator, treating the
// bare `-` as stdin. For commands that consume a document as untyped JSON
// (e.g. purify runs it through an operation-graph) rather than parsing it into
// an Interface.
func ReadDocumentBytes(path string) ([]byte, error) {
	return readLocatorBytes(path)
}

// readLocatorBytes reads the raw bytes for a file locator, treating the bare
// `-` as stdin. It is the single point where the stdin-filter convention is
// honored for document input.
func readLocatorBytes(path string) ([]byte, error) {
	if path == StdinLocator {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// DefaultProbeTimeout is the timeout for probing remote interfaces.
const DefaultProbeTimeout = 10 * time.Second

// ResolveInterface loads an OpenBindings interface from a locator.
// Locator types: local file path, HTTP(S) URL, exec: reference.
func ResolveInterface(locator string) (*openbindings.Interface, error) {
	return resolveInterface(locator)
}

func resolveInterface(locator string) (*openbindings.Interface, error) {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return nil, fmt.Errorf("empty locator")
	}

	// Preserve the document-editing and validation path: a local JSON object
	// that is not yet a valid OBI still parses into an Interface so callers can
	// diagnose or repair it. Raw-artifact synthesis is the additional behavior
	// of ResolveInterfaceDetailed.
	if !IsExecURL(locator) && !IsHTTPURL(locator) && !strings.Contains(locator, "://") {
		return loadInterfaceFile(locator)
	}

	result := ProbeOBI(locator, DefaultProbeTimeout)
	if result.Status != ProbeStatusOK || result.OBI == "" {
		detail := result.Detail
		if detail == "" {
			detail = "no OpenBindings interface found"
		}
		return nil, fmt.Errorf("%s", detail)
	}
	return parseInterfaceJSON([]byte(result.OBI), locator)
}

// ResolvedInterface is the interface plus acquisition evidence useful to
// consumers that care whether a raw artifact was synthesized.
type ResolvedInterface struct {
	Interface         *openbindings.Interface
	Synthesized       bool
	SourceBindingSpec string
	Coverage          *synthesize.SynthesisCoverage
}

// ResolveInterfaceDetailed loads an OBI or synthesizes one from a raw local or
// remote artifact, retaining synthesis coverage when available.
func ResolveInterfaceDetailed(locator string) (*ResolvedInterface, error) {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return nil, fmt.Errorf("empty locator")
	}

	// A local OBI takes the direct path. When the file is not an OBI, retry it
	// through the same raw-artifact synthesis chain used by file:// URLs.
	if !IsExecURL(locator) && !IsHTTPURL(locator) && !strings.Contains(locator, "://") {
		data, directErr := readLocatorBytes(locator)
		if directErr == nil {
			if _, isOBI := normalizeOBIJSON(data); isOBI {
				iface, parseErr := parseInterfaceJSON(data, locator)
				if parseErr != nil {
					return nil, parseErr
				}
				return &ResolvedInterface{Interface: iface}, nil
			}
			_, directErr = parseInterfaceJSON(data, locator)
			if directErr == nil {
				directErr = fmt.Errorf("%s: JSON object is not an OpenBindings interface", locator)
			}
		}
		if locator == StdinLocator {
			return nil, directErr
		}
		if _, statErr := os.Stat(locator); statErr != nil {
			return nil, directErr
		}
		fileURI, err := localPathFileURL(locator)
		if err != nil {
			return nil, err
		}
		locator = fileURI
	}

	// Remote or exec: probe.
	result := ProbeOBI(locator, DefaultProbeTimeout)
	if result.Status != "ok" {
		detail := result.Detail
		if detail == "" {
			detail = "probe failed"
		}
		return nil, fmt.Errorf("%s", detail)
	}

	iface, err := parseInterfaceJSON([]byte(result.OBI), locator)
	if err != nil {
		return nil, err
	}
	return &ResolvedInterface{
		Interface:         iface,
		Synthesized:       result.Synthesized,
		SourceBindingSpec: result.SourceBindingSpec,
		Coverage:          result.Coverage,
	}, nil
}

// loadInterfaceFile reads and parses an OpenBindings interface JSON file.
// The bare `-` reads the document from stdin (the CLI-filter convention).
func loadInterfaceFile(path string) (*openbindings.Interface, error) {
	data, err := readLocatorBytes(path)
	if err != nil {
		return nil, err
	}
	source := path
	if path == StdinLocator {
		source = "<stdin>"
	}
	return parseInterfaceJSON(data, source)
}

// parseInterfaceJSON unmarshals JSON into an Interface, providing clear error
// messages that distinguish "not JSON at all" from "JSON but not a valid interface".
func parseInterfaceJSON(data []byte, source string) (*openbindings.Interface, error) {
	// Quick sanity check: JSON documents must start with '{' (after whitespace).
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%s: file is empty", source)
	}
	if trimmed[0] != '{' {
		// Give a hint about what we actually got.
		preview := string(trimmed)
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		return nil, fmt.Errorf("%s: not a JSON object (starts with %q)", source, preview)
	}

	iface, err := openbindings.ParseDocument(data)
	if err != nil {
		var validationErr *openbindings.ValidationError
		if !errors.As(err, &validationErr) {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		var parsed openbindings.Interface
		if unmarshalErr := json.Unmarshal(data, &parsed); unmarshalErr != nil {
			return nil, fmt.Errorf("%s: invalid JSON: %w", source, unmarshalErr)
		}
		return &parsed, nil
	}
	return iface, nil
}

// WriteInterfaceFile writes an Interface to a file atomically using
// deterministic, value-preserving JSON formatting (D6). This ordinary writer
// is not a JCS export or a delegate trust-pin encoder. If the target exists,
// its permissions are preserved; otherwise 0644 is used.
//
// The bare `-` writes the document to stdout instead — the write side of the
// CLI-filter convention (readLocatorBytes is the read side): an editing
// command whose document argument is `-` reads the document from stdin and
// emits the modified document on stdout, with its human summary on stderr.
func WriteInterfaceFile(path string, iface *openbindings.Interface) error {
	// encoding/json orders map keys without converting retained numeric values.
	b, err := jsonvalue.Marshal(iface)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Pretty-print with 2-space indent (json.Indent operates on raw bytes directly).
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return fmt.Errorf("indent: %w", err)
	}
	buf.WriteByte('\n') // trailing newline

	if path == StdinLocator {
		_, err := os.Stdout.Write(buf.Bytes())
		return err
	}
	// Idempotent writes: unchanged retained material yields identical bytes,
	// and identical bytes leave the file untouched — a no-op pull
	// neither dirties a git tree nor manufactures same-line merge conflicts.
	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, buf.Bytes()) {
		return nil
	}
	return AtomicWriteFile(path, buf.Bytes(), FilePerm)
}

// WriteInterfaceToPath writes the interface to path. Format is inferred from path
// when format is empty or "text" (.yaml/.yml → yaml, else json).
// The bare `-` writes deterministic JSON to stdout (the filter lane's document
// channel is always JSON; a format override applies to the summary, not the
// document).
func WriteInterfaceToPath(path string, iface *openbindings.Interface, format string) error {
	if path == StdinLocator {
		return WriteInterfaceFile(path, iface)
	}
	lower := strings.ToLower(path)
	useYAML := strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
	if !useYAML && format != "" && format != "text" {
		if format == "yaml" || format == "yml" {
			useYAML = true
		}
	}
	if useYAML {
		b, err := FormatOutput(iface, OutputFormatYAML)
		if err != nil {
			return err
		}
		return AtomicWriteFile(path, b, FilePerm)
	}
	return WriteInterfaceFile(path, iface)
}

// buildNormalizerRoot constructs the root object for schema normalization
// from an Interface's schemas pool. The root is structured as the full
// interface would appear, so that $ref resolution works correctly
// (e.g., "#/schemas/Foo" resolves against root["schemas"]["Foo"]).
func buildNormalizerRoot(iface *openbindings.Interface) map[string]any {
	root := map[string]any{}
	if len(iface.Schemas) > 0 {
		schemas := map[string]any{}
		for k, v := range iface.Schemas {
			schemas[k] = v
		}
		root["schemas"] = schemas
	}
	return root
}

// OutputDocument emits a resulting OBI document through the one canonical
// serializer every document-writing command shares (canonical key order,
// two-space indent, trailing newline — the same bytes WriteInterfaceFile
// produces), so WHICH command wrote a file never changes its bytes: a
// synthesize followed by a no-op pull is byte-identical, with no one-time
// key-reorder or EOF-newline churn in the first diff. YAML (explicit
// -F yaml, or a .yaml/.yml output path) uses the YAML formatter; -F quiet
// suppresses output.
func OutputDocument(v any, format, outputPath string) error {
	if format == "quiet" {
		return ExitResult{Code: 0}
	}
	lowerPath := strings.ToLower(outputPath)
	wantYAML := format == "yaml" || format == "yml" ||
		strings.HasSuffix(lowerPath, ".yaml") || strings.HasSuffix(lowerPath, ".yml")
	if wantYAML {
		b, err := FormatOutput(v, OutputFormatYAML)
		if err != nil {
			return err
		}
		if outputPath != "" {
			if werr := AtomicWriteFile(outputPath, b, FilePerm); werr != nil {
				return ExitResult{Code: 1, Message: werr.Error(), ToStderr: true}
			}
			return ExitResult{Code: 0, Message: "Wrote " + outputPath}
		}
		return ExitResult{Code: 0, Message: strings.TrimRight(string(b), "\n")}
	}

	b, err := jsonvalue.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal document: %w", err)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return fmt.Errorf("indent document: %w", err)
	}
	if outputPath != "" {
		buf.WriteByte('\n')
		// Idempotent, like WriteInterfaceFile: identical bytes, untouched file.
		if existing, rerr := os.ReadFile(outputPath); rerr == nil && bytes.Equal(existing, buf.Bytes()) {
			return ExitResult{Code: 0, Message: "Wrote " + outputPath}
		}
		if werr := AtomicWriteFile(outputPath, buf.Bytes(), FilePerm); werr != nil {
			return ExitResult{Code: 1, Message: werr.Error(), ToStderr: true}
		}
		return ExitResult{Code: 0, Message: "Wrote " + outputPath}
	}
	return ExitResult{Code: 0, Message: buf.String()}
}
