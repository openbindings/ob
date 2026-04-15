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

// SyncInput represents input for the sync command.
type SyncInput struct {
	OBIPath       string   // path to the OBI file
	SourceKeys    []string // specific sources to sync (empty = all)
	OperationKeys []string // specific operations to sync (empty = all)
	Force         bool     // prefer source for all conflicts (overwrite local edits)
	Pure          bool     // strip all x-ob metadata from output
	OutputPath    string   // write to a different path (required for --pure)
	Format        string   // output format override
}

// SyncConflict describes a merge conflict on a specific object field.
type SyncConflict struct {
	Object string `json:"object"` // e.g. "operation:hello" or "binding:hello.src"
	FieldConflict
}

// SyncOutput represents the result of a sync operation.
// Each field is either a slice of keys (count = len) or a boolean. No redundant counts.
type SyncOutput struct {
	Sources           []string       `json:"sources,omitempty"`
	Skipped           []string       `json:"skipped,omitempty"`
	OperationsUpdated []string       `json:"operationsUpdated,omitempty"`
	OperationsAdded   []string       `json:"operationsAdded,omitempty"`
	BindingsUpdated   []string       `json:"bindingsUpdated,omitempty"`
	BindingsAdded     []string       `json:"bindingsAdded,omitempty"`
	Conflicts         []SyncConflict `json:"conflicts,omitempty"`
	Warnings          []string       `json:"warnings,omitempty"`
	Pure              bool           `json:"pure,omitempty"`
}

