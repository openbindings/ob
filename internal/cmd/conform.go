package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newConformCmd() *cobra.Command {
	var (
		yes    bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "conform <interface> <target-obi>",
		Short: "Scaffold or update operations so the target corresponds to another interface",
		Long: `Scaffold or update operations in a target OBI so it corresponds to another interface.

For each operation in the reference interface:
  - If missing from the target: scaffolds it (keyed by the contract operation
    name, copying its schemas)
  - If present but incompatible: offers to replace the schema, declaring the
    correspondence with an alias when the keys differ
  - If present and compatible: reports "in sync"

Correspondence is expressed purely through the operation key+alias namespace
(spec OBI-T-12): an operation corresponds to a contract operation by carrying
its name as the key or an alias.

Use --yes to auto-accept all changes (for CI/scripting).
Use --dry-run to preview changes without modifying the file.

Examples:
  ob conform document-store.json my-service.obi.json
  ob conform https://openbindings.org/interfaces/host.json ./interface.json --yes
  ob conform document-store.json my-service.obi.json --dry-run`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			confirm := func(op string, action string) bool {
				if yes {
					return true
				}
				fmt.Fprintf(os.Stderr, "  %s: %s? [Y/n] ", op, action)
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(strings.ToLower(line))
				return line == "" || line == "y" || line == "yes"
			}

			output := app.Conform(app.ConformInput{
				InterfaceLocator: args[0],
				TargetPath:       args[1],
				Yes:              yes,
				DryRun:           dryRun,
			}, confirm)

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(output, format, outputPath, output.Render)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "auto-accept all scaffolding and replacements")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without modifying files")

	return cmd
}
