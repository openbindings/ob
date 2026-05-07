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

For each operation in the left contract, the report checks whether the right
implementation is compatible per the OpenBindings comparison convention:

  • Method input:  candidate must accept everything the target defines
  • Method output: candidate must only return what the target defines
  • Event payload: candidate must only emit what the target defines

JSON output uses the ob-comparison-report/v1 shape with operation deltas,
directional schema verdicts, finding kinds, summary counts, and coverage.

Exit code 0 if the summary verdict is compatible, 1 otherwise.

Examples:
  ob compat target.json candidate.json
  ob compat https://api.example.com exec:my-server
  ob compat target.json https://staging.example.com -F json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			report := app.ComparisonCheck(app.ComparisonInput{
				Left:        args[0],
				Right:       args[1],
				Mode:        "subsume",
				Profile:     "OB-2020-12",
				ProfileHash: "local",
			})

			exitCode := 0
			if report.Error != nil || report.Summary.Verdict != "compatible" {
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
