package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateAddCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "add <url>",
		Short: "Add a delegate",
		Long: `Register a delegate URL in the environment.

ob probes delegates to discover which interface contracts they implement.

Examples:
  ob delegate add exec:my-cli
  ob delegate add https://api.example.com
  ob delegate add exec:./local-tool -o result.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.DelegateAdd(args[0])
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
