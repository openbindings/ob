package cmd

import (
	"encoding/json"
	"fmt"

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
  ob delegate resolve openbindings.key-value-store.get -F json`,
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

func newDelegateResolveFormatCmd() *cobra.Command {
	var inputJSON string

	c := &cobra.Command{
		Use:   "resolve-format [format-token]",
		Short: "Show which delegate ob's routing would select for a format",
		Long: `Report which delegate ob's routing would select for a binding format
token, and the capabilities it offers for it — ob's format-narrowing
diagnostic, layered on top of the operation-keyed resolve.

Machine callers pass the operation's wire input wholesale instead: --input
takes a ResolveDelegateForFormatInput ({"format": ...}) as a JSON string,
exclusive with the <format> argument. (The wire input's format field cannot
ride the flat field mapping — it collides with the root --format flag.)

Examples:
  ob delegate resolve-format usage@2.0.0
  ob delegate resolve-format openapi@3.1.0 -o result.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			formatToken := ""
			switch {
			case inputJSON != "":
				if len(args) > 0 {
					return app.ExitResult{Code: 2, Message: "--input is exclusive with the <format> argument", ToStderr: true}
				}
				var wire struct {
					Format string `json:"format"`
				}
				if err := json.Unmarshal([]byte(inputJSON), &wire); err != nil {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("parse --input: %v", err), ToStderr: true}
				}
				if wire.Format == "" {
					return app.ExitResult{Code: 2, Message: "--input: format is required", ToStderr: true}
				}
				formatToken = wire.Format
			case len(args) == 1:
				formatToken = args[0]
			default:
				return app.ExitResult{Code: 2, Message: "provide a <format> argument or --input", ToStderr: true}
			}

			result, err := app.ResolveDelegateForFormat(formatToken)
			if err != nil {
				return err
			}
			format, outputPath := getOutputFlags(cmd)
			if inputJSON != "" && format == "" {
				// Machine lane: wire input in, wire-shaped output out.
				format = "json"
			}
			return app.OutputResultText(result, format, outputPath, func() string {
				return result.Render()
			})
		},
	}
	c.Flags().StringVar(&inputJSON, "input", "", "ResolveDelegateForFormatInput as a JSON string (machine lane)")
	return c
}
