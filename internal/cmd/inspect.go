package cmd

import (
	"context"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/spf13/cobra"
)

func newInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <source>",
		Short: "Inspect a binding source and list its bindable targets",
		Long: `Inspect a binding source artifact and report the bindable targets it
contains, without creating an interface. Use it to preview the operations
that "ob create" would extract from a spec.

Source format: [format:]path[?option...]  (same form as "ob create")

Examples:
  ob inspect openapi.json
  ob inspect usage@2.13.1:./cli.kdl
  ob inspect https://api.example.com/openapi.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := app.ParseSource(args[0])
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			if parsed.Format == "" {
				detected, derr := app.DetectSourceFormat(parsed.Location)
				if derr != nil {
					return app.ExitResult{Code: 2, Message: derr.Error(), ToStderr: true}
				}
				parsed.Format = detected
			}

			source := &openbindings.Source{
				Format:      parsed.Format,
				Location:    parsed.Location,
				Description: parsed.Description,
			}

			inspection, err := app.InspectSource(context.Background(), source)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(inspection, format, outputPath, func() string {
				return app.RenderSourceInspection(source.Format, source.Location, inspection)
			})
		},
	}
	return cmd
}
