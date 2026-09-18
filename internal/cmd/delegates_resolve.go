package cmd

import (
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateResolveCmd() *cobra.Command {
	var role, bindingSpec, path, registrationID string
	c := &cobra.Command{
		Use:   "resolve",
		Short: "Explain delegate selection for a role and binding specification",
		Long: `Assess which provider ob would select for one role and exact binding
specification identifier. This checks live support, but does not invoke work.
The result is a point-in-time diagnostic, not a reservation or authorization.

Invocation defaults to ranked selection (ordinary operation and raw binding
invocation); --path native-first explains the frame entrypoint. Synthesis and
inspection use native-first selection. --registration checks only that enrolled
registration, without falling back to another provider or a built-in handler.
An unavailable result is successful; invalid state or failed assessment is an error.

Examples:
  ob delegate resolve --role invoke --binding-spec openbindings.usage@1
  ob delegate resolve --role invoke --binding-spec openbindings.openapi-3.1@1 --path native-first
  ob delegate resolve --role synthesize --binding-spec example.format@1 --registration <id> -F json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, name := range []string{"role", "binding-spec", "path", "registration"} {
				if value, _ := cmd.Flags().GetString(name); cmd.Flags().Changed(name) && value == "" {
					return fmt.Errorf("--%s must not be empty", name)
				}
			}
			result, err := app.ResolveRoleDelegate(cmd.Context(), app.RoleResolutionInput{
				Role: app.DelegateCapability(role), BindingSpec: bindingSpec, Path: path, RegistrationID: registrationID,
			})
			if err != nil {
				return err
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(result, format, outputPath, result.Render)
		},
	}
	c.Flags().StringVar(&role, "role", "", "Role: invoke, synthesize, or inspect (required)")
	c.Flags().StringVar(&bindingSpec, "binding-spec", "", "Exact binding specification identifier (required)")
	c.Flags().StringVar(&path, "path", "", "Selection policy: ranked or native-first; explicit with --registration")
	c.Flags().StringVar(&registrationID, "registration", "", "Check only this enrolled registration ID (no fallback)")
	_ = c.MarkFlagRequired("role")
	_ = c.MarkFlagRequired("binding-spec")
	return c
}
