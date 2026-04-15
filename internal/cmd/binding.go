package cmd

import "github.com/spf13/cobra"

func newBindingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "binding",
		Short: "Low-level binding operations",
		Long: `Low-level binding operations for machine-to-machine use.

These commands operate on pre-resolved bindings rather than OBI-level operations.
They are the building blocks used by delegates and orchestrators.`,
	}

	c.AddCommand(
		newBindingExecCmd(),
	)

	return c
}
