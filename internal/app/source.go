package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openbindings/ob/internal/execref"

	"github.com/openbindings/openbindings-go"
)

// SourceAddInput represents input for adding a source to an OBI file.
type SourceAddInput struct {
	OBIPath     string // path to the OBI file
	Format      string // e.g. "usage@2.13.1"
	Location    string // path to the source (relative to CWD)
	Key         string // explicit key override (optional)
	Resolve     string // resolve mode: "location" (default) or "content"
	URI         string // explicit published URI override (optional)
	Delegate    string // delegate that handles this source (e.g. "ob")
	Description string // human-readable description for the source entry (optional)
}

// SourceAddOutput represents the result of adding a source.
type SourceAddOutput struct {
	Key      string `json:"key"`
	Format   string `json:"format"`
	Resolve  string `json:"resolve"`
	Ref      string `json:"ref"`
	URI      string `json:"uri,omitempty"`
	Delegate string `json:"delegate,omitempty"`
}

// Render returns a human-friendly representation.
func (o SourceAddOutput) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Added source"))
	sb.WriteString("\n\n")
	sb.WriteString(s.Dim.Render("Key:      "))
	sb.WriteString(s.Key.Render(o.Key))
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("Format:   "))
	sb.WriteString(o.Format)
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("Ref:      "))
	sb.WriteString(o.Ref)
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("Resolve:  "))
	sb.WriteString(o.Resolve)
	if o.URI != "" {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("URI:      "))
		sb.WriteString(o.URI)
	}
	if o.Delegate != "" {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("Delegate: "))
		sb.WriteString(o.Delegate)
	}
	return sb.String()
}

// SourceListOutput is the listSources wire output: a bare array of entries per
// the contract's output schema (never null — a sourceless interface lists as []).
type SourceListOutput []SourceEntry

// SourceEntry is a single source in the list: the source as stored in the
// interface, tagged with its key.
type SourceEntry struct {
	Key    string              `json:"key"`
	Source openbindings.Source `json:"source"`
}

// Render returns a human-friendly representation.
func (o SourceListOutput) Render() string {
	s := Styles
	if len(o) == 0 {
		return s.Dim.Render("No sources registered")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Sources"))
	sb.WriteString("\n")
	for _, src := range o {
		sb.WriteString("\n  ")
		sb.WriteString(s.Key.Render(src.Key))
		sb.WriteString(s.Dim.Render("  "))
		sb.WriteString(src.Source.Format)
		if src.Source.Location != "" {
			sb.WriteString(s.Dim.Render("  → "))
			sb.WriteString(src.Source.Location)
		} else if src.Source.Content != nil {
			sb.WriteString(s.Dim.Render("  (content)"))
		}
	}
	return sb.String()
}

// SourceRemoveOutput represents the result of removing a source.
type SourceRemoveOutput struct {
	Key             string   `json:"key"`
	RemovedBindings int      `json:"removedBindings,omitempty"`
	UnboundOps      []string `json:"unboundOps,omitempty"`
}

// Render returns a human-friendly representation.
func (o SourceRemoveOutput) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Removed source"))
	sb.WriteString(" ")
	sb.WriteString(s.Key.Render(o.Key))
	if o.RemovedBindings > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render(fmt.Sprintf(
			"  %d binding(s) removed",
			o.RemovedBindings,
		)))
	}
	if len(o.UnboundOps) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Warning.Render(fmt.Sprintf(
			"  warning: %d operation(s) now have no bindings: %s",
			len(o.UnboundOps),
			strings.Join(o.UnboundOps, ", "),
		)))
	}
	return sb.String()
}

