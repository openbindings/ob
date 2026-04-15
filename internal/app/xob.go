package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/openbindings/ob/internal/execref"
	"github.com/openbindings/openbindings-go"
)

// OBVersion is the current ob CLI version, recorded in x-ob metadata.
// Set at build time via -ldflags "-X github.com/openbindings/ob/internal/app.OBVersion=..."
var OBVersion = "dev"

// xobKey is the extension key used for ob-specific metadata.
const xobKey = "x-ob"

// ResolveMode constants for source resolution.
const (
	ResolveModeLocation = "location"
	ResolveModeContent  = "content"
)

// SourceMeta is the x-ob metadata for a Source object.
type SourceMeta struct {
	Ref         string `json:"ref"`
	Resolve     string `json:"resolve"`              // "location" or "content"
	URI         string `json:"uri,omitempty"`         // override location URI
	ContentHash string `json:"contentHash,omitempty"` // "sha256:<hex>" of source at last sync
	LastSynced  string `json:"lastSynced,omitempty"`  // ISO 8601
	OBVersion   string `json:"obVersion,omitempty"`   // ob version that last synced
	Delegate    string `json:"delegate,omitempty"`    // delegate that handles this source (e.g. "ob", "exec:my-cli")
}

// GetSourceMeta reads x-ob metadata from a Source's Extensions.
// Returns nil, nil if the source has no x-ob metadata.
func GetSourceMeta(src openbindings.Source) (*SourceMeta, error) {
	if src.Extensions == nil {
		return nil, nil
	}
	raw, ok := src.Extensions[xobKey]
	if !ok {
		return nil, nil
	}
	var meta SourceMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse x-ob metadata: %w", err)
	}
	return &meta, nil
}

// SetSourceMeta writes x-ob metadata into a Source's Extensions.
func SetSourceMeta(src *openbindings.Source, meta SourceMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal x-ob metadata: %w", err)
	}
	if src.Extensions == nil {
		src.Extensions = map[string]json.RawMessage{}
	}
	src.Extensions[xobKey] = data
	return nil
}

// HasXOB checks whether a LosslessFields object has x-ob metadata (the managed signal).
func HasXOB(lf openbindings.LosslessFields) bool {
	if lf.Extensions == nil {
		return false
	}
	_, ok := lf.Extensions[xobKey]
	return ok
}

// SetXOB writes an empty x-ob: {} onto a LosslessFields object, marking it as managed.
func SetXOB(lf *openbindings.LosslessFields) {
	if lf.Extensions == nil {
		lf.Extensions = map[string]json.RawMessage{}
	}
	lf.Extensions[xobKey] = json.RawMessage(`{}`)
}

// OpBindingXOB is the x-ob metadata stored on managed operations and bindings.
// It holds the base snapshot from the last sync for three-way merge.
type OpBindingXOB struct {
	Base map[string]json.RawMessage `json:"base,omitempty"`
}

// GetBase extracts the base snapshot from a managed object's x-ob metadata.
// Returns nil (no error) if x-ob exists but has no base (e.g. legacy x-ob: {}).
func GetBase(lf openbindings.LosslessFields) (map[string]json.RawMessage, error) {
	if lf.Extensions == nil {
		return nil, nil
	}
	raw, ok := lf.Extensions[xobKey]
	if !ok {
		return nil, nil
	}
	var xob OpBindingXOB
	if err := json.Unmarshal(raw, &xob); err != nil {
		return nil, fmt.Errorf("parse x-ob: %w", err)
	}
	return xob.Base, nil
}

// SetBase stores a base snapshot in a managed object's x-ob metadata.
func SetBase(lf *openbindings.LosslessFields, base map[string]json.RawMessage) error {
	xob := OpBindingXOB{Base: base}
	data, err := json.Marshal(xob)
	if err != nil {
		return fmt.Errorf("marshal x-ob base: %w", err)
	}
	if lf.Extensions == nil {
		lf.Extensions = map[string]json.RawMessage{}
	}
	lf.Extensions[xobKey] = data
	return nil
}

// ObjectToFieldMap marshals any value to a JSON field map, stripping x-ob.
// Used to get the "content" of an operation or binding for merge comparison.
func ObjectToFieldMap(v any) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	delete(m, xobKey)
	return m, nil
}

