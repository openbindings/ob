package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegatePreferCmd() *cobra.Command {
	var role, bindingSpec string
	var clear bool

	cmd := &cobra.Command{
		Use:   "prefer <id> [preference]",
		Short: "Set or clear a registration's preference for one role (higher = more preferred)",
		Long: `Set or clear the explicit preference of one registration in one of its
roles. Higher numbers express stronger preference among eligible registrations
in that role; equal numbers express no ordering, and ob keeps registration
order on ties. Numbers are retained exactly; an explicit 0 stays explicit
until cleared. --clear removes the explicit entry (effective preference stays 0).

--binding-spec targets ob's native override instead: the preference applies
only when that exact binding specification is requested in that role, and it
takes priority over the role preference. Overrides are separate from the
shared role preference map and cannot enroll a role.

Examples:
  ob delegate prefer dlg_... 20 --role invoke
  ob delegate prefer dlg_... 0 --role synthesize
  ob delegate prefer dlg_... --clear --role invoke
  ob delegate prefer dlg_... 10 --role invoke --binding-spec openbindings.grpc@1
  ob delegate prefer dlg_... --clear --role invoke --binding-spec openbindings.grpc@1`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if looksLikeLocation(args[0]) {
				return app.ExitResult{Code: 2, Message: fmt.Sprintf("%q is a location; preferences belong to a registration ID and role (see 'ob delegate list')", args[0]), ToStderr: true}
			}
			if strings.TrimSpace(role) == "" {
				return app.ExitResult{Code: 2, Message: "--role is required: preferences are scoped to a registration and role", ToStderr: true}
			}
			if cmd.Flags().Changed("binding-spec") && bindingSpec == "" {
				return app.ExitResult{Code: 2, Message: "--binding-spec must name an exact binding specification identifier", ToStderr: true}
			}
			var preference *json.Number
			switch {
			case clear && len(args) == 2:
				return app.ExitResult{Code: 2, Message: "--clear takes no preference value", ToStderr: true}
			case clear:
				// nil clears the explicit entry
			case len(args) < 2:
				return app.ExitResult{Code: 2, Message: "provide a preference number, or pass --clear", ToStderr: true}
			default:
				number := json.Number(strings.TrimSpace(args[1]))
				if !app.ValidDelegatePreference(number) {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("%q is not an exact JSON number ob can retain", args[1]), ToStderr: true}
				}
				preference = &number
			}
			var err error
			if bindingSpec != "" {
				err = app.SetDelegateBindingPreference(args[0], role, bindingSpec, preference)
			} else {
				err = app.SetDelegatePreference(args[0], role, preference)
			}
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			text := fmt.Sprintf("Set preference of %s for role %s", args[0], role)
			if preference == nil {
				text = fmt.Sprintf("Cleared preference of %s for role %s", args[0], role)
			}
			if bindingSpec != "" {
				text += " (binding specification " + bindingSpec + ")"
			}
			format, outputPath := getOutputFlags(cmd)
			// The contract's output is null; the text rendering is the human view.
			return app.OutputResultText(nil, format, outputPath, func() string { return text })
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "role the preference applies to (required)")
	cmd.Flags().StringVar(&bindingSpec, "binding-spec", "", "target ob's native override for this exact binding specification instead of the role preference")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the explicit entry instead of setting it")
	cmd.SetFlagErrorFunc(retiredFlagGuidance(map[string]string{
		"operation":  "preferences are scoped to a registration and role; use --role <invoke|synthesize|inspect>, with --binding-spec for a native override",
		"capability": "roles replaced capabilities; use --role <invoke|synthesize|inspect>",
	}))
	return cmd
}
