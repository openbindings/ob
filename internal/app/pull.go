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

// SourcePullInput represents input for the `source pull` command.
type SourcePullInput struct {
	OBIPath    string   // path to the OBI file
	SourceKeys []string // specific sources to pull (empty = all)
	OutputPath string   // write to a different path
	Format     string   // output format override
}

// SourcePullOutput reports what a pull changed. Each field is the set of keys
// affected (count = len); no redundant counts.
type SourcePullOutput struct {
	Sources           []string `json:"sources,omitempty"`
	Skipped           []string `json:"skipped,omitempty"`
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
	sb.WriteString(s.Header.Render("Pull complete"))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("  %d source(s) pulled", len(o.Sources)))
	if len(o.Skipped) > 0 {
		sb.WriteString(fmt.Sprintf(", %d skipped", len(o.Skipped)))
	}
	if len(o.Sources) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  Sources: "))
		sb.WriteString(strings.Join(o.Sources, ", "))
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
		derived, ok, warning := reReadAndDerive(iface, key, obiDir)
		if warning != "" {
			out.Warnings = append(out.Warnings, warning)
		}
		if !ok {
			out.Skipped = append(out.Skipped, key)
			continue
		}
		out.Sources = append(out.Sources, key)
		pullSourceInto(iface, key, derived, &out)
	}

	outputPath := input.OBIPath
	if input.OutputPath != "" {
		outputPath = input.OutputPath
	}
	if err := WriteInterfaceToPath(outputPath, iface, input.Format); err != nil {
		return SourcePullOutput{}, fmt.Errorf("write OBI: %w", err)
	}

	sort.Strings(out.OperationsAdded)
	sort.Strings(out.OperationsUpdated)
	sort.Strings(out.OperationsPruned)
	sort.Strings(out.BindingsAdded)
	sort.Strings(out.BindingsUpdated)
	sort.Strings(out.BindingsPruned)
	return out, nil
}

// pullSourceInto applies one source's derived operations/bindings to the
// interface: overwrite source-owned, add new, prune the source's objects that
// are no longer derived, never touch hand-authored objects.
func pullSourceInto(iface *openbindings.Interface, sourceKey string, derived DeriveResult, out *SourcePullOutput) {
	derivedOps := map[string]bool{}
	for opKey, freshOp := range derived.Operations {
		derivedOps[opKey] = true
		existing, exists := iface.Operations[opKey]
		if exists && !HasXOB(existing.LosslessFields) {
			// A hand-authored operation owns this key; don't clobber it.
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"source %q: derived operation %q collides with a hand-authored operation; left unchanged", sourceKey, opKey))
			continue
		}
		markSourceOwned(&freshOp.LosslessFields, freshOp, out)
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
		_, exists := iface.Bindings[bk]
		markSourceOwned(&freshBind.LosslessFields, freshBind, out)
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
		if !HasXOB(be.LosslessFields) {
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
		if !exists || !HasXOB(op.LosslessFields) {
			continue
		}
		delete(iface.Operations, opKey)
		out.OperationsPruned = append(out.OperationsPruned, opKey)
	}
}

// markSourceOwned records the source-derived snapshot as the object's x-ob base.
// Its presence is the "source-owned" (managed) marker that pull, prune, and
// status key on, and it is the base a later `merge --from-sources` reconciles
// against. obj is the value being stored (op or binding).
func markSourceOwned(lossless *openbindings.LosslessFields, obj any, out *SourcePullOutput) {
	fields, err := ObjectToFieldMap(obj)
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("record provenance: %v", err))
		return
	}
	if err := SetBase(lossless, fields); err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("record provenance: %v", err))
	}
}

// reReadAndDerive re-reads a managed source, updates its x-ob metadata in the
// interface, and returns the operations/bindings it now derives. ok is false
// (with a possible warning) when the source is hand-authored (no x-ob) or
// cannot be read or derived.
//
// NOTE: this supersedes the inline per-source loop in sync.go; that loop is
// removed when `sync` is retired (P2).
func reReadAndDerive(iface *openbindings.Interface, key, obiDir string) (DeriveResult, bool, string) {
	src := iface.Sources[key]
	meta, err := GetSourceMeta(src)
	if err != nil {
		return DeriveResult{}, false, fmt.Sprintf("source %q: %v", key, err)
	}
	if meta == nil {
		return DeriveResult{}, false, "" // hand-authored source: silently skip
	}

	if needsLiveDiscovery(src.Format) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		derivedIface, derr := CreateInterfaceFromSource(ctx, &openbindings.CreateInput{
			Sources: []openbindings.CreateSource{{Format: src.Format, Location: meta.Ref}},
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
		meta.ContentHash = HashContent(data)
		meta.LastSynced = NowISO()
		meta.OBVersion = OBVersion
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
	meta.ContentHash = contentHash
	meta.LastSynced = NowISO()
	meta.OBVersion = OBVersion
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
