package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newEnvironmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "environment",
		Aliases: []string{"env"},
		Short:   "Show the active OpenBindings environment",
		Long: `Show the active OpenBindings environment: whether it is local or global,
its path, the number of delegates it holds, and the number of contexts in
the user's context store (contexts are user-scoped, not per-environment).

For an OBI file's drift against its sources, use 'ob status <obi-path>'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, outputPath := getOutputFlags(cmd)

			status, err := app.GetEnvironmentStatus()
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			return app.OutputResultText(status, format, outputPath, status.Render)
		},
	}

	return cmd
}
