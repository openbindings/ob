package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize an OpenBindings environment",
		Long: `Initialize an OpenBindings environment.

By default, creates a .openbindings/ directory in the current directory.
Use --global to initialize the global environment at ~/.config/openbindings/ instead.

Examples:
  ob init
  ob init --global
  ob init -F json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.Init(global)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(result, format, outputPath, func() string {
				return result.Render()
			})
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Initialize the global environment (~/.config/openbindings/)")
	return cmd
}
