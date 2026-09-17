package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateListCmd() *cobra.Command {
	var role string
	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List registrations with their retained interfaces, roles and preferences",
		Long: `List every registration in the active environment: its ID, complete role
set, explicit preference map and the retained interface document. --role
keeps only registrations enrolled in that exact role without trimming their
records; a role that is no longer advertised still finds its enrollments.

The text view is a summary; -F json carries the full values. Built-in handling
is not a registration and is not listed. Legacy, mixed or unreadable registry
state is an error, never an empty list.

Examples:
  ob delegate list
  ob delegate list --role invoke -F json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("role") && role == "" {
				return app.ExitResult{Code: 2, Message: "--role must name a role; omit it to list every registration", ToStderr: true}
			}
			output, err := app.ListDelegates(role)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(output, format, outputPath, output.Render)
		},
	}
	c.Flags().StringVar(&role, "role", "", "keep only registrations enrolled in this exact role")
	return c
}
