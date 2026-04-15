package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var (
		strict    bool
		quiet     bool
		skipRoles bool
	)

	cmd := &cobra.Command{
		Use:   "validate <locator>",
		Short: "Validate an OpenBindings interface document",
		Long: `Validate an OpenBindings interface document against the spec.

The locator may be a local file path, HTTP(S) URL, or exec: reference.

Checks structural correctness: required fields, operation kinds, alias
uniqueness, binding source references, transform validity, and more.

By default, also checks role conformance: for each declared role, fetches
the role interface and verifies that satisfies declarations are correct
(schema compatibility). Use --skip-roles to disable this check.

With --strict, additionally rejects unknown (non-x-) fields and requires
a supported OpenBindings version.

Exit code 0 if valid, 1 if invalid or an error occurred.

Examples:
  ob validate interface.json
  ob validate https://api.example.com
  ob validate exec:my-server
  ob validate interface.json --strict
  ob validate interface.json --skip-roles
  ob validate interface.json -F json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			report := app.ValidateInterface(app.ValidateInput{
				Locator:   args[0],
				Strict:    strict,
				SkipRoles: skipRoles,
			})

			exitCode := 0
			if report.Error != nil || !report.Valid {
				exitCode = 1
			}

			format, outputPath := getOutputFlags(cmd)
			if quiet {
				format = "quiet"
			}
			return app.OutputResultWithCode(report, format, outputPath, exitCode)
		},
	}

	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress output, exit code only")
	cmd.Flags().BoolVar(&strict, "strict", false, "reject unknown fields and require supported version")
	cmd.Flags().BoolVar(&skipRoles, "skip-roles", false, "skip role conformance checking")

	return cmd
}
