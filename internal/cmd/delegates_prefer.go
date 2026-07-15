package cmd

import (
	"strconv"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegatePreferCmd() *cobra.Command {
	var operation, capability, sourceFormat string
	var clear bool

	cmd := &cobra.Command{
		Use:   "prefer <location> [preference]",
		Short: "Set or clear a delegate's selection preference (higher = more preferred)",
		Long: `Set or clear a delegate's selection preference. Higher is more preferred;
the baseline is 0 (where an unset delegate sits, alongside ob's own native
handling), and negatives rank a delegate below that baseline.

With no scope, sets the delegate-level preference (the default for every
operation it carries). With --operation (or the --capability shorthand for
ob's three format needs), sets that operation's entry in the delegate's
preference index; --binding-spec additionally scopes an operation entry to
one binding-source format. --clear removes the targeted entry instead.

Preference orders the candidates 'ob delegate resolve' returns; which
candidate is used stays with the caller.

Examples:
  ob delegate prefer exec:acme 5
  ob delegate prefer exec:acme 10 --capability synthesize
  ob delegate prefer exec:acme 10 --operation openbindings.key-value-store.get
  ob delegate prefer exec:acme 10 --capability invoke --binding-spec openbindings.grpc@1
  ob delegate prefer exec:acme --clear --capability synthesize`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var pref *float64
			switch {
			case clear && len(args) == 2:
				return app.ExitResult{Code: 2, Message: "--clear takes no preference value", ToStderr: true}
			case clear:
				// pref stays nil: clear the targeted entry
			case len(args) < 2:
				return app.ExitResult{Code: 2, Message: "provide a preference value, or pass --clear", ToStderr: true}
			default:
				v, err := strconv.ParseFloat(strings.TrimSpace(args[1]), 64)
				if err != nil {
					return app.ExitResult{Code: 2, Message: "preference must be a number", ToStderr: true}
				}
				pref = &v
			}

			op := strings.TrimSpace(operation)
			if capability != "" {
				if op != "" {
					return app.ExitResult{Code: 2, Message: "use --operation or --capability, not both", ToStderr: true}
				}
				var ok bool
				op, ok = app.CapabilityOperation(app.DelegateCapability(strings.ToLower(strings.TrimSpace(capability))))
				if !ok {
					return app.ExitResult{Code: 2, Message: "capability must be invoke, synthesize, or inspect", ToStderr: true}
				}
			}

			result, err := app.SetDelegatePreference(app.SetDelegatePreferenceInput{
				Location:    args[0],
				Preference:  pref,
				Operation:   op,
				BindingSpec: sourceFormat,
			})
			if err != nil {
				return err
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&operation, "operation", "", "scope to one operation identifier")
	cmd.Flags().StringVar(&capability, "capability", "", "shorthand for the operation of an ob capability: invoke, synthesize, or inspect")
	cmd.Flags().StringVar(&sourceFormat, "binding-spec", "", "scope an operation entry to one binding specification (requires --operation or --capability)")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the targeted preference entry instead of setting it")

	return cmd
}
