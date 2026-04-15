package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

// Default interface values when not provided
const (
	DefaultInterfaceName = "My Interface"
)

// CreateInterfaceSource represents a source for interface creation.
type CreateInterfaceSource struct {
	Format         string
	Location       string
	Name           string // key in sources
	OutputLocation string
	Description    string
	Embed          bool
	Delegate       string // delegate identifier to store in x-ob
}

// CreateInterfaceInput represents input for the createInterface operation.
type CreateInterfaceInput struct {
	OpenBindingsVersion string
	Sources             []CreateInterfaceSource
	Name                string
	Version             string
	Description         string
}

// RenderInterface returns a human-friendly summary of a created interface.
func RenderInterface(iface *openbindings.Interface) string {
	s := Styles
	var sb strings.Builder

	if iface == nil {
		return s.Dim.Render("No interface created")
	}

	sb.WriteString(s.Header.Render("Created OpenBindings Interface"))
	sb.WriteString("\n\n")

	if iface.Name != "" {
		sb.WriteString(s.Dim.Render("Name: "))
		sb.WriteString(iface.Name)
		sb.WriteString("\n")
	}

	if iface.Version != "" {
		sb.WriteString(s.Dim.Render("Version: "))
		sb.WriteString(iface.Version)
		sb.WriteString("\n")
	}

	sb.WriteString(s.Dim.Render("Operations: "))
	sb.WriteString(fmt.Sprintf("%d", len(iface.Operations)))
	sb.WriteString("\n")

	sb.WriteString(s.Dim.Render("Sources: "))
	sb.WriteString(fmt.Sprintf("%d", len(iface.Sources)))
	sb.WriteString("\n")

	sb.WriteString(s.Dim.Render("Bindings: "))
	sb.WriteString(fmt.Sprintf("%d", len(iface.Bindings)))

	return sb.String()
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
//	usage@2.13.1:./cli.kdl?name=cli&embed
//	openapi.json
//	./api.yaml?name=restApi
func ParseSource(s string) (CreateInterfaceSource, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return CreateInterfaceSource{}, fmt.Errorf("empty source")
	}

	// Split main part from options (delimited by ?)
	mainPart := s
	optionsPart := ""
	if idx := strings.Index(s, "?"); idx >= 0 {
		mainPart = s[:idx]
		optionsPart = s[idx+1:]
	}

	var src CreateInterfaceSource

	// Determine if this is format:path or a bare path.
	// Format tokens contain '@' (e.g. openapi@3.1) or are short names
	// without path characters. If the prefix contains '@', it's definitely
	// a format token. Otherwise, if it contains '/', '.', or '\', it's a
	// file path (e.g. ./api.yaml, /tmp/spec.json).
	colonIdx := strings.Index(mainPart, ":")
	if colonIdx > 0 {
		prefix := mainPart[:colonIdx]
		if strings.Contains(prefix, "@") || !strings.ContainsAny(prefix, "/.\\ ") {
			src.Format = prefix
			src.Location = mainPart[colonIdx+1:]
		} else {
			src.Location = mainPart
		}
	} else {
		// No colon — bare path.
		src.Location = mainPart
	}

	if src.Location == "" {
		return CreateInterfaceSource{}, fmt.Errorf("source path cannot be empty")
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
					return CreateInterfaceSource{}, fmt.Errorf("unknown source option %q", key)
				}
			} else {
				return CreateInterfaceSource{}, fmt.Errorf("invalid source option %q (expected key=value or 'embed')", opt)
			}
		}
	}

	return src, nil
}

// DeriveSourceKey generates a default key for a binding source from its
// file path. The format is only used as a last-resort fallback when the
// filename yields nothing useful. Exported so cmd layer can use it for
// prompt defaults.
func DeriveSourceKey(src CreateInterfaceSource, index int) string {
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
		key := sb.String()
		if key != "" && len(key) <= 30 {
			return key
		}
	}

	formatName := strings.Split(src.Format, "@")[0]
	if index == 0 {
		return formatName
	}
	return fmt.Sprintf("%s%d", formatName, index)
}

