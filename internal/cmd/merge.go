package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newMergeCmd() *cobra.Command {
	var (
		fromSources bool
		onlySource  string
		all         bool
		dryRun      bool
		yes         bool
		outPath     string
		operations  []string
		excludeOps  []string
	)

	cmd := &cobra.Command{
		Use:   "merge <target> [source]",
		Short: "Selectively apply changes from one OBI into another",
		Long: `Merge operations and bindings from a source OBI into a target OBI.

Two modes:

  ob merge <target> <source>
    Merge from any source OBI into the target.

  ob merge <target> --from-sources
    Derive from the target's binding sources, then merge.
    Use --only <key> to scope to a specific source.

When run interactively (TTY detected), each change is presented for
accept/reject. Use --all to apply all changes in batch mode, or
--yes to auto-accept all prompts.

Use --op to cherry-pick specific operations, or --exclude-op to skip them:

  ob merge target.obi.json source.obi.json --op createUser --op deleteUser --yes
  ob merge target.obi.json source.obi.json --exclude-op internalOp --all

Merge rules:
  - Added operations: added to target with bindings
  - Changed operations: schema slots updated, user-authored fields preserved
  - Removed bindings: binding entries removed, operations kept
  - Unbound operations: untouched

Exit codes:
  0  Changes applied (or nothing to do)
  1  Conflicts or errors during merge
  2  Usage error`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateSourceModeArgs(args, fromSources, onlySource); err != nil {
				return err
			}

			input := app.MergeInput{
				TargetPath:  args[0],
				FromSources: fromSources,
				OnlySource:  onlySource,
				All:         all,
				DryRun:      dryRun,
				OutPath:     outPath,
				Operations:  operations,
				ExcludeOps:  excludeOps,
			}
			if len(args) == 2 {
				input.SourceLocator = args[1]
			}

			// Set up interactive prompting if TTY and not --all.
			if !all && !dryRun {
				isTTY := term.IsTerminal(int(os.Stdin.Fd()))
				if isTTY || yes {
					input.PromptFunc = func(entry app.MergeEntry) (bool, error) {
						if yes {
							return true, nil
						}
						return promptMergeEntry(entry)
					}
				}
			}

			result, err := app.Merge(input)
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	cmd.Flags().BoolVar(&fromSources, "from-sources", false, "derive from the target's binding sources")
	cmd.Flags().StringVar(&onlySource, "only", "", "scope to a specific source key (requires --from-sources)")
	cmd.Flags().BoolVar(&all, "all", false, "apply all changes (batch mode)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without writing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "auto-accept all prompts")
	cmd.Flags().StringVar(&outPath, "out", "", "write to alternate path instead of target")
	cmd.Flags().StringArrayVar(&operations, "op", nil, "only merge this operation (repeatable)")
	cmd.Flags().StringArrayVar(&excludeOps, "exclude-op", nil, "exclude this operation from merge (repeatable)")


	return cmd
}

// promptMergeEntry prompts the user to accept or reject a single merge entry.
func promptMergeEntry(entry app.MergeEntry) (bool, error) {
	s := app.Styles

	// Build the prompt description.
	var desc strings.Builder
	switch entry.Action {
	case app.MergeAdd:
		desc.WriteString(s.Added.Render("+ ADD"))
	case app.MergeUpdate:
		desc.WriteString(s.Warning.Render("~ UPDATE"))
	case app.MergeUnbind:
		desc.WriteString(s.Removed.Render("- REMOVE BINDING"))
	}
	desc.WriteString("  ")
	desc.WriteString(s.Key.Render(entry.Operation))

	if len(entry.Details) > 0 {
		desc.WriteString("\n")
		for _, d := range entry.Details {
			fmt.Fprintf(&desc, "  %s\n", s.Dim.Render(d))
		}
	}

	var accept bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(desc.String()).
				Description("Apply this change?").
				Value(&accept),
		),
	)

	if err := form.Run(); err != nil {
		return false, err
	}

	return accept, nil
}
