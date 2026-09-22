package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openbindings/openbindings-go"
)

// ValidateInput specifies the interface to validate. The served operation
// supplies the document inline as Document, the exact bytes of its
// `interface` member; the CLI supplies a Locator (file path, HTTP(S) URL, or
// exec: reference) that is resolved to the document's bytes. Interface
// validates a document already in memory, where OBI-D-01 cannot be decided.
// Document takes precedence, then Interface, then Locator.
type ValidateInput struct {
	Document  []byte
	Locator   string
	Interface *openbindings.Interface
}

// ValidationReport is the result of validating an OpenBindings interface
// document against the core specification's document rules.
//
// Conclusion is the §10.5 conformance conclusion. It is absent when the
// document was refused (OBI-T-04), which is not a conclusion, or when the
// locator could not be resolved.
type ValidationReport struct {
	Locator      string                 `json:"locator,omitempty"`
	Version      string                 `json:"version,omitempty"`
	Conclusion   string                 `json:"conclusion,omitempty"`
	Violated     []string               `json:"violated,omitempty"`
	Inconclusive []string               `json:"inconclusive,omitempty"`
	Findings     []ValidationFinding    `json:"findings,omitempty"`
	Diagnostics  []ValidationDiagnostic `json:"diagnostics,omitempty"`
	Refusal      *VersionRefusal        `json:"refusal,omitempty"`
	Error        *Error                 `json:"error,omitempty"`
}

// ValidationFinding locates one violation, or one check that could not be
// decided, at a position in the document.
type ValidationFinding struct {
	Rule    string `json:"rule"`
	Status  string `json:"status"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// ValidationDiagnostic is advice a rule asks tools to surface without
// affecting the conclusion, such as OBI-T-02's notice of an unknown field.
type ValidationDiagnostic struct {
	Rule    string `json:"rule"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// VersionRefusal reports that the document declares a version outside the
// supported set, so it was not interpreted (OBI-T-04).
type VersionRefusal struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

// Failed reports whether the report should fail a gate: a violation was
// established, the document was refused, or it could not be resolved. A
// conformance-undetermined report does not fail, because an inconclusive
// rule is not evidence of violation.
func (r ValidationReport) Failed() bool {
	return r.Error != nil || r.Refusal != nil || r.Conclusion == string(openbindings.ConclusionNonConformant)
}

// ValidateInterface validates an OpenBindings interface document.
func ValidateInterface(input ValidateInput) ValidationReport {
	out := ValidationReport{Locator: input.Locator}
	var (
		report openbindings.ValidationReport
		err    error
	)
	switch {
	case input.Document != nil:
		_, report, err = openbindings.ValidateDocument(input.Document)
		out.Version = declaredVersion(input.Document)
	case input.Interface != nil:
		report, err = input.Interface.Validate()
		out.Version = input.Interface.OpenBindings
	default:
		data, resolveErr := resolveInterfaceBytes(input.Locator)
		if resolveErr != nil {
			out.Error = &Error{Code: "resolve_error", Message: resolveErr.Error()}
			return out
		}
		_, report, err = openbindings.ValidateDocument(data)
		out.Version = declaredVersion(data)
	}

	var refusal *openbindings.VersionRefusalError
	if errors.As(err, &refusal) {
		out.Refusal = &VersionRefusal{Version: refusal.Version, Reason: refusal.Reason}
		return out
	}
	out.Conclusion = string(report.Conclusion)
	out.Violated = report.Violated
	out.Inconclusive = report.Inconclusive
	for _, finding := range report.Findings {
		out.Findings = append(out.Findings, ValidationFinding{
			Rule:    finding.Rule,
			Status:  string(finding.Status),
			Path:    finding.Path,
			Message: finding.Message,
		})
	}
	for _, diagnostic := range report.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, ValidationDiagnostic(diagnostic))
	}
	return out
}

