package cmd

import (
	"fmt"
	"os"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/codegen"
	"github.com/spf13/cobra"
)

func newCodegenCmd() *cobra.Command {
	var (
		lang        string
		output      string
		packageName string
	)

	cmd := &cobra.Command{
		Use:   "codegen <source>",
		Short: "Generate a typed client from an OpenBindings interface",
		Long: `Generate typed, transport-agnostic client code from an OBI.

The source may be a local file path or HTTP(S) URL. If the source is not
a native OBI, synthesis from supported formats (OpenAPI, AsyncAPI, etc.)
is attempted automatically.

Supported languages: typescript, go

Examples:
  ob codegen interface.json --lang typescript -o ./src/generated/client.ts
  ob codegen interface.json --lang go -o ./generated/client.go --package myapi
  ob codegen https://api.example.com --lang typescript`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if lang == "" {
				return app.ExitResult{Code: 2, Message: "--lang is required (typescript or go)", ToStderr: true}
			}
			if lang != "typescript" && lang != "ts" && lang != "go" && lang != "golang" {
				return app.ExitResult{Code: 2, Message: fmt.Sprintf("unsupported language %q (valid: typescript, go)", lang), ToStderr: true}
			}

			// Normalize language aliases.
			if lang == "ts" {
				lang = "typescript"
			}
			if lang == "golang" {
				lang = "go"
			}

			// Load the interface.
			iface, err := app.ResolveInterface(args[0])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to load interface: %v", err), ToStderr: true}
			}

			// Generate IR.
			result, err := codegen.Generate(iface)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("codegen failed: %v", err), ToStderr: true}
			}

			// Emit code.
			var code string
			switch lang {
			case "typescript":
				code = codegen.EmitTypeScript(result)
			case "go":
				code = codegen.EmitGo(result, packageName)
			}

			// Write output.
			if output != "" {
				if err := os.WriteFile(output, []byte(code), 0644); err != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("write output: %v", err), ToStderr: true}
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s\n", output)
				return nil
			}

			fmt.Print(code)
			return nil
		},
	}

	cmd.Flags().StringVarP(&lang, "lang", "l", "", "target language: typescript or go (required)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file path (default: stdout)")
	cmd.Flags().StringVar(&packageName, "package", "", "Go package name (default: derived from interface name)")

	return cmd
}
