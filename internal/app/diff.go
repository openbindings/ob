package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/canonicaljson"
	"github.com/openbindings/openbindings-go/schemaprofile"
)

// DiffStatus represents the status of a single operation in a diff.
type DiffStatus string

const (
	DiffInSync  DiffStatus = "in-sync"
	DiffChanged DiffStatus = "changed"
	DiffAdded   DiffStatus = "added"
	DiffRemoved DiffStatus = "removed"
)

// OperationDiff represents the diff result for a single operation.
type OperationDiff struct {
	Operation string     `json:"operation"`
	Status    DiffStatus `json:"status"`
	Details   []string   `json:"details,omitempty"` // what specifically changed
}

// MetadataDiff captures top-level metadata differences.
type MetadataDiff struct {
	Field    string `json:"field"`
	Baseline string `json:"baseline"`
	Compared string `json:"compared"`
}

// DriftEntry represents a cross-source drift: the same operation key
// produced by multiple sources with different schemas (D10).
type DriftEntry struct {
	Operation string        `json:"operation"`
	Sources   []DriftSource `json:"sources"`
	Details   []string      `json:"details"`
}

// DriftSource identifies which source produced a particular version of an operation.
type DriftSource struct {
	Key    string `json:"key"`
	Format string `json:"format"`
}

// DiffReport is the full diff result between two OBIs.
type DiffReport struct {
	Identical  bool            `json:"identical"`
	Operations []OperationDiff `json:"operations,omitempty"`
	Metadata   []MetadataDiff  `json:"metadata,omitempty"`
	Drift      []DriftEntry    `json:"drift,omitempty"`
	Warnings   []string        `json:"warnings,omitempty"`
}

// Render returns a human-friendly representation.
func (r DiffReport) Render() string {
	s := Styles
	if r.Identical {
		return s.Success.Render("Identical — no differences found")
	}

	var sb strings.Builder
	sb.WriteString(s.Header.Render("Differences"))
	sb.WriteString("\n")

	// Show metadata differences.
	if len(r.Metadata) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  Metadata:"))
		sb.WriteString("\n")
		for _, m := range r.Metadata {
			sb.WriteString(fmt.Sprintf("    %s: %s → %s\n",
				s.Key.Render(m.Field),
				s.Removed.Render(m.Baseline),
				s.Added.Render(m.Compared),
			))
		}
	}

	// Show operations grouped by status.
	added, removed, changed, inSync := groupByStatus(r.Operations)

	if len(added) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Added.Render(fmt.Sprintf("  + %d added", len(added))))
		sb.WriteString("\n")
		for _, op := range added {
			sb.WriteString(fmt.Sprintf("    %s %s\n", s.Added.Render("+"), op.Operation))
		}
	}

	if len(removed) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Removed.Render(fmt.Sprintf("  - %d removed", len(removed))))
		sb.WriteString("\n")
		for _, op := range removed {
			sb.WriteString(fmt.Sprintf("    %s %s\n", s.Removed.Render("-"), op.Operation))
		}
	}

	if len(changed) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Warning.Render(fmt.Sprintf("  ~ %d changed", len(changed))))
		sb.WriteString("\n")
		for _, op := range changed {
			sb.WriteString(fmt.Sprintf("    %s %s\n", s.Warning.Render("~"), op.Operation))
			for _, d := range op.Details {
				sb.WriteString(fmt.Sprintf("      %s\n", s.Dim.Render(d)))
			}
		}
	}

	if len(inSync) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Success.Render(fmt.Sprintf("  = %d in sync", len(inSync))))
		sb.WriteString("\n")
	}

	// Show cross-source drift (D10).
	if len(r.Drift) > 0 {
		sb.WriteString("\n")
		sb.WriteString(s.Error.Render(fmt.Sprintf("  Cross-source drift (%d):", len(r.Drift))))
		sb.WriteString("\n")
		for _, d := range r.Drift {
			var sourceNames []string
			for _, ds := range d.Sources {
				sourceNames = append(sourceNames, ds.Key)
			}
			sb.WriteString(fmt.Sprintf("    %s %s  differs between %s\n",
				s.Error.Render("!"),
				s.Key.Render(d.Operation),
				strings.Join(sourceNames, ", "),
			))
			for _, detail := range d.Details {
				sb.WriteString(fmt.Sprintf("      %s\n", s.Dim.Render(detail)))
			}
		}
	}

	// Show warnings.
	if len(r.Warnings) > 0 {
		sb.WriteString("\n")
		for _, w := range r.Warnings {
			sb.WriteString(fmt.Sprintf("  %s %s\n", s.Warning.Render("!"), w))
		}
	}

	return sb.String()
}

