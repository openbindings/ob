package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newFormatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "format",
		Aliases: []string{"formats"},
		Short:   "List format tokens this ob instance can handle",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			formats := app.ListFormats()
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(formats, format, outputPath, func() string {
				return app.RenderFormatList(formats)
			})
		},
	}
	return cmd
}
