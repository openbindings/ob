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
		Use:   "invoke [obi-path] [binding-key]",
		Short: "Invoke a binding directly, below the operation layer",
		Long: `Invoke a binding directly, below the operation layer.

With an OBI path and a binding key, resolves the named binding from the
document (embedded content or location) and drives it. Because this is not
an operation invocation, the operation-level input/output checks (spec
OBI-T-07/T-08) do not apply here. The binding's own declared inputTransform
still runs — it is part of the binding itself, not of operation validation.
The output is exactly what the source returned: decoded, but NOT reshaped by
the operation's output transform and NOT validated. This is how you read
what a drifted service actually returns while 'ob operation invoke' correctly
refuses it. --input carries the binding's input value.

With no positional arguments (machine-to-machine), --input carries a
BindingInvocationInput envelope wholesale; the result envelope
({"output": ...} or {"error": ...}) is written as one JSON line to stdout.
This machine lane corresponds to the invokeBinding operation from the
binding-invoker interface.

Examples:
  ob binding invoke orders.obi.json listOrders.api
  ob binding invoke orders.obi.json listOrders.api --input '{"limit": 10}'
  ob binding invoke --input '{"source":{"bindingSpec":"openbindings.openapi@4",...},"ref":"#/paths/~1orders/get"}'`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 && len(args) != 2 {
				return fmt.Errorf("accepts an OBI path and a binding key, or no arguments with a machine-lane --input envelope")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Document-addressed wire lane: <obi-path> <binding-key>.
			if len(args) == 2 {
				var value any
				if inputJSON != "" {
					if err := json.Unmarshal([]byte(inputJSON), &value); err != nil {
						return app.ExitResult{Code: 2, Message: fmt.Sprintf("parse --input: %v", err), ToStderr: true}
					}
				}
				input, err := app.ResolveBindingInvocation(args[0], args[1], value)
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				result := app.InvokeOperationWithContext(context.Background(), input)
				if result.Error != nil {
					return app.ExitResult{Code: 1, Message: result.Error.Message, ToStderr: true}
				}
				b, merr := json.Marshal(result.Output)
				if merr != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("marshal output: %v", merr), ToStderr: true}
				}
				format, outputPath := getOutputFlags(cmd)
				return app.OutputResultWithCode(json.RawMessage(b), format, outputPath, 0)
			}

			// Machine lane: the wholesale envelope.
			if inputJSON == "" {
				return app.ExitResult{Code: 1, Message: "--input is required (or pass <obi-path> <binding-key>)", ToStderr: true}
			}
			var input app.InvocationInput
			if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
				return writeInvokeOutput(app.InvocationResult{
					Error: &app.Error{Code: "invalid_input", Message: fmt.Sprintf("failed to parse input JSON: %v", err)},
				})
			}
			result := app.InvokeOperationWithContext(context.Background(), input)
			return writeInvokeOutput(result)
		},
	}

	c.Flags().StringVar(&inputJSON, "input", "", "binding input value (with positional args), or a BindingInvocationInput envelope (machine lane)")

	return c
}

func writeInvokeOutput(output app.InvocationResult) error {
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
