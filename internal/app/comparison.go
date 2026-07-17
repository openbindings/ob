package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/canonicaljson"
	"github.com/openbindings/openbindings-go/schemaprofile"
)

const comparisonReportVersion = "ob-comparison-report/v1"
const comparisonProfile = "OB-2020-12"

type ComparisonInput struct {
	// Left/Right are locators (CLI). LeftInterface/RightInterface, when set,
	// are inline documents (the served operation) and take precedence.
	Left           string
	Right          string
	LeftInterface  *openbindings.Interface
	RightInterface *openbindings.Interface
	Mode           string
	Profile        string
	GeneratedAt    string
	ToolVersion    string
	ProfileHash    string
	Suppressions   []SuppressionRule
}

type ComparisonReport struct {
	FormatVersion string              `json:"format_version"`
	Profile       string              `json:"profile"`
	Mode          string              `json:"mode"`
	Tool          ComparisonTool      `json:"tool"`
	GeneratedAt   string              `json:"generated_at"`
	Inputs        ComparisonInputs    `json:"inputs"`
	Summary       ComparisonSummary   `json:"summary"`
	Operations    []OperationDelta    `json:"operations"`
	Schemas       []map[string]any    `json:"schemas"`
	Metadata      []map[string]any    `json:"metadata"`
	Sources       []map[string]any    `json:"sources"`
	Bindings      []map[string]any    `json:"bindings"`
	Transforms    []map[string]any    `json:"transforms"`
	Security      []map[string]any    `json:"security"`
	Suppressed    []SuppressedFinding `json:"suppressed"`
	Error         *Error              `json:"error,omitempty"`
}

type ComparisonTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ComparisonInputs struct {
	Left  InputDescriptor `json:"left"`
	Right InputDescriptor `json:"right"`
}

type InputDescriptor struct {
	Source        string `json:"source"`
	URI           string `json:"uri"`
	ContentSHA256 string `json:"content_sha256"`
	Label         string `json:"label"`
}

type ComparisonSummary struct {
	Verdict    string             `json:"verdict"`
	Coverage   ComparisonCoverage `json:"coverage"`
	Counts     map[string]int     `json:"counts"`
	Categories CategoryCounts     `json:"categories"`
}

type ComparisonCoverage struct {
	TotalOperations int                 `json:"total_operations"`
	Paired          int                 `json:"paired"`
	OnlyLeft        int                 `json:"only_left"`
	OnlyRight       int                 `json:"only_right"`
	PairedVia       ComparisonPairedVia `json:"paired_via"`
}

type ComparisonPairedVia struct {
	Direct int `json:"direct"`
	Alias  int `json:"alias"`
}

type CategoryCounts struct {
	Breaking    int `json:"breaking"`
	NonBreaking int `json:"non_breaking"`
	Compliance  int `json:"compliance"`
	Structural  int `json:"structural"`
}

type OperationDelta struct {
	Status   string               `json:"status"`
	Left     *OperationRef        `json:"left,omitempty"`
	Right    *OperationRef        `json:"right,omitempty"`
	Match    *MatchRecord         `json:"match,omitempty"`
	Input    *SchemaCompatibility `json:"input,omitempty"`
	Output   *SchemaCompatibility `json:"output,omitempty"`
	Findings []Finding            `json:"findings"`
}

type OperationRef struct {
	Key     string `json:"key"`
	Pointer string `json:"pointer"`
}

type MatchRecord struct {
	Strategy string `json:"strategy"`
	Left     string `json:"left"`
	Right    string `json:"right"`
}

type SchemaCompatibility struct {
	Verdict   string       `json:"verdict"`
	Direction string       `json:"direction"`
	Reasons   []FindingRef `json:"reasons"`
}

type FindingRef struct {
	Kind    string `json:"kind"`
	Pointer string `json:"pointer"`
	Side    string `json:"side"`
}

type Finding struct {
	Kind     string          `json:"kind"`
	Category []string        `json:"category"`
	Severity string          `json:"severity"`
	Location FindingLocation `json:"location"`
	Before   *any            `json:"before,omitempty"`
	After    *any            `json:"after,omitempty"`
	// Detail carries prose the kind alone cannot: the schema-compatibility
	// engine's reason for a subsumption verdict.
	Detail string `json:"detail,omitempty"`
}

type FindingLocation struct {
	Side    string `json:"side"`
	Pointer string `json:"pointer"`
}

type SuppressionRule struct {
	Kind     string `json:"kind,omitempty"`
	Pointer  string `json:"pointer,omitempty"`
	Side     string `json:"side,omitempty"`
	Reason   string `json:"reason"`
	Category string `json:"category,omitempty"`
}

type SuppressedFinding struct {
	Finding        Finding         `json:"finding"`
	SuppressedBy   SuppressionRule `json:"suppressed_by"`
	ActualSeverity string          `json:"actual_severity"`
}

type resolvedComparisonInput struct {
	locator string
	iface   *openbindings.Interface
}

