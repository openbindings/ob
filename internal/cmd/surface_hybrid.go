package cmd

import "github.com/spf13/cobra"

// NewHybridSurfaceRoot keeps daily tasks direct and nests specialist tasks
// under the noun they act on. It has the same inert leaves and contracts as
// the broad and compact proposals.
func NewHybridSurfaceRoot() *cobra.Command {
	root := NewSurfaceRoot()
	root.Short = "Explore the task-first OpenBindings command surface"
	root.Long = `HYBRID COMMAND-SURFACE PREVIEW — no operational commands run.

The common OBI tasks are direct. Specialist operation and source tasks live
under those nouns; local services live under serve. Readers print a result
and -o saves it. OBI edits update the input or write a resulting JSON OBI to
-o. -F changes presentation, not content. Configuration edits update the
active environment; streams use stdout; foreground servers reject -o and -F.`
	root.Example = `  ob new -o api.obi.json
  ob show api.obi.json --full -F json
  ob source add api.obi.json api.yaml --pull
  ob status api.obi.json
  ob compat old.obi.json new.obi.json
  ob patch api.obi.json changes.patch.json`

	serve := newCompactGroup("serve", "Run local services and demos", "serve")
	hideSurfaceInheritedFlagsInHelp(serve)
	root.AddCommand(serve)
	for _, pair := range [][2]*cobra.Command{
		{surfaceCommand(root, "start"), serve},
		{surfaceCommand(root, "mcp"), serve},
		{surfaceCommand(root, "demo"), serve},
		{surfaceCommand(root, "inspect"), surfaceCommand(root, "source")},
		{surfaceCommand(root, "binding-specs"), surfaceCommand(root, "source")},
		{surfaceCommand(root, "conform"), surfaceCommand(root, "operation")},
	} {
		moveSurfaceCommand(root, pair[0], pair[1])
	}
	surfaceCommand(root, "serve", "start").Use = "api"
	surfaceCommand(root, "source").Short = "Manage sources; inspect or check binding-spec support"
	surfaceCommand(root, "source").Long += `

Use inspect to examine a source's bindable targets and binding-specs to list
or check installed binding specifications.`
	surfaceCommand(root, "operation").Long += `

Use conform to scaffold operation contracts from another interface under
ob's authoring policy.`
	for _, path := range [][2]string{
		{"ob start", "ob serve api"},
		{"ob mcp", "ob serve mcp"},
		{"ob demo", "ob serve demo"},
		{"ob inspect", "ob source inspect"},
		{"ob binding-specs", "ob source binding-specs"},
		{"ob conform", "ob operation conform"},
	} {
		rewriteSurfaceHelp(root, path[0], path[1])
	}
	return root
}
