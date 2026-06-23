package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDescribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "Show ob identity and metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := app.Info()
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(info, format, outputPath, func() string {
				return app.RenderObInfo(info)
			})
		},
	}
	return cmd
}
