package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateRequirementsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "requirements <role>",
		Short: "Print the interface a delegate must correspond to for a role",
		Long: `Print the accepted interface a delegate must correspond to for one role,
so a prospective delegate can be checked before enrollment:

  ob compat <(ob delegate requirements invoke) my-tool.obi.json

This is a convenience projection of 'ob delegate roles', available only while
the role accepts exactly one alternative; a role with several alternatives is
refused rather than reduced to its first entry. Correspondence is exact-key
plus directional schema compatibility for the whole alternative.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The projection emits the accepted interface document verbatim so
			// it can be piped straight into `ob compat`; it honors neither the
			// output file nor the format lane, so silently ignoring them would
			// hand back JSON on stdout after the caller asked for a YAML file.
			if err := refuseUnhonoredOutputFlags(cmd, "delegate requirements", "output", "format"); err != nil {
				return err
			}
			data, err := app.DelegateRoleRequirementJSON(args[0])
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			out := cmd.OutOrStdout()
			out.Write(data)
			return nil
		},
	}
}
