package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
)

// carryCodegenName copies an author-set codegen-name override from an existing
// operation onto its freshly source-derived replacement. Pull owns the spec
// fields a source produces, never this hint, so the override must survive
// regeneration. A no-op when the existing operation carries no override.
func carryCodegenName(existing openbindings.Operation, fresh *openbindings.Operation, opKey string, warnings *[]string) {
	cn := GetCodegenName(existing.LosslessFields)
	if cn == "" {
		return
	}
	if err := SetCodegenName(&fresh.LosslessFields, cn); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("op %q: preserve codegen name: %v", opKey, err))
	}
}

// carryOutputSchemaElection re-applies an author's output-schema election
// onto a freshly source-derived operation (the non-detaching contract: the
// election survives regeneration and is compared modulo the derivation).
// CONFLICT RULE: a grown, non-floor-stamped SOURCE output schema WINS — the
// source now speaks the real shape, so the election is displaced and the
// displacement is reported loudly. Otherwise (the source still derives the
// floor, or nothing) the elected schema replaces the derivation and the
// marker is re-stamped so the next pull compares against it too.
func carryOutputSchemaElection(existing openbindings.Operation, fresh *openbindings.Operation, opKey string, warnings *[]string) {
	elected, err := GetOutputSchemaElection(existing.LosslessFields)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("op %q: read output-schema election: %v", opKey, err))
		return
	}
	if elected == nil {
		return
	}
	if fresh.Output != nil && !openbindings.FloorStamped(fresh.Output) {
		// The source grew a real output schema: grown coverage wins.
		*warnings = append(*warnings, fmt.Sprintf(
			"op %q: the source now derives a real output schema; the prior output-schema election is displaced", opKey))
		return
	}
	fresh.Output = elected
	if err := SetOutputSchemaElection(&fresh.LosslessFields, elected); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("op %q: preserve output-schema election: %v", opKey, err))
	}
}

// carryAuthoredNames preserves the author's alias/tag overlay across a
// source-owned refresh. Satisfaction aliases (OBI-T-12) are spec-level author
// data no binding source owns, and tags may be author-curated on top of
// derived ones. Each list merges three-way against the base (the last pure
// derivation — recorded in x-ob, or reconstructed from embedded content):
// names the author added carry onto the fresh derivation, names the author
// removed stay removed, and everything else follows the source. A nil base
// treats every existing name as authored (the data-preserving fallback).
func carryAuthoredNames(existing openbindings.Operation, fresh *openbindings.Operation, base map[string]json.RawMessage) {
	fresh.Aliases = mergeNameList(baseNameList(base, "aliases"), existing.Aliases, fresh.Aliases)
	fresh.Tags = mergeNameList(baseNameList(base, "tags"), existing.Tags, fresh.Tags)
}