// StripAllXOB removes x-ob from all objects in an interface recursively.
func StripAllXOB(iface *openbindings.Interface) {
	// Top-level interface extensions.
	if iface.Extensions != nil {
		delete(iface.Extensions, xobKey)
	}

	// Sources.
	for k, src := range iface.Sources {
		if src.Extensions != nil {
			delete(src.Extensions, xobKey)
			iface.Sources[k] = src
		}
	}

	// Operations.
	for k, op := range iface.Operations {
		if op.Extensions != nil {
			delete(op.Extensions, xobKey)
			iface.Operations[k] = op
		}
	}

	// Bindings.
	for k, b := range iface.Bindings {
		if b.Extensions != nil {
			delete(b.Extensions, xobKey)
			iface.Bindings[k] = b
		}
	}
}

// HashContent returns "sha256:<hex>" for the given data.
func HashContent(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// NowISO returns the current time as an ISO 8601 string.
func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ReadSourceContent reads source content from a ref string.
// For exec: refs, the command is executed and stdout is returned.
// For file paths, the file is read. Relative paths are resolved against obiDir.
func ReadSourceContent(ref string, obiDir string) ([]byte, error) {
	if execref.IsExec(ref) {
		args, err := execref.Parse(ref)
		if err != nil {
			return nil, fmt.Errorf("parse exec ref: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("exec %q: %w", ref, err)
		}
		return out, nil
	}

	// File path. Resolve relative to obiDir.
	path := ref
	if !filepath.IsAbs(path) && obiDir != "" {
		path = filepath.Join(obiDir, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", ref, err)
	}
	return data, nil
}

// ReadAndHashSource reads source content from a ref and returns the data and its content hash.
func ReadAndHashSource(ref string, obiDir string) (data []byte, hash string, err error) {
	data, err = ReadSourceContent(ref, obiDir)
	if err != nil {
		return nil, "", err
	}
	return data, HashContent(data), nil
}

// ParseContentForEmbed reads raw bytes and returns an appropriate value for Source.Content.
// For JSON/YAML files, returns map[string]any (native object). For all other formats, returns string.
func ParseContentForEmbed(data []byte, format string) (any, error) {
	// Determine if the format is JSON or YAML-based by looking at the format token.
	formatLower := strings.ToLower(format)

	isJSON := strings.Contains(formatLower, "json") ||
		strings.HasPrefix(formatLower, "openapi") ||
		strings.HasPrefix(formatLower, "asyncapi")
	isYAML := strings.Contains(formatLower, "yaml") || strings.Contains(formatLower, "yml")

	if isJSON || isYAML {
		// Try parsing as JSON first.
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err == nil {
			return obj, nil
		}
		// For YAML, we'd need a YAML parser. For now, fall through to string.
		if isJSON {
			return nil, fmt.Errorf("parse JSON content: invalid JSON")
		}
	}

	// Default: return as string (works for KDL, protobuf, YAML-if-not-JSON, etc.)
	return string(data), nil
}

// ResolveSourceSpec applies an x-ob ref to populate spec-level fields on a source.
// Given raw source data and x-ob metadata, sets either Source.Location or Source.Content.
func ResolveSourceSpec(src *openbindings.Source, meta SourceMeta, data []byte, obiDir string) error {
	switch meta.Resolve {
	case ResolveModeContent:
		content, err := ParseContentForEmbed(data, src.Format)
		if err != nil {
			return fmt.Errorf("embed content: %w", err)
		}
		src.Content = content
		src.Location = "" // clear location if switching modes
	case ResolveModeLocation, "":
		// Use URI override if provided, otherwise derive from ref.
		if meta.URI != "" {
			src.Location = meta.URI
		} else {
			// Make ref relative to OBI directory.
			src.Location = makeRelativeRef(meta.Ref, obiDir)
		}
		src.Content = nil // clear content if switching modes
	default:
		return fmt.Errorf("unknown resolve mode: %q", meta.Resolve)
	}
	return nil
}

// makeRelativeRef normalizes a ref for the spec location field.
// exec: refs and absolute URIs are returned as-is.
// Absolute file paths are made relative to obiDir.
// Relative paths are assumed to already be relative to obiDir and returned as-is.
func makeRelativeRef(ref string, obiDir string) string {
	if execref.IsExec(ref) || strings.Contains(ref, "://") {
		return ref
	}
	if obiDir == "" {
		return ref
	}
	// If the ref is absolute, make it relative to obiDir.
	if filepath.IsAbs(ref) {
		rel, err := filepath.Rel(obiDir, ref)
		if err != nil {
			return ref
		}
		return rel
	}
	// Already relative — assumed to be relative to obiDir.
	return ref
}

