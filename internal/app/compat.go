package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/schemaprofile"
)

// SlotStatus represents the compatibility status of a single schema slot
// per the spec: compatible, incompatible, or unspecified.
type SlotStatus string

const (
	SlotCompatible   SlotStatus = "compatible"
	SlotIncompatible SlotStatus = "incompatible"
	SlotUnspecified  SlotStatus = "unspecified"
)

// CompatInput specifies the two interfaces to compare.
// Each locator may be a local file path, an HTTP(S) URL, or an exec: reference.
type CompatInput struct {
	Target    string
	Candidate string
}

// ConformanceLevel represents the degree of interface conformance.
type ConformanceLevel string

const (
	ConformanceFull    ConformanceLevel = "full"
	ConformancePartial ConformanceLevel = "partial"
	ConformanceNone    ConformanceLevel = "none"
)

// CoverageInfo summarizes operation-level coverage statistics.
type CoverageInfo struct {
	Matched      int `json:"matched"`
	Compatible   int `json:"compatible"`
	Incompatible int `json:"incompatible"`
	Total        int `json:"total"`
}

// CompatibilityReport is the result of a compatibility check, aligned with
// the spec: "Compatibility checking produces a report, not a binary verdict."
type CompatibilityReport struct {
	Target      string            `json:"target"`
	Candidate   string            `json:"candidate"`
	Operations  []OperationReport `json:"operations,omitempty"`
	Compatible  bool              `json:"compatible"`
	Conformance ConformanceLevel  `json:"conformance"`
	Coverage    CoverageInfo      `json:"coverage"`
	Error       *Error            `json:"error,omitempty"`
}

// OperationReport reports compatibility for a single operation, including
// per-slot status as required by the spec.
type OperationReport struct {
	Operation string `json:"operation"`

	// Matched is true if a matching operation exists in the candidate.
	Matched bool `json:"matched"`

	// Per-slot status: compatible, incompatible, or unspecified.
	Input  SlotStatus `json:"input,omitempty"`
	Output SlotStatus `json:"output,omitempty"`

	// Details provides human-readable context for incompatible or error slots.
	// Distinguishes schema incompatibility from normalization/analysis failures.
	Details []string `json:"details,omitempty"`

	// Compatible is true when the operation fully passes all applicable checks.
	Compatible bool `json:"compatible"`
}

// CompatibilityCheck compares two OpenBindings interfaces for schema compatibility
// per the v0.1 profile. Each locator may be a file path, HTTP URL, or exec: ref.
func CompatibilityCheck(input CompatInput) CompatibilityReport {
	// Resolve target interface.
	target, err := resolveInterface(input.Target)
	if err != nil {
		return CompatibilityReport{
			Target:    input.Target,
			Candidate: input.Candidate,
			Error: &Error{
				Code:    "resolve_error",
				Message: fmt.Sprintf("target: %v", err),
			},
		}
	}

	// Resolve candidate interface.
	candidate, err := resolveInterface(input.Candidate)
	if err != nil {
		return CompatibilityReport{
			Target:    input.Target,
			Candidate: input.Candidate,
			Error: &Error{
				Code:    "resolve_error",
				Message: fmt.Sprintf("candidate: %v", err),
			},
		}
	}

	// Build the report.
	// Pass target locator so satisfies/roles matching can resolve references.
	ops := compareOps(input.Target, target, candidate)

	compat, missing, incompat := countResults(ops)
	total := len(ops)
	matched := total - missing
	allCompat := compat == total && total > 0

	var conformance ConformanceLevel
	switch {
	case allCompat:
		conformance = ConformanceFull
	case matched > 0 && incompat == 0:
		// Some operations matched and all matched ones are compatible.
		conformance = ConformancePartial
	default:
		conformance = ConformanceNone
	}

	return CompatibilityReport{
		Target:      input.Target,
		Candidate:   input.Candidate,
		Operations:  ops,
		Compatible:  allCompat,
		Conformance: conformance,
		Coverage: CoverageInfo{
			Matched:      matched,
			Compatible:   compat,
			Incompatible: incompat,
			Total:        total,
		},
	}
}

