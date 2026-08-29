package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateResolveCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "resolve <operation>",
		Short: "Resolve an operation to the delegates that carry it",
		Long: `Resolve an operation to the registered delegates that carry it — those
whose interface answers to the operation's key or an alias — ordered by
effective preference, best first. Resolution returns candidates only; what
to do with them (route to one, aggregate, narrow further) stays with the
caller. An empty list is an answer, not an error.

Examples:
  ob delegate resolve openbindings.binding-invoker.invokeBinding
  ob delegate resolve openbindings.document-store.get -F json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.ResolveDelegate(args[0])
			if err != nil {
				return err
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	return c
}

func newDelegateResolveBindingSpecCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "resolve-binding-spec <binding-spec>",
		Short: "Show which delegate ob's routing would select for a binding specification",
		Long: `Report which delegate ob's routing would select for a binding
specification (by exact identifier), and the capabilities it offers for
it — ob's spec-narrowing diagnostic, layered on top of the operation-keyed
resolve.

Examples:
  ob delegate resolve-binding-spec openbindings.usage@1
  ob delegate resolve-binding-spec openbindings.openapi-3.1@1 -o result.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.ResolveDelegateForBindingSpec(args[0])
			if err != nil {
				return err
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(result, format, outputPath, func() string {
				return result.Render()
			})
		},
	}
	return c
}
