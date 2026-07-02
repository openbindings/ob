package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List delegates",
		Long: `List every registered delegate — the builtin self-delegate included —
with the operations each carried when last resolved, the capabilities and
formats ob derived for routing, and its selection preferences.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := app.ListDelegates()
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(output, format, outputPath)
		},
	}
	return c
}
