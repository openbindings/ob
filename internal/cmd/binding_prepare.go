package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newBindingPrepareCmd() *cobra.Command {
	var inputJSON string

	c := &cobra.Command{
		Use:   "prepare",
		Short: "Preflight a resolved binding (machine-to-machine)",
		Long: `Preflight a resolved binding (machine-facing).

Reads BindingInvocationInput from the --input flag (JSON string) and reports the
context the binding would require before it can be invoked, without invoking it.
Writes ContextRequiredDetails as JSON to stdout, or null when no context is
required.

This command corresponds to the prepareBinding operation from the
binding-invoker interface, and is the binding-level twin of operation prepare.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputJSON == "" {
				return app.ExitResult{Code: 1, Message: "--input is required", ToStderr: true}
			}

			var input app.InvocationInput
			if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to parse input JSON: %v", err), ToStderr: true}
			}

			details, err := app.PrepareBinding(context.Background(), input)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			b, err := json.Marshal(details)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to marshal output: %v", err), ToStderr: true}
			}
			fmt.Println(string(b))
			return nil
		},
	}

	c.Flags().StringVar(&inputJSON, "input", "", "BindingInvocationInput as a JSON string")

	return c
}
