package cmd

import "github.com/spf13/cobra"

func newDelegateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "delegate",
		Aliases: []string{"delegates"},
		Short:   "Manage delegates",
		Long: `Manage the delegate registry.

A delegate is any referenceable OpenBindings interface ob may route
operations to. Registration snapshots what a delegate carries and pins the
resolved document; resolution matches the operations ob needs against those
snapshots; preference orders the candidates. ob itself is the builtin
self-delegate.`,
	}
	markCommandGroup(c)

	c.AddCommand(
		newDelegateRegisterCmd(),
		newDelegateUnregisterCmd(),
		newDelegateListCmd(),
		newDelegateResolveCmd(),
		newDelegateResolveBindingSpecCmd(),
		newDelegateRequirementsCmd(),
		newDelegatePreferCmd(),
	)

	return c
}
