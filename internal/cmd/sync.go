package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var (
		force bool
		pure  bool
		ops   []string
	)

	cmd := &cobra.Command{
		Use:   "sync <obi-path> [source-key...]",
		Short: "Sync sources in an OBI from their x-ob references",
		Long: `Re-read binding sources and three-way merge into the OBI.

For each managed source, sync re-derives operations and bindings, then
performs a field-level three-way merge against the OBI. Local edits are
preserved by default; conflicting fields keep your local value and are
reported.

Use --force to prefer source values for all fields, overwriting local edits.

Sources without x-ob metadata (hand-authored) are skipped.

Scoping:
  Positional args scope by source key.
  --op scopes by operation key (and related bindings).
  Both can be combined.

The --pure flag strips all x-ob metadata from the output, producing a clean
spec-only OBI suitable for publishing. Requires -o to prevent accidental
in-place metadata loss.

Examples:
  ob sync interface.json                         # sync all sources
  ob sync interface.json usage                    # sync one source
  ob sync interface.json --force                 # overwrite local edits
  ob sync interface.json --force --op hello      # force-sync one operation
  ob sync interface.json -o dist/interface.json  # sync and write elsewhere
  ob sync interface.json -o pub.json --pure      # sync, strip x-ob, write`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, outputPath := getOutputFlags(cmd)

			if pure && outputPath == "" {
				return app.ExitResult{
					Code:     2,
					Message:  "--pure requires -o <path> to prevent accidental in-place metadata loss",
					ToStderr: true,
				}
			}

			result, err := app.Sync(app.SyncInput{
				OBIPath:       args[0],
				SourceKeys:    args[1:],
				OperationKeys: ops,
				Force:         force,
				Pure:          pure,
				OutputPath:    outputPath,
				Format:        format,
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			// Sync already wrote the OBI file (to outputPath or in-place).
			// Display the summary without re-writing.
			return app.OutputResultText(result, format, "", func() string {
				return result.Render()
			})
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "prefer source for all conflicts (overwrite local edits)")
	cmd.Flags().BoolVar(&pure, "pure", false, "strip all x-ob metadata from output (requires -o)")
	cmd.Flags().StringSliceVar(&ops, "op", nil, "sync only specific operations and their bindings (repeatable)")

	return cmd
}
