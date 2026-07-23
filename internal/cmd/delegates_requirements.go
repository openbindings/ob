package cmd

import (
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateRequirementsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "requirements <invoke|synthesize|inspect>",
		Short: "Print the operation subset a delegate must correspond to for a capability",
		Long: `Print the OpenBindings interface a delegate must correspond to in order to
provide a capability, so a prospective delegate can be checked against the
exact operation subset ob consumes:

  ob compat <(ob delegate requirements invoke) my-tool.obi.json

Capabilities derive their operations and schemas from published interfaces:
invoke → binding-invoker, synthesize → interface-synthesizer, inspect →
source-inspector. Correspondence is per operation; unrelated operations in the
same published interface are not required.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cap := app.DelegateCapability(strings.ToLower(strings.TrimSpace(args[0])))
			data, err := app.RequirementInterfaceJSON(cap)
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			out := cmd.OutOrStdout()
			out.Write(data)
			if len(data) == 0 || data[len(data)-1] != '\n' {
				out.Write([]byte{'\n'})
			}
			return nil
		},
	}
}
