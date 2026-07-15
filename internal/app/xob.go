package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/openbindings/ob/internal/execref"
	"github.com/openbindings/openbindings-go"
	"gopkg.in/yaml.v3"
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
	Resolve     string `json:"resolve"`               // "location" or "content"
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

// HasXOB checks whether a LosslessFields object has any x-ob metadata.
func HasXOB(lf openbindings.LosslessFields) bool {
	if lf.Extensions == nil {
		return false
	}
	_, ok := lf.Extensions[xobKey]
	return ok
}

// IsSourceOwned reports whether an operation or binding is owned by a source
// (produced by `ob synthesize`/`ob source pull`), and therefore may be
// overwritten by a future pull. This is the "source-owned, not hand-authored"
// signal the pull guards and edit gates key on.
//
// Source ownership is carried by the x-ob *base snapshot* (or a bare legacy
// `x-ob: {}` marker), NOT by the mere presence of x-ob. An operation whose only
// x-ob content is a `codegenName` override is author-set metadata on an
// otherwise hand-authored operation — it is NOT source-owned, so edits and
// pulls must treat it as hand-authored.
func IsSourceOwned(lf openbindings.LosslessFields) bool {
	if !HasXOB(lf) {
		return false
	}
	xob, err := getOpBindingXOB(lf)
	if err != nil {
		return true // malformed x-ob: treat as source-owned (conservative)
	}
	if xob.CodegenName != "" && xob.Base == nil {
		return false // codegen-name-only: authoring intent, not source ownership
	}
	return true
}

// SetXOB writes an empty x-ob: {} onto a LosslessFields object, marking it as source-owned.
func SetXOB(lf *openbindings.LosslessFields) {
	if lf.Extensions == nil {
		lf.Extensions = map[string]json.RawMessage{}
	}
	lf.Extensions[xobKey] = json.RawMessage(`{}`)
}

// OpBindingXOB is the x-ob metadata stored on source-owned operations and
// bindings. It holds the base snapshot from the last pull for three-way merge,
// plus any author-set codegen name override (operations only).
type OpBindingXOB struct {
	Base map[string]json.RawMessage `json:"base,omitempty"`
	// CodegenName overrides the symbol name emitted by `ob codegen` for this
	// operation. It is authoring intent, orthogonal to source ownership: the
	// source owns the spec fields it derives, never this hint, so the pull and
	// merge paths carry it forward. Bindings never set it (codegen is
	// per-operation).
	CodegenName string `json:"codegenName,omitempty"`
	// OutputSchemaElection records an author's explicit output-schema
	// election (`ob operation output-schema`): op.Output was set by the
	// author, not derived from the source (the non-detaching remedy for a
	// floor-stamped synthesis). The value is a copy of the elected schema
	// so pull can re-apply it onto a fresh derivation and compare content
	// modulo elections; a grown, non-floor-stamped SOURCE schema wins and
	// displaces the election loudly. `--pure`/purify strips this marker;
	// the elected value stays in op.Output. Bindings never set it.
	OutputSchemaElection json.RawMessage `json:"outputSchemaElection,omitempty"`
}

// getOpBindingXOB reads the full x-ob struct from an operation/binding.
// Returns a zero struct (no error) when x-ob is absent.
func getOpBindingXOB(lf openbindings.LosslessFields) (OpBindingXOB, error) {
	var xob OpBindingXOB
	if lf.Extensions == nil {
		return xob, nil
	}
	raw, ok := lf.Extensions[xobKey]
	if !ok {
		return xob, nil
	}
	if err := json.Unmarshal(raw, &xob); err != nil {
		return xob, fmt.Errorf("parse x-ob: %w", err)
	}
	return xob, nil
}

// setOpBindingXOB writes the x-ob struct onto an operation/binding. When the
// struct is empty (no base, no codegen name) the x-ob key is removed entirely
// rather than left as a bare {} — a lingering empty marker would misreport a
// hand-authored object as source-owned.
func setOpBindingXOB(lf *openbindings.LosslessFields, xob OpBindingXOB) error {
	if xob.Base == nil && xob.CodegenName == "" && xob.OutputSchemaElection == nil {
		if lf.Extensions != nil {
			delete(lf.Extensions, xobKey)
		}
		return nil
	}
	data, err := json.Marshal(xob)
	if err != nil {
		return fmt.Errorf("marshal x-ob: %w", err)
	}
	if lf.Extensions == nil {
		lf.Extensions = map[string]json.RawMessage{}
	}
	lf.Extensions[xobKey] = data
	return nil
}

// GetBase extracts the base snapshot from a source-owned object's x-ob metadata.
// Returns nil (no error) if x-ob exists but has no base (e.g. legacy x-ob: {}).
func GetBase(lf openbindings.LosslessFields) (map[string]json.RawMessage, error) {
	xob, err := getOpBindingXOB(lf)
	if err != nil {
		return nil, err
	}
	return xob.Base, nil
}

