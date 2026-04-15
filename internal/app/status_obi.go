package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OBIStatusInput represents input for the OBI status command.
type OBIStatusInput struct {
	OBIPath string
}

// SourceStatus represents the sync status of a single source.
type SourceStatus struct {
	Key        string `json:"key"`
	Format     string `json:"format"`
	Ref        string `json:"ref,omitempty"`
	Resolve    string `json:"resolve,omitempty"`
	InSync     bool   `json:"inSync"`
	Managed    bool   `json:"managed"`
	LastSynced string `json:"lastSynced,omitempty"`
	OBVersion  string `json:"obVersion,omitempty"`
	Error      string `json:"error,omitempty"`

	// Diff details: what ob sync would change for this source.
	OperationsAdded      []string `json:"operationsAdded,omitempty"`
	OperationsUpdated    []string `json:"operationsUpdated,omitempty"`
	OperationsConflicted []string `json:"operationsConflicted,omitempty"`
	BindingsAdded        []string `json:"bindingsAdded,omitempty"`
	BindingsUpdated      []string `json:"bindingsUpdated,omitempty"`
	BindingsConflicted   []string `json:"bindingsConflicted,omitempty"`
}

// OBIStatusOutput represents the result of the OBI status command.
type OBIStatusOutput struct {
	Name       string         `json:"name,omitempty"`
	Version    string         `json:"version,omitempty"`
	OBIVersion string         `json:"obiVersion"`
	Sources    []SourceStatus `json:"sources"`
	Operations ManagedKeys    `json:"operations"`
	Bindings   ManagedKeys    `json:"bindings"`
}

// ManagedKeys lists keys split by management status. Counts are len(Managed) + len(HandAuthored).
type ManagedKeys struct {
	Managed      []string `json:"managed,omitempty"`
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
		} else if !src.Managed {
			sb.WriteString(s.Dim.Render("hand-authored"))
		} else if src.InSync {
			sb.WriteString(s.Success.Render("in sync"))
		} else {
			sb.WriteString(s.Warning.Render("out of sync"))
		}
		if src.Managed && src.LastSynced != "" {
			sb.WriteString(s.Dim.Render(fmt.Sprintf(" (synced %s", formatTimeAgo(src.LastSynced))))
			if src.OBVersion != "" {
				sb.WriteString(s.Dim.Render(", ob " + src.OBVersion))
			}
			sb.WriteString(s.Dim.Render(")"))
		}
		sb.WriteString("\n")

		// Show diff details for out-of-sync sources.
		if !src.InSync && src.Managed {
			renderSourceDiff(&sb, s, src)
		}
	}

	// Operations.
	renderManagedSection(&sb, s, "Operations", o.Operations)

	// Bindings.
	renderManagedSection(&sb, s, "Bindings", o.Bindings)

	// Sync summary.
	outOfSync := 0
	for _, src := range o.Sources {
		if src.Managed && !src.InSync {
			outOfSync++
		}
	}
	if outOfSync > 0 {
		sb.WriteString(fmt.Sprintf("\n%s",
			s.Warning.Render(fmt.Sprintf("%d source(s) out of sync. Run 'ob sync <obi>' to update.", outOfSync))))
	} else if len(o.Sources) > 0 {
		sb.WriteString(fmt.Sprintf("\n%s", s.Success.Render("All sources in sync.")))
	}

	return sb.String()
}

