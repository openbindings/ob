package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newBindingInvokeCmd() *cobra.Command {
	var inputJSON string

	c := &cobra.Command{
		Use:   "invoke",
		Short: "Invoke a resolved binding (machine-to-machine)",
		Long: `Invoke a resolved binding (machine-facing).

Reads BindingInvocationInput from the --input flag (JSON string), invokes the
binding using ob's native format support and available delegates (excluding
itself to prevent recursion), and writes the result envelope ({"output": ...}
or {"error": ...}) as one JSON line to stdout. Always JSON — this is the
exec-lane realization of the binding-invoker contract, not a human view.

This command satisfies the invokeBinding operation from the
binding-invoker interface.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputJSON == "" {
				return app.ExitResult{Code: 1, Message: "--input is required", ToStderr: true}
			}

			var input app.InvokeOperationInput
			if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
				return writeInvokeOutput(app.InvokeOperationOutput{
					Error: &app.Error{Code: "invalid_input", Message: fmt.Sprintf("failed to parse input JSON: %v", err)},
				})
			}

			result := app.InvokeOperationWithContext(context.Background(), input)
			return writeInvokeOutput(result)
		},
	}

	c.Flags().StringVar(&inputJSON, "input", "", "BindingInvocationInput as a JSON string")

	return c
}

func writeInvokeOutput(output app.InvokeOperationOutput) error {
	b, err := json.Marshal(output)
	if err != nil {
		return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to marshal output: %v", err), ToStderr: true}
	}
	fmt.Println(string(b))
	if output.Error != nil {
		return app.ExitResult{Code: 1}
	}
	return nil
}
