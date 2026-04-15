package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openbindings/openbindings-go"
)

// ValidateInput specifies the interface to validate.
// Locator may be a local file path, HTTP(S) URL, or exec: reference.
type ValidateInput struct {
	Locator   string
	Strict    bool
	SkipRoles bool
}

// RoleConformanceReport summarizes conformance against one role interface.
type RoleConformanceReport struct {
	Role        string            `json:"role"`
	Location    string            `json:"location"`
	Resolved    bool              `json:"resolved"`
	Error       string            `json:"error,omitempty"`
	Conformance ConformanceLevel  `json:"conformance,omitempty"`
	Operations  []OperationReport `json:"operations,omitempty"`
	Coverage    *CoverageInfo     `json:"coverage,omitempty"`
}

// ValidationReport is the result of validating an OpenBindings interface.
type ValidationReport struct {
	Locator  string   `json:"locator"`
	Valid    bool     `json:"valid"`
	Version  string   `json:"version,omitempty"`
	Problems []string `json:"problems,omitempty"`
	Error    *Error   `json:"error,omitempty"`

	// RoleReports contains per-role conformance results.
	// Only populated when roles are present and --skip-roles is not set.
	RoleReports []RoleConformanceReport `json:"roleReports,omitempty"`
}

// ValidateInterface loads and validates an OpenBindings interface document.
func ValidateInterface(input ValidateInput) ValidationReport {
	iface, err := resolveInterface(input.Locator)
	if err != nil {
		return ValidationReport{
			Locator: input.Locator,
			Error: &Error{
				Code:    "resolve_error",
				Message: err.Error(),
			},
		}
	}

	var opts []openbindings.ValidateOption
	if input.Strict {
		opts = append(opts,
			openbindings.WithRejectUnknownTypedFields(),
			openbindings.WithRequireSupportedVersion(),
		)
	}

	err = iface.Validate(opts...)
	if err != nil {
		ve, ok := err.(*openbindings.ValidationError)
		if ok {
			return ValidationReport{
				Locator:  input.Locator,
				Valid:    false,
				Version:  iface.OpenBindings,
				Problems: ve.Problems,
			}
		}
		return ValidationReport{
			Locator:  input.Locator,
			Valid:    false,
			Version:  iface.OpenBindings,
			Problems: []string{err.Error()},
		}
	}

	report := ValidationReport{
		Locator: input.Locator,
		Valid:   true,
		Version: iface.OpenBindings,
	}

	// Check role conformance unless skipped.
	if !input.SkipRoles && len(iface.Roles) > 0 {
		report.RoleReports = checkRoleConformance(iface)

		// Add conformance problems to the main report.
		for _, rr := range report.RoleReports {
			if !rr.Resolved {
				report.Problems = append(report.Problems,
					fmt.Sprintf("role %q: could not resolve (%s)", rr.Role, rr.Error))
				continue
			}
			if rr.Conformance == ConformanceNone {
				report.Valid = false
				report.Problems = append(report.Problems,
					fmt.Sprintf("role %q: not conformant (%d/%d operations compatible)",
						rr.Role, rr.Coverage.Compatible, rr.Coverage.Total))
			} else if rr.Conformance == ConformancePartial {
				// Partial conformance is not a validation failure but worth reporting.
				report.Problems = append(report.Problems,
					fmt.Sprintf("role %q: partial conformance (%d/%d operations compatible)",
						rr.Role, rr.Coverage.Compatible, rr.Coverage.Total))
			}
			// Report individual incompatible operations.
			for _, op := range rr.Operations {
				if op.Matched && !op.Compatible {
					for _, d := range op.Details {
						report.Problems = append(report.Problems,
							fmt.Sprintf("role %q, operation %q: %s", rr.Role, op.Operation, d))
					}
				}
			}
		}
	}

	return report
}

// checkRoleConformance resolves each role interface and checks that the
// candidate's satisfies declarations actually hold.
func checkRoleConformance(candidate *openbindings.Interface) []RoleConformanceReport {
	roleKeys := make([]string, 0, len(candidate.Roles))
	for k := range candidate.Roles {
		roleKeys = append(roleKeys, k)
	}
	sort.Strings(roleKeys)

	var reports []RoleConformanceReport
	for _, key := range roleKeys {
		location := candidate.Roles[key]
		rr := RoleConformanceReport{
			Role:     key,
			Location: location,
		}

		// Try to resolve the role interface.
		roleIface := resolveRoleInterface(location)
		if roleIface == nil {
			rr.Resolved = false
			rr.Error = "could not fetch or parse role interface"
			reports = append(reports, rr)
			continue
		}
		rr.Resolved = true

		// Use the existing compareOps logic: the role interface is the "target"
		// and our candidate is checked against it. We pass the role location
		// as the targetLocator so satisfies matching can resolve role keys.
		ops := compareOps(location, roleIface, candidate)

		compat, missing, incompat := countResults(ops)
		total := len(ops)
		matched := total - missing

		var conformance ConformanceLevel
		switch {
		case compat == total && total > 0:
			conformance = ConformanceFull
		case matched > 0 && incompat == 0:
			conformance = ConformancePartial
		default:
			conformance = ConformanceNone
		}

		rr.Operations = ops
		rr.Conformance = conformance
		rr.Coverage = &CoverageInfo{
			Matched:      matched,
			Compatible:   compat,
			Incompatible: incompat,
			Total:        total,
		}
		reports = append(reports, rr)
	}

	return reports
}

