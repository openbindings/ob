package app

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/openbindings/openbindings-go"
)

// FieldConflict records a three-way conflict on a single field.
type FieldConflict struct {
	Field  string          `json:"field"`
	Base   json.RawMessage `json:"base,omitempty"`
	Local  json.RawMessage `json:"local,omitempty"`
	Source json.RawMessage `json:"source,omitempty"`
}

// MergeResult is the outcome of a three-way merge on a JSON object.
type MergeResult struct {
	// Merged is the resulting field map after merge.
	Merged map[string]json.RawMessage

	// Updated lists fields that were auto-merged from source (user hadn't changed them).
	Updated []string

	// Preserved lists fields where the user's local value was kept (source didn't change them).
	Preserved []string

	// Added lists fields that were new from the source.
	Added []string

	// Conflicts lists fields where both sides changed differently. Local value is kept.
	Conflicts []FieldConflict
}

// IsClean returns true if the merge had no conflicts.
func (m MergeResult) IsClean() bool {
	return len(m.Conflicts) == 0
}

// HasChanges returns true if the merge would change anything compared to local.
func (m MergeResult) HasChanges() bool {
	return len(m.Updated) > 0 || len(m.Added) > 0
}

// ThreeWayMerge performs a field-level three-way merge.
//
// For each top-level field across base, local, and source:
//
//	base  local  source  → action
//	─────────────────────────────────────────
//	 -      -      S     → add from source
//	 -      L      -     → keep local (user addition)
//	 -      L      S     → L==S: keep; else conflict
//	 B      -      -     → both removed, drop
//	 B      -      S     → B==S: user removed, drop; else conflict (user removed, source changed)
//	 B      L      -     → B==L: source removed, drop; else conflict (source removed, user changed)
//	 B      L      S     → see below
//
// For the B/L/S case:
//
//	B==L && B==S: no change
//	B==L && B!=S: source changed, user didn't → accept source
//	B!=L && B==S: user changed, source didn't → keep local
//	B!=L && B!=S && L==S: both changed to same → keep (no conflict)
//	B!=L && B!=S && L!=S: CONFLICT → keep local, record conflict
func ThreeWayMerge(base, local, source map[string]json.RawMessage) MergeResult {
	result := MergeResult{
		Merged: make(map[string]json.RawMessage),
	}

	// Collect all keys.
	keys := make(map[string]struct{})
	for k := range base {
		keys[k] = struct{}{}
	}
	for k := range local {
		keys[k] = struct{}{}
	}
	for k := range source {
		keys[k] = struct{}{}
	}

	for k := range keys {
		bVal, inBase := base[k]
		lVal, inLocal := local[k]
		sVal, inSource := source[k]

		switch {
		// Only in source: new from source → add.
		case !inBase && !inLocal && inSource:
			result.Merged[k] = sVal
			result.Added = append(result.Added, k)

		// Only in local: user addition → keep.
		case !inBase && inLocal && !inSource:
			result.Merged[k] = lVal
			result.Preserved = append(result.Preserved, k)

		// In local and source but no base: both added.
		case !inBase && inLocal && inSource:
			if jsonEqual(lVal, sVal) {
				result.Merged[k] = lVal
			} else {
				// Conflict: both sides added different values.
				result.Merged[k] = lVal
				result.Conflicts = append(result.Conflicts, FieldConflict{
					Field: k, Local: lVal, Source: sVal,
				})
			}

		// Only in base: both removed → drop.
		case inBase && !inLocal && !inSource:
			// Gone from both sides, nothing to add.

		// In base and source but not local: user removed.
		case inBase && !inLocal && inSource:
			if jsonEqual(bVal, sVal) {
				// Source didn't change it; user intentionally removed → respect removal.
			} else {
				// Source changed it but user removed it → conflict.
				result.Conflicts = append(result.Conflicts, FieldConflict{
					Field: k, Base: bVal, Source: sVal,
				})
			}

		// In base and local but not source: source removed.
		case inBase && inLocal && !inSource:
			if jsonEqual(bVal, lVal) {
				// User didn't change it; source removed → accept removal.
			} else {
				// User changed it but source removed → conflict, keep local.
				result.Merged[k] = lVal
				result.Conflicts = append(result.Conflicts, FieldConflict{
					Field: k, Base: bVal, Local: lVal,
				})
			}

		// All three present: full three-way comparison.
		case inBase && inLocal && inSource:
			baseEqLocal := jsonEqual(bVal, lVal)
			baseEqSource := jsonEqual(bVal, sVal)

			switch {
			case baseEqLocal && baseEqSource:
				// No change on either side.
				result.Merged[k] = lVal

			case baseEqLocal && !baseEqSource:
				// Source changed, user didn't → accept source.
				result.Merged[k] = sVal
				result.Updated = append(result.Updated, k)

			case !baseEqLocal && baseEqSource:
				// User changed, source didn't → keep local.
				result.Merged[k] = lVal
				result.Preserved = append(result.Preserved, k)

			case jsonEqual(lVal, sVal):
				// Both changed to the same value → no conflict.
				result.Merged[k] = lVal

			default:
				// Both changed differently → conflict, keep local.
				result.Merged[k] = lVal
				result.Conflicts = append(result.Conflicts, FieldConflict{
					Field: k, Base: bVal, Local: lVal, Source: sVal,
				})
			}
		}
	}

	return result
}

