package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateUnregisterCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "unregister <location>",
		Aliases: []string{"remove", "rm"},
		Short:   "Unregister a delegate",
		Long: `Remove a registered delegate by its location.

Idempotent: unregistering a location that is not registered succeeds.

Examples:
  ob delegate unregister exec:my-cli
  ob delegate rm https://api.example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removed, err := app.UnregisterDelegate(args[0])
			if err != nil {
				return err
			}
			// The contract's output is null; the text rendering is the human view.
			text := "Unregistered delegate " + args[0]
			if !removed {
				text = "Delegate " + args[0] + " was not registered (no-op)"
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(nil, format, outputPath, func() string {
				return text
			})
		},
	}
	return c
}
