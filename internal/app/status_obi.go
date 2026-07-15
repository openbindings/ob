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
	// (the served operation) and takes precedence; relative x-ob pull paths
	// then resolve against the current directory.
	OBIPath   string
	Interface *openbindings.Interface
}

// SourceStatus represents the sync status of a single source. Tracked reports
// whether ob tracks the source via x-ob provenance metadata (written by
// `source add` / synthesize); untracked sources are hand-authored and pull
// skips them.
type SourceStatus struct {
	Key         string `json:"key"`
	BindingSpec string `json:"bindingSpec"`
	Ref         string `json:"ref,omitempty"`
	Resolve     string `json:"resolve,omitempty"`
	InSync      bool   `json:"inSync"`
	Tracked     bool   `json:"tracked"`
	LastSynced  string `json:"lastSynced,omitempty"`
	OBVersion   string `json:"obVersion,omitempty"`
	Error       string `json:"error,omitempty"`

	// ContentDrift reports that the artifact's bytes have diverged from the
	// last-synced state (the recorded contentHash — and in embed mode, the
	// embedded copy itself) even when no derived operation or binding
	// changes. In embed mode the embedded copy is the invocation authority
	// (server URLs, auth, envelopes), so content-only drift is real drift.
	ContentDrift bool `json:"contentDrift,omitempty"`
	// PullPathUnreachable reports that the x-ob pull path cannot be read
	// from here while the document itself remains fully usable (embedded
	// content). The pull path is tool metadata and does not travel with the
	// document; the embedded copy remains authoritative. This is its own
	// state, not drift: a pull from here can never succeed, so it is
	// excluded from the out-of-sync rollup and from HasDrift.
	PullPathUnreachable bool `json:"pullPathUnreachable,omitempty"`

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
// add/update/remove objects, content has drifted, or a hand-authored binding
// has custodial drift). An unreachable pull path is NOT drift: the document is
// self-contained and no pull from here can act on it.
func (o OBIStatusOutput) HasDrift() bool {
	for _, src := range o.Sources {
		if src.Tracked && !src.InSync && !src.PullPathUnreachable {
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
		sb.WriteString(padRight(src.BindingSpec, 16))
		if src.Ref != "" {
			sb.WriteString(padRight(src.Ref, 24))
		}
		if src.Error != "" {
			sb.WriteString(s.Warning.Render("error: " + src.Error))
		} else if !src.Tracked {
			sb.WriteString(s.Dim.Render("hand-authored"))
		} else if src.PullPathUnreachable {
			sb.WriteString(s.Warning.Render("pull path unreachable"))
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

		if src.PullPathUnreachable {
			if strings.Contains(src.Ref, "://") {
				sb.WriteString(s.Dim.Render(fmt.Sprintf(
					"    ↳ %s could not be fetched from here — the embedded copy remains authoritative; pull again when the artifact is reachable", src.Ref)))
			} else {
				sb.WriteString(s.Dim.Render(fmt.Sprintf(
					"    ↳ %s is not readable from here — the pull path is tool metadata and does not travel with the document; the embedded copy remains authoritative", src.Ref)))
			}
			sb.WriteString("\n")
		}

		// Show diff details for out-of-sync sources.
		if !src.InSync && src.Tracked && !src.PullPathUnreachable {
			renderSourceDiff(&sb, s, src)
		}
	}

	// Operations.
	renderProvenanceSection(&sb, s, "Operations", o.Operations)

	// Bindings.
	renderProvenanceSection(&sb, s, "Bindings", o.Bindings)

	// Sync summary. Unreachable pull paths are their own class: a pull from
	// here can never act on them, so the pull advice would be a lie.
	outOfSync := 0
	unreachable := 0
	for _, src := range o.Sources {
		if !src.Tracked {
			continue
		}
		if src.PullPathUnreachable {
			unreachable++
		} else if !src.InSync {
			outOfSync++
		}
	}
	if outOfSync > 0 {
		sb.WriteString(fmt.Sprintf("\n%s",
			s.Warning.Render(fmt.Sprintf("%d source(s) out of sync. Run 'ob source pull <obi>' to update.", outOfSync))))
	}
	if unreachable > 0 {
		sb.WriteString(fmt.Sprintf("\n%s",
			s.Dim.Render(fmt.Sprintf("%d source(s) with unreachable pull paths — the document is self-contained; pull where the artifact is reachable.", unreachable))))
	}
	if outOfSync == 0 && unreachable == 0 && len(o.Sources) > 0 {
		sb.WriteString(fmt.Sprintf("\n%s", s.Success.Render("All sources in sync.")))
	}

	return sb.String()
}

// renderSourceDiff appends per-source diff details (what ob source pull would change).
func renderSourceDiff(sb *strings.Builder, s styles, src SourceStatus) {
	lines := make([]string, 0, 8)
	if src.ContentDrift {
		if src.Resolve == ResolveModeContent {
			lines = append(lines, fmt.Sprintf(
				"content drifted: the embedded copy no longer matches %s — invocation uses the embedded copy; run 'ob source pull'", src.Ref))
		} else {
			lines = append(lines, "artifact content changed since last sync — run 'ob source pull' to refresh the sync cursor")
		}
	}
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
			Key:         key,
			BindingSpec: src.BindingSpec,
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

		// Content integrity for file-lane sources. The dry pull below only
		// compares DERIVED objects, but in embed mode the embedded copy is
		// the invocation authority: a changed server URL, auth scheme, or a
		// corrupted embed alters invocation without touching any operation.
		// The recorded contentHash seals the pull-path file; the embedded
		// copy is verified against a fresh parse of those bytes.
		if !needsLiveDiscovery(src.BindingSpec, meta.Ref) {
			data, rerr := ReadSourceContent(meta.Ref, obiDir)
			if rerr != nil {
				if meta.Resolve == ResolveModeContent && src.Content != nil {
					// The pull path does not travel with the document; the
					// embedded copy keeps it fully usable. Own state, no
					// drift verdict, no pull advice.
					ss.PullPathUnreachable = true
					sources = append(sources, ss)
					continue
				}
				// Location mode: the ref is also the resolution path, so an
				// unreadable artifact is a real error.
				ss.Error = fmt.Sprintf("read failed: %v", rerr)
				sources = append(sources, ss)
				continue
			}
			if HashContent(data) != meta.ContentHash {
				ss.ContentDrift = true
			} else if meta.Resolve == ResolveModeContent && src.Content != nil {
				// File unchanged: verify the embedded copy still matches it.
				// A hand-edited or corrupted embed is invisible to the hash,
				// which covers the file, not the copy.
				if fresh, perr := ParseContentForEmbed(data, src.BindingSpec); perr == nil && !sameContentValue(src.Content, fresh) {
					ss.ContentDrift = true
				}
			}
		}

		if err := detectSourceDrift(iface, key, obiDir, &ss); err != nil {
			ss.Error = err.Error()
		}
		if ss.ContentDrift {
			ss.InSync = false
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
	// The JSON round-trip drops empty maps to nil (omitempty); the dry pull
	// assigns into both.
	if clone.Operations == nil {
		clone.Operations = map[string]openbindings.Operation{}
	}
	if clone.Bindings == nil {
		clone.Bindings = map[string]openbindings.BindingEntry{}
	}
	// Reconstruct embed-lane bases from the OLD content before the dry
	// refresh replaces it (mirrors SourcePull).
	var oldBases *reconstructedBases
	if rb, rok := reconstructBases(clone, key); rok {
		oldBases = &rb
	}
	derived, ok, warning := reReadAndDerive(clone, key, obiDir)
	if !ok {
		if warning != "" {
			return fmt.Errorf("%s", warning)
		}
		return nil
	}

	var pull SourcePullOutput
	pullSourceInto(clone, key, derived, oldBases, &pull)
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

// sameContentValue reports whether two embedded-content values (string for
// textual formats, object for JSON formats) are semantically equal, via a
// JSON round-trip (encoding/json sorts map keys, so key order is irrelevant).
func sameContentValue(a, b any) bool {
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(aj) == string(bj)
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
