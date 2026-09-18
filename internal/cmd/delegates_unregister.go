package cmd

import (
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateUnregisterCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "unregister <id>",
		Aliases: []string{"remove", "rm"},
		Short:   "Remove a registration by ID",
		Long: `Remove a registration: all of its role memberships and preferences go
together. Repeating the removal, including for an ID that was never
registered, succeeds. Removal stops new lookups from offering the record; it
does not cancel in-flight work, revoke credentials or undo provider effects.

Examples:
  ob delegate unregister dlg_...
  ob delegate rm dlg_...`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if looksLikeLocation(args[0]) {
				return app.ExitResult{Code: 2, Message: fmt.Sprintf("%q is a location; unregister takes a registration ID (see 'ob delegate list'); legacy location-based rows need 'ob delegate migrate'", args[0]), ToStderr: true}
			}
			if err := app.UnregisterDelegate(args[0]); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			// The contract's output is null; the text rendering is the human view.
			return app.OutputResultText(nil, format, outputPath, func() string {
				return "Registration " + args[0] + " is absent"
			})
		},
	}
	return c
}
