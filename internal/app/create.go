package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/synthesize"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Default interface values when not provided
const (
	DefaultInterfaceName = "My Interface"
)

// SynthesizeInterfaceSource represents a source for interface creation.
// The json tags realize the wire contract's SynthesizeInterfaceSource schema
// (interface-synthesizer requirement), so machine callers — the serve route,
// `ob synthesize --input`, delegate invocations — decode without an adapter.
type SynthesizeInterfaceSource struct {
	BindingSpec    string          `json:"bindingSpec"`
	Location       string          `json:"location,omitempty"`
	Name           string          `json:"name,omitempty"` // key in sources
	Content        json.RawMessage `json:"content,omitempty"`
	OutputLocation string          `json:"outputLocation,omitempty"`
	Description    string          `json:"description,omitempty"`
	Embed          bool            `json:"embed,omitempty"`
	Delegate       string          `json:"-"` // delegate identifier to store in x-ob; not part of the wire contract
}

// SynthesizeInterfaceInput represents input for the synthesizeInterface operation.
type SynthesizeInterfaceInput struct {
	OpenBindingsVersion string                      `json:"openbindingsVersion,omitempty"`
	Sources             []SynthesizeInterfaceSource `json:"sources,omitempty"`
	Name                string                      `json:"name,omitempty"`
	Version             string                      `json:"version,omitempty"`
	Description         string                      `json:"description,omitempty"`
}

// AddInterfaceSourceInput is the transport-independent addSource operation
// input. Unlike SourceAddInput, it contains values rather than CLI file
// locators and can therefore represent embedded source artifacts faithfully.
type AddInterfaceSourceInput struct {
	Interface *openbindings.Interface   `json:"interface"`
	Source    SynthesizeInterfaceSource `json:"source"`
}

// SourceExistsError reports the conflict specific to AddInterfaceSource.
type SourceExistsError struct {
	Key string
}

func (e *SourceExistsError) Error() string {
	return fmt.Sprintf("source key %q already exists", e.Key)
}

// AddInterfaceSource realizes addSource on an in-memory interface. The CLI
// machine lane and ob start share it so location- and content-based artifacts
// have one semantic implementation.
func AddInterfaceSource(input AddInterfaceSourceInput) (*openbindings.Interface, error) {
	if input.Interface == nil {
		return nil, fmt.Errorf("interface is required")
	}
	if input.Source.BindingSpec == "" {
		return nil, fmt.Errorf("source.bindingSpec is required")
	}

	derived, err := SynthesizeInterface(SynthesizeInterfaceInput{Sources: []SynthesizeInterfaceSource{input.Source}})
	if err != nil {
		return nil, err
	}
	if len(derived.Sources) != 1 {
		return nil, fmt.Errorf("source synthesis returned %d source entries; expected exactly one", len(derived.Sources))
	}
	if input.Interface.Sources == nil {
		input.Interface.Sources = map[string]openbindings.Source{}
	}
	for key, source := range derived.Sources {
		if _, exists := input.Interface.Sources[key]; exists {
			return nil, &SourceExistsError{Key: key}
		}
		input.Interface.Sources[key] = source
	}
	return input.Interface, nil
}

