package cmd

import (
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newDelegateMigrateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Convert a legacy location-based delegate registry to role-scoped registrations",
		Long: `Convert the retired location-based delegate registry of the active
environment into role-scoped, by-value registrations.

Conversion is deliberate: 'preview' inventories every legacy row without
changing anything; you review the plan (choose convert or exclude for each
row, supply the recovered interface document and explicit roles); 'apply'
validates the reviewed plan against the unchanged environment and commits the
whole conversion once, with private backups and a receipt; 'rollback' restores
the exact original when nothing has changed since.

Stop every older ob process and any other writer of this environment before
applying or rolling back; --confirm-quiesced records that you did. An older
binary does not honor the new registry lock. See docs/delegate-manager-migration.md.`,
	}
	markCommandGroup(c)
	c.AddCommand(newDelegateMigratePreviewCmd(), newDelegateMigrateApplyCmd(), newDelegateMigrateRollbackCmd())
	return c
}

func newDelegateMigratePreviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "preview",
		Short: "Inventory the legacy registry as a reviewable plan (no changes)",
		Long: `Inventory the active environment's legacy delegate rows as a migration plan.
Every row starts unresolved; nothing is fetched, executed or written.

The text output is a summary. -F json prints the complete plan (it carries the
original rows, which may include sensitive configuration). -o <new-file> writes
the plan as an owner-only file and refuses an existing path.

Examples:
  ob delegate migrate preview
  ob delegate migrate preview -o review/plan.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := app.PreviewDelegateMigration()
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			if outputPath != "" {
				if err := app.WriteMigrationPlanFile(outputPath, plan); err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s\n  plan written to %s (owner-only)\n", plan.Render(), outputPath)
				return nil
			}
			return app.OutputResultText(plan, format, "", plan.Render)
		},
	}
}

func newDelegateMigrateApplyCmd() *cobra.Command {
	var confirmQuiesced bool
	c := &cobra.Command{
		Use:   "apply <reviewed-plan-file>",
		Short: "Apply a reviewed migration plan once, with backups and a receipt",
		Long: `Apply a reviewed migration plan. The plan must match the environment's
current configuration, catalogue and row order exactly; every row needs an
explicit disposition, and converted rows need a recovered interface document
and explicit roles that pass admission. A stale or incomplete plan is refused.

Apply writes owner-only backups (original configuration and the plan) under
.delegate-migrations/<plan digest>/ in the environment, then replaces the
registry in one commit and records a receipt. Repeating the same apply reports
the recorded completion; it never converts rows twice. A failure after the
commit is reported as uncertain completion: inspect 'ob delegate list' and the
receipt before retrying.

--confirm-quiesced acknowledges that every older ob process and other writer
of this environment has been stopped. It is your acknowledgment, not proof.

Example:
  ob delegate migrate apply review/plan.json --confirm-quiesced`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmQuiesced {
				return app.ExitResult{Code: 2, Message: "stop every older writer of this environment, then pass --confirm-quiesced", ToStderr: true}
			}
			plan, err := app.ReadMigrationPlanFile(args[0])
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			receipt, err := app.ApplyDelegateMigration(plan, true)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(receipt, format, outputPath, receipt.Render)
		},
	}
	c.Flags().BoolVar(&confirmQuiesced, "confirm-quiesced", false, "acknowledge that all older writers of this environment are stopped (required)")
	return c
}

func newDelegateMigrateRollbackCmd() *cobra.Command {
	var confirmQuiesced bool
	c := &cobra.Command{
		Use:   "rollback <plan-hash>",
		Short: "Restore the original configuration of an applied migration",
		Long: `Restore the exact original configuration backed up by an applied migration,
identified by the plan hash from its receipt (sha256:...). Rollback verifies
the backup, the reviewed plan, the receipt and the current state under the
environment lock. If the registry or any other configuration changed after
the conversion, rollback keeps a private recovery copy and refuses; reconcile
explicitly instead of forcing the old bytes over newer work.

Rollback restores configuration only: it does not undo delegated work,
revoke credentials or cancel in-flight invocations.

Example:
  ob delegate migrate rollback sha256:... --confirm-quiesced`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmQuiesced {
				return app.ExitResult{Code: 2, Message: "stop every older writer of this environment, then pass --confirm-quiesced", ToStderr: true}
			}
			if err := app.RollbackDelegateMigration(args[0], true); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(nil, format, outputPath, func() string {
				return "Restored the original configuration recorded for " + args[0]
			})
		},
	}
	c.Flags().BoolVar(&confirmQuiesced, "confirm-quiesced", false, "acknowledge that all older writers of this environment are stopped (required)")
	return c
}