// SetBase stores a base snapshot in a source-owned object's x-ob metadata,
// preserving any existing codegen-name override (a pull must not clobber it).
func SetBase(lf *openbindings.LosslessFields, base map[string]json.RawMessage) error {
	xob, err := getOpBindingXOB(*lf)
	if err != nil {
		return err
	}
	xob.Base = base
	return setOpBindingXOB(lf, xob)
}

// GetCodegenName reads an operation's codegen-name override ("" if unset).
func GetCodegenName(lf openbindings.LosslessFields) string {
	xob, err := getOpBindingXOB(lf)
	if err != nil {
		return ""
	}
	return xob.CodegenName
}

// SetCodegenName sets (or, with name == "", clears) an operation's codegen-name
// override, preserving any base snapshot. Clearing the last field removes x-ob.
func SetCodegenName(lf *openbindings.LosslessFields, name string) error {
	xob, err := getOpBindingXOB(*lf)
	if err != nil {
		return err
	}
	xob.CodegenName = name
	return setOpBindingXOB(lf, xob)
}

// GetOutputSchemaElection reads an operation's output-schema election marker
// as a decoded schema (nil when unset).
func GetOutputSchemaElection(lf openbindings.LosslessFields) (openbindings.JSONSchema, error) {
	xob, err := getOpBindingXOB(lf)
	if err != nil {
		return nil, err
	}
	if xob.OutputSchemaElection == nil {
		return nil, nil
	}
	var schema openbindings.JSONSchema
	if err := json.Unmarshal(xob.OutputSchemaElection, &schema); err != nil {
		return nil, fmt.Errorf("parse output-schema election: %w", err)
	}
	return schema, nil
}

// SetOutputSchemaElection records (or, with schema == nil, clears) an
// operation's output-schema election marker, preserving any base snapshot
// and codegen name. Clearing the last field removes x-ob.
func SetOutputSchemaElection(lf *openbindings.LosslessFields, schema openbindings.JSONSchema) error {
	xob, err := getOpBindingXOB(*lf)
	if err != nil {
		return err
	}
	if schema == nil {
		xob.OutputSchemaElection = nil
	} else {
		data, merr := json.Marshal(schema)
		if merr != nil {
			return fmt.Errorf("marshal output-schema election: %w", merr)
		}
		xob.OutputSchemaElection = data
	}
	return setOpBindingXOB(lf, xob)
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

// StripAllXOB removes x-ob from an interface: the object-level extensions on
// the interface, sources, operations, and bindings (the four-level walk that
// carries the op-level election marker and codegen-name hint), AND — walking
// into the schema BODIES — any in-schema x-ob (the synthesis floor-stamp
// `{"type":"string","x-ob":{"floor":...}}`). Schema bodies are reached
// through each operation's input/output and the shared schemas section
// (including nested subschemas and the $ref'd-schema case), so a floor-stamp
// buried in a $def strips too. The elected output VALUE stays; only its x-ob
// marker (op-level) and any floor-stamp (in-schema) are removed.
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

	// Operations: object-level x-ob AND the input/output schema bodies.
	for k, op := range iface.Operations {
		if op.Extensions != nil {
			delete(op.Extensions, xobKey)
		}
		stripXOBFromSchema(op.Input)
		stripXOBFromSchema(op.Output)
		iface.Operations[k] = op
	}

	// Bindings (no schema bodies: bindings carry transforms, not schemas).
	for k, b := range iface.Bindings {
		if b.Extensions != nil {
			delete(b.Extensions, xobKey)
			iface.Bindings[k] = b
		}
	}

	// The shared schemas section (JSON Pointer $ref targets).
	for _, schema := range iface.Schemas {
		stripXOBFromSchema(schema)
	}
}

// stripXOBFromSchema removes the x-ob key at every level of a JSON Schema
// body — the map itself and any nested subschema reached through the
// keywords that carry them (properties, patternProperties, definitions/
// $defs, items/prefixItems, additionalProperties, oneOf/anyOf/allOf/not,
// if/then/else). Content-independent: it deletes only the extension key.
func stripXOBFromSchema(schema openbindings.JSONSchema) {
	if schema == nil {
		return
	}
	delete(schema, xobKey)
	for key, v := range schema {
		switch key {
		case "properties", "patternProperties", "definitions", "$defs":
			if sub, ok := v.(map[string]any); ok {
				for _, child := range sub {
					stripXOBFromValue(child)
				}
			}
		case "items", "prefixItems", "oneOf", "anyOf", "allOf":
			stripXOBFromValue(v) // schema OR array of schemas
		case "additionalProperties", "not", "if", "then", "else", "contains", "propertyNames":
			stripXOBFromValue(v)
		}
	}
}

