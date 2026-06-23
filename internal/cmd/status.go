package cmd

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show environment status",
		Long: `Show the active OpenBindings environment: whether it is local or global,
its path, and counts of the delegates and contexts it holds.

For an OBI file's drift against its sources, use 'ob source status <obi-path>'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, outputPath := getOutputFlags(cmd)

			status, err := app.GetEnvironmentStatus()
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			return app.OutputResultText(status, format, outputPath, func() string {
				var sb strings.Builder
				fmt.Fprintf(&sb, "Environment: %s (%s)\n", status.EnvironmentType, status.EnvironmentPath)
				fmt.Fprintf(&sb, "Delegates: %d\n", status.DelegateCount)
				fmt.Fprintf(&sb, "Contexts: %d", status.ContextCount)
				return sb.String()
			})
		},
	}

	return cmd
}
