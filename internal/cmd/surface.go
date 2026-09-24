package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewSurfaceRoot is the exploratory CLI on the cli-surface-lab branch.
// It reuses the production command declarations for realistic flags and
// argument shapes, then replaces every handler before returning the tree.
// The production NewRoot and its contracts are deliberately left intact.
func NewSurfaceRoot() *cobra.Command {
	root := NewRoot()
	root.Short = "Explore the proposed OpenBindings command surface"
	root.Long = `COMMAND-SURFACE PREVIEW — no operational commands run on this branch.

Explore command names, arguments, flags, and help with "ob --help" and
"ob <command> --help". Valid invocations report a placeholder message;
they do not read or write OBIs, call services, or change configuration.

Proposed output rule: -o/--output names the primary result. An editing
command writes its resulting OBI there (or edits its input in place when
-o is absent); a read-only command writes its selected result there. -F/--format
changes encoding, never the result's shape.`
	root.Example = `  ob synthesize api.yaml -o api.obi.json
  ob show api.obi.json
  ob source add api.obi.json cli.usage.kdl --pull
  ob status api.obi.json
  ob operation invoke api.obi.json listPets --input '{"limit":10}'`
	root.Version = "surface-lab (preview; commands do not run)"
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().Lookup("output").Usage = "write the primary result to a file (edits: OBI; reads: result)"
	root.PersistentFlags().Lookup("format").Usage = "render the same result as text, json, or yaml"
	for _, name := range []string{"agent-primer", "openbindings", "usage-spec"} {
		_ = root.Flags().MarkHidden(name)
	}

	// Arrange the first help screen by what someone is trying to do.
	for _, group := range root.Groups() {
		switch group.ID {
		case "setup":
			group.Title = "start here"
		case "explore":
			group.Title = "inspect and compare"
		case "authoring":
			group.Title = "author and edit"
		case "delegates":
			group.Title = "configure and extend"
		case "serve":
			group.Title = "generate and serve"
		case "introspect":
			group.Title = "about ob"
		}
	}
	for name, group := range map[string]string{
		"demo": "setup", "new": "setup", "resolve": "setup", "synthesize": "setup",
		"show": "explore", "status": "explore", "inspect": "explore", "diff": "explore",
		"compat": "explore", "validate": "explore",
		"source": "authoring", "operation": "authoring", "binding": "authoring",
		"meta": "authoring", "merge": "authoring", "conform": "authoring", "purify": "authoring",
		"init": "delegates", "environment": "delegates", "context": "delegates",
		"delegate": "delegates", "binding-specs": "delegates",
		"codegen": "serve", "mcp": "serve", "start": "serve",
		"describe": "introspect",
	} {
		if command := surfaceCommand(root, name); command != nil {
			command.GroupID = group
		}
	}

	// Reading a document or one of its parts should be a direct CLI task.
	root.AddCommand(&cobra.Command{
		Use:     "show <obi>",
		Short:   "Show an OBI summary or its complete document",
		Long:    "Show an OBI summary by default. -F json or -F yaml emits the complete OBI document. The locator may be a file, URL, exec: reference, or - for stdin.",
		Example: "  ob show api.obi.json\n  ob show https://api.example.com -F json",
		Args:    cobra.ExactArgs(1),
		GroupID: "explore",
	})
	for _, item := range []struct {
		parent, use, short string
	}{
		{"source", "show <obi> <source>", "Show one source, including its binding specification and provenance"},
		{"operation", "show <obi> <operation>", "Show one operation, its schemas, aliases, and bindings"},
		{"binding", "show <obi> <binding>", "Show one binding, including its selector and transforms"},
	} {
		parent := surfaceCommand(root, item.parent)
		parent.AddCommand(&cobra.Command{
			Use:   item.use,
			Short: item.short,
			Long:  item.short + ". Use -F json or -F yaml for the complete stored value.",
			Args:  cobra.ExactArgs(2),
		})
	}

	// The common tracked-source path can be requested as one atomic action.
	sourceAdd := surfaceCommand(root, "source", "add")
	sourceAdd.Flags().Bool("pull", false, "register and derive this source in one atomic action")
	sourceAdd.Long = `Register a binding source on an OBI. By default this only records
the source; use --pull to register it and derive its operations and bindings
in one atomic action. Without --pull, "ob source pull <obi>" derives later.`

	// These flags express distinct output modes instead of changing the
	// meaning of -F/--format.
	resolve := surfaceCommand(root, "resolve")
	resolve.Flags().Bool("with-origin", false, "include discovery provenance in an envelope")
	resolve.Long = `Resolve an OBI from a URL or host and print the document to stdout.
Use -o to save the document. -F json or -F yaml changes only its encoding;
--with-origin instead emits an envelope with the interface and its origin.`
	invoke := surfaceCommand(root, "operation", "invoke")
	invoke.Flags().Bool("envelope", false, "collect the operation output into one JSON envelope")
	invoke.Long = `Invoke an OBI operation. The default output is one JSON value per
event. --envelope collects the output into one JSON object. -F controls
encoding only; it does not switch between streaming and aggregate output.`
	merge := surfaceCommand(root, "merge")
	_ = merge.Flags().MarkHidden("out") // Superseded by the proposed -o rule.
	merge.Long = `Merge changes from a source OBI into a target OBI. The target is
edited in place by default; -o writes the resulting OBI to another path.
The merge report goes to stdout, or to stderr when stdout carries an OBI.`

	// "describe" currently describes ob itself, while "show" describes an
	// OBI. "about" makes that distinction visible on the first help screen.
	about := surfaceCommand(root, "describe")
	about.Use = "about"
	about.Aliases = []string{"describe"}
	about.Short = "Show ob's identity, version, and supported spec range"
	about.Long = about.Short

	disableSurfaceHandlers(root)
	return root
}

func surfaceCommand(root *cobra.Command, path ...string) *cobra.Command {
	current := root
	for _, name := range path {
		var next *cobra.Command
		for _, candidate := range current.Commands() {
			if candidate.Name() == name {
				next = candidate
				break
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

func disableSurfaceHandlers(command *cobra.Command) {
	command.Run = nil
	command.PreRun = nil
	command.PreRunE = nil
	command.PostRun = nil
	command.PostRunE = nil
	command.PersistentPreRun = nil
	command.PersistentPreRunE = nil
	command.PersistentPostRun = nil
	command.PersistentPostRunE = nil
	if len(command.Commands()) == 0 && command.Parent() != nil {
		command.Annotations = map[string]string{"surface-preview": "placeholder"}
		command.RunE = func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("%s is a command-surface placeholder; no operation ran", cmd.CommandPath())
		}
	} else {
		command.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
	for _, child := range command.Commands() {
		disableSurfaceHandlers(child)
	}
}
