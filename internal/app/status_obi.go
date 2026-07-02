package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
)

// OBIStatusInput represents input for the OBI status command.
type OBIStatusInput struct {
	// OBIPath is the file path (CLI). Interface, when set, is the inline document
	// (the served operation) and takes precedence; relative source locations
	// then resolve against the current directory.
	OBIPath   string
	Interface *openbindings.Interface
}

// SourceStatus represents the sync status of a single source. Tracked reports
// whether ob tracks the source via x-ob provenance metadata (written by
// `source add` / synthesize); untracked sources are hand-authored and pull
// skips them.
type SourceStatus struct {
	Key        string `json:"key"`
	Format     string `json:"format"`
	Ref        string `json:"ref,omitempty"`
	Resolve    string `json:"resolve,omitempty"`
	InSync     bool   `json:"inSync"`
	Tracked    bool   `json:"tracked"`
	LastSynced string `json:"lastSynced,omitempty"`
	OBVersion  string `json:"obVersion,omitempty"`
	Error      string `json:"error,omitempty"`

	// Drift details: what `ob source pull` would change for this source —
	// added/updated/removed source-owned objects — plus custodial drift on
	// hand-authored bindings whose target no longer exists in the source.
	OperationsAdded   []string `json:"operationsAdded,omitempty"`
	OperationsUpdated []string `json:"operationsUpdated,omitempty"`
	OperationsRemoved []string `json:"operationsRemoved,omitempty"`
	BindingsAdded     []string `json:"bindingsAdded,omitempty"`
	BindingsUpdated   []string `json:"bindingsUpdated,omitempty"`
	BindingsRemoved   []string `json:"bindingsRemoved,omitempty"`
	Custodial         []string `json:"custodial,omitempty"`
}

// OBIStatusOutput represents the result of the OBI status command.
type OBIStatusOutput struct {
	Name       string         `json:"name,omitempty"`
	Version    string         `json:"version,omitempty"`
	OBIVersion string         `json:"obiVersion"`
	Sources    []SourceStatus `json:"sources"`
	Operations ProvenanceKeys `json:"operations"`
	Bindings   ProvenanceKeys `json:"bindings"`
}

// HasDrift reports whether any tracked source is out of sync (the source would
// add/update/remove objects, or a hand-authored binding has custodial drift).
func (o OBIStatusOutput) HasDrift() bool {
	for _, src := range o.Sources {
		if src.Tracked && !src.InSync {
			return true
		}
	}
	return false
}

// ProvenanceKeys lists keys split by provenance. Counts are len(SourceOwned) + len(HandAuthored).
type ProvenanceKeys struct {
	SourceOwned  []string `json:"sourceOwned,omitempty"`
	HandAuthored []string `json:"handAuthored,omitempty"`
}

// Render returns a human-friendly representation.
func (o OBIStatusOutput) Render() string {
	s := Styles
	var sb strings.Builder

	// Header.
	header := o.Name
	if header == "" {
		header = "(unnamed)"
	}
	if o.Version != "" {
		header += " " + o.Version
	}
	header += "  (openbindings " + o.OBIVersion + ")"
	sb.WriteString(s.Header.Render(header))
	sb.WriteString("\n")

	// Sources.
	sb.WriteString(fmt.Sprintf("\nSources (%d)\n", len(o.Sources)))
	if len(o.Sources) == 0 {
		sb.WriteString(s.Dim.Render("  (none)"))
	}
	for _, src := range o.Sources {
		sb.WriteString("  ")
		sb.WriteString(s.Key.Render(padRight(src.Key, 18)))
		sb.WriteString(padRight(src.Format, 16))
		if src.Ref != "" {
			sb.WriteString(padRight(src.Ref, 24))
		}
		if src.Error != "" {
			sb.WriteString(s.Warning.Render("error: " + src.Error))
		} else if !src.Tracked {
			sb.WriteString(s.Dim.Render("hand-authored"))
		} else if src.InSync {
			sb.WriteString(s.Success.Render("in sync"))
		} else {
			sb.WriteString(s.Warning.Render("out of sync"))
		}
		if src.Tracked && src.LastSynced != "" {
			sb.WriteString(s.Dim.Render(fmt.Sprintf(" (synced %s", formatTimeAgo(src.LastSynced))))
			if src.OBVersion != "" {
				sb.WriteString(s.Dim.Render(", ob " + src.OBVersion))
			}
			sb.WriteString(s.Dim.Render(")"))
		}
		sb.WriteString("\n")

		// Show diff details for out-of-sync sources.
		if !src.InSync && src.Tracked {
			renderSourceDiff(&sb, s, src)
		}
	}

	// Operations.
	renderProvenanceSection(&sb, s, "Operations", o.Operations)

	// Bindings.
	renderProvenanceSection(&sb, s, "Bindings", o.Bindings)

	// Sync summary.
	outOfSync := 0
	for _, src := range o.Sources {
		if src.Tracked && !src.InSync {
			outOfSync++
		}
	}
	if outOfSync > 0 {
		sb.WriteString(fmt.Sprintf("\n%s",
			s.Warning.Render(fmt.Sprintf("%d source(s) out of sync. Run 'ob source pull <obi>' to update.", outOfSync))))
	} else if len(o.Sources) > 0 {
		sb.WriteString(fmt.Sprintf("\n%s", s.Success.Render("All sources in sync.")))
	}

	return sb.String()
}