// baseNameList extracts a string-list field from a recorded x-ob base snapshot.
func baseNameList(base map[string]json.RawMessage, field string) []string {
	if base == nil {
		return nil
	}
	raw, ok := base[field]
	if !ok {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// mergeNameList three-way-merges a name list: the fresh derivation is the new
// baseline, author additions (in existing but not in base) are appended in
// their existing order, and author removals (in base but not in existing)
// stay removed even when the source derives them again.
func mergeNameList(base, existing, fresh []string) []string {
	inBase := make(map[string]bool, len(base))
	for _, n := range base {
		inBase[n] = true
	}
	inExisting := make(map[string]bool, len(existing))
	for _, n := range existing {
		inExisting[n] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, n := range fresh {
		if inBase[n] && !inExisting[n] {
			continue // author removed a derived name: stays removed
		}
		if !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	for _, n := range existing {
		if inBase[n] {
			continue // derived-or-kept: the fresh derivation decides
		}
		if !seen[n] {
			out = append(out, n) // author addition survives the refresh
			seen[n] = true
		}
	}
	return out
}

// sameXOB reports whether two lossless field sets carry identical x-ob
// payloads (base snapshot, codegen-name override, output-schema election),
// so the no-op check recognizes a rebuilt operation whose stored bytes would
// not change.
func sameXOB(a, b openbindings.LosslessFields) bool {
	ax, err1 := getOpBindingXOB(a)
	bx, err2 := getOpBindingXOB(b)
	if err1 != nil || err2 != nil {
		return false
	}
	aj, _ := json.Marshal(ax)
	bj, _ := json.Marshal(bx)
	return string(aj) == string(bj)
}

// SourcePullInput represents input for the `source pull` command.
type SourcePullInput struct {
	OBIPath    string   // path to the OBI file
	SourceKeys []string // specific sources to pull (empty = all)
	OutputPath string   // write to a different path
	Format     string   // output format override
	Pure       bool     // strip all x-ob metadata from the output (publish-clean); requires OutputPath
}

// SourcePullOutput reports what a pull changed. Each field is the set of keys
// affected (count = len); no redundant counts. Interface carries the resulting
// document itself — contract-required, like ConformResult/MergeResult: a wire
// consumer pulling an inline interface has no file to read the result back
// from, so the report is the only channel that can return it.
type SourcePullOutput struct {
	Interface *openbindings.Interface `json:"interface"`
	Sources   []string                `json:"sources,omitempty"`
	// Skipped lists sources pull passes over by design (hand-authored, no
	// x-ob tracking). Failed lists tracked sources whose read/derive FAILED —
	// a pull with failures is incomplete and exits non-zero, never "complete".
	Skipped           []string `json:"skipped,omitempty"`
	Failed            []string `json:"failed,omitempty"`
	OperationsAdded   []string `json:"operationsAdded,omitempty"`
	OperationsUpdated []string `json:"operationsUpdated,omitempty"`
	OperationsPruned  []string `json:"operationsPruned,omitempty"`
	BindingsAdded     []string `json:"bindingsAdded,omitempty"`
	BindingsUpdated   []string `json:"bindingsUpdated,omitempty"`
	BindingsPruned    []string `json:"bindingsPruned,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
}

// Render returns a human-friendly representation.
func (o SourcePullOutput) Render() string {
	s := Styles
	var sb strings.Builder
	if len(o.Failed) > 0 {
		sb.WriteString(s.Warning.Render("Pull incomplete"))
	} else {
		sb.WriteString(s.Header.Render("Pull complete"))
	}
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("  %d source(s) pulled", len(o.Sources)))
	if len(o.Skipped) > 0 {
		sb.WriteString(fmt.Sprintf(", %d skipped", len(o.Skipped)))
	}
	if len(o.Failed) > 0 {
		sb.WriteString(fmt.Sprintf(", %d failed", len(o.Failed)))
	}
	if len(o.Sources) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  Sources: "))
		sb.WriteString(strings.Join(o.Sources, ", "))
	}
	if len(o.Failed) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Warning.Render("  Failed: " + strings.Join(o.Failed, ", ")))
	}
	renderKeyGroup(&sb, s, "Operations", o.OperationsUpdated, o.OperationsAdded)
	if len(o.OperationsPruned) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  Operations pruned: "))
		sb.WriteString(s.Removed.Render(strings.Join(o.OperationsPruned, ", ")))
	}
	renderKeyGroup(&sb, s, "Bindings", o.BindingsUpdated, o.BindingsAdded)
	if len(o.BindingsPruned) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  Bindings pruned: "))
		sb.WriteString(s.Removed.Render(strings.Join(o.BindingsPruned, ", ")))
	}
	for _, w := range o.Warnings {
		sb.WriteString("\n")
		sb.WriteString(s.Warning.Render("  warning: " + w))
	}
	return sb.String()
}

// renderKeyGroup appends a labeled section for updated/added keys to sb.
func renderKeyGroup(sb *strings.Builder, s styles, label string, updated, added []string) {
	if len(updated) == 0 && len(added) == 0 {
		return
	}
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  " + label + ": "))
	parts := make([]string, 0, 2)
	if len(updated) > 0 {
		parts = append(parts, fmt.Sprintf("%d updated (%s)", len(updated), strings.Join(updated, ", ")))
	}
	if len(added) > 0 {
		parts = append(parts, fmt.Sprintf("%d added (%s)", len(added), strings.Join(added, ", ")))
	}
	sb.WriteString(strings.Join(parts, ", "))
}

// SourceRefs returns the sorted, unique bindable refs a registered source
// exposes — the candidates for `operation bind`.
func SourceRefs(obiPath, sourceKey string) ([]string, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI: %w", err)
	}
	src, ok := iface.Sources[sourceKey]
	if !ok {
		return nil, fmt.Errorf("source %q is not registered", sourceKey)
	}
	derived, err := DeriveFromSource(src, sourceKey, filepath.Dir(obiPath))
	if err != nil {
		return nil, fmt.Errorf("derive source %q: %w", sourceKey, err)
	}
	seen := map[string]bool{}
	var refs []string
	for _, b := range derived.Bindings {
		if b.Ref != "" && !seen[b.Ref] {
			seen[b.Ref] = true
			refs = append(refs, b.Ref)
		}
	}
	sort.Strings(refs)
	return refs, nil
}

// SourcePull re-derives operations and bindings from registered sources,
// overwriting source-owned objects and pruning ones the source no longer
// emits. Hand-authored operations and bindings (no x-ob provenance) are never
// touched. Unlike the retired three-way `sync`, pull does not merge: the source
// is authoritative for what it owns.
func SourcePull(input SourcePullInput) (SourcePullOutput, error) {
	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return SourcePullOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	if iface.Operations == nil {
		iface.Operations = map[string]openbindings.Operation{}
	}
	if iface.Bindings == nil {
		iface.Bindings = map[string]openbindings.BindingEntry{}
	}

	obiDir := filepath.Dir(input.OBIPath)
	targetKeys, err := resolveTargetKeys(iface, input.SourceKeys)
	if err != nil {
		return SourcePullOutput{}, err
	}

	out := SourcePullOutput{}

	for _, key := range targetKeys {
		// Reconstruct the last-synced bases from the OLD embedded content
		// BEFORE the refresh replaces it (nil for location-mode sources,
		// which carry recorded bases instead).
		var oldBases *reconstructedBases
		if rb, ok := reconstructBases(iface, key); ok {
			oldBases = &rb
		}
		derived, ok, warning := reReadAndDerive(iface, key, obiDir)
		if warning != "" {
			out.Warnings = append(out.Warnings, warning)
		}
		if !ok {
			// A warning marks a real failure (unreadable, underivable); a
			// silent skip is a hand-authored source pull ignores by design.
			if warning != "" {
				out.Failed = append(out.Failed, key)
			} else {
				out.Skipped = append(out.Skipped, key)
			}
			continue
		}
		out.Sources = append(out.Sources, key)
		pullSourceInto(iface, key, derived, oldBases, &out)
	}

	outputPath := input.OBIPath
	if input.OutputPath != "" {
		outputPath = input.OutputPath
	}
	if input.Pure {
		if input.OutputPath == "" || input.OutputPath == input.OBIPath {
			return SourcePullOutput{}, fmt.Errorf("--pure requires -o to write a separate stripped copy (refusing to strip x-ob in place)")
		}
		StripAllXOB(iface)
	}
	if err := WriteInterfaceToPath(outputPath, iface, input.Format); err != nil {
		return SourcePullOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	// After the --pure strip, so the report carries exactly what was written.
	out.Interface = iface

	sort.Strings(out.OperationsAdded)
	sort.Strings(out.OperationsUpdated)
	sort.Strings(out.OperationsPruned)
	sort.Strings(out.BindingsAdded)
	sort.Strings(out.BindingsUpdated)
	sort.Strings(out.BindingsPruned)
	sort.Strings(out.Failed)
	return out, nil
}

// pullSourceInto applies one source's derived operations/bindings to the
// interface: overwrite source-owned, add new, prune the source's objects that
// are no longer derived, never touch hand-authored objects.
func pullSourceInto(iface *openbindings.Interface, sourceKey string, derived DeriveResult, oldBases *reconstructedBases, out *SourcePullOutput) {
	// The embed lane elides per-object x-ob.base copies: the embedded content
	// IS the last-synced artifact, so bases are reconstructable on demand
	// (see reconstructBases) and storing them roughly doubles the committed
	// document. Location-mode sources keep recorded bases (the artifact is
	// not carried, so there is nothing to reconstruct from).
	elideBase := sourceEmbedsContent(iface, sourceKey)
	derivedOps := map[string]bool{}
	for opKey, freshOp := range derived.Operations {
		derivedOps[opKey] = true
		existing, exists := iface.Operations[opKey]
		if exists && !IsSourceOwned(existing.LosslessFields) {
			// A hand-authored operation owns this key; don't clobber it.
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"source %q: derived operation %q collides with a hand-authored operation; left unchanged", sourceKey, opKey))
			continue
		}
		// The x-ob merge base must stay the PURE derivation: authored data is
		// carried on top of it, and recording carried data into the base would
		// make the next pull read it as source-derived (and drop it).
		pureDerived := freshOp
		if exists {
			// The source owns the spec fields it derives, never the author's
			// overlay: satisfaction aliases (OBI-T-12) and author-curated tags
			// merge three-way against the recorded base (additions survive,
			// removals stay removed, source evolution propagates); the
			// codegen-name override and any output-schema election re-apply
			// (grown source coverage displaces the election, loudly).
			carryAuthoredNames(existing, &freshOp, baseForOp(existing.LosslessFields, opKey, oldBases))
			carryCodegenName(existing, &freshOp, opKey, &out.Warnings)
			carryOutputSchemaElection(existing, &freshOp, opKey, &out.Warnings)
		}
		markSourceOwned(&freshOp.LosslessFields, pureDerived, elideBase, out)
		if exists && sameContent(existing, freshOp) && sameXOB(existing.LosslessFields, freshOp.LosslessFields) {
			continue // unchanged source-owned op: no churn, no drift, no report
		}
		iface.Operations[opKey] = freshOp
		if exists {
			out.OperationsUpdated = append(out.OperationsUpdated, opKey)
		} else {
			out.OperationsAdded = append(out.OperationsAdded, opKey)
		}
	}

	derivedBinds := map[string]bool{}
	for bk, freshBind := range derived.Bindings {
		derivedBinds[bk] = true
		existingBind, exists := iface.Bindings[bk]
		if exists && !IsSourceOwned(existingBind.LosslessFields) {
			// A hand-authored binding owns this key; don't clobber it.
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"source %q: derived binding %q collides with a hand-authored binding; left unchanged", sourceKey, bk))
			continue
		}
		// Build the full candidate first (mirrors the operation flow): the
		// no-op check must also compare x-ob, so a pre-elision document
		// migrates its binding bases to markers on the first pull instead of
		// keeping them until content happens to change.
		markSourceOwned(&freshBind.LosslessFields, freshBind, elideBase, out)
		if exists && sameContent(existingBind, freshBind) && sameXOB(existingBind.LosslessFields, freshBind.LosslessFields) {
			continue // unchanged source-owned binding: no churn, no drift
		}
		iface.Bindings[bk] = freshBind
		if exists {
			out.BindingsUpdated = append(out.BindingsUpdated, bk)
		} else {
			out.BindingsAdded = append(out.BindingsAdded, bk)
		}
	}

	// Prune source-owned bindings to this source that are no longer derived.
	// Track the operations that lose a binding so we can orphan-prune them.
	losingBinding := map[string]bool{}
	for bk, be := range iface.Bindings {
		if be.Source != sourceKey || derivedBinds[bk] {
			continue
		}
		if !IsSourceOwned(be.LosslessFields) {
			continue // hand-authored binding: leave it
		}
		losingBinding[be.Operation] = true
		delete(iface.Bindings, bk)
		out.BindingsPruned = append(out.BindingsPruned, bk)
	}

	// Recompute which operations still have any binding.
	stillBound := map[string]bool{}
	for _, be := range iface.Bindings {
		stillBound[be.Operation] = true
	}
	// Prune source-owned operations that lost their binding to this source,
	// are no longer derived, and have no surviving binding from any source.
	for opKey := range losingBinding {
		if derivedOps[opKey] || stillBound[opKey] {
			continue
		}
		op, exists := iface.Operations[opKey]
		if !exists || !IsSourceOwned(op.LosslessFields) {
			continue
		}
		delete(iface.Operations, opKey)
		out.OperationsPruned = append(out.OperationsPruned, opKey)
	}
}

// markSourceOwned records source ownership on an object. In the recorded
// lane the source-derived snapshot is stored as the x-ob base (what a later
// `merge --from-sources` reconciles against); in the embed lane (elideBase)
// only the bare x-ob marker is written and the base is reconstructed from
// the embedded content on demand. obj is the value being stored.
func markSourceOwned(lossless *openbindings.LosslessFields, obj any, elideBase bool, out *SourcePullOutput) {
	if elideBase {
		SetXOB(lossless)
		return
	}
	fields, err := ObjectToFieldMap(obj)
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("record provenance: %v", err))
		return
	}
	if err := SetBase(lossless, fields); err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("record provenance: %v", err))
	}
}

// sameContent reports whether two objects (operations or bindings) have
// identical content, ignoring x-ob provenance metadata. Used so an overwrite
// with unchanged content is neither written nor reported as drift.
func sameContent(a, b any) bool {
	am, err1 := ObjectToFieldMap(a)
	bm, err2 := ObjectToFieldMap(b)
	if err1 != nil || err2 != nil {
		return false
	}
	aj, _ := json.Marshal(am)
	bj, _ := json.Marshal(bm)
	return string(aj) == string(bj)
}

// reReadAndDerive re-reads a tracked source, updates its x-ob metadata in the
// interface, and returns the operations/bindings it now derives. ok is false
// (with a possible warning) when the source is hand-authored (no x-ob) or
// cannot be read or derived.
func reReadAndDerive(iface *openbindings.Interface, key, obiDir string) (DeriveResult, bool, string) {
	src := iface.Sources[key]
	meta, err := GetSourceMeta(src)
	if err != nil {
		return DeriveResult{}, false, fmt.Sprintf("source %q: %v", key, err)
	}
	if meta == nil {
		return DeriveResult{}, false, "" // hand-authored source: silently skip
	}

	if needsLiveDiscovery(src.BindingSpec, meta.Ref) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		derivedIface, derr := SynthesizeInterfaceFromSource(ctx, &openbindings.SynthesizeInput{
			Sources: []openbindings.SynthesizeSource{{BindingSpec: src.BindingSpec, Location: meta.Ref}},
		})
		cancel()
		if derr != nil {
			return DeriveResult{}, false, fmt.Sprintf("source %q: discover failed: %v", key, derr)
		}
		data, merr := json.Marshal(derivedIface)
		if merr != nil {
			return DeriveResult{}, false, fmt.Sprintf("source %q: marshal derived interface: %v", key, merr)
		}
		if rerr := ResolveSourceSpec(&src, *meta, data, obiDir); rerr != nil {
			return DeriveResult{}, false, fmt.Sprintf("source %q: resolve failed: %v", key, rerr)
		}
		stampSyncCursor(meta, HashContent(data))
		if serr := SetSourceMeta(&src, *meta); serr != nil {
			return DeriveResult{}, false, fmt.Sprintf("source %q: write meta failed: %v", key, serr)
		}
		iface.Sources[key] = src
		return remapBindings(*derivedIface, key), true, ""
	}

	data, contentHash, rerr := ReadAndHashSource(meta.Ref, obiDir)
	if rerr != nil {
		return DeriveResult{}, false, fmt.Sprintf("source %q: read failed: %v", key, rerr)
	}
	if serr := ResolveSourceSpec(&src, *meta, data, obiDir); serr != nil {
		return DeriveResult{}, false, fmt.Sprintf("source %q: resolve failed: %v", key, serr)
	}
	stampSyncCursor(meta, contentHash)
	if serr := SetSourceMeta(&src, *meta); serr != nil {
		return DeriveResult{}, false, fmt.Sprintf("source %q: write meta failed: %v", key, serr)
	}
	iface.Sources[key] = src

	// Derive from a copy carrying fresh inline content, to bypass the invoker's
	// in-process spec cache without persisting content into the stored source.
	deriveSrc := src
	deriveSrc.Content = string(data)
	derived, derr := DeriveFromSource(deriveSrc, key, obiDir)
	if derr != nil {
		return DeriveResult{}, false, fmt.Sprintf("source %q: derive failed: %v", key, derr)
	}
	return derived, true, ""
}

// stampSyncCursor updates a source's sync cursor only when something actually
// changed (the artifact bytes, or the deriving tool's version). An unchanged
// artifact leaves lastSynced untouched, so a no-op pull is byte-identical:
// no dirty git tree, no guaranteed same-line merge conflicts on parallel
// branches, and back-to-back pulls converge instead of churning timestamps.
func stampSyncCursor(meta *SourceMeta, contentHash string) {
	if meta.ContentHash == contentHash && meta.OBVersion == OBVersion {
		return
	}
	meta.ContentHash = contentHash
	meta.LastSynced = NowISO()
	meta.OBVersion = OBVersion
}

// resolveTargetKeys returns the sorted list of source keys to pull.
// If sourceKeys is empty, returns all source keys. If specified, validates they exist.
func resolveTargetKeys(iface *openbindings.Interface, sourceKeys []string) ([]string, error) {
	if len(sourceKeys) == 0 {
		// All sources.
		var keys []string
		for k := range iface.Sources {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys, nil
	}

	// Validate requested keys exist.
	for _, k := range sourceKeys {
		if _, exists := iface.Sources[k]; !exists {
			return nil, fmt.Errorf("source %q not found", k)
		}
	}
	sorted := make([]string, len(sourceKeys))
	copy(sorted, sourceKeys)
	sort.Strings(sorted)
	return sorted, nil
}

// needsLiveDiscovery reports whether a source must be re-read by connecting
// to a live endpoint rather than by reading a file. Classification is by ref
// SHAPE, not format alone: a grpc source may be backed by a .proto file on
// disk (file lane, same rule the grpc format itself dispatches on) or by a
// reflection address (live lane). Treating a file-backed source as live
// re-derives an interface and embeds THAT marshaled interface in place of
// the artifact text — corrupting an embedded source on every pull.
func needsLiveDiscovery(format, ref string) bool {
	name := SpecFamily(format)
	switch name {
	case "mcp":
		// MCP source locations are always live HTTP(S) endpoints.
		return true
	case "grpc":
		return !strings.HasSuffix(ref, ".proto")
	default:
		return false
	}
}
