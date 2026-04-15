package app

import (
	"path/filepath"
	"sort"

	"github.com/openbindings/openbindings-go"
)

// MergePreviewEntry describes the merge outcome for a single object.
type MergePreviewEntry struct {
	Key       string          // operation or binding key
	Type      string          // "operation" or "binding"
	Result    MergeResult     // three-way merge result
	IsNew     bool            // true if the object doesn't exist in the OBI yet
}

// MergePreview holds the merge preview for all sources.
type MergePreview struct {
	Entries []MergePreviewEntry
}

// OperationsAdded returns keys of operations that would be added.
func (p MergePreview) OperationsAdded() []string {
	return p.keysByFilter("operation", true, false, false)
}

// OperationsUpdated returns keys of operations that would be updated (non-conflicting changes).
func (p MergePreview) OperationsUpdated() []string {
	return p.keysByFilter("operation", false, true, false)
}

// OperationsConflicted returns keys of operations that have conflicts.
func (p MergePreview) OperationsConflicted() []string {
	return p.keysByFilter("operation", false, false, true)
}

// BindingsAdded returns keys of bindings that would be added.
func (p MergePreview) BindingsAdded() []string {
	return p.keysByFilter("binding", true, false, false)
}

// BindingsUpdated returns keys of bindings that would be updated.
func (p MergePreview) BindingsUpdated() []string {
	return p.keysByFilter("binding", false, true, false)
}

// BindingsConflicted returns keys of bindings that have conflicts.
func (p MergePreview) BindingsConflicted() []string {
	return p.keysByFilter("binding", false, false, true)
}

// HasChanges returns true if any entry would cause a change.
func (p MergePreview) HasChanges() bool {
	for _, e := range p.Entries {
		if e.IsNew || e.Result.HasChanges() || len(e.Result.Conflicts) > 0 {
			return true
		}
	}
	return false
}

// Conflicts returns all FieldConflict entries tagged with their object key/type.
func (p MergePreview) Conflicts() []ObjectConflict {
	var out []ObjectConflict
	for _, e := range p.Entries {
		if len(e.Result.Conflicts) > 0 {
			out = append(out, ObjectConflict{
				Key:    e.Key,
				Type:   e.Type,
				Fields: e.Result.Conflicts,
			})
		}
	}
	return out
}

func (p MergePreview) keysByFilter(typ string, isNew, hasChanges, hasConflicts bool) []string {
	var keys []string
	for _, e := range p.Entries {
		if e.Type != typ {
			continue
		}
		if isNew && e.IsNew {
			keys = append(keys, e.Key)
		}
		if hasChanges && !e.IsNew && e.Result.HasChanges() {
			keys = append(keys, e.Key)
		}
		if hasConflicts && len(e.Result.Conflicts) > 0 {
			keys = append(keys, e.Key)
		}
	}
	return keys
}

// PreviewSourceMerge derives content from a source and performs a three-way
// merge preview against the OBI, returning one MergePreviewEntry per object.
// This is the shared core for ob status, ob conflicts, and dry-run logic.
func PreviewSourceMerge(src openbindings.Source, srcKey string, iface *openbindings.Interface, obiDir string) (MergePreview, error) {
	derived, err := DeriveFromSource(src, srcKey, obiDir)
	if err != nil {
		return MergePreview{}, err
	}

	var entries []MergePreviewEntry

	opKeys := make([]string, 0, len(derived.Operations))
	for opKey := range derived.Operations {
		opKeys = append(opKeys, opKey)
	}
	sort.Strings(opKeys)
	for _, opKey := range opKeys {
		freshOp := derived.Operations[opKey]
		existing, exists := iface.Operations[opKey]
		if !exists {
			entries = append(entries, MergePreviewEntry{
				Key:    opKey,
				Type:   "operation",
				IsNew:  true,
			})
			continue
		}
		if !HasXOB(existing.LosslessFields) {
			continue // hand-authored, untouched
		}

		base, _ := GetBase(existing.LosslessFields)
		_, mr, err := MergeOperation(base, existing, freshOp)
		if err != nil {
			continue
		}
		if mr.HasChanges() || len(mr.Conflicts) > 0 {
			entries = append(entries, MergePreviewEntry{
				Key:    opKey,
				Type:   "operation",
				Result: mr,
			})
		}
	}

	bindKeys := make([]string, 0, len(derived.Bindings))
	for bindKey := range derived.Bindings {
		bindKeys = append(bindKeys, bindKey)
	}
	sort.Strings(bindKeys)
	for _, bindKey := range bindKeys {
		freshBind := derived.Bindings[bindKey]
		existing, exists := iface.Bindings[bindKey]
		if !exists {
			entries = append(entries, MergePreviewEntry{
				Key:    bindKey,
				Type:   "binding",
				IsNew:  true,
			})
			continue
		}
		if !HasXOB(existing.LosslessFields) {
			continue
		}

		base, _ := GetBase(existing.LosslessFields)
		_, mr, err := MergeBinding(base, existing, freshBind)
		if err != nil {
			continue
		}
		if mr.HasChanges() || len(mr.Conflicts) > 0 {
			entries = append(entries, MergePreviewEntry{
				Key:    bindKey,
				Type:   "binding",
				Result: mr,
			})
		}
	}

	return MergePreview{Entries: entries}, nil
}

// PreviewAllSourceMerges runs PreviewSourceMerge for every managed source in the OBI.
func PreviewAllSourceMerges(iface *openbindings.Interface, obiPath string) map[string]MergePreview {
	obiDir := filepath.Dir(obiPath)
	previews := make(map[string]MergePreview)

	for srcKey, src := range iface.Sources {
		meta, err := GetSourceMeta(src)
		if err != nil || meta == nil {
			continue
		}
		preview, err := PreviewSourceMerge(src, srcKey, iface, obiDir)
		if err != nil {
			continue
		}
		previews[srcKey] = preview
	}

	return previews
}
