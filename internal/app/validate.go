package app

import (
	"fmt"
	"strings"

	"github.com/openbindings/openbindings-go"
)

// ValidateInput specifies the interface to validate. The served operation
// supplies it inline as Interface (per the contract's ValidateInterfaceInput);
// the CLI supplies a Locator (file path, HTTP(S) URL, or exec: reference) that
// is resolved to the document. Interface takes precedence when set.
type ValidateInput struct {
	Locator   string
	Interface *openbindings.Interface
	Strict    bool
}

// ValidationReport is the result of validating an OpenBindings interface.
type ValidationReport struct {
	Locator  string   `json:"locator,omitempty"`
	Valid    bool     `json:"valid"`
	Version  string   `json:"version,omitempty"`
	Problems []string `json:"problems,omitempty"`
	Error    *Error   `json:"error,omitempty"`
}

// ValidateInterface loads and validates an OpenBindings interface document.
func ValidateInterface(input ValidateInput) ValidationReport {
	iface := input.Interface
	var err error
	if iface == nil {
		iface, err = resolveInterface(input.Locator)
		if err != nil {
			return ValidationReport{
				Locator: input.Locator,
				Error: &Error{
					Code:    "resolve_error",
					Message: err.Error(),
				},
			}
		}
	}

	var opts []openbindings.ValidateOption
	if input.Strict {
		opts = append(opts,
			openbindings.WithRejectUnknownTypedFields(),
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

	return ValidationReport{
		Locator: input.Locator,
		Valid:   true,
		Version: iface.OpenBindings,
	}
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
		sb.WriteString(s.Success.Render("  ✓ No rule violations found"))
	} else if r.Valid {
		// Valid but with warnings.
		sb.WriteString(s.Success.Render("  ✓ No rule violations found"))
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

	return sb.String()
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
