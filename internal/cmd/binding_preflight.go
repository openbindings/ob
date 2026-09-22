package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openbindings/openbindings-go/jsonvalue"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newBindingPreflightCmd() *cobra.Command {
	var inputJSON string

	c := &cobra.Command{
		Use:   "preflight",
		Short: "Preflight a resolved binding (machine-to-machine)",
		Long: `Preflight a resolved binding (machine-facing).

Reads BindingInvocationInput from the --input flag (JSON string), tells the
binding that an invocation of that selection may follow, and reports the
context requirements it can already identify. Writes ContextRequiredDetails
as JSON to stdout, or null when the binding knows of none. The answer is
advisory (the live CONTEXT_REQUIRED challenge is authoritative) and never
dispatches the requested operation.

This command corresponds to the preflightBinding operation from the
binding-invoker interface, and is the binding-level twin of operation preflight.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputJSON == "" {
				return app.ExitResult{Code: 1, Message: "--input is required", ToStderr: true}
			}

			var input app.InvocationInput
			if err := jsonvalue.Unmarshal([]byte(inputJSON), &input); err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to parse input JSON: %v", err), ToStderr: true}
			}

			details, err := app.PreflightBinding(context.Background(), input)
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
