package cmd

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "delegate",
		Aliases: []string{"delegates"},
		Short:   "Manage delegates",
		Long: `Manage the delegate registry.

A delegate is an OpenBindings interface value enrolled for one or more of
ob's roles: invoke, synthesize and inspect. 'ob delegate roles' prints each
role's accepted interface; 'ob delegate register' enrolls an interface
document for explicit roles under a manager-issued registration ID; 'list',
'prefer' and 'unregister' manage those registrations by ID. Built-in handling
needs no registration and is not an editable row.

Registration establishes candidacy, not trust: it does not authorize a local
executable, disclose credentials, or promise a remote provider is unchanged.
Legacy location-based registrations require explicit conversion; see
'ob delegate migrate'.`,
	}
	markCommandGroup(c)

	c.AddCommand(
		newDelegateRolesCmd(),
		newDelegateRegisterCmd(),
		newDelegateListCmd(),
		newDelegatePreferCmd(),
		newDelegateUnregisterCmd(),
		newDelegateRequirementsCmd(),
		newDelegateResolveCmd(),
		newDelegateMigrateCmd(),
	)

	return c
}

// retiredFlagGuidance turns an unknown-flag error for a retired flag into
// actionable guidance instead of a bare parse failure. Retired flags never
// create legacy state or infer roles.
func retiredFlagGuidance(retired map[string]string) func(*cobra.Command, error) error {
	return func(cmd *cobra.Command, err error) error {
		message := err.Error()
		for flag, guidance := range retired {
			if strings.Contains(message, "unknown flag: --"+flag) || strings.Contains(message, "unknown shorthand flag") && strings.Contains(message, "'"+flag+"'") {
				return app.ExitResult{Code: 2, Message: fmt.Sprintf("--%s is retired: %s", flag, guidance), ToStderr: true}
			}
		}
		return app.ExitResult{Code: 2, Message: message + "\n" + cmd.UsageString(), ToStderr: true}
	}
}

// looksLikeLocation recognizes the retired locator vocabulary so a user of the
// old surface gets guidance rather than a silent no-op or a file-not-found.
func looksLikeLocation(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "exec:") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