// renderSourceDiff appends per-source diff details (what ob source pull would change).
func renderSourceDiff(sb *strings.Builder, s styles, src SourceStatus) {
	lines := make([]string, 0, 7)
	if len(src.OperationsAdded) > 0 {
		lines = append(lines, fmt.Sprintf("operations to add: %s", strings.Join(src.OperationsAdded, ", ")))
	}
	if len(src.OperationsUpdated) > 0 {
		lines = append(lines, fmt.Sprintf("operations to update: %s", strings.Join(src.OperationsUpdated, ", ")))
	}
	if len(src.OperationsRemoved) > 0 {
		lines = append(lines, fmt.Sprintf("operations to remove (orphaned): %s", strings.Join(src.OperationsRemoved, ", ")))
	}
	if len(src.BindingsAdded) > 0 {
		lines = append(lines, fmt.Sprintf("bindings to add: %s", strings.Join(src.BindingsAdded, ", ")))
	}
	if len(src.BindingsUpdated) > 0 {
		lines = append(lines, fmt.Sprintf("bindings to update: %s", strings.Join(src.BindingsUpdated, ", ")))
	}
	if len(src.BindingsRemoved) > 0 {
		lines = append(lines, fmt.Sprintf("bindings to remove (orphaned): %s", strings.Join(src.BindingsRemoved, ", ")))
	}
	if len(src.Custodial) > 0 {
		lines = append(lines, fmt.Sprintf("custodial drift (hand-authored, target gone): %s", strings.Join(src.Custodial, ", ")))
	}
	for _, line := range lines {
		sb.WriteString(s.Dim.Render("    ↳ " + line))
		sb.WriteString("\n")
	}
}

// renderProvenanceSection appends a labeled section showing source-owned vs hand-authored keys.
func renderProvenanceSection(sb *strings.Builder, s styles, label string, pk ProvenanceKeys) {
	total := len(pk.SourceOwned) + len(pk.HandAuthored)
	sb.WriteString(fmt.Sprintf("\n%s (%d)", label, total))
	if total > 0 {
		parts := make([]string, 0, 2)
		if len(pk.SourceOwned) > 0 {
			parts = append(parts, fmt.Sprintf("%d source-owned", len(pk.SourceOwned)))
		}
		if len(pk.HandAuthored) > 0 {
			parts = append(parts, fmt.Sprintf("%d hand-authored", len(pk.HandAuthored)))
		}
		sb.WriteString(s.Dim.Render("  — " + strings.Join(parts, ", ")))
	}
	sb.WriteString("\n")
}

