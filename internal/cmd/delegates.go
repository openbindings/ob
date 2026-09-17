package cmd

import "github.com/spf13/cobra"

func newDelegateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "delegate",
		Aliases: []string{"delegates"},
		Short:   "Manage delegates",
		Long: `Manage the delegate registry.

Inspect accepted interfaces and diagnose selection for invoke, synthesize and
inspect roles. Role-aware routing uses explicitly enrolled registrations;
built-in handling is not an editable registration.

This development candidate is migrating its management surfaces. The remaining
legacy mutation commands do not enroll roles; see docs/delegate-manager-migration.md
before changing an environment.`,
	}
	markCommandGroup(c)

	c.AddCommand(
		newDelegateRegisterCmd(),
		newDelegateUnregisterCmd(),
		newDelegateListCmd(),
		newDelegateResolveCmd(),
		newDelegateRequirementsCmd(),
		newDelegatePreferCmd(),
	)

	return c
}
