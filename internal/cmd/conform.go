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
		roleKey string
		yes     bool
		dryRun  bool
	)

	cmd := &cobra.Command{
		Use:   "conform <role-interface> <target-obi>",
		Short: "Scaffold or update operations to conform to a role interface",
		Long: `Scaffold or update operations in a target OBI to conform to a role interface.

For each operation in the role interface:
  - If missing from the target: scaffolds it (copies schema, adds satisfies)
  - If present but incompatible: offers to replace the schema
  - If present and compatible: reports "in sync"

Also adds the role to the target's roles map.

Use --yes to auto-accept all changes (for CI/scripting).
Use --dry-run to preview changes without modifying the file.

Examples:
  ob conform context-store.json my-service.obi.json
  ob conform https://openbindings.org/interfaces/host.json ./interface.json --yes
  ob conform context-store.json my-service.obi.json --role-key openbindings.context-store
  ob conform context-store.json my-service.obi.json --dry-run`,
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

			output := app.ConformToRole(app.ConformInput{
				RoleLocator: args[0],
				RoleKey:     roleKey,
				TargetPath:  args[1],
				Yes:         yes,
				DryRun:      dryRun,
			}, confirm)

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(output, format, outputPath, output.Render)
		},
	}

	cmd.Flags().StringVar(&roleKey, "role-key", "", "key for the role in the target's roles map (default: derived from interface name)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "auto-accept all scaffolding and replacements")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without modifying files")

	return cmd
}