// ParseSource parses a source string in one of two forms:
//
//	format:path[?option&option...]   — explicit format
//	path[?option&option...]          — bare path (format auto-detected later)
//
// Options are specified after a '?' delimiter (like URL query params):
//   - name=X             Key name in sources
//   - outputLocation=Y   Location to use in output (instead of input path)
//   - description=Z      Description for this binding source
//   - embed              Embed content inline
//
// Examples:
//
//	usage@2.0.0:./cli.kdl?name=cli&embed
//	openapi.json
//	./api.yaml?name=restApi
func ParseSource(s string) (SynthesizeInterfaceSource, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return SynthesizeInterfaceSource{}, fmt.Errorf("empty source")
	}

	// Split main part from options (delimited by ?)
	mainPart := s
	optionsPart := ""
	if idx := strings.Index(s, "?"); idx >= 0 {
		mainPart = s[:idx]
		optionsPart = s[idx+1:]
	}

	var src SynthesizeInterfaceSource

	// Determine if this is format:path or a bare path.
	// Format tokens contain '@' (e.g. openapi@3.1) or are short names
	// without path characters. If the prefix contains '@', it's definitely
	// a format token. Otherwise, if it contains '/', '.', or '\', it's a
	// file path (e.g. ./api.yaml, /tmp/spec.json). A `scheme://` URL is never
	// a format:path — the `//` after the colon marks it as the whole location
	// (so http://host, https://…, ws://…, wss://… are not split on their scheme).
	colonIdx := strings.Index(mainPart, ":")
	if colonIdx > 0 && !strings.HasPrefix(mainPart[colonIdx+1:], "//") {
		prefix := mainPart[:colonIdx]
		if strings.Contains(prefix, "@") || !strings.ContainsAny(prefix, "/.\\ ") {
			src.BindingSpec = prefix
			src.Location = mainPart[colonIdx+1:]
		} else {
			src.Location = mainPart
		}
	} else {
		// No colon — bare path.
		src.Location = mainPart
	}

	if src.Location == "" {
		return SynthesizeInterfaceSource{}, fmt.Errorf("source path cannot be empty")
	}

	// Parse options (& delimited)
	if optionsPart != "" {
		opts := strings.Split(optionsPart, "&")
		for _, opt := range opts {
			opt = strings.TrimSpace(opt)
			if opt == "" {
				continue
			}
			if opt == "embed" {
				src.Embed = true
				continue
			}

			// Handle key=value options
			if idx := strings.Index(opt, "="); idx > 0 {
				key := opt[:idx]
				value := opt[idx+1:]
				switch key {
				case "name":
					src.Name = value
				case "outputLocation":
					src.OutputLocation = value
				case "description":
					src.Description = value
				default:
					return SynthesizeInterfaceSource{}, fmt.Errorf("unknown source option %q", key)
				}
			} else {
				return SynthesizeInterfaceSource{}, fmt.Errorf("invalid source option %q (expected key=value or 'embed')", opt)
			}
		}
	}

	return src, nil
}

// StdinSourceContent converts artifact bytes read from the stdin lane (a
// source location of `-`, the same filter convention the editing family's
// <obi-path> honors) into the content-mode carrier: the format is detected
// from the bytes when not explicit, and the bytes embed per that format's
// content convention — the same parse `?embed` and `source add --resolve
// content` use. What stdin delivers is CONTENT, not a location: callers
// leave the source's location empty, so the output document records the
// artifact exactly as a wire-supplied content source does (inline content,
// no pull path — never a fabricated "-" path). An `?outputLocation=` still
// writes the spec-level location, the same as on every other lane.
func StdinSourceContent(bindingSpec string, data []byte) (string, json.RawMessage, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "", nil, fmt.Errorf("stdin is empty (a source location of - reads the artifact from stdin)")
	}
	if bindingSpec == "" {
		detected, err := DetectSourceFormatFromBytes(data)
		if err != nil {
			return "", nil, err
		}
		bindingSpec = detected
	}
	content, err := ParseContentForEmbed(data, bindingSpec)
	if err != nil {
		return "", nil, err
	}
	return bindingSpec, content, nil
}

