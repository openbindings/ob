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
		Long:    "List delegates registered in the environment with their supported formats.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := app.BuildDelegateListOutput(app.DelegateListParams{})
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(output, format, outputPath)
		},
	}
	return c
}
