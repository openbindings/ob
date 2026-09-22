package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "validate <locator>",
		Short: "Validate an OpenBindings interface document and report its conformance conclusion",
		Long: `Validate an OpenBindings interface document against the core
specification's document rules and report its conformance conclusion.

The locator may be a local file path, HTTP(S) URL, or exec: reference.

The conclusion is one of:
  conformant                every rule was checked and none is violated
  non-conformant            at least one violation was established
  conformance undetermined  no violation, but some rules are inconclusive

A rule is inconclusive when deciding it needs knowledge the core does not
carry, such as whether each binding identifies its target, which only its
binding specification decides. Unknown fields are ignored and reported as
diagnostics (OBI-T-02).

A document declaring a version this build does not support is refused
rather than validated (OBI-T-04).

Exit code 1 if the document is non-conformant, refused, or cannot be
resolved; 0 otherwise.

Examples:
  ob validate interface.json
  ob validate https://api.example.com
  ob validate exec:my-server
  ob validate interface.json -F json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			report := app.ValidateInterface(app.ValidateInput{Locator: args[0]})

			exitCode := 0
			if report.Failed() {
				exitCode = 1
			}

			format, outputPath := getOutputFlags(cmd)
			if quiet {
				format = "quiet"
			}
			return app.OutputResultWithCode(report, format, outputPath, exitCode)
		},
	}

	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "exit code only, no output")

	return cmd
}