// Render returns a human-friendly representation.
func (o SyncOutput) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Sync complete"))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("  %d source(s) synced", len(o.Sources)))
	if len(o.Skipped) > 0 {
		sb.WriteString(fmt.Sprintf(", %d skipped", len(o.Skipped)))
	}
	if len(o.Sources) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  Sources: "))
		sb.WriteString(strings.Join(o.Sources, ", "))
	}
	renderKeyGroup(&sb, s, "Operations", o.OperationsUpdated, o.OperationsAdded)
	renderKeyGroup(&sb, s, "Bindings", o.BindingsUpdated, o.BindingsAdded)
	if len(o.Conflicts) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Warning.Render(fmt.Sprintf("  %d conflict(s) (local values kept):", len(o.Conflicts))))
		for _, c := range o.Conflicts {
			sb.WriteString("\n")
			sb.WriteString(s.Warning.Render(fmt.Sprintf("    %s → %s", c.Object, c.Field)))
		}
	}
	if o.Pure {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  x-ob metadata stripped (--pure)"))
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

// Sync performs a full or partial sync on an OBI.
// For each targeted source that has x-ob metadata, it re-reads from x-ob.ref,
// applies the resolution mode to update spec-level fields, and updates
// per-source contentHash/lastSynced/obVersion.
func Sync(input SyncInput) (SyncOutput, error) {
	iface, err := loadInterfaceFile(input.OBIPath)
	if err != nil {
		return SyncOutput{}, fmt.Errorf("load OBI: %w", err)
	}

	// Ensure maps exist so we can assign during sync (OBIs from source add may omit bindings/operations).
	if iface.Operations == nil {
		iface.Operations = map[string]openbindings.Operation{}
	}
	if iface.Bindings == nil {
		iface.Bindings = map[string]openbindings.BindingEntry{}
	}

	obiDir := filepath.Dir(input.OBIPath)

	// Determine which source keys to sync.
	targetKeys, err := resolveTargetKeys(iface, input.SourceKeys)
	if err != nil {
		return SyncOutput{}, err
	}

	var (
		syncedKeys  []string
		skippedKeys []string
		warnings    []string
	)

	// preDiscovered holds results from live-discovery sources, keyed by source key.
	// These are used in Phase 2 instead of calling DeriveFromSource again.
	preDiscovered := map[string]DeriveResult{}

	// sourceData holds fresh file content read in phase 1, passed to phase 2 as
	// Content to bypass the executor's in-process spec cache.
	sourceData := map[string][]byte{}

	for _, key := range targetKeys {
		src := iface.Sources[key]
		meta, err := GetSourceMeta(src)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("source %q: %v", key, err))
			skippedKeys = append(skippedKeys, key)
			continue
		}
		if meta == nil {
			// No x-ob metadata — hand-authored source, skip.
			skippedKeys = append(skippedKeys, key)
			continue
		}

		if needsLiveDiscovery(src.Format) {
			createIn := &openbindings.CreateInput{
				Sources: []openbindings.CreateSource{{
					Format:   src.Format,
					Location: meta.Ref,
				}},
			}
			discoverCtx, discoverCancel := context.WithTimeout(context.Background(), 60*time.Second)
			derived, discoverErr := CreateInterfaceFromSource(discoverCtx, createIn)
			discoverCancel()
			if discoverErr != nil {
				warnings = append(warnings, fmt.Sprintf("source %q: discover failed: %v", key, discoverErr))
				continue
			}

			data, err := json.Marshal(derived)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("source %q: failed to marshal derived interface: %v", key, err))
				continue
			}
			contentHash := HashContent(data)

			if resolveErr := ResolveSourceSpec(&src, *meta, data, obiDir); resolveErr != nil {
				warnings = append(warnings, fmt.Sprintf("source %q: resolve failed: %v", key, resolveErr))
				continue
			}

			meta.ContentHash = contentHash
			meta.LastSynced = NowISO()
			meta.OBVersion = OBVersion

			if setErr := SetSourceMeta(&src, *meta); setErr != nil {
				warnings = append(warnings, fmt.Sprintf("source %q: write meta failed: %v", key, setErr))
				continue
			}

			iface.Sources[key] = src
			syncedKeys = append(syncedKeys, key)

			remapped := remapBindings(*derived, key)
			preDiscovered[key] = remapped
			continue
		}

		// Default path for file-based sources.
		// Re-read source content from ref.
		data, contentHash, err := ReadAndHashSource(meta.Ref, obiDir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("source %q: read failed: %v", key, err))
			continue
		}
		sourceData[key] = data

		// Apply resolve mode to update spec-level fields.
		if err := ResolveSourceSpec(&src, *meta, data, obiDir); err != nil {
			warnings = append(warnings, fmt.Sprintf("source %q: resolve failed: %v", key, err))
			continue
		}

		// Update per-source metadata.
		meta.ContentHash = contentHash
		meta.LastSynced = NowISO()
		meta.OBVersion = OBVersion

		if err := SetSourceMeta(&src, *meta); err != nil {
			warnings = append(warnings, fmt.Sprintf("source %q: write meta failed: %v", key, err))
			continue
		}

		iface.Sources[key] = src
		syncedKeys = append(syncedKeys, key)
	}

	// Phase 2: Three-way merge of operations and bindings.
	// For each synced source, re-derive via the executor and merge against the OBI.
	// Hand-authored objects (no x-ob) are never touched.
	opFilter := toStringSet(input.OperationKeys)

	var (
		opsUpdated, opsAdded     []string
		bindsUpdated, bindsAdded []string
		conflicts                []SyncConflict
	)

	for _, key := range syncedKeys {
		src := iface.Sources[key]

		var derived DeriveResult
		if pre, ok := preDiscovered[key]; ok {
			// Use the pre-discovered result (live discovery path).
			derived = pre
		} else {
			// Pass fresh content from phase 1 to bypass the executor's spec cache.
			if freshData, ok := sourceData[key]; ok {
				src.Content = string(freshData)
			}
			var err error
			derived, err = DeriveFromSource(src, key, obiDir)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("source %q: derive failed: %v", key, err))
				continue
			}
		}

		// The source-derived fields serve as the new base after this sync.
		for opKey, freshOp := range derived.Operations {
			if opFilter != nil {
				if _, ok := opFilter[opKey]; !ok {
					continue
				}
			}

			existing, exists := iface.Operations[opKey]

			if !exists {
				// New operation from source.
				sourceFields, err := ObjectToFieldMap(freshOp)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("op %q: field map: %v", opKey, err))
					continue
				}
				if err := SetBase(&freshOp.LosslessFields, sourceFields); err != nil {
					warnings = append(warnings, fmt.Sprintf("op %q: set base: %v", opKey, err))
					continue
				}
				iface.Operations[opKey] = freshOp
				opsAdded = append(opsAdded, opKey)
				continue
			}

			if !HasXOB(existing.LosslessFields) {
				// Hand-authored — never touch.
				continue
			}

			sourceFields, err := ObjectToFieldMap(freshOp)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("op %q: field map: %v", opKey, err))
				continue
			}

			if input.Force {
				// Force: overwrite with source, no merge.
				if err := SetBase(&freshOp.LosslessFields, sourceFields); err != nil {
					warnings = append(warnings, fmt.Sprintf("op %q: set base: %v", opKey, err))
					continue
				}
				iface.Operations[opKey] = freshOp
				opsUpdated = append(opsUpdated, opKey)
				continue
			}

			// Managed: three-way merge.
			base, _ := GetBase(existing.LosslessFields)
			mergedOp, mr, err := MergeOperation(base, existing, freshOp)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("op %q: merge failed: %v", opKey, err))
				continue
			}

			// Store source-derived content as the new base.
			if err := SetBase(&mergedOp.LosslessFields, sourceFields); err != nil {
				warnings = append(warnings, fmt.Sprintf("op %q: set base: %v", opKey, err))
				continue
			}

			iface.Operations[opKey] = mergedOp
			if mr.HasChanges() || len(mr.Conflicts) > 0 {
				opsUpdated = append(opsUpdated, opKey)
			}
			for _, c := range mr.Conflicts {
				conflicts = append(conflicts, SyncConflict{
					Object:        "operation:" + opKey,
					FieldConflict: c,
				})
			}
		}

		for bindKey, freshBind := range derived.Bindings {
			if opFilter != nil {
				if _, ok := opFilter[freshBind.Operation]; !ok {
					continue
				}
			}

			existing, exists := iface.Bindings[bindKey]

			if !exists {
				sourceFields, err := ObjectToFieldMap(freshBind)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("binding %q: field map: %v", bindKey, err))
					continue
				}
				if err := SetBase(&freshBind.LosslessFields, sourceFields); err != nil {
					warnings = append(warnings, fmt.Sprintf("binding %q: set base: %v", bindKey, err))
					continue
				}
				iface.Bindings[bindKey] = freshBind
				bindsAdded = append(bindsAdded, bindKey)
				continue
			}

			if !HasXOB(existing.LosslessFields) {
				continue
			}

			sourceFields, err := ObjectToFieldMap(freshBind)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("binding %q: field map: %v", bindKey, err))
				continue
			}

			if input.Force {
				if err := SetBase(&freshBind.LosslessFields, sourceFields); err != nil {
					warnings = append(warnings, fmt.Sprintf("binding %q: set base: %v", bindKey, err))
					continue
				}
				iface.Bindings[bindKey] = freshBind
				bindsUpdated = append(bindsUpdated, bindKey)
				continue
			}

			base, _ := GetBase(existing.LosslessFields)
			mergedBind, mr, err := MergeBinding(base, existing, freshBind)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("binding %q: merge failed: %v", bindKey, err))
				continue
			}

			if err := SetBase(&mergedBind.LosslessFields, sourceFields); err != nil {
				warnings = append(warnings, fmt.Sprintf("binding %q: set base: %v", bindKey, err))
				continue
			}

			iface.Bindings[bindKey] = mergedBind
			if mr.HasChanges() || len(mr.Conflicts) > 0 {
				bindsUpdated = append(bindsUpdated, bindKey)
			}
			for _, c := range mr.Conflicts {
				conflicts = append(conflicts, SyncConflict{
					Object:        "binding:" + bindKey,
					FieldConflict: c,
				})
			}
		}
	}

	// Strip x-ob metadata if --pure.
	if input.Pure {
		StripAllXOB(iface)
	}

	// Write the updated OBI.
	outputPath := input.OBIPath
	if input.OutputPath != "" {
		outputPath = input.OutputPath
	}
	if err := WriteInterfaceToPath(outputPath, iface, input.Format); err != nil {
		return SyncOutput{}, fmt.Errorf("write OBI: %w", err)
	}

	sort.Strings(opsUpdated)
	sort.Strings(opsAdded)
	sort.Strings(bindsUpdated)
	sort.Strings(bindsAdded)

	return SyncOutput{
		Sources:           syncedKeys,
		Skipped:           skippedKeys,
		OperationsUpdated: opsUpdated,
		OperationsAdded:   opsAdded,
		BindingsUpdated:   bindsUpdated,
		BindingsAdded:     bindsAdded,
		Conflicts:         conflicts,
		Warnings:          warnings,
		Pure:              input.Pure,
	}, nil
}

// toStringSet builds a set from a slice, or returns nil if the slice is empty.
func toStringSet(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

// resolveTargetKeys returns the sorted list of source keys to sync.
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

func needsLiveDiscovery(format string) bool {
	name := strings.ToLower(strings.SplitN(format, "@", 2)[0])
	return name == "mcp" || name == "grpc"
}