// resolveInterfaceBytes returns the exact bytes of the document a locator
// names, so validation decides OBI-D-01 on them.
func resolveInterfaceBytes(locator string) ([]byte, error) {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return nil, fmt.Errorf("empty locator")
	}
	if !IsExecURL(locator) && !IsHTTPURL(locator) && !strings.Contains(locator, "://") {
		return readLocatorBytes(locator)
	}
	result := ProbeOBI(locator, DefaultProbeTimeout)
	if result.Status != ProbeStatusOK || result.OBI == "" {
		detail := result.Detail
		if detail == "" {
			detail = "no OpenBindings interface found"
		}
		return nil, fmt.Errorf("%s", detail)
	}
	return []byte(result.OBI), nil
}

// declaredVersion reads the document's declared openbindings value when it
// is a string, for display beside the conclusion.
func declaredVersion(data []byte) string {
	var head struct {
		OpenBindings any `json:"openbindings"`
	}
	if json.Unmarshal(data, &head) != nil {
		return ""
	}
	version, _ := head.OpenBindings.(string)
	return version
}

// Render returns a human-friendly representation of the validation report.
func (r ValidationReport) Render() string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Validation Report"))
	sb.WriteString("\n")
	if r.Locator != "" {
		sb.WriteString(s.Dim.Render("  locator: "))
		sb.WriteString(r.Locator)
		sb.WriteString("\n")
	}
	if r.Version != "" {
		sb.WriteString(s.Dim.Render("  version: "))
		sb.WriteString(r.Version)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	switch {
	case r.Error != nil:
		sb.WriteString(s.Error.Render("  ✗ Error: "))
		sb.WriteString(r.Error.Message)
		return sb.String()
	case r.Refusal != nil:
		sb.WriteString(s.Error.Render("  ✗ Refused: "))
		sb.WriteString(r.Refusal.Reason)
		sb.WriteString(" (OBI-T-04)")
		return sb.String()
	}

	violations := r.findingsWith(string(openbindings.EvidenceViolated))
	inconclusive := r.findingsWith(string(openbindings.EvidenceInconclusive))
	switch r.Conclusion {
	case string(openbindings.ConclusionConformant):
		sb.WriteString(s.Success.Render("  ✓ Conformant"))
	case string(openbindings.ConclusionNonConformant):
		sb.WriteString(s.Error.Render("  ✗ Non-conformant"))
		sb.WriteString(fmt.Sprintf(" — %d %s", len(violations), pluralize(len(violations), "violation", "violations")))
	default:
		sb.WriteString(s.Warning.Render("  ? Conformance undetermined"))
		sb.WriteString(fmt.Sprintf(" — %d %s", len(r.Inconclusive), pluralize(len(r.Inconclusive), "rule inconclusive", "rules inconclusive")))
	}
	writeFindings(&sb, s.Error, violations)
	if len(inconclusive) > 0 {
		if r.Conclusion == string(openbindings.ConclusionNonConformant) {
			sb.WriteString("\n\n  Inconclusive:")
		}
		writeFindings(&sb, s.Warning, inconclusive)
	}
	if len(r.Diagnostics) > 0 {
		sb.WriteString("\n\n  Diagnostics:")
		for _, d := range r.Diagnostics {
			sb.WriteString("\n    ")
			sb.WriteString(s.Dim.Render("• " + formatValidationLine(d.Path, d.Message, d.Rule)))
		}
	}
	return sb.String()
}

func (r ValidationReport) findingsWith(status string) []ValidationFinding {
	var out []ValidationFinding
	for _, finding := range r.Findings {
		if finding.Status == status {
			out = append(out, finding)
		}
	}
	return out
}

func writeFindings(sb *strings.Builder, style interface{ Render(...string) string }, findings []ValidationFinding) {
	for _, f := range findings {
		sb.WriteString("\n    ")
		sb.WriteString(style.Render("• " + formatValidationLine(f.Path, f.Message, f.Rule)))
	}
}

func formatValidationLine(path, message, rule string) string {
	if path == "" {
		return fmt.Sprintf("%s (%s)", message, rule)
	}
	return fmt.Sprintf("%s: %s (%s)", path, message, rule)
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
