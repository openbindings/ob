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
	"github.com/openbindings/openbindings-go/canonicaljson"
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

// resolveInterface loads an OpenBindings interface from a locator.
// Locator types: local file path, HTTP(S) URL, exec: reference.
func resolveInterface(locator string) (*openbindings.Interface, error) {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return nil, fmt.Errorf("empty locator")
	}

	// Local file: anything that isn't an exec: ref or URL with scheme.
	if !IsExecURL(locator) && !IsHTTPURL(locator) && !strings.Contains(locator, "://") {
		return loadInterfaceFile(locator)
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

	return parseInterfaceJSON([]byte(result.OBI), locator)
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
// canonical JSON formatting (D6). If the target file already exists,
// its permissions are preserved; otherwise 0644 is used.
func WriteInterfaceFile(path string, iface *openbindings.Interface) error {
	// Marshal with canonical key ordering.
	b, err := canonicaljson.Marshal(iface)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Pretty-print with 2-space indent (json.Indent operates on raw bytes directly).
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return fmt.Errorf("indent: %w", err)
	}
	buf.WriteByte('\n') // trailing newline

	return AtomicWriteFile(path, buf.Bytes(), FilePerm)
}

// WriteInterfaceToPath writes the interface to path. Format is inferred from path
// when format is empty or "text" (.yaml/.yml → yaml, else json).
func WriteInterfaceToPath(path string, iface *openbindings.Interface, format string) error {
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
			schemas[k] = map[string]any(v)
		}
		root["schemas"] = schemas
	}
	return root
}