// CreateInterface creates an OpenBindings interface from the given input.
func CreateInterface(input CreateInterfaceInput) (*openbindings.Interface, error) {
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
			return nil, fmt.Errorf("source %s (%s): %w", src.Location, src.Format, err)
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

// processSource processes a single source and adds its operations/bindings to the interface.
// It uses the OperationExecutor to dispatch format-specific conversion, then applies
// format-agnostic merge logic.
func processSource(iface *openbindings.Interface, src CreateInterfaceSource, index int) error {
	sourceKey := DeriveSourceKey(src, index)

	generated, err := CreateInterfaceFromSource(context.Background(), &openbindings.CreateInput{
		Sources: []openbindings.CreateSource{
			{
				Format:   src.Format,
				Location: src.Location,
			},
		},
	})
	if err != nil {
		return err
	}

	return mergeGeneratedSource(iface, generated, src, sourceKey)
}

// mergeGeneratedSource merges a handler-generated Interface into the target,
// applying format-agnostic merge logic for metadata, operations, sources, and bindings.
// It writes x-ob metadata on sources and marks generated operations/bindings as managed.
func mergeGeneratedSource(iface *openbindings.Interface, generated *openbindings.Interface, src CreateInterfaceSource, sourceKey string) error {
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

	// Add operations, marking each as managed and recording the source
	// fields as the initial three-way-merge base. Storing the base at
	// create time means the very first `ob sync` already has a real
	// base to merge against, so hand-authored local-only fields
	// (satisfies, aliases, deprecated, tags) are preserved correctly
	// instead of falling through to the legacy heuristic in
	// MergeOperation.
	//
	// First source to define an operation wins for the definition
	// (kind, schemas, description). Subsequent sources only contribute
	// bindings under their own source key.
	for key, op := range generated.Operations {
		if _, exists := iface.Operations[key]; exists {
			continue
		}
		baseFields, err := ObjectToFieldMap(op)
		if err != nil {
			return fmt.Errorf("op %q: build base for x-ob: %w", key, err)
		}
		if err := SetBase(&op.LosslessFields, baseFields); err != nil {
			return fmt.Errorf("op %q: set base: %w", key, err)
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
		Format:      src.Format,
		Description: src.Description,
	}

	// Determine resolve mode and build x-ob metadata.
	var resolveMode string
	if src.Embed {
		resolveMode = ResolveModeContent
	} else {
		resolveMode = ResolveModeLocation
	}

	meta := SourceMeta{
		Ref:      src.Location,
		Resolve:  resolveMode,
		Delegate: src.Delegate,
	}

	// If OutputLocation is set, use it as the published URI.
	if src.OutputLocation != "" && src.OutputLocation != src.Location {
		meta.URI = src.OutputLocation
	}

	if src.Embed {
		// Read and embed content.
		content, err := readEmbedContent(src.Location)
		if err != nil {
			return fmt.Errorf("embed content: %w", err)
		}
		bsrc.Content = content
	} else {
		// Use outputLocation if provided, otherwise input location.
		if src.OutputLocation != "" {
			bsrc.Location = src.OutputLocation
		} else {
			bsrc.Location = src.Location
		}
	}

	// Compute contentHash from the source file (same path for both modes).
	if data, err := os.ReadFile(src.Location); err == nil {
		meta.ContentHash = HashContent(data)
	}

	// Set sync timestamps.
	meta.LastSynced = NowISO()
	meta.OBVersion = OBVersion

	// Write x-ob metadata onto the source.
	if err := SetSourceMeta(&bsrc, meta); err != nil {
		return fmt.Errorf("set source x-ob: %w", err)
	}

	iface.Sources[sourceKey] = bsrc

	// Add bindings, remapping source key. Each is marked managed and
	// gets its initial base recorded in x-ob (same reason as the
	// operations loop above).
	if iface.Bindings == nil {
		iface.Bindings = map[string]openbindings.BindingEntry{}
	}
	for bk, entry := range remapBindingKeys(generated.Bindings, sourceKey) {
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

// readEmbedContent reads a file and returns its content as map[string]any for embedding.
// Supports JSON and YAML files. For other formats, returns an error with guidance.
func readEmbedContent(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".json":
		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		return result, nil

	case ".yaml", ".yml":
		var result map[string]any
		if err := yaml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("parse YAML: %w", err)
		}
		return result, nil

	default:
		// Non-JSON/YAML formats (KDL, protobuf, etc.) are embedded as raw string content.
		return string(data), nil
	}
}