// stripXOBFromValue descends a JSON value that is a schema or an array of
// schemas, applying stripXOBFromSchema to each schema map it finds.
func stripXOBFromValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		stripXOBFromSchema(openbindings.JSONSchema(t))
	case []any:
		for _, e := range t {
			stripXOBFromValue(e)
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
// For http(s) URLs, the artifact is fetched (bounded, loud).
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

	// URL refs fetch: a tracked remote artifact embeds and pulls through the
	// same read path as a local file, so `?embed` on a URL pins the remote
	// descriptor and `ob source pull` refreshes the pinned copy.
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return fetchSourceContent(ref)
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

// maxSourceFetchBytes caps a fetched source artifact (16 MiB). Exceeding it
// is a loud error, never a truncation that would surface as a parse failure.
const maxSourceFetchBytes = 16 << 20

// fetchSourceContent GETs a source artifact over HTTP(S), bounded and loud.
func fetchSourceContent(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %q: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", url, err)
	}
	if len(data) > maxSourceFetchBytes {
		return nil, fmt.Errorf("fetch %q: artifact exceeds the %d-byte cap", url, maxSourceFetchBytes)
	}
	return data, nil
}

// IsEmbeddableLocalFile reports whether a source ref names a readable local
// file artifact — the lane that embeds by default under the D-05 ruling: a
// relative path in the spec-level location field can never be conformant
// (OBI-D-05) and a file:// URL is machine-coupled, so the portable form
// carries the artifact and the local path lives on in x-ob.ref as the pull
// path. Exec refs, URLs, and host:port live addresses are not local files.
func IsEmbeddableLocalFile(ref, baseDir string) bool {
	if ref == "" || execref.IsExec(ref) || strings.Contains(ref, "://") || isHostPort(ref) {
		return false
	}
	path := ref
	if !filepath.IsAbs(path) && baseDir != "" {
		path = filepath.Join(baseDir, path)
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
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
	// Determine the artifact family from the binding-specification identifier.
	family := SpecFamily(format)
	formatLower := strings.ToLower(format)

	isJSON := strings.Contains(formatLower, "json") ||
		family == "openapi" || family == "asyncapi" || family == "mcp"
	isYAML := strings.Contains(formatLower, "yaml") || strings.Contains(formatLower, "yml")

	if isJSON || isYAML {
		// Embed structured formats as a parsed object. OpenAPI/AsyncAPI artifacts
		// are commonly authored in YAML, so try JSON first and fall back to YAML
		// (every JSON document is also valid YAML, but JSON is the cheaper parse).
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err == nil {
			return obj, nil
		}
		if err := yaml.Unmarshal(data, &obj); err == nil {
			return obj, nil
		}
		if isJSON {
			return nil, fmt.Errorf("parse content for format %q: not valid JSON or YAML", format)
		}
	}

	// Binary artifacts cannot be embedded: the spec carries binaries via
	// location only (§6.4), and a byte-for-string conversion silently mangles
	// them into U+FFFD soup that still validates. Refuse loudly, naming the
	// gap and the supported lanes.
	if !utf8.Valid(data) {
		return nil, fmt.Errorf(
			"artifact is not valid UTF-8 and cannot be embedded: the OpenBindings spec carries binary artifacts via location only (a known gap for repo-local binaries). For protobuf, embed the .proto source or use a live reflection address (host:port); otherwise publish the artifact and point at it with --uri",
		)
	}

	// Embedded content must be SELF-CONTAINED (§6.4): no base URI exists for
	// references internal to an embedded artifact. A proto source with
	// imports cannot resolve them from inside a document, so deriving from
	// the embed fails later and cryptically — refuse now, naming the lanes
	// that do work.
	if strings.HasPrefix(SpecFamily(format), "grpc") {
		if imp := protoImportStatement(data); imp != "" {
			return nil, fmt.Errorf(
				"embedded content must be self-contained (spec §6.4): this .proto imports %s, which cannot resolve from inside a document — use a live reflection address (host:port), or keep the multi-file source via --resolve location with --uri",
				imp)
		}
	}

	// Default: return as string (works for KDL, protobuf, and other text formats).
	return string(data), nil
}

// protoImportRe matches a protobuf import statement (incl. public/weak forms).
var protoImportRe = regexp.MustCompile(`(?m)^\s*import\s+(?:public\s+|weak\s+)?"([^"]+)"`)

// protoImportStatement returns the first import path in a .proto source, or
// "" when the file is self-contained. The inline proto compile lane resolves
// no imports (not even the google well-known types), so any import makes an
// embedded proto underivable.
func protoImportStatement(data []byte) string {
	m := protoImportRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("%q", string(m[1]))
}

// ResolveSourceSpec applies an x-ob ref to populate spec-level fields on a source.
// Given raw source data and x-ob metadata, sets Source.Location and/or Source.Content.
func ResolveSourceSpec(src *openbindings.Source, meta SourceMeta, data []byte, obiDir string) error {
	switch meta.Resolve {
	case ResolveModeContent:
		content, err := ParseContentForEmbed(data, src.BindingSpec)
		if err != nil {
			return fmt.Errorf("embed content: %w", err)
		}
		src.Content = content
		// A URI alongside embedded content is spec-legal and format-defined
		// (§6.4): the artifact's canonical origin for document-located
		// formats, the service's dial address for service-addressed formats
		// (a gRPC host:port). Without one, embedding carries no location.
		src.Location = meta.URI
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

// FindXOBPaths walks a document value and returns the JSON paths of every
// x-ob key, sorted — the mechanical purity check `purify --check` and
// registry pre-publish gates key on.
func FindXOBPaths(doc any) []string {
	var paths []string
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				childPath := path + "/" + k
				if k == xobKey {
					paths = append(paths, childPath)
					continue
				}
				walk(child, childPath)
			}
		case []any:
			for i, child := range t {
				walk(child, fmt.Sprintf("%s/%d", path, i))
			}
		}
	}
	walk(doc, "")
	sort.Strings(paths)
	return paths
}

