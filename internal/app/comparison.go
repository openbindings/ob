package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/canonicaljson"
)

const comparisonReportVersion = "ob-comparison-report/v1"
const comparisonProfile = "OB-2020-12"

type ComparisonInput struct {
	Left         string
	Right        string
	Mode         string
	Profile      string
	GeneratedAt  string
	ToolVersion  string
	ProfileHash  string
	Suppressions []SuppressionRule
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
	Direct    int `json:"direct"`
	Alias     int `json:"alias"`
	Satisfies int `json:"satisfies"`
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
	left, err := resolveInterface(input.Left)
	if err != nil {
		return comparisonErrorReport(input, fmt.Sprintf("left: %v", err))
	}
	right, err := resolveInterface(input.Right)
	if err != nil {
		return comparisonErrorReport(input, fmt.Sprintf("right: %v", err))
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
	var deltas []OperationDelta

	for _, key := range leftKeys {
		leftOp := left.iface.Operations[key]
		matchKey, strategy, matched := matchOperationKey(key, leftOp, left.locator, right.iface, usedRight)
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
		if leftOp.Input != nil || rightOp.Input != nil {
			delta.Input = compareSchemaSlot(key, "input", leftOp.Input, rightOp.Input, mode)
			delta.Findings = append(delta.Findings, schemaFindings(key, "input", leftOp.Input, rightOp.Input, mode)...)
			delta.Input = compatibilityForFindings("input", delta.Input.Verdict, delta.Findings)
		}
		if leftOp.Output != nil || rightOp.Output != nil {
			delta.Output = compareSchemaSlot(key, "output", leftOp.Output, rightOp.Output, mode)
			delta.Findings = append(delta.Findings, schemaFindings(key, "output", leftOp.Output, rightOp.Output, mode)...)
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

func compareSchemaSlot(opKey, direction string, left, right map[string]any, mode string) *SchemaCompatibility {
	verdict := "unspecified"
	if left != nil && right != nil {
		verdict = "compatible"
		if strippedCanonicalEqual(left, right) {
			verdict = "identical"
		}
	}
	return &SchemaCompatibility{Verdict: verdict, Direction: direction, Reasons: []FindingRef{}}
}

func compatibilityForFindings(direction, defaultVerdict string, findings []Finding) *SchemaCompatibility {
	reasons := []FindingRef{}
	verdict := defaultVerdict
	for _, f := range findings {
		if !strings.Contains(f.Location.Pointer, "/"+direction+"/") {
			continue
		}
		reasons = append(reasons, FindingRef{Kind: f.Kind, Pointer: f.Location.Pointer, Side: f.Location.Side})
		switch {
		case strings.HasPrefix(f.Kind, "profile.schema."):
			verdict = "indeterminate"
		case f.Kind == "unverified.regex_containment" && verdict != "indeterminate":
			verdict = "unverified"
		case hasCategory(f, "breaking") && verdict != "indeterminate":
			verdict = "incompatible"
		case verdict != "incompatible" && verdict != "unverified" && verdict != "indeterminate" && defaultVerdict != "identical":
			verdict = "compatible"
		}
	}
	return &SchemaCompatibility{Verdict: verdict, Direction: direction, Reasons: reasons}
}

func schemaFindings(opKey, direction string, left, right map[string]any, mode string) []Finding {
	if left == nil || right == nil {
		return nil
	}
	var findings []Finding
	compareSchemaAt(&findings, opKey, direction, left, right, "/operations/"+escapePointer(opKey)+"/"+direction)
	return findings
}

func compareSchemaAt(findings *[]Finding, opKey, direction string, left, right map[string]any, ptr string) {
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
			compareSchemaAt(findings, opKey, direction, lm, rm, ptr+"/properties/"+escapePointer(prop))
		}
	}
}

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
			return ""
		}
	}
	la, lok := stringArray(left)
	ra, rok := stringArray(right)
	if lok && rok {
		if len(ra) < len(la) {
			return "type.set.narrowed"
		}
		if len(ra) > len(la) {
			return "type.set.widened"
		}
	}
	return ""
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
	case "unverified.regex_containment":
		return []string{"non_breaking"}, "warn"
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
				case "satisfies":
					summary.Coverage.PairedVia.Satisfies++
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
			switch {
			case strings.HasPrefix(f.Kind, "profile.schema."):
				summary.Verdict = "indeterminate"
			case f.Kind == "unverified.regex_containment" && summary.Verdict != "indeterminate":
				summary.Verdict = "unverified"
			case hasCategory(f, "breaking") && summary.Verdict != "indeterminate":
				summary.Verdict = "incompatible"
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

func matchOperationKey(name string, leftOp openbindings.Operation, leftLocator string, right *openbindings.Interface, used map[string]bool) (string, string, bool) {
	if right.Roles != nil && leftLocator != "" {
		roleKeys := map[string]bool{}
		for key, loc := range right.Roles {
			if loc == leftLocator {
				roleKeys[key] = true
			}
		}
		names := map[string]bool{name: true}
		for _, alias := range leftOp.Aliases {
			names[alias] = true
		}
		for _, key := range sortedOperationKeys(right) {
			if used[key] {
				continue
			}
			for _, sat := range right.Operations[key].Satisfies {
				if roleKeys[sat.Role] && names[sat.Operation] {
					return key, "satisfies", true
				}
			}
		}
	}

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
	sb.WriteString(s.Dim.Render("  left:  "))
	sb.WriteString(r.Inputs.Left.URI)
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  right: "))
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
			sb.WriteString(finding.Location.Pointer)
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
