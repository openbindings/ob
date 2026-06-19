package cmd

import (
	"strconv"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegatePreferCmd() *cobra.Command {
	var capability, sourceFormat string

	cmd := &cobra.Command{
		Use:   "prefer <location> <preference>",
		Short: "Set a delegate's selection preference (higher = more preferred)",
		Long: `Set a delegate's selection preference. Higher is more preferred; the
baseline is 0 (where an unset delegate sits, alongside ob's own native
handling), and negatives rank a delegate below that baseline.

With no --capability, sets the delegate-level preference (the default for
all its offerings). With --capability (optionally scoped to a format via
--source-format), sets a per-offering override — this is how you route, say,
create to one delegate and invoke to another.

Examples:
  ob delegate prefer exec:acme 5
  ob delegate prefer exec:acme 10 --capability create
  ob delegate prefer exec:acme 10 --capability invoke --source-format grpc`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pref, err := strconv.ParseFloat(strings.TrimSpace(args[1]), 64)
			if err != nil {
				return app.ExitResult{Code: 2, Message: "preference must be a number", ToStderr: true}
			}
			result, err := app.SetDelegatePreference(app.SetDelegatePreferenceInput{
				Location:   args[0],
				Preference: pref,
				Capability: app.DelegateCapability(strings.ToLower(strings.TrimSpace(capability))),
				Format:     sourceFormat,
			})
			if err != nil {
				return err
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&capability, "capability", "", "scope to a capability: invoke, create, or inspect")
	cmd.Flags().StringVar(&sourceFormat, "source-format", "", "scope to a binding-source format (requires --capability)")

	return cmd
}