// DeriveSourceKey generates a default key for a binding source from its
// file path. The format is only used as a last-resort fallback when the
// filename yields nothing useful. Exported so cmd layer can use it for
// prompt defaults.
func DeriveSourceKey(src SynthesizeInterfaceSource, index int) string {
	if src.Name != "" {
		return src.Name
	}

	baseName := filepath.Base(src.Location)
	if ext := filepath.Ext(baseName); ext != "" {
		baseName = baseName[:len(baseName)-len(ext)]
	}

	// Convert separators to camelCase boundaries.
	parts := strings.FieldsFunc(baseName, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	if len(parts) > 0 {
		var sb strings.Builder
		sb.WriteString(strings.ToLower(parts[0]))
		for _, p := range parts[1:] {
			sb.WriteString(cases.Title(language.English).String(strings.ToLower(p)))
		}
		// A live-address location (localhost:9090, https://host) derives a
		// name with characters OBI-D-03 forbids in map keys; sanitize so
		// synthesize output always validates.
		key := synthesize.SanitizeKey(sb.String())
		if key != "" && len(key) <= 30 {
			return key
		}
	}

	formatName := SpecFamily(src.BindingSpec)
	if index == 0 {
		return formatName
	}
	return fmt.Sprintf("%s%d", formatName, index)
}

// SynthesizeInterface creates an OpenBindings interface from the given input.
func SynthesizeInterface(input SynthesizeInterfaceInput) (*openbindings.Interface, error) {
	targetVersion := input.OpenBindingsVersion
	if targetVersion == "" {
		targetVersion = openbindings.MaxTestedVersion
	}

	ok, err := openbindings.IsSupportedVersion(targetVersion)
	if err != nil || !ok {
		return nil, fmt.Errorf("unsupported openbindings version %q", targetVersion)
	}

	iface := openbindings.Interface{
		OpenBindings: targetVersion,
		Name:         DefaultInterfaceName,
		Operations:   map[string]openbindings.Operation{},
		Sources:      map[string]openbindings.Source{},
		Bindings:     map[string]openbindings.BindingEntry{},
	}

	for i, src := range input.Sources {
		if err := processSource(&iface, src, i); err != nil {
			return nil, fmt.Errorf("source %s (%s): %w", src.Location, src.BindingSpec, err)
		}
	}

	if input.Name != "" {
		iface.Name = input.Name
	}
	if input.Version != "" {
		iface.Version = input.Version
	}
	if input.Description != "" {
		iface.Description = input.Description
	}

	return &iface, nil
}

// printSynthesizerWarning surfaces a non-fatal synthesis limitation on stderr —
// the tooling-output surface SynthesizerWarning documents (lossy conversions
// such as grpc skipping an unmappable construct).
func printSynthesizerWarning(w synthesize.SynthesizerWarning) {
	if w.Path != "" {
		fmt.Fprintf(os.Stderr, "warning: %s: %s (%s)\n", w.Code, w.Message, w.Path)
		return
	}
	fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
}

// processSource processes a single source and adds its operations/bindings to the interface.
// It uses the OperationInvoker to dispatch format-specific conversion, then applies
// format-agnostic merge logic.
func processSource(iface *openbindings.Interface, src SynthesizeInterfaceSource, index int) error {
	sourceKey := DeriveSourceKey(src, index)

	generated, err := SynthesizeInterfaceFromSource(context.Background(), &synthesize.SynthesizeInput{
		Sources: []synthesize.SynthesizeSource{
			{
				BindingSpec: src.BindingSpec,
				Location:    src.Location,
				Content:     src.Content,
				Embed:       src.Embed || IsEmbeddableLocalFile(src.Location, ""),
			},
		},
		OnWarning: printSynthesizerWarning,
	})
	if err != nil {
		return err
	}

	return mergeGeneratedSource(iface, generated, src, sourceKey)
}

// mergeGeneratedSource merges a handler-generated Interface into the target,
// applying format-agnostic merge logic for metadata, operations, sources, and bindings.
// It writes x-ob metadata on sources and marks generated operations/bindings as
// source-owned.
func mergeGeneratedSource(iface *openbindings.Interface, generated *openbindings.Interface, src SynthesizeInterfaceSource, sourceKey string) error {
	// Merge metadata from first source if not set.
	if iface.Name == DefaultInterfaceName && generated.Name != "" {
		iface.Name = generated.Name
	}
	if iface.Description == "" && generated.Description != "" {
		iface.Description = generated.Description
	}
	if iface.Version == "" && generated.Version != "" {
		iface.Version = generated.Version
	}

	// Determine the resolve mode up front: it decides both how the source
	// entry stores its artifact and whether per-object x-ob bases are
	// recorded or elided (the embed lane reconstructs them from content).
	var resolveMode string
	switch {
	case src.Embed || src.Content != nil:
		resolveMode = ResolveModeContent
	case src.OutputLocation != "":
		// The author supplied the published pointer: location mode.
		resolveMode = ResolveModeLocation
	case IsEmbeddableLocalFile(src.Location, ""):
		// THE FLIP (D-05 ruling): a local file artifact embeds by default.
		// A relative path in the spec-level location field can never be
		// conformant (OBI-D-05) and file:// is machine-coupled; the portable
		// form carries the artifact, and the local path lives on in x-ob.ref
		// as the pull path.
		resolveMode = ResolveModeContent
	default:
		resolveMode = ResolveModeLocation
	}
	elideBase := resolveMode == ResolveModeContent

	// Add operations, marking each as source-owned. In location mode the
	// source fields are recorded as the initial three-way-merge base, so the
	// very first `ob merge --from-sources` already has a real base and
	// hand-authored local-only fields (satisfies, aliases, deprecated, tags)
	// are preserved instead of falling through to the legacy heuristic in
	// MergeOperation. In embed mode the base is elided: the embedded content
	// IS the last-synced artifact, so bases reconstruct on demand and
	// storing copies would roughly double the committed document.
	//
	// First source to define an operation wins for the definition
	// (kind, schemas, description). Subsequent sources only contribute
	// bindings under their own source key.
	for key, op := range generated.Operations {
		if _, exists := iface.Operations[key]; exists {
			continue
		}
		if elideBase {
			SetXOB(&op.LosslessFields)
		} else {
			baseFields, err := ObjectToFieldMap(op)
			if err != nil {
				return fmt.Errorf("op %q: build base for x-ob: %w", key, err)
			}
			if err := SetBase(&op.LosslessFields, baseFields); err != nil {
				return fmt.Errorf("op %q: set base: %w", key, err)
			}
		}
		iface.Operations[key] = op
	}

	// Merge schemas from subsequent sources that aren't already present.
	if generated.Schemas != nil {
		if iface.Schemas == nil {
			iface.Schemas = map[string]openbindings.JSONSchema{}
		}
		for key, schema := range generated.Schemas {
			if _, exists := iface.Schemas[key]; !exists {
				iface.Schemas[key] = schema
			}
		}
	}

	// Create source entry.
	bsrc := openbindings.Source{
		BindingSpec: src.BindingSpec,
		Description: src.Description,
	}
	// Preserve the provider's source identity. Embedded content is authoritative,
	// but may still need its explicitly carried base for references or servers.
	if len(generated.Sources) == 1 {
		for _, source := range generated.Sources {
			bsrc.Location = source.Location
			bsrc.Content = source.Content
		}
	}

	// Build x-ob metadata (resolve mode determined above).
	meta := SourceMeta{
		Ref:      src.Location,
		Resolve:  resolveMode,
		Delegate: src.Delegate,
	}

	// If OutputLocation is set, use it as the published URI.
	if src.OutputLocation != "" {
		meta.URI = src.OutputLocation
	}

	switch {
	case src.Content != nil:
		// Content provided directly on the wire (SynthesizeInterfaceSource
		// content, the schema's alternative to location): carry it inline.
		bsrc.Content = src.Content
		// outputLocation= means spec-level location in EVERY synthesis lane:
		// content + outputLocation carries both, exactly like the
		// `?embed&outputLocation=` lane below — the artifact plus the
		// format-defined location (§6.4). The value is recorded verbatim,
		// matching the file lane's posture: synthesis never polices it, the
		// invoke-time gate (resolveSourceLocation) refuses a relative
		// spec-level location under OBI-D-05 for all lanes alike.
		if meta.URI != "" {
			bsrc.Location = meta.URI
		}
	case resolveMode == ResolveModeContent:
		// Read and embed content (format decides object vs string, the same
		// parse `source add --resolve content` and pull refreshes use).
		// Prefer the provider's already-analyzed artifact. Re-fetching here
		// could embed a different revision from the operation projection.
		var data []byte
		var err error
		if bsrc.Content != nil {
			data, err = openbindings.ContentToBytes(bsrc.Content)
		} else {
			data, err = ReadSourceContent(src.Location, "")
		}
		if err != nil {
			return fmt.Errorf("embed content: %w", err)
		}
		content, err := ParseContentForEmbed(data, src.BindingSpec)
		if err != nil {
			return fmt.Errorf("embed content: %w", err)
		}
		bsrc.Content = content
		meta.ContentHash = HashContent(data)
		// `?embed&outputLocation=` carries both: the pinned artifact plus
		// the format-defined location (canonical origin, or the service's
		// dial address for service-addressed formats — §6.4).
		if meta.URI != "" {
			bsrc.Location = meta.URI
		}
	default:
		// Use outputLocation if provided, otherwise input location.
		if src.OutputLocation != "" {
			bsrc.Location = src.OutputLocation
		} else {
			bsrc.Location = src.Location
		}
	}

	// Compute the contentHash when the embed lane hasn't already (location
	// mode). URLs fetch; live addresses have no bytes to hash and skip.
	if meta.ContentHash == "" {
		if data, err := ReadSourceContent(src.Location, ""); err == nil {
			meta.ContentHash = HashContent(data)
		}
	}

	// Set sync timestamps.
	meta.LastSynced = NowISO()
	meta.OBVersion = OBVersion

	// Write x-ob metadata onto the source.
	if err := SetSourceMeta(&bsrc, meta); err != nil {
		return fmt.Errorf("set source x-ob: %w", err)
	}

	iface.Sources[sourceKey] = bsrc

	// Add bindings, remapping source key. Each is marked source-owned and
	// gets its initial base recorded in x-ob (same reason as the
	// operations loop above).
	if iface.Bindings == nil {
		iface.Bindings = map[string]openbindings.BindingEntry{}
	}
	for bk, entry := range remapBindingKeys(generated.Bindings, sourceKey) {
		if elideBase {
			SetXOB(&entry.LosslessFields)
			iface.Bindings[bk] = entry
			continue
		}
		baseFields, err := ObjectToFieldMap(entry)
		if err != nil {
			return fmt.Errorf("binding %q: build base for x-ob: %w", bk, err)
		}
		if err := SetBase(&entry.LosslessFields, baseFields); err != nil {
			return fmt.Errorf("binding %q: set base: %w", bk, err)
		}
		iface.Bindings[bk] = entry
	}

	return nil
}
