package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newBindingSpecsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "binding-specs",
		Aliases: []string{"binding-spec"},
		Short:   "List the binding specifications this ob instance can handle",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			specs := app.ListBindingSpecs()
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(specs, format, outputPath, func() string {
				return app.RenderBindingSpecList(specs)
			})
		},
	}
	return cmd
}
