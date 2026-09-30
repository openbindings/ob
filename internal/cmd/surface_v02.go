package cmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// NewV02SurfaceRoot is a standalone, inert command-surface proposal for the
// unreleased OpenBindings 0.2 draft. It does not copy production NewRoot.
// Variants are separate candidate trees, never aliases within one tree.
func NewV02SurfaceRoot(variant string) *cobra.Command {
	if variant != "action" && variant != "capability" && variant != "binding-invoke" && variant != "invoke-root" && variant != "init" && variant != "new" && variant != "read-direct" && variant != "read-summary" && variant != "plain-workflows" && variant != "idempotent-value" && variant != "task-language" {
		variant = "object"
	}
	root := &cobra.Command{
		Use:           "ob",
		Short:         "Explore the OpenBindings 0.2 command surface",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `COMMAND-SURFACE PREVIEW — no operational command runs.

An OBI is a JSON document. Core editing commands preserve exact kind strings
and arbitrary source and binding content. Installed kind capabilities, source
import, invocation, and comparison are separate ob/tool concerns. Use
--sample-output for an illustrative result; samples never read or write a
locator. This preview is pinned to the unreleased OpenBindings 0.2 draft.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return v02UnknownPath(cmd, args[0])
			}
			return nil
		},
	}
	root.PersistentFlags().StringP("output", "o", "", "save a result or a resulting JSON OBI (preview never writes)")
	root.PersistentFlags().VarP(&v02OnceString{}, "format", "F", "render a result as text, json, or a YAML display view")
	root.PersistentFlags().Bool("sample-output", false, "show an illustrative result without operating on a locator")
	_ = root.PersistentFlags().MarkHidden("sample-output")
	root.CompletionOptions.DisableDefaultCmd = true
	root.Version = "0.2-surface-preview (no operations run)"
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return err })

	for _, group := range []*cobra.Group{
		{ID: "start", Title: "start and inspect"},
		{ID: "author", Title: "author and edit"},
		{ID: "operate", Title: "installed capabilities and ob policies"},
	} {
		root.AddGroup(group)
	}
	kindCommand := v02KindCommand()
	newCommand := v02InitCommand()
	if variant == "new" {
		newCommand = v02NewCommand()
	}
	if variant == "capability" {
		kindCommand.Use = "capability"
		kindCommand.Short = "Inspect this installation's abilities for exact OBI kinds"
	}
	root.AddCommand(
		newCommand, v02ShowCommand(), v02ValidateCommand(),
		v02PatchCommand(), v02DiffCommand(), kindCommand,
	)
	for _, item := range []struct {
		name, short string
		children    []*cobra.Command
	}{
		{"source", "Manage OBI sources, each with an exact kind and optional content", v02SourceCommands()},
		{"binding", "Manage named realizations of operations through sources", v02BindingCommands()},
		{"dependency", "Manage named operation consumption points", v02DependencyCommands()},
		{"operation", "Manage protocol-independent, per-value contracts", v02OperationCommands(variant == "idempotent-value")},
		{"schema", "Manage reusable JSON Schemas", v02SchemaCommands()},
	} {
		group := v02Group(item.name, item.short)
		group.GroupID = "author"
		group.AddCommand(item.children...)
		root.AddCommand(group)
	}
	if variant == "action" {
		add := v02Group("add", "Add a core OBI entry")
		add.GroupID = "author"
		for _, noun := range []string{"operation", "source", "binding", "dependency", "schema"} {
			parent, _, _ := root.Find([]string{noun})
			child, _, _ := parent.Find([]string{"add"})
			parent.RemoveCommand(child)
			child.Use = strings.Replace(child.Use, "add", noun, 1)
			add.AddCommand(child)
		}
		root.AddCommand(add)
	}
	if variant != "binding-invoke" {
		binding, _, _ := root.Find([]string{"binding"})
		invoke, _, _ := binding.Find([]string{"invoke"})
		binding.RemoveCommand(invoke)
		invoke.GroupID = "operate"
		root.AddCommand(invoke)
	}
	if variant != "read-summary" {
		v02ConfigureDirectReads(root)
	}
	if variant == "plain-workflows" {
		v02ConfigurePlainWorkflows(root)
	}
	if variant == "task-language" {
		v02ConfigureTaskLanguage(root)
	}
	v02SetExamples(root, variant)
	return root
}

func v02ConfigureTaskLanguage(root *cobra.Command) {
	root.Annotations = map[string]string{"surface-variant": "task-language"}
	document := v02Group("document", "Create, read, validate, compare, and patch an OBI")
	document.GroupID = "start"
	for _, name := range []string{"init", "show", "validate", "diff", "patch"} {
		command, _, _ := root.Find([]string{name})
		root.RemoveCommand(command)
		command.GroupID = ""
		document.AddCommand(command)
	}
	root.AddCommand(document)
	for _, item := range []struct{ old, new, short string }{
		{"binding", "realization", "Manage 0.2 bindings: concrete realizations through sources"},
		{"dependency", "consumption", "Manage 0.2 dependencies: named consumption points"},
		{"kind", "handler", "Inspect local abilities for exact source kind tokens"},
	} {
		command, _, _ := root.Find([]string{item.old})
		root.RemoveCommand(command)
		command.Use = item.new
		command.Short = item.short
		root.AddCommand(command)
	}
	realization, _, _ := root.Find([]string{"realization"})
	invoke, _, _ := root.Find([]string{"invoke"})
	root.RemoveCommand(invoke)
	invoke.GroupID = ""
	realization.AddCommand(invoke)
	root.Long += `

This task-language proposal calls 0.2 bindings "realizations" and
dependencies "consumption points". A handler is a locally installed ob
ability for an exact source kind; it is not a Core registry.`
}

func v02ConfigurePlainWorkflows(root *cobra.Command) {
	source, _, _ := root.Find([]string{"source"})
	for _, item := range []struct{ old, new, short string }{
		{"import", "acquire", "Create a source from an input using an installed kind handler"},
		{"inspect", "interpret", "Get a kind-specific report about a source"},
		{"pull", "refresh", "Update source content through its installed kind handler"},
		{"synthesize", "propose", "Draft or apply entries suggested by a source"},
	} {
		cmd, _, _ := source.Find([]string{item.old})
		source.RemoveCommand(cmd)
		cmd.Use = strings.Replace(cmd.Use, item.old, item.new, 1)
		cmd.Short = item.short
		source.AddCommand(cmd)
	}
	root.Long = strings.Replace(root.Long, "source\nimport", "source\nacquisition", 1)
}

// v02OnceString rejects conflicting repeated formats instead of silently
// accepting whichever -F/--format spelling happened to come last.
type v02OnceString struct {
	value string
	set   bool
}

func (f *v02OnceString) Set(value string) error {
	if f.set {
		return fmt.Errorf("-F/--format may be given only once")
	}
	f.value, f.set = value, true
	return nil
}

func (f *v02OnceString) String() string { return f.value }
func (f *v02OnceString) Type() string   { return "string" }

func v02Group(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("%s needs a subcommand; run %q", cmd.CommandPath(), cmd.CommandPath()+" --help")
	}, Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return v02UnknownPath(cmd, args[0])
		}
		return fmt.Errorf("%s needs a subcommand; run %q", cmd.CommandPath(), cmd.CommandPath()+" --help")
	}}
}

func v02Leaf(id, use, short, long string, args cobra.PositionalArgs) *cobra.Command {
	cmd := &cobra.Command{
		Use: use, Short: short, Long: long, Args: args,
		Annotations: map[string]string{"surface-id": id},
		RunE:        v02Run,
	}
	return cmd
}

func v02Run(cmd *cobra.Command, args []string) error {
	if err := v02ValidateShape(cmd, args); err != nil {
		return err
	}
	format, err := v02OutputFormat(cmd)
	if err != nil {
		return err
	}
	if format != "" && format != "text" && format != "json" && format != "yaml" {
		return fmt.Errorf("-F must be text, json, or yaml")
	}
	if cmd.Annotations["surface-id"] == "binding.invoke" {
		if cmd.Root().PersistentFlags().Changed("output") || cmd.Root().PersistentFlags().Changed("format") {
			return fmt.Errorf("%s streams NDJSON envelopes; -o and -F do not apply", cmd.CommandPath())
		}
		if cmd.Flags().Changed("input") && cmd.Flags().Changed("input-stream") {
			return fmt.Errorf("--input and --input-stream are mutually exclusive")
		}
		if cmd.Flags().Changed("input-stream") {
			stream, _ := cmd.Flags().GetString("input-stream")
			if stream != "-" {
				return fmt.Errorf("--input-stream currently accepts only - for NDJSON stdin")
			}
		}
		if cmd.Flags().Changed("context") {
			context, _ := cmd.Flags().GetString("context")
			if context == "" {
				return fmt.Errorf("--context must name a local ob context")
			}
		}
	}
	if cmd.Annotations["surface-id"] == "diff" && format == "yaml" {
		return fmt.Errorf("ob diff supports text or json reports; -F yaml does not apply")
	}
	id := cmd.Annotations["surface-id"]
	contract := v02Contracts[id]
	writesOBI := contract.Result == "obi"
	if id == "source.synthesize" {
		apply, _ := cmd.Flags().GetBool("apply")
		writesOBI = apply
	}
	if writesOBI {
		if format == "text" {
			return fmt.Errorf("%s produces a JSON OBI; use -F json or a YAML stdout view", cmd.CommandPath())
		}
		if format == "yaml" && cmd.Root().PersistentFlags().Changed("output") {
			return fmt.Errorf("-F yaml is a stdout view; -o writes canonical JSON, so choose one")
		}
	}
	if id == "source.synthesize" && !writesOBI && format == "text" {
		return fmt.Errorf("%s proposes a JSON Patch; use -F json or a YAML stdout view", cmd.CommandPath())
	}
	if id == "validate" {
		quiet, _ := cmd.Flags().GetBool("quiet")
		if quiet && (cmd.Root().PersistentFlags().Changed("output") || cmd.Root().PersistentFlags().Changed("format")) {
			return fmt.Errorf("validate --quiet emits no report; -o and -F do not apply")
		}
	}
	stdin := 0
	for _, arg := range args {
		if arg == "-" {
			stdin++
		}
	}
	for _, flag := range []string{"content", "input-schema", "output-schema", "value", "input"} {
		if f := cmd.Flags().Lookup(flag); f != nil && f.Changed {
			value, _ := cmd.Flags().GetString(flag)
			if value == "-" {
				stdin++
			}
		}
	}
	if f := cmd.Flags().Lookup("input-stream"); f != nil && f.Changed {
		stream, _ := cmd.Flags().GetString("input-stream")
		if stream == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return fmt.Errorf("stdin (-) can supply only one input per command")
	}
	sample, _ := cmd.Flags().GetBool("sample-output")
	if sample {
		return v02SampleOutput(cmd, args)
	}
	return fmt.Errorf("command-surface placeholder; no operation ran (%s)", cmd.CommandPath())
}

func v02OutputFormat(cmd *cobra.Command) (string, error) {
	format, _ := cmd.Flags().GetString("format")
	if f := cmd.Flags().Lookup("json"); f != nil {
		jsonMode, _ := cmd.Flags().GetBool("json")
		if jsonMode {
			if cmd.Root().PersistentFlags().Changed("format") {
				return "", fmt.Errorf("--json and -F cannot be combined; choose one output format")
			}
			return "json", nil
		}
	}
	return format, nil
}

func v02ConfigureDirectReads(root *cobra.Command) {
	oldShow, _, _ := root.Find([]string{"show"})
	root.RemoveCommand(oldShow)
	show := v02Leaf("show", "show <obi>", "Show the complete stored OBI", `Show the complete stored JSON OBI by default, preserving exact members
and absent-versus-null values. --summary selects a short human overview.
--json selects a machine JSON rendering. -F yaml is a display view, not
the normative OBI serialization.`, cobra.ExactArgs(1))
	show.GroupID = "start"
	show.Flags().Bool("summary", false, "show a short overview instead of the complete OBI")
	root.AddCommand(show)
	for _, noun := range []string{"source", "binding", "dependency", "operation", "schema"} {
		group, _, _ := root.Find([]string{noun})
		old, _, _ := group.Find([]string{"show"})
		group.RemoveCommand(old)
		lookup := "by key"
		if noun == "operation" {
			lookup = "by key or alias"
		}
		child := v02Leaf(old.Annotations["surface-id"], old.Use, "Show the complete stored "+noun, "Show the exact stored value "+lookup+`, preserving
all Core members, x- extensions, and absent-versus-null distinctions.`, cobra.ExactArgs(2))
		group.AddCommand(child)
	}
	source, _, _ := root.Find([]string{"source"})
	sourceList, _, _ := source.Find([]string{"list"})
	sourceList.Long = `List source keys and exact kinds without interpreting source content.
--full returns the stored source map with complete values, including whether
content is absent or explicitly null.`
	sourceList.Flags().Bool("full", false, "include each complete stored source value")
	sourceInspect, _, _ := source.Find([]string{"inspect"})
	sourceInspect.Long = strings.Replace(sourceInspect.Long, "source show --full", "source show", 1)
	var addJSON func(*cobra.Command)
	addJSON = func(command *cobra.Command) {
		if id := command.Annotations["surface-id"]; id != "" {
			result := v02Contracts[id].Result
			if result == "report" || result == "report-or-obi" || result == "verdict-report" || result == "structural-report" || result == "handler-report" || result == "local-capability-report" || result == "patch-or-obi" {
				command.Flags().Bool("json", false, "render this report as machine JSON")
			}
		}
		for _, child := range command.Commands() {
			addJSON(child)
		}
	}
	addJSON(root)
}

func v02ValidateShape(cmd *cobra.Command, args []string) error {
	for _, flag := range []string{"content", "input-schema", "output-schema", "value", "input"} {
		if f := cmd.Flags().Lookup(flag); f != nil && f.Changed {
			s, _ := cmd.Flags().GetString(flag)
			if err := v02JSONValueSyntax(s); err != nil {
				return fmt.Errorf("--%s: %w", flag, err)
			}
			if flag == "input-schema" || flag == "output-schema" || flag == "value" && cmd.Annotations["surface-id"] == "schema.add" {
				if !v02IsExternalValue(s) {
					var value any
					_ = json.Unmarshal([]byte(s), &value)
					if _, isObject := value.(map[string]any); !isObject {
						if _, isBool := value.(bool); !isBool {
							return fmt.Errorf("--%s must be a JSON Schema object or boolean", flag)
						}
					}
				}
			}
		}
	}
	return nil
}

func v02JSONValueSyntax(s string) error {
	if v02IsExternalValue(s) {
		if s == "@" {
			return fmt.Errorf("@ must be followed by a file path")
		}
		return nil
	}
	if !json.Valid([]byte(s)) {
		return fmt.Errorf("expected one JSON value, @file, or - for stdin")
	}
	return nil
}

func v02IsExternalValue(s string) bool { return s == "-" || strings.HasPrefix(s, "@") }

func v02RequiredFlag(cmd *cobra.Command, name string) error {
	f := cmd.Flags().Lookup(name)
	if f == nil || !f.Changed {
		return fmt.Errorf("--%s is required", name)
	}
	return nil
}

var v02NamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func v02CoreName(kind, value string) error {
	if !v02NamePattern.MatchString(value) || strings.Contains(value, "\n") {
		return fmt.Errorf("%s %q must match ^[A-Za-z0-9_][A-Za-z0-9_.-]*$", kind, value)
	}
	return nil
}

func v02Preference(cmd *cobra.Command) error {
	f := cmd.Flags().Lookup("preference")
	if f == nil || !f.Changed {
		return nil
	}
	s, _ := cmd.Flags().GetString("preference")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < -9007199254740991 || n > 9007199254740991 {
		return fmt.Errorf("--preference must be an integer from -9007199254740991 through 9007199254740991")
	}
	return nil
}

func v02UnknownPath(current *cobra.Command, word string) error {
	root := current.Root()
	path := current.CommandPath()
	if current == root {
		if root.Annotations["surface-variant"] == "task-language" {
			switch word {
			case "obi", "interface", "create", "documents":
				return fmt.Errorf("unknown command %q; use %q to create an OBI or %q to inspect it", word, "ob document init", "ob document show")
			case "binding", "bindings", "realizations":
				return fmt.Errorf("unknown command %q; 0.2 bindings are managed with %q", word, "ob realization")
			case "dependency", "dependencies", "consume", "consumptions":
				return fmt.Errorf("unknown command %q; 0.2 dependencies are managed with %q", word, "ob consumption")
			case "kind", "kinds", "handlers", "capability", "capabilities":
				return fmt.Errorf("unknown command %q; local abilities for exact kinds are under %q", word, "ob handler")
			case "sources":
				return fmt.Errorf("unknown command %q; use %q to manage sources", word, "ob source")
			case "operations":
				return fmt.Errorf("unknown command %q; use %q to manage operations", word, "ob operation")
			case "schemas":
				return fmt.Errorf("unknown command %q; use %q to manage schemas", word, "ob schema")
			}
		}
		switch word {
		case "obi", "interface":
			return fmt.Errorf("unknown command %q; OBI tasks are direct: try %q or %q", word, "ob init", "ob show")
		case "create":
			first := "ob new"
			if child, _, _ := root.Find([]string{"init"}); child != root {
				first = "ob init"
			}
			return fmt.Errorf("unknown command %q; use %q to create an empty OBI", word, first)
		case "bindings", "realizations":
			return fmt.Errorf("unknown command %q; use %q for 0.2 bindings", word, "ob binding")
		case "dependencies", "consumptions":
			return fmt.Errorf("unknown command %q; use %q for named consumption points", word, "ob dependency")
		case "sources":
			return fmt.Errorf("unknown command %q; use %q to manage sources", word, "ob source")
		case "operations":
			return fmt.Errorf("unknown command %q; use %q to manage operations", word, "ob operation")
		case "schemas":
			return fmt.Errorf("unknown command %q; use %q to manage schemas", word, "ob schema")
		case "kinds", "handlers", "capability", "capabilities":
			return fmt.Errorf("unknown command %q; use %q for local kind abilities", word, "ob kind")
		case "binding-spec", "binding-specs", "spec":
			return fmt.Errorf("unknown command %q; use %q to inspect installed kind capabilities", word, "ob kind")
		case "consume", "consumption", "consumption-point":
			return fmt.Errorf("unknown command %q; use %q for named consumption points", word, "ob dependency")
		case "document":
			return fmt.Errorf("unknown command %q; OBI tasks are direct: try %q or %q", word, "ob init", "ob show")
		case "realization":
			return fmt.Errorf("unknown command %q; 0.2 realizations are declared with %q", word, "ob binding")
		case "handler":
			return fmt.Errorf("unknown command %q; local kind abilities are under %q", word, "ob kind")
		}
	}
	return fmt.Errorf("unknown command %q for %q; run %q for available commands", word, path, path+" --help")
}

// V02PreflightArgs catches invalid command paths before Cobra short-circuits
// on --help or reports a flag error for a command that does not exist.
func V02PreflightArgs(root *cobra.Command, args []string) error {
	current := root
	for i := 0; i < len(args); i++ {
		word := args[i]
		if word == "--" {
			return nil
		}
		switch word {
		case "-o", "--output", "-F", "--format":
			i++
			continue
		}
		if strings.HasPrefix(word, "-") {
			continue
		}
		if !current.HasAvailableSubCommands() || current == root && word == "help" {
			return nil
		}
		var next *cobra.Command
		for _, child := range current.Commands() {
			if child.Name() == word || child.HasAlias(word) {
				next = child
				break
			}
		}
		if next == nil {
			return v02UnknownPath(current, word)
		}
		current = next
	}
	return nil
}

func v02SetExamples(root *cobra.Command, variant string) {
	if variant == "task-language" {
		root.Example = `  ob document init api.obi.json --name "Acme API"
  ob source add api.obi.json api --kind my-team.http@1 --content '{"address":"https://api.example.test"}'
  ob realization add api.obi.json get.http --operation get --source api --content '{"target":"/items"}'
  ob document validate api.obi.json --json`
	} else if variant == "new" {
		root.Example = `  ob new --name "Acme API" -o api.obi.json
  ob source add api.obi.json api --kind my-team.http@1 --content '{"address":"https://api.example.test"}'
  ob binding add api.obi.json get.http --operation get --source api --content '{"target":"/items"}'
  ob validate api.obi.json`
	} else if variant != "action" {
		root.Example = `  ob init api.obi.json --name "Acme API"
  ob source add api.obi.json api --kind my-team.http@1 --content '{"address":"https://api.example.test"}'
  ob binding add api.obi.json get.http --operation get --source api --content '{"target":"/items"}'
  ob validate api.obi.json`
		if variant != "read-summary" {
			root.Example += "\n  ob show api.obi.json --json"
		}
	} else if variant == "action" {
		root.Example = `  ob init api.obi.json --name "Acme API"
  ob add source api.obi.json api --kind my-team.http@1 --content '{"address":"https://api.example.test"}'
  ob add binding api.obi.json get.http --operation get --source api --content '{"target":"/items"}'
  ob validate api.obi.json`
	}
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if id := cmd.Annotations["surface-id"]; id != "" {
			if variant == "task-language" && id == "new" {
				cmd.Example = `  ob document init api.obi.json --name "Acme API"`
			} else if variant == "task-language" && id == "show" {
				cmd.Example = `  ob document show api.obi.json --json`
			} else if variant == "task-language" && id == "validate" {
				cmd.Example = `  ob document validate api.obi.json --json`
			} else if variant == "task-language" && id == "diff" {
				cmd.Example = `  ob document diff before.obi.json after.obi.json --json`
			} else if variant == "task-language" && id == "patch" {
				cmd.Example = `  ob document patch api.obi.json changes.patch.json`
			} else if variant != "read-summary" && id == "show" {
				cmd.Example = `  ob show api.obi.json --json`
			} else if variant != "read-summary" && id == "validate" {
				cmd.Example = `  ob validate api.obi.json --json`
			} else if variant != "read-summary" && id == "diff" {
				cmd.Example = `  ob diff before.obi.json after.obi.json --json`
			} else if variant != "read-summary" && strings.HasSuffix(id, ".show") {
				cmd.Example = `  ` + cmd.CommandPath() + ` api.obi.json example --json`
			} else if variant != "read-summary" && id == "source.list" {
				cmd.Example = `  ob source list api.obi.json --full --json`
			} else {
				cmd.Example = v02Example(id, cmd.CommandPath())
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func v02Example(id, path string) string {
	switch id {
	case "new":
		if strings.HasPrefix(path, "ob init") {
			return `  ob init api.obi.json --name "Acme API"`
		}
		return `  ob new --name "Acme API" -o api.obi.json`
	case "source.add":
		return `  ` + path + ` api.obi.json api --kind my-team.http@1 --content '{"address":"https://api.example.test"}'`
	case "binding.add":
		return `  ` + path + ` api.obi.json get.http --operation get --source api --content '{"target":"/items"}'`
	case "dependency.add":
		return `  ` + path + ` api.obi.json billing --operation charge --kind my-team.http@1 --kind private.rpc@1`
	case "operation.add":
		return `  ` + path + ` api.obi.json get --output-schema '{"type":"object"}'`
	case "schema.add":
		return `  ` + path + ` api.obi.json Item --value '{"type":"object"}'`
	case "show":
		return `  ob show api.obi.json --full -F json`
	case "validate":
		return `  ob validate api.obi.json -F json`
	case "patch":
		return `  ob patch api.obi.json changes.patch.json`
	case "kind.check":
		return `  ` + path + ` my-team.http@1 --action invoke`
	case "source.import":
		return `  ` + path + ` api.obi.json api ./api.yaml --kind example.openapi@1`
	case "source.inspect":
		return `  ` + path + ` api.obi.json api`
	case "source.pull":
		return `  ` + path + ` api.obi.json api`
	case "source.synthesize":
		return `  ` + path + ` api.obi.json api`
	case "binding.invoke":
		return `  ` + path + ` api.obi.json get.http --input '{"id":1}'`
	case "diff":
		return `  ob diff before.obi.json after.obi.json -F json`
	}
	return ""
}