// OBIStatus computes the drift and management status of an OBI.
func OBIStatus(input OBIStatusInput) (OBIStatusOutput, error) {
	iface := input.Interface
	obiDir := "."
	if iface == nil {
		var err error
		iface, err = loadInterfaceFile(input.OBIPath)
		if err != nil {
			return OBIStatusOutput{}, fmt.Errorf("load OBI: %w", err)
		}
		obiDir = filepath.Dir(input.OBIPath)
	}

	// Collect source statuses. Initialized non-nil so the wire output is
	// always an array (the InterfaceStatus schema requires it), never null.
	sources := []SourceStatus{}

	var srcKeys []string
	for k := range iface.Sources {
		srcKeys = append(srcKeys, k)
	}
	sort.Strings(srcKeys)

	for _, key := range srcKeys {
		src := iface.Sources[key]
		ss := SourceStatus{
			Key:    key,
			Format: src.Format,
		}

		meta, err := GetSourceMeta(src)
		if err != nil {
			ss.Error = err.Error()
			sources = append(sources, ss)
			continue
		}

		if meta == nil {
			// Hand-authored source.
			if src.Location != "" {
				ss.Ref = src.Location
			}
			sources = append(sources, ss)
			continue
		}

		ss.Tracked = true
		ss.Ref = meta.Ref
		ss.Resolve = meta.Resolve
		ss.LastSynced = meta.LastSynced
		ss.OBVersion = meta.OBVersion

		if err := detectSourceDrift(iface, key, obiDir, &ss); err != nil {
			ss.Error = err.Error()
		}
		sources = append(sources, ss)
	}

	// Classify operations by provenance.
	var ops ProvenanceKeys
	for key, op := range iface.Operations {
		if IsSourceOwned(op.LosslessFields) {
			ops.SourceOwned = append(ops.SourceOwned, key)
		} else {
			ops.HandAuthored = append(ops.HandAuthored, key)
		}
	}
	sort.Strings(ops.SourceOwned)
	sort.Strings(ops.HandAuthored)

	// Classify bindings by provenance.
	var binds ProvenanceKeys
	for key, b := range iface.Bindings {
		if IsSourceOwned(b.LosslessFields) {
			binds.SourceOwned = append(binds.SourceOwned, key)
		} else {
			binds.HandAuthored = append(binds.HandAuthored, key)
		}
	}
	sort.Strings(binds.SourceOwned)
	sort.Strings(binds.HandAuthored)

	return OBIStatusOutput{
		Name:       iface.Name,
		Version:    iface.Version,
		OBIVersion: iface.OpenBindings,
		Sources:    sources,
		Operations: ops,
		Bindings:   binds,
	}, nil
}

// detectSourceDrift fills a source's drift fields by performing a dry `pull`
// on a throwaway clone of the interface — so status reports exactly what
// `ob source pull` would change (added/updated/removed source-owned objects) —
// then adds custodial drift for hand-authored bindings to this source whose
// target the source no longer emits (pull leaves those alone, so they'd
// silently rot into a dead ref).
func detectSourceDrift(iface *openbindings.Interface, key, obiDir string, ss *SourceStatus) error {
	clone, err := cloneInterface(iface)
	if err != nil {
		return err
	}
	derived, ok, warning := reReadAndDerive(clone, key, obiDir)
	if !ok {
		if warning != "" {
			return fmt.Errorf("%s", warning)
		}
		return nil
	}

	var pull SourcePullOutput
	pullSourceInto(clone, key, derived, &pull)
	ss.OperationsAdded = pull.OperationsAdded
	ss.OperationsUpdated = pull.OperationsUpdated
	ss.OperationsRemoved = pull.OperationsPruned
	ss.BindingsAdded = pull.BindingsAdded
	ss.BindingsUpdated = pull.BindingsUpdated
	ss.BindingsRemoved = pull.BindingsPruned

	derivedRefs := map[string]bool{}
	for _, b := range derived.Bindings {
		derivedRefs[b.Ref] = true
	}
	var custodial []string
	for _, b := range iface.Bindings {
		if b.Source != key || IsSourceOwned(b.LosslessFields) {
			continue // source-owned bindings are handled by the pull pass above
		}
		if !derivedRefs[b.Ref] {
			custodial = append(custodial, fmt.Sprintf("%s → %s (target gone)", b.Operation, b.Ref))
		}
	}
	sort.Strings(custodial)
	ss.Custodial = custodial

	ss.InSync = len(ss.OperationsAdded) == 0 && len(ss.OperationsUpdated) == 0 &&
		len(ss.OperationsRemoved) == 0 && len(ss.BindingsAdded) == 0 &&
		len(ss.BindingsUpdated) == 0 && len(ss.BindingsRemoved) == 0 &&
		len(ss.Custodial) == 0
	return nil
}

// cloneInterface deep-copies an interface via a JSON round-trip, for read-only
// dry-run computations that must not mutate the original.
func cloneInterface(iface *openbindings.Interface) (*openbindings.Interface, error) {
	data, err := json.Marshal(iface)
	if err != nil {
		return nil, err
	}
	var clone openbindings.Interface
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

// padRight pads a string to the given width with spaces.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-len(s))
}

// formatTimeAgo returns a human-friendly relative time string.
func formatTimeAgo(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
