package cmd

import "github.com/spf13/cobra"

func newDelegateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "delegate",
		Aliases: []string{"delegates"},
		Short:   "Manage delegates",
		Long: `Manage delegates registered in the environment.

A delegate is any software that implements an OpenBindings interface contract
to receive delegated work. ob currently delegates binding format handling;
delegates are discovered by probing the interface contracts they satisfy.`,
	}

	c.AddCommand(
		newDelegateListCmd(),
		newDelegateAddCmd(),
		newDelegateRemoveCmd(),
		newDelegateResolveCmd(),
	)

	return c
}
