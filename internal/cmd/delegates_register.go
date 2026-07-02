package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateRegisterCmd() *cobra.Command {
	var preference float64

	c := &cobra.Command{
		Use:     "register <location>",
		Aliases: []string{"add"},
		Short:   "Register a delegate",
		Long: `Register a delegate — any referenceable OpenBindings interface ob may
route operations to.

ob resolves the location to the delegate's interface and records a
snapshot: the operations it carries, a content digest pinning the resolved
document, and the capabilities and formats ob derives for routing.
Registration fails when the location cannot be resolved — a delegate is
its interface. Re-registering a location refreshes the snapshot and
preserves your preferences.

Examples:
  ob delegate register exec:my-cli
  ob delegate register https://api.example.com --preference 5
  ob delegate register ./local-tool`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var pref *float64
			if cmd.Flags().Changed("preference") {
				pref = &preference
			}
			result, err := app.RegisterDelegate(args[0], pref)
			if err != nil {
				return err
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	c.Flags().Float64Var(&preference, "preference", 0, "initial delegate-level selection preference (higher = more preferred)")
	return c
}