// resolveRoleInterface attempts to load a role interface from its location.
// It tries local file resolution first, then HTTP probing with a short timeout.
// Returns nil if the interface cannot be resolved.
func resolveRoleInterface(location string) *openbindings.Interface {
	// Try as a local file first (relative paths, absolute paths).
	iface, err := resolveInterface(location)
	if err == nil {
		return iface
	}

	// Try with probing (handles URLs, exec: refs).
	result := ProbeOBI(location, 5*time.Second)
	if result.Status == ProbeStatusOK && result.OBI != "" {
		parsed, parseErr := parseInterfaceJSON([]byte(result.OBI), location)
		if parseErr == nil {
			return parsed
		}
	}

	return nil
}

// Render returns a human-friendly representation of the validation report.
func (r ValidationReport) Render() string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Validation Report"))
	sb.WriteString("\n")
	sb.WriteString(s.Dim.Render("  locator: "))
	sb.WriteString(r.Locator)
	if r.Version != "" {
		sb.WriteString("\n")
		sb.WriteString(s.Dim.Render("  version: "))
		sb.WriteString(r.Version)
	}
	sb.WriteString("\n\n")

	if r.Error != nil {
		sb.WriteString(s.Error.Render("  ✗ Error: "))
		sb.WriteString(r.Error.Message)
		return sb.String()
	}

	if r.Valid && len(r.Problems) == 0 {
		sb.WriteString(s.Success.Render("  ✓ Valid"))
	} else if r.Valid {
		// Valid but with warnings (e.g., partial conformance, unreachable roles).
		sb.WriteString(s.Success.Render("  ✓ Valid"))
		sb.WriteString(fmt.Sprintf(" — %d %s",
			len(r.Problems), pluralize(len(r.Problems), "warning", "warnings")))
		for _, p := range r.Problems {
			sb.WriteString("\n    ")
			sb.WriteString(s.Warning.Render("• " + p))
		}
	} else {
		sb.WriteString(s.Error.Render("  ✗ Invalid"))
		sb.WriteString(fmt.Sprintf(" — %d %s",
			len(r.Problems), pluralize(len(r.Problems), "problem", "problems")))
		for _, p := range r.Problems {
			sb.WriteString("\n    ")
			sb.WriteString(s.Warning.Render("• " + p))
		}
	}

	// Render role conformance details.
	if len(r.RoleReports) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(s.Header.Render("Role Conformance"))
		for _, rr := range r.RoleReports {
			sb.WriteString("\n")
			if !rr.Resolved {
				sb.WriteString(s.Warning.Render(fmt.Sprintf("  ⚠ %s", rr.Role)))
				sb.WriteString(s.Dim.Render(" — could not resolve"))
				continue
			}

			switch rr.Conformance {
			case ConformanceFull:
				sb.WriteString(s.Success.Render(fmt.Sprintf("  ✓ %s", rr.Role)))
				sb.WriteString(s.Dim.Render(fmt.Sprintf(" — %d/%d operations", rr.Coverage.Compatible, rr.Coverage.Total)))
			case ConformancePartial:
				sb.WriteString(s.Warning.Render(fmt.Sprintf("  ◐ %s", rr.Role)))
				sb.WriteString(s.Dim.Render(fmt.Sprintf(" — %d/%d operations compatible", rr.Coverage.Compatible, rr.Coverage.Total)))
			default:
				sb.WriteString(s.Error.Render(fmt.Sprintf("  ✗ %s", rr.Role)))
				sb.WriteString(s.Dim.Render(fmt.Sprintf(" — %d/%d operations compatible", rr.Coverage.Compatible, rr.Coverage.Total)))
			}

			// Show per-operation details for non-full conformance.
			if rr.Conformance != ConformanceFull {
				for _, op := range rr.Operations {
					if !op.Matched {
						sb.WriteString(fmt.Sprintf("\n      %s %s",
							s.Error.Render("✗"), s.Key.Render(op.Operation)))
						sb.WriteString(s.Dim.Render(" — not satisfied"))
					} else if !op.Compatible {
						sb.WriteString(fmt.Sprintf("\n      %s %s",
							s.Error.Render("✗"), s.Key.Render(op.Operation)))
						for _, d := range op.Details {
							sb.WriteString(fmt.Sprintf("\n        %s", s.Warning.Render(d)))
						}
					}
				}
			}
		}
	}

	return sb.String()
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
