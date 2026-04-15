package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	var (
		fromSources bool
		onlySource  string
		quiet       bool
	)

	cmd := &cobra.Command{
		Use:   "diff <baseline> [comparison]",
		Short: "Structural comparison of two OBIs",
		Long: `Compare two OpenBindings interfaces for structural differences.

Two modes:

  ob diff <baseline> <comparison>
    Compare any two OBIs directly.

  ob diff <obi> --from-sources
    Compare an OBI against what its binding sources currently produce.
    Use --only <key> to scope to a specific source.

Operations are compared for structural equality: schemas are normalized
before comparison, so semantically equivalent schemas (e.g., with
different key ordering) are treated as identical.

Exit codes:
  0  Identical (no differences)
  1  Differences found
  2  Error`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateSourceModeArgs(args, fromSources, onlySource); err != nil {
				return err
			}

			input := app.DiffInput{
				BaselineLocator: args[0],
				FromSources:     fromSources,
				OnlySource:      onlySource,
			}
			if len(args) == 2 {
				input.ComparisonLocator = args[1]
			}

			result, err := app.Diff(input)
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}

			if quiet {
				if result.Identical {
					return nil
				}
				return app.ExitResult{Code: 1, Message: "", ToStderr: false}
			}

			// Exit code 1 if differences found.
			exitCode := 0
			if !result.Identical {
				exitCode = 1
			}

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultWithCode(result, format, outputPath, exitCode)
		},
	}

	cmd.Flags().BoolVar(&fromSources, "from-sources", false, "compare against what binding sources produce")
	cmd.Flags().StringVar(&onlySource, "only", "", "scope to a specific source key (requires --from-sources)")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "exit code only, no output")

	return cmd
}
