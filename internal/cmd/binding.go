package cmd

import (
	"github.com/spf13/cobra"

	"github.com/openbindings/ob/internal/app"
)

func newBindingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "binding",
		Short: "Low-level binding operations",
		Long: `Low-level binding operations.

'binding invoke' and 'binding prepare' operate on pre-resolved bindings — the
machine-to-machine building blocks used by delegates and orchestrators.
'binding list' reads the bindings an OBI declares, so what 'operation
bind'/'unbind' produced is visible without opening the raw JSON.`,
	}
	markCommandGroup(c)

	c.AddCommand(
		newBindingInvokeCmd(),
		newBindingPrepareCmd(),
		newBindingListCmd(),
	)

	return c
}

func newBindingListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list <obi-path> [operation]",
		Aliases: []string{"ls"},
		Short:   "List the bindings an OBI declares (optionally for one operation)",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opFilter := ""
			if len(args) == 2 {
				opFilter = args[1]
			}
			result, err := app.BindingList(args[0], opFilter)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	return cmd
}
