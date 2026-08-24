package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newBindingSpecsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "binding-specs",
		Aliases: []string{"binding-spec"},
		Short:   "List or check binding specifications",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the binding specifications this ob instance can name",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			specs := app.ListBindingSpecs()
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(specs, format, outputPath, func() string {
				return app.RenderBindingSpecList(specs)
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "check [binding-spec]...",
		Short: "Authoritatively check exact binding-specification identifiers",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			verdicts := app.CheckBindingSpecs(args)
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(verdicts, format, outputPath, func() string {
				return app.RenderBindingSpecVerdicts(verdicts)
			})
		},
	})
	return cmd
}