// renderSourceDiff appends per-source diff details (what ob sync would change).
func renderSourceDiff(sb *strings.Builder, s styles, src SourceStatus) {
	lines := make([]string, 0, 6)
	if len(src.OperationsAdded) > 0 {
		lines = append(lines, fmt.Sprintf("operations to add: %s", strings.Join(src.OperationsAdded, ", ")))
	}
	if len(src.OperationsUpdated) > 0 {
		lines = append(lines, fmt.Sprintf("operations to update: %s", strings.Join(src.OperationsUpdated, ", ")))
	}
	if len(src.OperationsConflicted) > 0 {
		lines = append(lines, fmt.Sprintf("operations with conflicts: %s", strings.Join(src.OperationsConflicted, ", ")))
	}
	if len(src.BindingsAdded) > 0 {
		lines = append(lines, fmt.Sprintf("bindings to add: %s", strings.Join(src.BindingsAdded, ", ")))
	}
	if len(src.BindingsUpdated) > 0 {
		lines = append(lines, fmt.Sprintf("bindings to update: %s", strings.Join(src.BindingsUpdated, ", ")))
	}
	if len(src.BindingsConflicted) > 0 {
		lines = append(lines, fmt.Sprintf("bindings with conflicts: %s", strings.Join(src.BindingsConflicted, ", ")))
	}
	for _, line := range lines {
		sb.WriteString(s.Dim.Render("    ↳ " + line))
		sb.WriteString("\n")
	}
}

// renderManagedSection appends a labeled section showing managed vs hand-authored keys.
func renderManagedSection(sb *strings.Builder, s styles, label string, mk ManagedKeys) {
	total := len(mk.Managed) + len(mk.HandAuthored)
	sb.WriteString(fmt.Sprintf("\n%s (%d)", label, total))
	if total > 0 {
		parts := make([]string, 0, 2)
		if len(mk.Managed) > 0 {
			parts = append(parts, fmt.Sprintf("%d managed", len(mk.Managed)))
		}
		if len(mk.HandAuthored) > 0 {
			parts = append(parts, fmt.Sprintf("%d hand-authored", len(mk.HandAuthored)))
		}
		sb.WriteString(s.Dim.Render("  — " + strings.Join(parts, ", ")))
	}
	sb.WriteString("\n")
}

// OBIStatus computes the drift and management status of an OBI.
func OBIStatus(input OBIStatusInput) (OBIStatusOutput, error) {
	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return OBIStatusOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	obiDir := filepath.Dir(input.OBIPath)

	// Collect source statuses.
	var sources []SourceStatus

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

		ss.Managed = true
		ss.Ref = meta.Ref
		ss.Resolve = meta.Resolve
		ss.LastSynced = meta.LastSynced
		ss.OBVersion = meta.OBVersion

		// Preview what sync would change for this source.
		preview, err := PreviewSourceMerge(src, key, iface, obiDir)
		if err != nil {
			ss.Error = err.Error()
			sources = append(sources, ss)
			continue
		}

		populateSourceDiff(&ss, preview)
		sources = append(sources, ss)
	}

	// Classify operations by management status.
	var ops ManagedKeys
	for key, op := range iface.Operations {
		if HasXOB(op.LosslessFields) {
			ops.Managed = append(ops.Managed, key)
		} else {
			ops.HandAuthored = append(ops.HandAuthored, key)
		}
	}
	sort.Strings(ops.Managed)
	sort.Strings(ops.HandAuthored)

	// Classify bindings by management status.
	var binds ManagedKeys
	for key, b := range iface.Bindings {
		if HasXOB(b.LosslessFields) {
			binds.Managed = append(binds.Managed, key)
		} else {
			binds.HandAuthored = append(binds.HandAuthored, key)
		}
	}
	sort.Strings(binds.Managed)
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

// populateSourceDiff fills SourceStatus diff fields from a MergePreview.
// InSync is true only when the preview has no changes or conflicts.
func populateSourceDiff(ss *SourceStatus, preview MergePreview) {
	ss.OperationsAdded = preview.OperationsAdded()
	ss.OperationsUpdated = preview.OperationsUpdated()
	ss.OperationsConflicted = preview.OperationsConflicted()
	ss.BindingsAdded = preview.BindingsAdded()
	ss.BindingsUpdated = preview.BindingsUpdated()
	ss.BindingsConflicted = preview.BindingsConflicted()
	ss.InSync = !preview.HasChanges()
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