// inferBaseFromLocal builds a synthetic base map for the no-recorded-base
// case. The rule is: for any field the source has, assume local matches
// the previous source value (so source wins on update). Fields that exist
// only in local are deliberately left out of the inferred base — that
// makes ThreeWayMerge see them as "user addition, only in local" and
// preserve them.
//
// This is the legacy/upgrade path. After `ob create` and the first sync
// the base field is populated for real and the three-way merge is exact.
// The heuristic is only used when no base was ever recorded — typically
// because the OBI was bootstrapped by an older `ob create` that wrote
// `x-ob: {}` without a base, or because the user hand-authored the x-ob
// marker.
func inferBaseFromLocal(localFields, sourceFields map[string]json.RawMessage) map[string]json.RawMessage {
	base := make(map[string]json.RawMessage, len(sourceFields))
	for k := range sourceFields {
		if v, ok := localFields[k]; ok {
			base[k] = v
		}
	}
	return base
}

// MergeOperation performs a three-way merge on an operation.
//
// base may be nil. This happens on the first sync after a legacy
// `ob create` that wrote `x-ob: {}` without populating the base, or
// when the OBI was hand-authored with empty x-ob markers. In that case
// we infer a synthetic base from local: for any field source has,
// assume local matches the previous source value. This means
//
//   - fields source has: source wins on update (recovers the previous
//     "first sync after create overwrites local with fresh source"
//     behavior)
//   - fields only local has (satisfies, aliases, custom description on
//     a no-description-source operation): preserved as user additions
//
// Previously this short-circuited to "return source as-is" on nil base,
// which silently dropped every hand-authored local-only field. That
// made `satisfies`, `aliases`, and other authored metadata unsafe to
// add until the OBI happened to get a real base from a non-empty sync.
func MergeOperation(base map[string]json.RawMessage, local openbindings.Operation, source openbindings.Operation) (openbindings.Operation, MergeResult, error) {
	localFields, err := ObjectToFieldMap(local)
	if err != nil {
		return openbindings.Operation{}, MergeResult{}, err
	}
	sourceFields, err := ObjectToFieldMap(source)
	if err != nil {
		return openbindings.Operation{}, MergeResult{}, err
	}

	if base == nil {
		base = inferBaseFromLocal(localFields, sourceFields)
	}

	mr := ThreeWayMerge(base, localFields, sourceFields)

	// Convert merged fields back to an Operation.
	merged, err := json.Marshal(mr.Merged)
	if err != nil {
		return openbindings.Operation{}, mr, err
	}
	var op openbindings.Operation
	if err := json.Unmarshal(merged, &op); err != nil {
		return openbindings.Operation{}, mr, err
	}

	return op, mr, nil
}

// MergeBinding performs a three-way merge on a binding entry.
//
// As with MergeOperation, a nil base is handled with inferBaseFromLocal
// so local-only fields (security overrides, transform refs) are preserved
// on the first sync after bootstrap while previously-synced fields still
// pick up source updates.
func MergeBinding(base map[string]json.RawMessage, local openbindings.BindingEntry, source openbindings.BindingEntry) (openbindings.BindingEntry, MergeResult, error) {
	localFields, err := ObjectToFieldMap(local)
	if err != nil {
		return openbindings.BindingEntry{}, MergeResult{}, err
	}
	sourceFields, err := ObjectToFieldMap(source)
	if err != nil {
		return openbindings.BindingEntry{}, MergeResult{}, err
	}

	if base == nil {
		base = inferBaseFromLocal(localFields, sourceFields)
	}

	mr := ThreeWayMerge(base, localFields, sourceFields)

	merged, err := json.Marshal(mr.Merged)
	if err != nil {
		return openbindings.BindingEntry{}, mr, err
	}
	var b openbindings.BindingEntry
	if err := json.Unmarshal(merged, &b); err != nil {
		return openbindings.BindingEntry{}, mr, err
	}

	return b, mr, nil
}

// jsonEqual compares two JSON values for semantic equality.
func jsonEqual(a, b json.RawMessage) bool {
	// Fast path: byte-equal.
	if bytes.Equal(a, b) {
		return true
	}
	// Semantic comparison: unmarshal and use reflect.DeepEqual.
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}
