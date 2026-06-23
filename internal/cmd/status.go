package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var exitCode bool

	cmd := &cobra.Command{
		Use:   "status <obi-path>",
		Short: "Report an OBI's drift against its sources (read-only)",
		Long: `Report an OpenBindings interface's status against its registered sources:
a per-source drift report (operations and bindings the sources would add,
update, or remove, plus custodial drift on hand-authored bindings whose
target is gone) with managed vs hand-authored breakdowns.

This is read-only — it never modifies the OBI. It is the preview of what
'ob source pull' would apply.

Use --exit-code to exit non-zero when any source has drift (a CI gate).

For the active environment (local vs global, delegate and context counts),
use 'ob environment' (alias 'ob env').

Examples:
  ob status interface.json
  ob status interface.json --exit-code`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, outputPath := getOutputFlags(cmd)
			result, err := app.OBIStatus(app.OBIStatusInput{OBIPath: args[0]})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			code := 0
			if exitCode && result.HasDrift() {
				code = 1
			}
			return app.OutputResultWithCode(result, format, outputPath, code)
		},
	}

	cmd.Flags().BoolVar(&exitCode, "exit-code", false, "exit non-zero if any source has drift (CI gate)")
	return cmd
}