// ValidateDocumentValue runs the SDK's document validation over an untyped
// JSON value (a purified graph output, say) and returns the problems, or nil
// when the document is conformant. Unparseable input reports as one problem.
func ValidateDocumentValue(doc any) []string {
	data, err := json.Marshal(doc)
	if err != nil {
		return []string{fmt.Sprintf("marshal document: %v", err)}
	}
	iface, err := openbindings.ParseDocument(data)
	if err != nil {
		if ve, ok := err.(*openbindings.ValidationError); ok {
			return ve.Problems
		}
		return []string{err.Error()}
	}
	if err := iface.Validate(); err != nil {
		if ve, ok := err.(*openbindings.ValidationError); ok {
			return ve.Problems
		}
		return []string{err.Error()}
	}
	return nil
}

// sourceEmbedsContent reports whether a tracked source stores its artifact as
// embedded content (the D-05 ruling's local lane).
func sourceEmbedsContent(iface *openbindings.Interface, sourceKey string) bool {
	src, ok := iface.Sources[sourceKey]
	if !ok || src.Content == nil {
		return false
	}
	meta, err := GetSourceMeta(src)
	return err == nil && meta != nil && meta.Resolve == ResolveModeContent
}

// reconstructedBases holds per-key base snapshots rebuilt from a source's
// embedded content — the elided x-ob.base of the embed lane.
type reconstructedBases struct {
	ops   map[string]map[string]json.RawMessage
	binds map[string]map[string]json.RawMessage
}

// reconstructBases re-derives the last-synced snapshot from a source's
// embedded content. In embed mode the embedded artifact IS the last-synced
// artifact, so per-object x-ob.base copies are redundant (measured at ~40%
// of a committed document); they are elided at write time and rebuilt here
// on demand. Returns ok=false when the source is not embed-mode or was last
// synced by a DIFFERENT ob version (derivation rules may have changed;
// callers fall back to base-less behavior, which preserves author data at
// the cost of not detecting author-removals).
func reconstructBases(iface *openbindings.Interface, sourceKey string) (reconstructedBases, bool) {
	rb := reconstructedBases{}
	if !sourceEmbedsContent(iface, sourceKey) {
		return rb, false
	}
	src := iface.Sources[sourceKey]
	meta, err := GetSourceMeta(src)
	if err != nil || meta == nil || meta.OBVersion != OBVersion {
		return rb, false
	}
	derived, err := DeriveFromSource(src, sourceKey, "")
	if err != nil {
		return rb, false
	}
	rb.ops = make(map[string]map[string]json.RawMessage, len(derived.Operations))
	for k, op := range derived.Operations {
		if m, merr := ObjectToFieldMap(op); merr == nil {
			rb.ops[k] = m
		}
	}
	rb.binds = make(map[string]map[string]json.RawMessage, len(derived.Bindings))
	for k, b := range derived.Bindings {
		if m, merr := ObjectToFieldMap(b); merr == nil {
			rb.binds[k] = m
		}
	}
	return rb, true
}

// baseForOp returns the recorded x-ob.base for an operation when present,
// falling back to the reconstructed embed-lane base.
func baseForOp(lf openbindings.LosslessFields, opKey string, rb *reconstructedBases) map[string]json.RawMessage {
	if base, err := GetBase(lf); err == nil && base != nil {
		return base
	}
	if rb != nil && rb.ops != nil {
		return rb.ops[opKey]
	}
	return nil
}