// SourceAdd adds a source reference to an OBI file.
// It reads the file, adds the source entry with x-ob metadata, and writes it
// back atomically. Local file artifacts embed by default (the D-05 ruling);
// the pull path is stored relative to the OBI file's directory in x-ob.ref.
func SourceAdd(input SourceAddInput) (SourceAddOutput, error) {
	// Load the OBI file.
	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return SourceAddOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	// Compute source path relative to OBI file directory.
	obiDir := filepath.Dir(input.OBIPath)
	relRef := makeRelativeToDir(input.Location, obiDir)

	// Default resolve mode. THE FLIP (D-05 ruling): a local file artifact
	// embeds by default — a relative path in the spec-level location field
	// can never be conformant (OBI-D-05) and file:// is machine-coupled, so
	// the portable form carries the artifact and the local path lives on in
	// x-ob.ref as the pull path. An explicit --resolve or a published --uri
	// keeps location mode; URLs, exec refs, and live addresses stay pointers.
	resolveMode := input.Resolve
	if resolveMode == "" {
		if input.URI == "" && IsEmbeddableLocalFile(relRef, obiDir) {
			resolveMode = ResolveModeContent
		} else {
			resolveMode = ResolveModeLocation
		}
	}
	if resolveMode != ResolveModeLocation && resolveMode != ResolveModeContent {
		return SourceAddOutput{}, fmt.Errorf("invalid --resolve value %q; must be %q or %q", resolveMode, ResolveModeLocation, ResolveModeContent)
	}

	// Derive the source key.
	key := input.Key
	if key == "" {
		key = DeriveSourceKey(SynthesizeInterfaceSource{
			Format:   input.Format,
			Location: input.Location,
		}, 0)
	}

	if iface.Sources == nil {
		iface.Sources = map[string]openbindings.Source{}
	}

	// Check if this source artifact is already registered under any key —
	// by spec location or, for embedded sources, by the x-ob pull path.
	for existingKey, src := range iface.Sources {
		if src.Location == relRef {
			return SourceAddOutput{}, fmt.Errorf(
				"source %q is already registered as %q",
				relRef, existingKey,
			)
		}
		if m, merr := GetSourceMeta(src); merr == nil && m != nil && m.Ref == relRef {
			return SourceAddOutput{}, fmt.Errorf(
				"source %q is already registered as %q",
				relRef, existingKey,
			)
		}
	}

	// Check for key collision.
	if _, exists := iface.Sources[key]; exists {
		return SourceAddOutput{}, fmt.Errorf(
			"source key %q already exists; use --key <name> to specify a different key",
			key,
		)
	}

	// Read source content to compute hash and potentially embed.
	data, contentHash, err := ReadAndHashSource(relRef, obiDir)
	if err != nil {
		return SourceAddOutput{}, fmt.Errorf("read source: %w", err)
	}

	// Build the source entry.
	src := openbindings.Source{
		Format:      input.Format,
		Description: input.Description,
	}

	// Build x-ob metadata.
	meta := SourceMeta{
		Ref:         relRef,
		Resolve:     resolveMode,
		URI:         input.URI,
		ContentHash: contentHash,
		LastSynced:  NowISO(),
		OBVersion:   OBVersion,
		Delegate:    input.Delegate,
	}

	// Apply resolve mode to set spec-level fields.
	if err := ResolveSourceSpec(&src, meta, data, obiDir); err != nil {
		return SourceAddOutput{}, fmt.Errorf("resolve source: %w", err)
	}

	// Write x-ob metadata.
	if err := SetSourceMeta(&src, meta); err != nil {
		return SourceAddOutput{}, fmt.Errorf("set x-ob metadata: %w", err)
	}

	// Add the source entry.
	iface.Sources[key] = src

	// Write back atomically (D6).
	if err := WriteInterfaceFile(input.OBIPath, iface); err != nil {
		return SourceAddOutput{}, fmt.Errorf("write OBI: %w", err)
	}

	return SourceAddOutput{
		Key:      key,
		Format:   input.Format,
		Resolve:  resolveMode,
		Ref:      relRef,
		URI:      input.URI,
		Delegate: input.Delegate,
	}, nil
}

// SourceList lists all sources in an OBI file.
func SourceList(obiPath string) (SourceListOutput, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI: %w", err)
	}

	entries := SourceListOutput{}
	for key, src := range iface.Sources {
		entries = append(entries, SourceEntry{Key: key, Source: src})
	}

	// Sort by key for deterministic output.
	sortSourceEntries(entries)

	return entries, nil
}

// SourceRemove removes a source reference and its bindings from an OBI file.
// Operations are preserved but warned about if they become unbound.
func SourceRemove(obiPath string, key string) (SourceRemoveOutput, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return SourceRemoveOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	if _, exists := iface.Sources[key]; !exists {
		// Tolerant: removing an absent source succeeds with nothing removed.
		return SourceRemoveOutput{Key: key}, nil
	}

	// Remove bindings that reference this source.
	removedBindings := 0
	affectedOps := map[string]bool{}
	for bk, b := range iface.Bindings {
		if b.Source == key {
			affectedOps[b.Operation] = true
			delete(iface.Bindings, bk)
			removedBindings++
		}
	}

	// Remove the source.
	delete(iface.Sources, key)

	// Check which affected operations are now fully unbound.
	var unboundOps []string
	for opKey := range affectedOps {
		if _, exists := iface.Operations[opKey]; !exists {
			continue
		}
		stillBound := false
		for _, b := range iface.Bindings {
			if b.Operation == opKey {
				stillBound = true
				break
			}
		}
		if !stillBound {
			unboundOps = append(unboundOps, opKey)
		}
	}

	// Write back atomically (D6).
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return SourceRemoveOutput{}, fmt.Errorf("write OBI: %w", err)
	}

	return SourceRemoveOutput{
		Key:             key,
		RemovedBindings: removedBindings,
		UnboundOps:      unboundOps,
	}, nil
}

// makeRelativeToDir converts a path to be relative to the given directory.
// If the path is already absolute, it is made relative to dir.
// If the path is relative (to CWD), it is resolved to absolute first, then
// made relative to dir. Falls back to the original path on any error.
func makeRelativeToDir(path string, dir string) string {
	// Only FILE paths relativize. URLs, exec refs, and host:port live
	// addresses are not filesystem locations; treating a URL as a path
	// mangled it into ../..-prefixed garbage that could never be read.
	if execref.IsExec(path) || strings.Contains(path, "://") || isHostPort(path) {
		return path
	}

	// If already absolute, just make relative to dir.
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return path // fall back to absolute
		}
		return rel
	}

	// Resolve relative path against CWD.
	abs, err := filepath.Abs(path)
	if err != nil {
		return path // fall back to original
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return path
	}

	rel, err := filepath.Rel(absDir, abs)
	if err != nil {
		return path
	}

	return rel
}

// sortSourceEntries sorts source entries by key.
func sortSourceEntries(entries []SourceEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
}
