package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateRolesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "roles",
		Short: "List the roles ob accepts delegates for, with their accepted interfaces",
		Long: `List every role this ob advertises for delegation: its purpose and use
policy, and the complete accepted interface value(s) a delegate must
correspond to. Registration is candidacy under the role's documented rules;
it does not activate a provider or prove executable binding support.

Examples:
  ob delegate roles
  ob delegate roles -F json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			roles, err := app.ListDelegateRoles()
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(roles, format, outputPath, roles.Render)
		},
	}
}