// DiffInput represents input for the diff command.
type DiffInput struct {
	// For two-arg mode:
	BaselineLocator   string
	ComparisonLocator string

	// For --from-sources mode:
	FromSources bool
	OnlySource  string // scope to a specific source key
}

// Diff computes a structural comparison between two OBIs.
// It supports both two-arg mode and --from-sources mode.
func Diff(input DiffInput) (DiffReport, error) {
	if input.FromSources {
		return diffFromSources(input)
	}
	return diffTwoOBIs(input.BaselineLocator, input.ComparisonLocator)
}

// diffTwoOBIs loads two OBIs and compares them.
func diffTwoOBIs(baselineLocator, comparisonLocator string) (DiffReport, error) {
	baseline, err := resolveInterface(baselineLocator)
	if err != nil {
		return DiffReport{}, fmt.Errorf("baseline: %w", err)
	}

	comparison, err := resolveInterface(comparisonLocator)
	if err != nil {
		return DiffReport{}, fmt.Errorf("comparison: %w", err)
	}

	return computeDiff(baseline, comparison, nil)
}

// diffFromSources loads an OBI and compares it against what its sources produce.
func diffFromSources(input DiffInput) (DiffReport, error) {
	baseline, err := resolveInterface(input.BaselineLocator)
	if err != nil {
		return DiffReport{}, fmt.Errorf("load OBI: %w", err)
	}

	obiDir := filepath.Dir(input.BaselineLocator)

	derived, err := deriveFromAllSources(baseline, obiDir, input.OnlySource)
	if err != nil {
		return DiffReport{}, err
	}

	// Cross-source drift detection (D10).
	drift := detectCrossSourceDrift(derived.PerSource)

	sort.Strings(derived.Warnings)

	report, err := computeDiff(baseline, derived.Assembled, derived.Warnings)
	if err != nil {
		return DiffReport{}, err
	}

	// Attach drift to the report.
	report.Drift = drift
	if len(drift) > 0 {
		report.Identical = false
	}

	return report, nil
}

// detectCrossSourceDrift compares per-source derivation results and flags
// operations that appear in multiple sources with different schemas (D10).
func detectCrossSourceDrift(perSource []perSourceDerivation) []DriftEntry {
	// Build map: operation key → list of (sourceKey, format, operation).
	type sourceOp struct {
		sourceKey string
		format    string
		op        openbindings.Operation
	}
	opSources := make(map[string][]sourceOp)

	for _, ps := range perSource {
		for opKey, op := range ps.result.Operations {
			opSources[opKey] = append(opSources[opKey], sourceOp{
				sourceKey: ps.key,
				format:    ps.format,
				op:        op,
			})
		}
	}

	// For each operation that appears in 2+ sources, check for schema differences.
	var drift []DriftEntry
	opKeys := make([]string, 0, len(opSources))
	for k := range opSources {
		opKeys = append(opKeys, k)
	}
	sort.Strings(opKeys)

	for _, opKey := range opKeys {
		sources := opSources[opKey]
		if len(sources) < 2 {
			continue
		}

		// Compare each pair against the first source.
		base := sources[0]
		emptyRoot := map[string]any{}
		var driftDetails []string
		var driftSources []DriftSource

		hasDrift := false
		for i := 1; i < len(sources); i++ {
			other := sources[i]
			details := diffOperation(base.op, other.op, emptyRoot, emptyRoot)
			if len(details) > 0 {
				hasDrift = true
				for _, d := range details {
					driftDetails = append(driftDetails,
						fmt.Sprintf("%s vs %s: %s", base.sourceKey, other.sourceKey, d))
				}
			}
		}

		if hasDrift {
			for _, s := range sources {
				driftSources = append(driftSources, DriftSource{
					Key:    s.sourceKey,
					Format: s.format,
				})
			}
			drift = append(drift, DriftEntry{
				Operation: opKey,
				Sources:   driftSources,
				Details:   driftDetails,
			})
		}
	}

	return drift
}

