package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newConflictsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conflicts <obi-path>",
		Short: "List merge conflicts between local edits and source changes",
		Long: `Show all field-level merge conflicts in an OBI.

A conflict occurs when both you and the binding source have changed the
same field on a managed operation or binding since the last sync.

Conflicts are resolved by:
  ob sync <obi> --force --op <key>   Accept the source value
  ob sync <obi>                      Keep local values (default behavior)

Or edit the OBI directly and run ob sync to update the baseline.

Examples:
  ob conflicts interface.json
  ob conflicts interface.json -F json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.Conflicts(app.ConflictsInput{
				OBIPath: args[0],
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	return cmd
}