// ComparisonCheck emits the v1 comparison convention report used by ob compat.
func ComparisonCheck(input ComparisonInput) ComparisonReport {
	left := input.LeftInterface
	if left == nil {
		var err error
		left, err = resolveInterface(input.Left)
		if err != nil {
			return comparisonErrorReport(input, fmt.Sprintf("left: %v", err))
		}
	}
	right := input.RightInterface
	if right == nil {
		var err error
		right, err = resolveInterface(input.Right)
		if err != nil {
			return comparisonErrorReport(input, fmt.Sprintf("right: %v", err))
		}
	}

	return CompareInterfaces(CompareInterfacesInput{
		Left:         resolvedComparisonInput{locator: input.Left, iface: left},
		Right:        resolvedComparisonInput{locator: input.Right, iface: right},
		Mode:         input.Mode,
		Profile:      input.Profile,
		GeneratedAt:  input.GeneratedAt,
		ToolVersion:  input.ToolVersion,
		ProfileHash:  input.ProfileHash,
		Suppressions: input.Suppressions,
	})
}

type CompareInterfacesInput struct {
	Left         resolvedComparisonInput
	Right        resolvedComparisonInput
	Mode         string
	Profile      string
	GeneratedAt  string
	ToolVersion  string
	ProfileHash  string
	Suppressions []SuppressionRule
}