// compareOps checks each target operation against the candidate per the spec.
// targetLocator is the original locator string for the target (file path, URL, exec: ref)
// so that satisfies/roles matching can resolve references.
func compareOps(targetLocator string, target, candidate *openbindings.Interface) []OperationReport {
	// Sort operation keys for deterministic output.
	opKeys := make([]string, 0, len(target.Operations))
	for k := range target.Operations {
		opKeys = append(opKeys, k)
	}
	sort.Strings(opKeys)

	tgtRoot := buildNormalizerRoot(target)
	candRoot := buildNormalizerRoot(candidate)

	var reports []OperationReport
	for _, opName := range opKeys {
		tgtOp := target.Operations[opName]

		// Operation matching per spec: satisfies first (preferred), then key/alias fallback.
		candOp, matched := matchOperation(opName, tgtOp, targetLocator, candidate)

		if !matched {
			reports = append(reports, OperationReport{
				Operation:  opName,
				Matched:    false,
				Compatible: false,
			})
			continue
		}

		report := buildOperationReport(opName, tgtOp, candOp, tgtRoot, candRoot)
		reports = append(reports, report)
	}

	return reports
}

// matchOperation finds a matching operation in the candidate per the spec's
// deterministic matching algorithm:
//
//  1. Explicit match (preferred): any candidate operation that declares
//     satisfies: [{ role: <roleKey>, operation: <opOrAlias> }]
//     where candidate.roles[roleKey] resolves to targetLocator,
//     and opOrAlias resolves to the target operation name or one of its aliases.
//  2. Fallback match: key or alias matching (only if no explicit match exists).
//
// The match MUST be unique — if multiple candidates match, we take the first
// explicit match (satisfies) or first fallback match.
func matchOperation(name string, tgtOp openbindings.Operation, targetLocator string, candidate *openbindings.Interface) (openbindings.Operation, bool) {
	// Phase 1: Check satisfies declarations (preferred).
	if candidate.Roles != nil && targetLocator != "" {
		// Build a set of role keys that resolve to the target.
		targetRoleKeys := make(map[string]bool)
		for key, loc := range candidate.Roles {
			if loc == targetLocator {
				targetRoleKeys[key] = true
			}
		}

		if len(targetRoleKeys) > 0 {
			// Build the set of names that resolve to this target operation.
			targetNames := map[string]bool{name: true}
			for _, alias := range tgtOp.Aliases {
				targetNames[alias] = true
			}

			// Scan candidate operations for satisfies declarations.
			for _, candOp := range candidate.Operations {
				for _, sat := range candOp.Satisfies {
					if targetRoleKeys[sat.Role] && targetNames[sat.Operation] {
						return candOp, true
					}
				}
			}
		}
	}

	// Phase 2: Fallback — key and alias matching.

	// Direct key match.
	if op, ok := candidate.Operations[name]; ok {
		return op, true
	}

	// Check target aliases against candidate keys.
	for _, alias := range tgtOp.Aliases {
		if op, ok := candidate.Operations[alias]; ok {
			return op, true
		}
	}

	// Check candidate aliases against target key.
	for _, candOp := range candidate.Operations {
		for _, alias := range candOp.Aliases {
			if alias == name {
				return candOp, true
			}
		}
	}

	return openbindings.Operation{}, false
}

// buildOperationReport compares schemas for a matched operation pair.
func buildOperationReport(
	name string,
	tgtOp, candOp openbindings.Operation,
	tgtRoot, candRoot map[string]any,
) OperationReport {
	report := OperationReport{
		Operation: name,
		Matched:   true,
	}

	tgtNorm := &schemaprofile.Normalizer{Root: tgtRoot}
	candNorm := &schemaprofile.Normalizer{Root: candRoot}

	var details []string

	var detail string
	report.Input, detail = slotCompat("input", tgtOp.Input, candOp.Input, tgtNorm, candNorm, true)
	if detail != "" {
		details = append(details, detail)
	}
	report.Output, detail = slotCompat("output", tgtOp.Output, candOp.Output, tgtNorm, candNorm, false)
	if detail != "" {
		details = append(details, detail)
	}

	report.Details = details

	report.Compatible =
		report.Input != SlotIncompatible &&
		report.Output != SlotIncompatible

	return report
}

