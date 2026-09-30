package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// NewCompactSurfaceRoot is the competing information architecture. It moves
// the same inert leaves into fewer root entries; their flags and output
// contracts remain identical to the broad variant.
func NewCompactSurfaceRoot() *cobra.Command {
	root := NewSurfaceRoot()
	root.Short = "Explore a compact OpenBindings command surface"
	root.Long = `COMPACT COMMAND-SURFACE PREVIEW — no operational commands run.

Common OBI tasks stay at the root. Compare, edit, configuration, and server
tasks live in families. Readers print a result and -o saves it. OBI edits
update the input or write a resulting JSON OBI to -o. -F changes presentation,
not content.
Configuration edits update the active environment, streams use stdout, and
foreground servers reject -o and -F. Use "ob <command> --help" to inspect
a task.`
	root.Example = `  ob new -o api.obi.json
  ob show api.obi.json --full -F json
  ob source add api.obi.json api.yaml --pull
  ob source status api.obi.json
  ob compare compat old.obi.json new.obi.json
  ob edit patch api.obi.json changes.patch.json`

	compare := newCompactGroup("compare", "Compare OBIs and check compatibility", "explore")
	edit := newCompactGroup("edit", "Apply document transformations", "authoring")
	config := newCompactGroup("config", "Manage local configuration and extensions", "delegates")
	serve := newCompactGroup("serve", "Run local services and demos", "serve")
	hideSurfaceInheritedFlagsInHelp(serve)
	root.AddCommand(compare, edit, config, serve)

	for _, pair := range [][2]*cobra.Command{
		{surfaceCommand(root, "diff"), compare},
		{surfaceCommand(root, "compat"), compare},
		{surfaceCommand(root, "patch"), edit},
		{surfaceCommand(root, "merge"), edit},
		{surfaceCommand(root, "conform"), edit},
		{surfaceCommand(root, "meta"), edit},
		{surfaceCommand(root, "strip-ob-metadata"), edit},
		{surfaceCommand(root, "init"), config},
		{surfaceCommand(root, "environment"), config},
		{surfaceCommand(root, "context"), config},
		{surfaceCommand(root, "delegate"), config},
		{surfaceCommand(root, "binding-specs"), config},
		{surfaceCommand(root, "start"), serve},
		{surfaceCommand(root, "mcp"), serve},
		{surfaceCommand(root, "demo"), serve},
		{surfaceCommand(root, "inspect"), surfaceCommand(root, "source")},
		{surfaceCommand(root, "status"), surfaceCommand(root, "source")},
	} {
		moveSurfaceCommand(root, pair[0], pair[1])
	}

	surfaceCommand(root, "source").Long += `

Use inspect to identify a source's binding specification and status to check
whether registered sources have drifted from the stored OBI.`
	surfaceCommand(root, "serve", "start").Use = "api"
	for _, path := range [][2]string{
		{"ob diff", "ob compare diff"},
		{"ob compat", "ob compare compat"},
		{"ob patch", "ob edit patch"},
		{"ob merge", "ob edit merge"},
		{"ob conform", "ob edit conform"},
		{"ob meta", "ob edit meta"},
		{"ob strip-ob-metadata", "ob edit strip-ob-metadata"},
		{"ob init", "ob config init"},
		{"ob environment", "ob config environment"},
		{"ob context", "ob config context"},
		{"ob delegate", "ob config delegate"},
		{"ob binding-specs", "ob config binding-specs"},
		{"ob start", "ob serve api"},
		{"ob mcp", "ob serve mcp"},
		{"ob demo", "ob serve demo"},
		{"ob inspect", "ob source inspect"},
		{"ob status", "ob source status"},
	} {
		rewriteSurfaceHelp(root, path[0], path[1])
	}
	return root
}

func rewriteSurfaceHelp(cmd *cobra.Command, oldPath, newPath string) {
	cmd.Long = strings.ReplaceAll(cmd.Long, oldPath, newPath)
	cmd.Example = strings.ReplaceAll(cmd.Example, oldPath, newPath)
	for _, child := range cmd.Commands() {
		rewriteSurfaceHelp(child, oldPath, newPath)
	}
}

func newCompactGroup(use, short, group string) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, GroupID: group}
	markCommandGroup(cmd)
	return cmd
}

func moveSurfaceCommand(root, command, destination *cobra.Command) {
	if command == nil || destination == nil {
		panic(fmt.Sprintf("compact surface move has missing command: %v -> %v", command, destination))
	}
	root.RemoveCommand(command)
	command.GroupID = ""
	destination.AddCommand(command)
}
