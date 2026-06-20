package cmd

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var exitCode bool

	cmd := &cobra.Command{
		Use:   "status [obi-path]",
		Short: "Show environment status or OBI drift report",
		Long: `Show current environment status.

If an OBI file path is provided, shows a per-source drift report
(operations/bindings the sources would add, update, or remove, plus
custodial drift on hand-authored bindings whose target is gone) with
managed vs hand-authored breakdowns. This is read-only — it never
modifies the OBI. Without arguments, shows environment info.

Use --exit-code to exit non-zero when any source has drift (a CI gate).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, outputPath := getOutputFlags(cmd)

			if len(args) == 1 {
				result, err := app.OBIStatus(app.OBIStatusInput{OBIPath: args[0]})
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				code := 0
				if exitCode && result.HasDrift() {
					code = 1
				}
				return app.OutputResultWithCode(result, format, outputPath, code)
			}

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

	cmd.Flags().BoolVar(&exitCode, "exit-code", false, "exit non-zero if any source has drift (CI gate)")
	return cmd
}