// computeDiff performs the actual structural comparison between two Interfaces.
func computeDiff(baseline, comparison *openbindings.Interface, warnings []string) (DiffReport, error) {
	report := DiffReport{
		Identical: true,
		Warnings:  warnings,
	}

	// Diff metadata.
	report.Metadata = diffMetadata(baseline, comparison)
	if len(report.Metadata) > 0 {
		report.Identical = false
	}

	// Collect all operation keys from both sides.
	allOps := make(map[string]bool)
	for k := range baseline.Operations {
		allOps[k] = true
	}
	for k := range comparison.Operations {
		allOps[k] = true
	}

	sortedKeys := make([]string, 0, len(allOps))
	for k := range allOps {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	// Build normalizers for schema comparison using each OBI's schemas as root.
	baselineRoot := buildNormalizerRoot(baseline)
	comparisonRoot := buildNormalizerRoot(comparison)

	for _, key := range sortedKeys {
		baseOp, inBaseline := baseline.Operations[key]
		compOp, inComparison := comparison.Operations[key]

		switch {
		case inBaseline && inComparison:
			details := diffOperation(baseOp, compOp, baselineRoot, comparisonRoot)
			if len(details) > 0 {
				report.Operations = append(report.Operations, OperationDiff{
					Operation: key,
					Status:    DiffChanged,
					Details:   details,
				})
				report.Identical = false
			} else {
				report.Operations = append(report.Operations, OperationDiff{
					Operation: key,
					Status:    DiffInSync,
				})
			}
		case inBaseline && !inComparison:
			report.Operations = append(report.Operations, OperationDiff{
				Operation: key,
				Status:    DiffRemoved,
			})
			report.Identical = false
		case !inBaseline && inComparison:
			report.Operations = append(report.Operations, OperationDiff{
				Operation: key,
				Status:    DiffAdded,
			})
			report.Identical = false
		}
	}

	return report, nil
}

// diffMetadata compares top-level metadata fields.
func diffMetadata(a, b *openbindings.Interface) []MetadataDiff {
	var diffs []MetadataDiff

	check := func(field, av, bv string) {
		if av != bv {
			diffs = append(diffs, MetadataDiff{Field: field, Baseline: av, Compared: bv})
		}
	}

	check("name", a.Name, b.Name)
	check("version", a.Version, b.Version)
	check("description", a.Description, b.Description)

	return diffs
}

// diffOperation compares two operations and returns the specific differences.
func diffOperation(a, b openbindings.Operation, aRoot, bRoot map[string]any) []string {
	var details []string

	// Compare schemas (input, output) using normalization.
	for _, slot := range []struct {
		name string
		a, b map[string]any
	}{
		{"input", a.Input, b.Input},
		{"output", a.Output, b.Output},
	} {
		if !schemasEqual(slot.a, slot.b, aRoot, bRoot) {
			details = append(details, fmt.Sprintf("%s schema differs", slot.name))
		}
	}

	return details
}

// schemasEqual compares two schemas for structural equality after normalization.
// Returns true if both are nil/empty, or if their canonical JSON is identical
// after normalization.
func schemasEqual(a, b map[string]any, aRoot, bRoot map[string]any) bool {
	aEmpty := len(a) == 0
	bEmpty := len(b) == 0

	if aEmpty && bEmpty {
		return true
	}
	if aEmpty != bEmpty {
		return false
	}

	// Normalize both schemas.
	aNorm := &schemaprofile.Normalizer{Root: aRoot}
	bNorm := &schemaprofile.Normalizer{Root: bRoot}

	aNormalized, err := aNorm.Normalize(a)
	if err != nil {
		// If normalization fails, fall back to canonical JSON comparison.
		return canonicalEqual(a, b)
	}
	bNormalized, err := bNorm.Normalize(b)
	if err != nil {
		return canonicalEqual(a, b)
	}

	return canonicalEqual(aNormalized, bNormalized)
}

// canonicalEqual compares two values using canonical JSON representation.
func canonicalEqual(a, b any) bool {
	aJSON, err := canonicaljson.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := canonicaljson.Marshal(b)
	if err != nil {
		return false
	}
	return string(aJSON) == string(bJSON)
}

// groupByStatus groups operation diffs by their status.
func groupByStatus(ops []OperationDiff) (added, removed, changed, inSync []OperationDiff) {
	for _, op := range ops {
		switch op.Status {
		case DiffAdded:
			added = append(added, op)
		case DiffRemoved:
			removed = append(removed, op)
		case DiffChanged:
			changed = append(changed, op)
		case DiffInSync:
			inSync = append(inSync, op)
		}
	}
	return
}