func CompareInterfaces(input CompareInterfacesInput) ComparisonReport {
	mode := input.Mode
	if mode == "" {
		mode = "subsume"
	}
	profileName := input.Profile
	if profileName == "" {
		profileName = comparisonProfile
	}
	profileHash := input.ProfileHash
	if profileHash == "" {
		profileHash = "local"
	}
	generatedAt := input.GeneratedAt
	if generatedAt == "" {
		generatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	toolVersion := input.ToolVersion
	if toolVersion == "" {
		toolVersion = OBVersion
	}

	report := ComparisonReport{
		FormatVersion: comparisonReportVersion,
		Profile:       profileName + "@" + profileHash,
		Mode:          mode,
		Tool:          ComparisonTool{Name: "ob", Version: toolVersion},
		GeneratedAt:   generatedAt,
		Inputs: ComparisonInputs{
			Left:  inputDescriptor(input.Left.locator, "left", input.Left.iface),
			Right: inputDescriptor(input.Right.locator, "right", input.Right.iface),
		},
		Schemas:    []map[string]any{},
		Metadata:   []map[string]any{},
		Sources:    []map[string]any{},
		Bindings:   []map[string]any{},
		Transforms: []map[string]any{},
		Security:   []map[string]any{},
		Suppressed: []SuppressedFinding{},
	}

	report.Operations = compareOperationDeltas(input.Left, input.Right, mode)
	report.Suppressed = append(report.Suppressed, applySuppressions(&report, input.Suppressions)...)
	report.Summary = summarizeComparison(report.Operations)
	return report
}

func comparisonErrorReport(input ComparisonInput, message string) ComparisonReport {
	mode := input.Mode
	if mode == "" {
		mode = "subsume"
	}
	generatedAt := input.GeneratedAt
	if generatedAt == "" {
		generatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	version := input.ToolVersion
	if version == "" {
		version = OBVersion
	}
	return ComparisonReport{
		FormatVersion: comparisonReportVersion,
		Profile:       comparisonProfile + "@local",
		Mode:          mode,
		Tool:          ComparisonTool{Name: "ob", Version: version},
		GeneratedAt:   generatedAt,
		Inputs: ComparisonInputs{
			Left:  InputDescriptor{Source: locatorSource(input.Left), URI: input.Left, Label: "left"},
			Right: InputDescriptor{Source: locatorSource(input.Right), URI: input.Right, Label: "right"},
		},
		Summary: ComparisonSummary{
			Verdict: "indeterminate",
			Coverage: ComparisonCoverage{
				PairedVia: ComparisonPairedVia{},
			},
			Counts:     map[string]int{},
			Categories: CategoryCounts{},
		},
		Operations: []OperationDelta{},
		Schemas:    []map[string]any{},
		Metadata:   []map[string]any{},
		Sources:    []map[string]any{},
		Bindings:   []map[string]any{},
		Transforms: []map[string]any{},
		Security:   []map[string]any{},
		Suppressed: []SuppressedFinding{},
		Error:      &Error{Code: "resolve_error", Message: message},
	}
}

func compareOperationDeltas(left, right resolvedComparisonInput, mode string) []OperationDelta {
	leftKeys := sortedOperationKeys(left.iface)
	usedRight := map[string]bool{}
	leftRoot := schemaRoot(left.iface)
	rightRoot := schemaRoot(right.iface)
	deltas := []OperationDelta{} // wire shape: always an array, never null

	for _, key := range leftKeys {
		leftOp := left.iface.Operations[key]
		matchKey, strategy, matched := matchOperationKey(key, leftOp, right.iface, usedRight)
		if !matched {
			finding := operationFinding("operation.removed", "left", operationPointer(key), leftOp, nil)
			deltas = append(deltas, OperationDelta{
				Status:   "only_left",
				Left:     &OperationRef{Key: key, Pointer: operationPointer(key)},
				Findings: []Finding{finding},
			})
			continue
		}

		usedRight[matchKey] = true
		rightOp := right.iface.Operations[matchKey]
		delta := OperationDelta{
			Status:   "paired",
			Left:     &OperationRef{Key: key, Pointer: operationPointer(key)},
			Right:    &OperationRef{Key: matchKey, Pointer: operationPointer(matchKey)},
			Match:    &MatchRecord{Strategy: strategy, Left: key, Right: matchKey},
			Findings: []Finding{},
		}
		// Boolean schemas take their equivalent object spellings (true = {},
		// false = {"not": {}}) so the slot comparators see one form; a nil
		// (absent) slot stays nil.
		leftIn, _ := openbindings.SchemaObjectForm(leftOp.Input)
		rightIn, _ := openbindings.SchemaObjectForm(rightOp.Input)
		leftOut, _ := openbindings.SchemaObjectForm(leftOp.Output)
		rightOut, _ := openbindings.SchemaObjectForm(rightOp.Output)
		if leftOp.Input != nil || rightOp.Input != nil {
			delta.Input = compareSchemaSlot("input", leftIn, rightIn, leftRoot, rightRoot)
			delta.Findings = append(delta.Findings, schemaFindings(key, "input", leftIn, rightIn, leftRoot, rightRoot)...)
			delta.Findings = append(delta.Findings, subsumptionFindings(key, "input", leftIn, rightIn, leftRoot, rightRoot, delta.Findings)...)
			delta.Input = compatibilityForFindings("input", delta.Input.Verdict, delta.Findings)
		}
		if leftOp.Output != nil || rightOp.Output != nil {
			delta.Output = compareSchemaSlot("output", leftOut, rightOut, leftRoot, rightRoot)
			delta.Findings = append(delta.Findings, schemaFindings(key, "output", leftOut, rightOut, leftRoot, rightRoot)...)
			delta.Findings = append(delta.Findings, subsumptionFindings(key, "output", leftOut, rightOut, leftRoot, rightRoot, delta.Findings)...)
			delta.Output = compatibilityForFindings("output", delta.Output.Verdict, delta.Findings)
		}
		deltas = append(deltas, delta)
	}

	for _, key := range sortedOperationKeys(right.iface) {
		if usedRight[key] {
			continue
		}
		if _, exists := left.iface.Operations[key]; exists {
			continue
		}
		rightOp := right.iface.Operations[key]
		finding := operationFinding("operation.added", "right", operationPointer(key), nil, rightOp)
		deltas = append(deltas, OperationDelta{
			Status:   "only_right",
			Right:    &OperationRef{Key: key, Pointer: operationPointer(key)},
			Findings: []Finding{finding},
		})
	}

	sort.SliceStable(deltas, func(i, j int) bool {
		return deltaSortKey(deltas[i]) < deltaSortKey(deltas[j])
	})
	return deltas
}

func compareSchemaSlot(direction string, left, right, leftRoot, rightRoot map[string]any) *SchemaCompatibility {
	verdict := "unspecified"
	if left != nil && right != nil {
		verdict = "compatible"
		// Resolve top-level $refs before the identity check so a slot bound by
		// reference is recognised as identical to its inline equivalent.
		lr, ls, _ := derefSchema(leftRoot, left, map[string]bool{})
		rr, rs, _ := derefSchema(rightRoot, right, map[string]bool{})
		// The fast path compares raw JSON, so on its own it is blind to any
		// $ref left nested inside the resolved subtree: two documents can
		// carry the identical $ref pointer at a nested position (e.g. inside
		// "items") while each document's OWN schema registry binds that
		// pointer to different content. Only take the identity shortcut when
		// neither side has a $ref left to resolve anywhere in the subtree;
		// otherwise fall through and let the structural/subsumption checks
		// run the deep, ref-aware comparison.
		if ls == "" && rs == "" && !containsRef(lr) && !containsRef(rr) && strippedCanonicalEqual(lr, rr) {
			verdict = "identical"
		}
	}
	return &SchemaCompatibility{Verdict: verdict, Direction: direction, Reasons: []FindingRef{}}
}

// subsumptionFindings runs the SDK's schema-compatibility engine over a
// paired slot as the SAFETY NET behind the structural walk: the walk
// (schemaFindings) reports precise per-keyword findings but does not
// descend into union combinators (oneOf/anyOf), so a slot it cannot fault
// still needs a verdict. It is the same engine CheckInterfaceCompatibility
// uses, so `ob compat` and the SDKs reach the same verdict on the same
// pair — the shared-semantics promise.
//
// Composition rules: when the walk already faulted the slot as breaking,
// the engine adds nothing (the walk's pointers are sharper). When either
// schema falls outside the compatibility profile, the walk's graded
// findings (unverified.*, profile.schema.*) own the verdict and the engine
// stays silent — outside-profile is deliberately "cannot verify", never
// "incompatible".
func subsumptionFindings(opKey, direction string, left, right, leftRoot, rightRoot map[string]any, collected []Finding) []Finding {
	if left == nil || right == nil {
		return nil
	}
	if slotAlreadyFaulted(collected, direction) {
		return nil
	}
	lr, ls, _ := derefSchema(leftRoot, left, map[string]bool{})
	rr, rs, _ := derefSchema(rightRoot, right, map[string]bool{})
	if ls != "" || rs != "" {
		return nil // external/unresolved/cycle: the structural walk reports it
	}
	// Same $ref-blindness guard as compareSchemaSlot's fast path: a nested
	// $ref can bind divergent content per document even when the raw JSON is
	// byte-identical, so the "identical slot, no engine needed" shortcut only
	// applies when neither side has a $ref left anywhere in the subtree.
	if !containsRef(lr) && !containsRef(rr) && strippedCanonicalEqual(lr, rr) {
		return nil // identical slots need no engine
	}

	// Each side normalizes against its own document root so cross-document
	// $refs resolve, then the directional check runs on the results.
	ln, lerr := (&schemaprofile.Normalizer{Root: leftRoot}).Normalize(lr)
	rn, rerr := (&schemaprofile.Normalizer{Root: rightRoot}).Normalize(rr)
	if lerr != nil || rerr != nil {
		return nil // outside profile or unresolvable: graded by the walk
	}

	norm := &schemaprofile.Normalizer{}
	var ok bool
	var reason string
	var err error
	if direction == "input" {
		ok, reason, err = norm.InputCompatible(ln, rn)
	} else {
		ok, reason, err = norm.OutputCompatible(ln, rn)
	}
	if err != nil || ok {
		return nil
	}
	f := finding("subsume.violated", "right", "/operations/"+escapePointer(opKey)+"/"+direction, nil, nil, direction)
	f.Detail = reason
	return []Finding{f}
}

// slotAlreadyFaulted reports whether the structural walk already carries a
// breaking, unverified, or indeterminate finding for the slot — any of
// which owns the verdict, so the engine must not double-report.
func slotAlreadyFaulted(findings []Finding, direction string) bool {
	for _, f := range findings {
		ptr := f.Location.Pointer
		if !strings.Contains(ptr, "/"+direction+"/") && !strings.HasSuffix(ptr, "/"+direction) {
			continue
		}
		if findingVerdict(f) != "" {
			return true
		}
	}
	return false
}

func compatibilityForFindings(direction, defaultVerdict string, findings []Finding) *SchemaCompatibility {
	reasons := []FindingRef{}
	verdict := defaultVerdict
	for _, f := range findings {
		// Match findings inside the slot ("/input/...") and AT the slot
		// ("/input" — the engine's subsumption verdict is slot-level).
		if !strings.Contains(f.Location.Pointer, "/"+direction+"/") &&
			!strings.HasSuffix(f.Location.Pointer, "/"+direction) {
			continue
		}
		reasons = append(reasons, FindingRef{Kind: f.Kind, Pointer: f.Location.Pointer, Side: f.Location.Side})
		// Take the strongest verdict any finding implies. Ranking by severity
		// (rather than last-write-wins) keeps a benign or unverifiable finding
		// from masking a real incompatibility reported earlier in the slot.
		if cand := findingVerdict(f); verdictRank(cand) > verdictRank(verdict) {
			verdict = cand
		}
	}
	return &SchemaCompatibility{Verdict: verdict, Direction: direction, Reasons: reasons}
}

// verdictRank orders schema verdicts by how strongly they constrain the
// result, weakest to strongest, so the comparison can keep the strongest
// verdict any finding implies regardless of finding order.
func verdictRank(v string) int {
	switch v {
	case "identical":
		return 0
	case "compatible", "unspecified", "":
		return 1
	case "unverified":
		return 2
	case "incompatible":
		return 3
	case "indeterminate":
		return 4
	}
	return 1
}

// findingVerdict maps a finding to the verdict it implies, or "" if it does not
// affect the verdict. A failed ref resolution or off-profile schema is
// indeterminate (the comparison could not be performed); anything unverifiable
// (an external ref, a regex containment) is unverified; a breaking finding is
// incompatible.
func findingVerdict(f Finding) string {
	switch {
	case f.Kind == "profile.ref.resolution_failed" || strings.HasPrefix(f.Kind, "profile.schema."):
		return "indeterminate"
	case strings.HasPrefix(f.Kind, "unverified."):
		return "unverified"
	case hasCategory(f, "breaking"):
		return "incompatible"
	}
	return ""
}

func schemaFindings(opKey, direction string, left, right, leftRoot, rightRoot map[string]any) []Finding {
	if left == nil || right == nil {
		return nil
	}
	var findings []Finding
	compareSchemaAt(&findings, opKey, direction, left, right,
		"/operations/"+escapePointer(opKey)+"/"+direction,
		leftRoot, rightRoot, map[string]bool{}, map[string]bool{})
	return findings
}

func compareSchemaAt(findings *[]Finding, opKey, direction string, left, right map[string]any, ptr string, leftRoot, rightRoot map[string]any, leftSeen, rightSeen map[string]bool) {
	// Resolve $ref schema bindings against each side's document before
	// comparing. Without this, a "{\"$ref\": ...}" wrapper compares as an empty
	// schema, fabricating differences against an inline counterpart and hiding
	// real ones when both sides are bound by reference.
	leftRef, _ := left["$ref"].(string)
	rightRef, _ := right["$ref"].(string)
	left, ls, leftSeen := derefSchema(leftRoot, left, leftSeen)
	right, rs, rightSeen := derefSchema(rightRoot, right, rightSeen)
	if ls == "cycle" || rs == "cycle" {
		// Recursive type: a $ref back to an ancestor schema. The ancestor's
		// comparison already covers this shape; stop to avoid looping.
		return
	}
	if ls == "external" || rs == "external" {
		if ls == "external" {
			*findings = append(*findings, finding("unverified.external_ref", "left", ptr+"/$ref", leftRef, nil, direction))
		}
		if rs == "external" {
			*findings = append(*findings, finding("unverified.external_ref", "right", ptr+"/$ref", nil, rightRef, direction))
		}
		return
	}
	if ls == "unresolved" || rs == "unresolved" {
		if ls == "unresolved" {
			*findings = append(*findings, finding("profile.ref.resolution_failed", "left", ptr+"/$ref", leftRef, nil, direction))
		}
		if rs == "unresolved" {
			*findings = append(*findings, finding("profile.ref.resolution_failed", "right", ptr+"/$ref", nil, rightRef, direction))
		}
		return
	}

	if v, ok := left["exclusiveMinimum"].(bool); ok {
		*findings = append(*findings, finding("profile.schema.not_2020_12", "left", ptr+"/exclusiveMinimum", v, nil, direction))
	}
	if v, ok := right["exclusiveMinimum"].(bool); ok {
		*findings = append(*findings, finding("profile.schema.not_2020_12", "right", ptr+"/exclusiveMinimum", nil, v, direction))
	}

	if !jsonValuesEqual(left["type"], right["type"]) {
		if kind := typeFindingKind(left["type"], right["type"]); kind != "" {
			*findings = append(*findings, finding(kind, "right", ptr+"/type", left["type"], right["type"], direction))
		}
	}

	if !jsonValuesEqual(left["enum"], right["enum"]) {
		*findings = append(*findings, enumFindings(ptr, direction, left["enum"], right["enum"])...)
	}
	if !jsonValuesEqual(left["required"], right["required"]) {
		*findings = append(*findings, requiredFindings(ptr, direction, left["required"], right["required"])...)
	}

	if boolValue(left["additionalProperties"], true) != boolValue(right["additionalProperties"], true) {
		if boolValue(left["additionalProperties"], true) && !boolValue(right["additionalProperties"], true) {
			*findings = append(*findings, finding("object.additional_properties.disabled", "right", ptr+"/additionalProperties", true, false, direction))
		}
	}

	compareNumeric(findings, ptr, direction, "minimum", left["minimum"], right["minimum"])
	compareNumeric(findings, ptr, direction, "maximum", left["maximum"], right["maximum"])

	if lp, lok := left["pattern"].(string); lok {
		if rp, rok := right["pattern"].(string); rok && lp != rp {
			*findings = append(*findings, finding("unverified.regex_containment", "right", ptr+"/pattern", lp, rp, direction))
		}
	}

	leftProps, _ := left["properties"].(map[string]any)
	rightProps, _ := right["properties"].(map[string]any)
	for _, prop := range sortedMapKeys(leftProps) {
		lm, lok := leftProps[prop].(map[string]any)
		rm, rok := rightProps[prop].(map[string]any)
		if lok && rok {
			compareSchemaAt(findings, opKey, direction, lm, rm, ptr+"/properties/"+escapePointer(prop), leftRoot, rightRoot, leftSeen, rightSeen)
		}
	}

	// Array items: the schema every element must satisfy. Profile v0.1 (like
	// the engine's compatArray) treats "items" as one schema applied to every
	// element, not JSON Schema's separate tuple ("prefixItems") form.
	if li, lok := left["items"].(map[string]any); lok {
		if ri, rok := right["items"].(map[string]any); rok {
			compareSchemaAt(findings, opKey, direction, li, ri, ptr+"/items", leftRoot, rightRoot, leftSeen, rightSeen)
		}
	}

	// additionalProperties as a schema (rather than a bare true/false): the
	// boolean check above only catches a true<->false flip, so a schema-typed
	// additionalProperties needs the same structural descent as
	// "properties"/"items".
	if lap, lok := left["additionalProperties"].(map[string]any); lok {
		if rap, rok := right["additionalProperties"].(map[string]any); rok {
			compareSchemaAt(findings, opKey, direction, lap, rap, ptr+"/additionalProperties", leftRoot, rightRoot, leftSeen, rightSeen)
		}
	}
}

// schemaRoot builds the JSON-Pointer resolution root for one side: a document
// map whose "schemas" member is the interface's named-schema map, so a local
// "#/schemas/Foo" $ref resolves the same way it does in the OBI document.
func schemaRoot(iface *openbindings.Interface) map[string]any {
	schemas := make(map[string]any, len(iface.Schemas))
	for name, s := range iface.Schemas {
		schemas[name] = s
	}
	return map[string]any{"schemas": schemas}
}

// derefSchema follows local "#/..." $refs in node against root until it reaches
// a non-ref schema, a cycle, or a failure. seen holds the ref pointers already
// followed to reach node on this side; derefSchema returns the path extended
// with any refs it follows (a copy, so a sibling reusing a schema is not
// mistaken for a cycle while a $ref back to an ancestor is). The status is ""
// on success, "external" for a non-local $ref, "unresolved" for a local $ref
// that does not resolve, or "cycle" for a $ref back to an ancestor.
func derefSchema(root, node map[string]any, seen map[string]bool) (map[string]any, string, map[string]bool) {
	cur := node
	out := seen
	for {
		ref, ok := cur["$ref"].(string)
		if !ok {
			return cur, "", out
		}
		if !strings.HasPrefix(ref, "#") {
			return nil, "external", out
		}
		if out[ref] {
			return nil, "cycle", out
		}
		target, ok := resolveJSONPointer(root, ref)
		if !ok {
			return nil, "unresolved", out
		}
		tm, ok := target.(map[string]any)
		if !ok {
			return nil, "unresolved", out
		}
		next := make(map[string]bool, len(out)+1)
		for k := range out {
			next[k] = true
		}
		next[ref] = true
		out = next
		cur = tm
	}
}

// resolveJSONPointer resolves a "#/a/b" fragment against root per RFC 6901,
// decoding "~1" to "/" and "~0" to "~".
func resolveJSONPointer(root map[string]any, ref string) (any, bool) {
	frag := strings.TrimPrefix(ref, "#")
	if frag == "" {
		return root, true
	}
	if !strings.HasPrefix(frag, "/") {
		return nil, false
	}
	var cur any = root
	for _, raw := range strings.Split(frag[1:], "/") {
		token := strings.ReplaceAll(raw, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[token]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// typeFindingKind classifies a change to a schema's "type". The
// integer/number pair keeps its direction-nuanced kinds; growing or shrinking
// a type set is widened/narrowed; any other change — including an outright
// scalar swap like string→integer — is type.changed, breaking in both
// directions. Type present on only one side stays unflagged (an untyped
// schema is a deliberate accept-anything, common at intermediate levels).
func typeFindingKind(left, right any) string {
	ls, lok := left.(string)
	rs, rok := right.(string)
	if lok && rok {
		switch {
		case ls == "integer" && rs == "number":
			return "type.integer_to_number"
		case ls == "number" && rs == "integer":
			return "type.number_to_integer"
		default:
			return "type.changed"
		}
	}
	la, lok := typeSet(left)
	ra, rok := typeSet(right)
	if lok && rok {
		switch {
		case typeSubset(ra, la) && typeSubset(la, ra):
			return "" // same set, different order or form — not a change
		case typeSubset(ra, la):
			return "type.set.narrowed"
		case typeSubset(la, ra):
			return "type.set.widened"
		default:
			return "type.changed"
		}
	}
	return ""
}

// typeSet normalizes a schema "type" value — a scalar or an array of strings —
// to a set. ok is false when the value is absent or not a type value.
func typeSet(v any) (map[string]bool, bool) {
	if s, ok := v.(string); ok {
		return map[string]bool{s: true}, true
	}
	arr, ok := stringArray(v)
	if !ok {
		return nil, false
	}
	set := make(map[string]bool, len(arr))
	for _, s := range arr {
		set[s] = true
	}
	return set, true
}

// typeSubset reports whether every member of a is in b.
func typeSubset(a, b map[string]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func enumFindings(ptr, direction string, left, right any) []Finding {
	la, lok := arrayValue(left)
	ra, rok := arrayValue(right)
	if !lok || !rok {
		return nil
	}
	var out []Finding
	for i, v := range ra {
		if !arrayContains(la, v) {
			out = append(out, finding("enum.value.added", "right", ptr+"/enum/"+fmt.Sprint(i), left, right, direction))
		}
	}
	for i, v := range la {
		if !arrayContains(ra, v) {
			out = append(out, finding("enum.value.removed", "left", ptr+"/enum/"+fmt.Sprint(i), left, right, direction))
		}
	}
	return out
}

func requiredFindings(ptr, direction string, left, right any) []Finding {
	la := stringArrayOrEmpty(left)
	ra := stringArrayOrEmpty(right)
	var out []Finding
	for i, v := range ra {
		if !stringSliceContains(la, v) {
			out = append(out, finding("required.added", "right", ptr+"/required/"+fmt.Sprint(i), la, ra, direction))
		}
	}
	for i, v := range la {
		if !stringSliceContains(ra, v) {
			out = append(out, finding("required.removed", "left", ptr+"/required/"+fmt.Sprint(i), la, ra, direction))
		}
	}
	return out
}

func stringArrayOrEmpty(v any) []string {
	values, ok := stringArray(v)
	if !ok || values == nil {
		return []string{}
	}
	return values
}

func compareNumeric(findings *[]Finding, ptr, direction, keyword string, left, right any) {
	lf, lok := numberValue(left)
	rf, rok := numberValue(right)
	if !lok || !rok || lf == rf {
		return
	}
	switch keyword {
	case "minimum":
		if rf > lf {
			*findings = append(*findings, finding("numeric.minimum.tightened", "right", ptr+"/minimum", left, right, direction))
		}
	case "maximum":
		if rf < lf {
			*findings = append(*findings, finding("numeric.maximum.tightened", "right", ptr+"/maximum", left, right, direction))
		} else {
			*findings = append(*findings, finding("numeric.maximum.loosened", "right", ptr+"/maximum", left, right, direction))
		}
	}
}

func finding(kind, side, pointer string, before, after any, direction string) Finding {
	category, severity := projectedCategory(kind, direction)
	f := Finding{
		Kind:     kind,
		Category: category,
		Severity: severity,
		Location: FindingLocation{Side: side, Pointer: pointer},
	}
	if before != nil {
		f.Before = anyPtr(before)
	}
	if after != nil {
		f.After = anyPtr(after)
	}
	return f
}

func operationFinding(kind, side, pointer string, before, after any) Finding {
	category, severity := projectedCategory(kind, "")
	f := Finding{
		Kind:     kind,
		Category: category,
		Severity: severity,
		Location: FindingLocation{Side: side, Pointer: pointer},
	}
	if before != nil {
		f.Before = anyPtr(before)
	}
	if after != nil {
		f.After = anyPtr(after)
	}
	return f
}

func projectedCategory(kind, direction string) ([]string, string) {
	switch kind {
	case "operation.added":
		return []string{"non_breaking"}, "info"
	case "operation.removed":
		return []string{"breaking"}, "error"
	case "profile.schema.not_2020_12":
		return []string{"structural"}, "error"
	case "profile.ref.resolution_failed":
		return []string{"structural"}, "error"
	case "unverified.regex_containment", "unverified.external_ref":
		return []string{"non_breaking"}, "warn"
	case "type.changed":
		// A type swap breaks both directions: the candidate rejects inputs the
		// contract defines AND returns outputs the contract consumer does not
		// expect.
		return []string{"breaking"}, "error"
	case "subsume.violated":
		// The compatibility engine already ran directionally; a violation is
		// breaking by definition (the candidate cannot stand in).
		return []string{"breaking"}, "error"
	case "required.added", "object.additional_properties.disabled", "numeric.minimum.tightened", "type.number_to_integer", "type.set.narrowed":
		if direction == "input" {
			return []string{"breaking"}, "error"
		}
		return []string{"non_breaking"}, "warn"
	case "required.removed":
		if direction == "output" {
			return []string{"breaking"}, "error"
		}
		return []string{"non_breaking"}, "warn"
	case "enum.value.added", "type.integer_to_number", "type.set.widened":
		if direction == "output" {
			return []string{"breaking"}, "error"
		}
		return []string{"non_breaking"}, "warn"
	case "enum.value.removed":
		if direction == "input" {
			return []string{"breaking"}, "error"
		}
		return []string{"non_breaking"}, "warn"
	case "numeric.maximum.tightened":
		return []string{"non_breaking"}, "warn"
	case "numeric.maximum.loosened":
		if direction == "output" {
			return []string{"breaking"}, "error"
		}
		return []string{"non_breaking"}, "warn"
	default:
		return []string{"structural"}, "warn"
	}
}

func summarizeComparison(ops []OperationDelta) ComparisonSummary {
	summary := ComparisonSummary{
		Verdict: "compatible",
		Coverage: ComparisonCoverage{
			PairedVia: ComparisonPairedVia{},
		},
		Counts:     map[string]int{},
		Categories: CategoryCounts{},
	}
	for _, op := range ops {
		switch op.Status {
		case "paired":
			summary.Coverage.Paired++
			if op.Match != nil {
				switch op.Match.Strategy {
				case "direct":
					summary.Coverage.PairedVia.Direct++
				case "alias":
					summary.Coverage.PairedVia.Alias++
				}
			}
		case "only_left":
			summary.Coverage.OnlyLeft++
		case "only_right":
			summary.Coverage.OnlyRight++
		}
		for _, f := range op.Findings {
			summary.Counts[f.Kind]++
			for _, category := range f.Category {
				switch category {
				case "breaking":
					summary.Categories.Breaking++
				case "non_breaking":
					summary.Categories.NonBreaking++
				case "compliance":
					summary.Categories.Compliance++
				case "structural":
					summary.Categories.Structural++
				}
			}
			if cand := findingVerdict(f); verdictRank(cand) > verdictRank(summary.Verdict) {
				summary.Verdict = cand
			}
		}
	}
	summary.Coverage.TotalOperations = summary.Coverage.Paired + summary.Coverage.OnlyLeft
	return summary
}

func applySuppressions(report *ComparisonReport, rules []SuppressionRule) []SuppressedFinding {
	var suppressed []SuppressedFinding
	if len(rules) == 0 {
		return suppressed
	}
	for i := range report.Operations {
		active := report.Operations[i].Findings[:0]
		for _, finding := range report.Operations[i].Findings {
			if rule, ok := matchingSuppression(finding, rules); ok {
				suppressed = append(suppressed, SuppressedFinding{
					Finding:        finding,
					SuppressedBy:   rule,
					ActualSeverity: finding.Severity,
				})
				continue
			}
			active = append(active, finding)
		}
		report.Operations[i].Findings = active
		if report.Operations[i].Input != nil {
			report.Operations[i].Input = compatibilityForFindings("input", "compatible", active)
		}
		if report.Operations[i].Output != nil {
			report.Operations[i].Output = compatibilityForFindings("output", "compatible", active)
		}
	}
	return suppressed
}

func matchingSuppression(f Finding, rules []SuppressionRule) (SuppressionRule, bool) {
	for _, rule := range rules {
		if rule.Kind != "" && rule.Kind != f.Kind {
			continue
		}
		if rule.Pointer != "" && rule.Pointer != f.Location.Pointer {
			continue
		}
		if rule.Side != "" && rule.Side != f.Location.Side {
			continue
		}
		if rule.Category != "" && !hasCategory(f, rule.Category) {
			continue
		}
		return rule, true
	}
	return SuppressionRule{}, false
}

// matchOperationKey pairs a left operation to a right operation by the spec's
// key+alias resolution (OBI-T-12): direct key match first, then the left
// operation's aliases against right keys, then right operations carrying the
// left name as an alias.
func matchOperationKey(name string, leftOp openbindings.Operation, right *openbindings.Interface, used map[string]bool) (string, string, bool) {
	if _, ok := right.Operations[name]; ok && !used[name] {
		return name, "direct", true
	}
	for _, alias := range leftOp.Aliases {
		if _, ok := right.Operations[alias]; ok && !used[alias] {
			return alias, "alias", true
		}
	}
	for _, key := range sortedOperationKeys(right) {
		if used[key] {
			continue
		}
		for _, alias := range right.Operations[key].Aliases {
			if alias == name {
				return key, "alias", true
			}
		}
	}
	return "", "", false
}

func inputDescriptor(locator, label string, iface *openbindings.Interface) InputDescriptor {
	return InputDescriptor{
		Source:        locatorSource(locator),
		URI:           locator,
		ContentSHA256: contentHash(iface),
		Label:         label,
	}
}

func locatorSource(locator string) string {
	switch {
	case locator == "-":
		return "stdin"
	case strings.HasPrefix(locator, "http://"), strings.HasPrefix(locator, "https://"):
		return "url"
	default:
		return "file"
	}
}

func contentHash(v any) string {
	normalized, err := NormalizeJSON(v)
	if err != nil {
		return ""
	}
	b, err := canonicaljson.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func strippedCanonicalEqual(left, right any) bool {
	l := stripAnnotations(left)
	r := stripAnnotations(right)
	return canonicalString(l) == canonicalString(r)
}

// containsRef reports whether v (a decoded JSON value) carries a "$ref" key
// anywhere in its structure. It gates the identity fast paths in
// compareSchemaSlot and subsumptionFindings: comparing raw JSON is only safe
// when there is no $ref left to resolve, because two documents can share the
// exact same $ref pointer at a nested position while each document's own
// schema registry binds that pointer to different content — byte-identical
// wrapping JSON, divergent resolved meaning.
func containsRef(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["$ref"]; ok {
			return true
		}
		for _, child := range x {
			if containsRef(child) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if containsRef(child) {
				return true
			}
		}
	}
	return false
}

func stripAnnotations(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			switch k {
			case "title", "description", "default", "examples", "$comment", "readOnly", "writeOnly", "deprecated":
				continue
			default:
				out[k] = stripAnnotations(v)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = stripAnnotations(x[i])
		}
		return out
	default:
		return v
	}
}

func canonicalString(v any) string {
	b, err := canonicaljson.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func sortedOperationKeys(iface *openbindings.Interface) []string {
	keys := make([]string, 0, len(iface.Operations))
	for k := range iface.Operations {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func operationPointer(key string) string {
	return "/operations/" + escapePointer(key)
}

func escapePointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}

func deltaSortKey(delta OperationDelta) string {
	if delta.Left != nil {
		return delta.Left.Key
	}
	if delta.Right != nil {
		return delta.Right.Key
	}
	return ""
}

func hasCategory(f Finding, category string) bool {
	for _, c := range f.Category {
		if c == category {
			return true
		}
	}
	return false
}

func anyPtr(v any) *any {
	return &v
}

func jsonValuesEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func boolValue(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func stringArray(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func arrayValue(v any) ([]any, bool) {
	a, ok := v.([]any)
	return a, ok
}

func arrayContains(values []any, needle any) bool {
	n := canonicalString(needle)
	for _, v := range values {
		if canonicalString(v) == n {
			return true
		}
	}
	return false
}

func stringSliceContains(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func numberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func (r ComparisonReport) Render() string {
	var sb strings.Builder
	s := Styles
	sb.WriteString(s.Header.Render("Comparison Report"))
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  target:    "))
	sb.WriteString(r.Inputs.Left.URI)
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  candidate: "))
	sb.WriteString(r.Inputs.Right.URI)
	sb.WriteString("\n\n")
	if r.Error != nil {
		sb.WriteString(s.Error.Render("  Error: "))
		sb.WriteString(r.Error.Message)
		return sb.String()
	}
	for _, op := range r.Operations {
		switch op.Status {
		case "paired":
			sb.WriteString("  ")
			sb.WriteString(s.Success.Render("paired "))
			sb.WriteString(op.Left.Key)
			if op.Right != nil && op.Right.Key != op.Left.Key {
				sb.WriteString(" -> ")
				sb.WriteString(op.Right.Key)
			}
		case "only_left":
			sb.WriteString("  ")
			sb.WriteString(s.Error.Render("removed "))
			sb.WriteString(op.Left.Key)
		case "only_right":
			sb.WriteString("  ")
			sb.WriteString(s.Success.Render("added "))
			sb.WriteString(op.Right.Key)
		}
		for _, finding := range op.Findings {
			sb.WriteString("\n      ")
			sb.WriteString(finding.Kind)
			sb.WriteString(" ")
			sb.WriteString(findingLocationLabel(finding))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString("  verdict: ")
	switch r.Summary.Verdict {
	case "compatible":
		sb.WriteString(s.Success.Render(r.Summary.Verdict))
	case "incompatible", "indeterminate":
		sb.WriteString(s.Error.Render(r.Summary.Verdict))
	default:
		sb.WriteString(s.Warning.Render(r.Summary.Verdict))
	}
	return sb.String()
}

// findingLocationLabel renders a finding's pointer for humans. When the
// pointer ends in an array index — a required-field or enum position — it
// resolves that index to the actual value the finding carries, so the reader
// sees which field changed by name (…/required/lang) instead of by position
// (…/required/0). The JSON model keeps the raw indexed pointer.
func findingLocationLabel(f Finding) string {
	ptr := f.Location.Pointer
	slash := strings.LastIndex(ptr, "/")
	if slash < 0 {
		return ptr
	}
	idx, err := strconv.Atoi(ptr[slash+1:])
	if err != nil {
		return ptr
	}
	// An added element (side "right") is named in After; a removed one
	// (side "left") in Before.
	src := f.After
	if f.Location.Side == "left" {
		src = f.Before
	}
	name, ok := arrayElementLabel(src, idx)
	if !ok {
		return ptr
	}
	return ptr[:slash+1] + name
}

// arrayElementLabel returns the idx-th element of an array carried in a
// finding's before/after value, as a display string.
func arrayElementLabel(v *any, idx int) (string, bool) {
	if v == nil || idx < 0 {
		return "", false
	}
	switch arr := (*v).(type) {
	case []string:
		if idx < len(arr) {
			return arr[idx], true
		}
	case []any:
		if idx < len(arr) {
			return fmt.Sprintf("%v", arr[idx]), true
		}
	}
	return "", false
}
