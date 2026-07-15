package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
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
	BindingSpec    string `json:"bindingSpec"`
	Location       string `json:"location,omitempty"`
	Name           string `json:"name,omitempty"` // key in sources
	Content        any    `json:"content,omitempty"`
	OutputLocation string `json:"outputLocation,omitempty"`
	Description    string `json:"description,omitempty"`
	Embed          bool   `json:"embed,omitempty"`
	Delegate       string `json:"-"` // delegate identifier to store in x-ob; not part of the wire contract
}

// SynthesizeInterfaceInput represents input for the synthesizeInterface operation.
type SynthesizeInterfaceInput struct {
	OpenBindingsVersion string                      `json:"openbindingsVersion,omitempty"`
	Sources             []SynthesizeInterfaceSource `json:"sources,omitempty"`
	Name                string                      `json:"name,omitempty"`
	Version             string                      `json:"version,omitempty"`
	Description         string                      `json:"description,omitempty"`
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
	// file path (e.g. ./api.yaml, /tmp/spec.json).
	colonIdx := strings.Index(mainPart, ":")
	if colonIdx > 0 {
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
		key := openbindings.SanitizeKey(sb.String())
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
func printSynthesizerWarning(w openbindings.SynthesizerWarning) {
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

	generated, err := SynthesizeInterfaceFromSource(context.Background(), &openbindings.SynthesizeInput{
		Sources: []openbindings.SynthesizeSource{
			{
				BindingSpec: src.BindingSpec,
				Location:    src.Location,
				Content:     src.Content,
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

	// Build x-ob metadata (resolve mode determined above).
	meta := SourceMeta{
		Ref:      src.Location,
		Resolve:  resolveMode,
		Delegate: src.Delegate,
	}

	// If OutputLocation is set, use it as the published URI.
	if src.OutputLocation != "" && src.OutputLocation != src.Location {
		meta.URI = src.OutputLocation
	}

	switch {
	case src.Content != nil:
		// Content provided directly on the wire (SynthesizeInterfaceSource
		// content, the schema's alternative to location): carry it inline.
		bsrc.Content = src.Content
	case resolveMode == ResolveModeContent:
		// Read and embed content (format decides object vs string, the same
		// parse `source add --resolve content` and pull refreshes use).
		data, err := ReadSourceContent(src.Location, "")
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
		bsrc.Location = meta.URI
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
