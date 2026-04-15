package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateResolveCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "resolve <format>",
		Short: "Show which delegate handles a format",
		Long: `Show which delegate would handle a given binding format based on preferences and available delegates.

Resolves against binding format delegates.

Examples:
  ob delegate resolve usage@2.0.0
  ob delegate resolve openapi@3.1.0 -o result.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.DelegateResolve(args[0])
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(result, format, outputPath, func() string {
				return result.Render()
			})
		},
	}
	return c
}
