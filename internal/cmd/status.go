package cmd

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [obi-path]",
		Short: "Show environment status or OBI sync report",
		Long: `Show current environment status.

If an OBI file path is provided, shows a per-source sync report with
managed vs hand-authored breakdowns. Without arguments, shows environment
info.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, outputPath := getOutputFlags(cmd)

			if len(args) == 1 {
				result, err := app.OBIStatus(app.OBIStatusInput{OBIPath: args[0]})
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				return app.OutputResult(result, format, outputPath)
			}

			status, err := app.GetEnvironmentStatus()
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			return app.OutputResultText(status, format, outputPath, func() string {
				var sb strings.Builder
				fmt.Fprintf(&sb, "Environment: %s (%s)\n", status.EnvironmentType, status.EnvironmentPath)
				fmt.Fprintf(&sb, "Delegates: %d", status.DelegateCount)
				return sb.String()
			})
		},
	}
	return cmd
}
