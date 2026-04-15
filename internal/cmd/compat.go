package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newCompatCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "compat <target> <candidate>",
		Short: "Check interface conformance between two interfaces",
		Long: `Compare a candidate OpenBindings interface against a target interface and
produce a conformance report.

Each argument is a locator: a local file path, HTTP(S) URL, or exec: reference.

For each operation in the target, the report checks whether the candidate's
schemas are compatible per the OpenBindings Schema Compatibility Profile (v0.1):

  • Method input:  candidate must accept everything the target defines
  • Method output: candidate must only return what the target defines
  • Event payload: candidate must only emit what the target defines

Per-slot status is one of: compatible, incompatible, or unspecified.

The report includes a conformance level:

  • full:    all target operations matched and compatible
  • partial: some operations matched and compatible, others unmatched
  • none:    no compatible operations (all unmatched or incompatible)

Exit code 0 if all operations are compatible (full conformance), 1 otherwise.

Examples:
  ob compat target.json candidate.json
  ob compat https://api.example.com exec:my-server
  ob compat target.json https://staging.example.com -F json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			report := app.CompatibilityCheck(app.CompatInput{
				Target:    args[0],
				Candidate: args[1],
			})

			exitCode := 0
			if report.Error != nil || !report.Compatible {
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

	return cmd
}
