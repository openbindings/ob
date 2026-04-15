package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newBindingExecCmd() *cobra.Command {
	var inputJSON string

	c := &cobra.Command{
		Use:   "exec",
		Short: "Execute a resolved binding",
		Long: `Execute a resolved binding (machine-facing).

Reads ExecuteBindingInput from the --input flag (JSON string), executes the
binding using ob's native format support and available delegates (excluding
itself to prevent recursion), and writes ExecuteBindingOutput as JSON to stdout.

This command satisfies the executeBinding operation from the
openbindings.binding-executor interface.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputJSON == "" {
				return app.ExitResult{Code: 1, Message: "--input is required", ToStderr: true}
			}

			var input app.ExecuteOperationInput
			if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
				return writeExecOutput(app.ExecuteOperationOutput{
					Error: &app.Error{Code: "invalid_input", Message: fmt.Sprintf("failed to parse input JSON: %v", err)},
				})
			}

			result := app.ExecuteOperationWithContext(context.Background(), input)
			return writeExecOutput(result)
		},
	}

	c.Flags().StringVar(&inputJSON, "input", "", "ExecuteBindingInput as a JSON string")

	return c
}

func writeExecOutput(output app.ExecuteOperationOutput) error {
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