// slotCompat evaluates a single schema slot (input, output, or payload).
// Returns the status and, for incompatible slots, a human-readable detail
// that distinguishes normalization failures from genuine schema mismatches.
func slotCompat(
	slotName string,
	tgtSchema, candSchema map[string]any,
	tgtNorm, candNorm *schemaprofile.Normalizer,
	isInput bool,
) (SlotStatus, string) {
	// Unspecified if either side omits the schema.
	if tgtSchema == nil || candSchema == nil {
		return SlotUnspecified, ""
	}

	// Normalize both schemas (resolves $ref, strips annotations, flattens allOf).
	tgtNormalized, err := tgtNorm.Normalize(tgtSchema)
	if err != nil {
		return SlotIncompatible, fmt.Sprintf("%s: target schema could not be normalized: %v", slotName, err)
	}
	candNormalized, err := candNorm.Normalize(candSchema)
	if err != nil {
		return SlotIncompatible, fmt.Sprintf("%s: candidate schema could not be normalized: %v", slotName, err)
	}

	// Compare with a fresh normalizer (schemas are already normalized,
	// $refs resolved — empty root is fine).
	n := &schemaprofile.Normalizer{Root: map[string]any{}}
	var ok bool
	var reason string
	if isInput {
		ok, reason, err = n.InputCompatible(tgtNormalized, candNormalized)
	} else {
		ok, reason, err = n.OutputCompatible(tgtNormalized, candNormalized)
	}
	if err != nil {
		return SlotIncompatible, fmt.Sprintf("%s: compatibility check error: %v", slotName, err)
	}
	if !ok {
		if reason != "" {
			return SlotIncompatible, fmt.Sprintf("%s: incompatible: %s", slotName, reason)
		}
		return SlotIncompatible, fmt.Sprintf("%s: incompatible", slotName)
	}
	return SlotCompatible, ""
}

// Render returns a human-friendly representation of the compatibility report.
func (r CompatibilityReport) Render() string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Compatibility Report"))
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  target:    "))
	sb.WriteString(r.Target)
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  candidate: "))
	sb.WriteString(r.Candidate)
	sb.WriteString("\n\n")

	if r.Error != nil {
		sb.WriteString(s.Error.Render("  ✗ Error: "))
		sb.WriteString(r.Error.Message)
		return sb.String()
	}

	for _, op := range r.Operations {
		if op.Compatible {
			sb.WriteString(s.Success.Render("  ✓ "))
		} else {
			sb.WriteString(s.Error.Render("  ✗ "))
		}
		sb.WriteString(s.Key.Render(op.Operation))

		if !op.Matched {
			sb.WriteString(s.Warning.Render(" — not found in candidate"))
		} else if len(op.Details) > 0 {
			for _, d := range op.Details {
				sb.WriteString("\n")
				sb.WriteString("      ")
				sb.WriteString(s.Warning.Render(d))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	switch r.Conformance {
	case ConformanceFull:
		sb.WriteString(s.Success.Render(fmt.Sprintf("  ✓ Full conformance: all %d operations compatible", r.Coverage.Total)))
	case ConformancePartial:
		sb.WriteString(fmt.Sprintf("  %s %d/%d operations compatible",
			s.Warning.Render("◐ Partial conformance:"), r.Coverage.Compatible, r.Coverage.Total))
		if missing := r.Coverage.Total - r.Coverage.Matched; missing > 0 {
			sb.WriteString(fmt.Sprintf(" (%d unmatched)", missing))
		}
	default:
		sb.WriteString(fmt.Sprintf("  %s %d/%d compatible",
			s.Error.Render("✗ Not conformant:"), r.Coverage.Compatible, r.Coverage.Total))
		if missing := r.Coverage.Total - r.Coverage.Matched; missing > 0 {
			sb.WriteString(", ")
			sb.WriteString(s.Warning.Render(fmt.Sprintf("%d unmatched", missing)))
		}
		if r.Coverage.Incompatible > 0 {
			sb.WriteString(", ")
			sb.WriteString(s.Error.Render(fmt.Sprintf("%d incompatible", r.Coverage.Incompatible)))
		}
	}

	return sb.String()
}

// countResults tallies compatible, missing, and incompatible operations.
func countResults(ops []OperationReport) (compat, missing, incompat int) {
	for _, op := range ops {
		switch {
		case !op.Matched:
			missing++
		case op.Compatible:
			compat++
		default:
			incompat++
		}
	}
	return
}
