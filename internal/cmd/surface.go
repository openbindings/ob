package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

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
"ob <command> --help". Use --sample-output on selected commands to see
illustrative results. Samples and placeholders never read or write OBIs,
call services, or change configuration.

Proposed output rule: readers print a result; -o saves it. Editors update
their input OBI, or write a resulting JSON OBI to -o, and print a summary.
-F changes the presentation of a result or summary, never its content.
JSON is the OBI document format; YAML is a stdout display view for document
producers and cannot be saved with -o. Foreground servers
do not accept -o or -F. Configuration edits update the active environment;
streams use stdout redirection to save their output.`
	root.Example = `  ob synthesize api.yaml -o api.obi.json
  ob show api.obi.json
  ob source add api.obi.json cli.usage.kdl --pull
  ob binding add api.obi.json listPets.http --operation listPets --source api
  ob dependency add api.obi.json billing --operation chargeCard
  ob status api.obi.json
  ob invoke api.obi.json listPets --input '{"limit":10}'`
	root.Version = "surface-lab (preview; commands do not run)"
	root.Args = surfaceRootArgs
	root.SuggestionsMinimumDistance = 2
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if args := cmd.Flags().Args(); cmd == root && len(args) > 0 {
			return surfaceRootArgs(cmd, args)
		}
		return err
	})
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().Lookup("output").Usage = "save a selected result or write a resulting JSON OBI to a file"
	root.PersistentFlags().Lookup("format").Usage = "render the selected result or summary as text, json, or a YAML view"
	var sampleOutput bool
	root.PersistentFlags().BoolVar(&sampleOutput, "sample-output", false, "show an illustrative result without running the command (lab only)")
	_ = root.PersistentFlags().MarkHidden("sample-output")
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
			group.Title = "other tools"
		}
	}
	for name, group := range map[string]string{
		"new": "setup", "resolve": "setup", "synthesize": "setup", "invoke": "setup",
		"show": "explore", "status": "explore", "inspect": "explore", "diff": "explore",
		"compat": "explore", "validate": "explore",
		"source": "authoring", "operation": "authoring", "binding": "authoring",
		"dependency": "authoring", "patch": "authoring", "meta": "authoring", "merge": "authoring", "conform": "authoring", "purify": "authoring",
		"init": "delegates", "environment": "delegates", "context": "delegates",
		"delegate": "delegates", "binding-specs": "delegates",
		"codegen": "serve", "mcp": "serve", "start": "serve",
		"describe": "introspect", "demo": "introspect",
	} {
		if command := surfaceCommand(root, name); command != nil {
			command.GroupID = group
		}
	}

	// Reading a document or one of its parts should be a direct CLI task.
	show := &cobra.Command{
		Use:     "show <obi>",
		Short:   "Show an OBI summary, or its full document with --full",
		Long:    "Show an OBI summary by default. --full selects the complete OBI value; -F changes its presentation, never its content. JSON is the OBI document format, while YAML is a display view. The locator may be a file, URL, exec: reference, or - for stdin.",
		Example: "  ob show api.obi.json\n  ob show api.obi.json --full -F json\n  ob show https://api.example.com -F json",
		Args:    cobra.ExactArgs(1),
		GroupID: "explore",
	}
	show.Flags().Bool("full", false, "show the complete OBI value instead of a summary")
	root.AddCommand(show)
	for _, item := range []struct {
		parent, use, short string
	}{
		{"source", "show <obi> <source>", "Show one source, including its binding specification and provenance"},
		{"operation", "show <obi> <operation>", "Show one operation, its schemas, aliases, and bindings"},
		{"binding", "show <obi> <binding>", "Show one binding, including its selector and transforms"},
	} {
		parent := surfaceCommand(root, item.parent)
		showPart := &cobra.Command{
			Use:   item.use,
			Short: item.short,
			Long:  item.short + ". Use --full for the complete stored value; -F only changes its presentation.",
			Args:  cobra.ExactArgs(2),
		}
		showPart.Flags().Bool("full", false, "show the complete stored value instead of a summary")
		parent.AddCommand(showPart)
	}

	root.AddCommand(newSurfaceDependencyCmd(), newSurfacePatchCmd(), newSurfaceInvokeCmd())
	newOBI := surfaceCommand(root, "new")
	newOBI.Use = "new"
	newOBI.Long = `Create an empty JSON OBI on stdout, or save it with -o.
Use -F yaml only to preview a YAML view on stdout; an OBI artifact written
with -o is JSON. Add operations, sources, bindings, and dependencies with
their command families. For local ob settings, use "ob init" instead.`
	newOBI.Example = `  ob new -o api.obi.json --name "Acme API"
  ob new --name "Acme API" -F yaml
  ob new | ob operation add - listPets`
	newOBI.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --title") {
			return fmt.Errorf("%w; use --name for the OBI display name", err)
		}
		return err
	})
	newOBI.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("ob new takes no positional arguments; use --name <name> for its name and -o <path> to save it")
		}
		if cmd.Flags().Changed("force") && !cmd.Root().PersistentFlags().Changed("output") {
			return fmt.Errorf("--force requires -o to name the file to overwrite")
		}
		return nil
	}
	init := surfaceCommand(root, "init")
	init.Long += `

This initializes local ob configuration; use "ob new" to create an OBI.`
	init.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --name") {
			return fmt.Errorf("%w; use %q with --name and -o to create a named OBI", err,
				surfacePathForContract(cmd.Root(), "ob new"))
		}
		return err
	})
	surfaceCommand(root, "binding").AddCommand(newSurfaceBindingAddCmd(), newSurfaceBindingRemoveCmd())
	surfaceCommand(root, "binding").Short = "Manage and invoke an OBI's bindings"
	surfaceCommand(root, "dependency").Short = "Declare named consumption points (dependencies)"
	surfaceCommand(root, "context").Short = "Manage URL-scoped credentials and binding context"
	surfaceCommand(root, "binding").Long = `Bindings are named realizations of operations through sources.
Use add/remove to author exact binding keys; one operation may have several
bindings, including several through the same source. Use list/show to inspect,
and invoke/preflight for an explicitly selected binding. To check installed
binding-specification support, use "ob binding-specs check <id>".`
	surfaceCommand(root, "operation").Long = `Manage operation contracts. An operation is neutral: it may have bindings,
dependencies, both, or neither. Use binding commands for concrete realizations
and dependency commands for named consumption points.`
	operationAdd := surfaceCommand(root, "operation", "add")
	operationAdd.Long = `Add a protocol-independent operation contract with the given key.
Use flags to set its description, correspondence aliases, tags, and per-value
input/output schemas. The new operation may remain unbound, gain one or more
bindings, become the contract of a dependency, or both. Schema flags accept
inline JSON, @file, or - for stdin.`
	operationAdd.Example = `  ob operation add api.obi.json listPets --output-schema @pet-list.schema.json
  ob operation add app.obi.json chargeCard --description "Charge a payment method"`
	surfaceCommand(root, "source").Short = "Manage sources and binding-specification support"
	surfaceCommand(root, "source").Long = `Manage binding-specification-governed sources on an OBI. A source may carry
or point at an artifact, address a live surface without an artifact, or both.
Use "source add --pull" to register and derive in one action, or "source pull"
later to derive from an already registered source.`

	// The common tracked-source path can be requested as one atomic action.
	sourceAdd := surfaceCommand(root, "source", "add")
	sourceAdd.Use = "add <obi> <source>"
	sourceAdd.Flags().Bool("pull", false, "register and derive this source in one atomic action")
	sourceAdd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --derive") {
			return fmt.Errorf("%w; use --pull to register and derive operations", err)
		}
		if strings.Contains(err.Error(), "unknown flag: --file") {
			return fmt.Errorf("%w; pass the source path as the second argument: %q", err,
				cmd.CommandPath()+" <obi> <source> --pull")
		}
		return err
	})
	sourceAdd.Long = `Register a binding source on an OBI. The source may be an artifact
path, a URL, or a live address. By default this only records the source; use
--pull to register it and derive its operations and bindings in one atomic
action. Without --pull, "ob source pull <obi>" derives later.

Machine callers may instead pass the complete AddSource input with --input;
that form takes no positional arguments and cannot be combined with --pull.`
	sourceAdd.Example = `  ob source add api.obi.json ./openapi.yaml --pull
  ob source add api.obi.json openbindings.mcp@1:https://example.test/mcp
  ob source add --input '{"interface":{"openbindings":"0.2.0","operations":{}},"source":{"bindingSpec":"example.custom@1","location":"https://example.test/live"}}'`
	sourceAdd.Args = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("input") {
			if len(args) != 0 || cmd.Flags().Changed("pull") {
				return fmt.Errorf("--input takes no positional arguments and cannot be combined with --pull")
			}
			return nil
		}
		return cobra.ExactArgs(2)(cmd, args)
	}
	sourcePull := surfaceCommand(root, "source", "pull")
	_ = sourcePull.Flags().MarkHidden("pure")
	sourcePull.Flags().Bool("strip-ob-metadata", false, "strip x-ob authoring metadata from the saved OBI (requires -o)")
	sourcePull.Long = strings.ReplaceAll(sourcePull.Long,
		"Use --pure with -o to write a clean, spec-only copy (x-ob metadata\nstripped) suitable for publishing.",
		"Use --strip-ob-metadata with -o to write a distribution copy without\nx-ob authoring metadata. x- extensions are spec-valid; this is optional.")
	sourcePull.Long = strings.ReplaceAll(sourcePull.Long, "--pure", "--strip-ob-metadata")
	sourcePull.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
			return err
		}
		strip, _ := cmd.Flags().GetBool("strip-ob-metadata")
		pure, _ := cmd.Flags().GetBool("pure")
		if (strip || pure) && !cmd.Root().PersistentFlags().Changed("output") {
			return fmt.Errorf("--strip-ob-metadata requires -o to name the distribution copy")
		}
		return nil
	}

	// These flags express distinct output modes instead of changing the
	// meaning of -F/--format.
	resolve := surfaceCommand(root, "resolve")
	resolve.Flags().Bool("with-origin", false, "include discovery provenance in an envelope")
	resolve.Long = `Resolve an OBI from a URL or host and print the document to stdout.
Use -o to save canonical JSON. -F yaml displays a YAML view on stdout;
--with-origin selects an envelope containing the interface and its origin.
With --with-origin, -o saves a JSON envelope rather than an OBI artifact.`
	invoke := surfaceCommand(root, "operation", "invoke")
	invoke.SetFlagErrorFunc(surfaceInvokeFlagError)
	invoke.Flags().Bool("envelope", false, "collect the operation output into one JSON envelope")
	invoke.Flags().String("context", "", "context for this call as JSON, @file, or - for stdin")
	invoke.Flags().String("configuration", "", "binding-spec configuration as JSON, @file, or - for stdin")
	invoke.Flags().String("input-stream", "", "input values as newline-delimited JSON from @file or - (stdin)")
	invoke.Flags().Int("max-events", 1000, "maximum events to collect with --envelope")
	invoke.Flags().Duration("timeout", 30*time.Second, "maximum collection time with --envelope")
	invoke.Args = surfaceInvokeArgs
	invoke.Long = `Invoke an OBI operation. The default output is one JSON value per
event. --input supplies one JSON value; --input-stream supplies newline-delimited
JSON values where the selected binding supports ongoing input. The selected
binding determines the interaction pattern. --context and --configuration
apply to this call only. --envelope
collects into one JSON object, capped by --max-events and --timeout; the
envelope reports whether collection was truncated. -F changes encoding, not
streaming versus aggregation.
For the shorter path, use "ob invoke" with the same arguments and flags.
Proposed exits: 0 for complete success, 1 for an unsuccessful invocation,
2 for usage or execution failure, and 3 for an incomplete bounded envelope.
A stream can have emitted earlier values before a later failure; diagnostics
go to stderr and the exit code remains nonzero.`
	invoke.Example = `  ob operation invoke api.obi.json listPets --input '{"limit":10}'
  ob operation invoke api.obi.json listPets --context @context.json
  ob operation invoke api.obi.json chat --input-stream -`
	preferenceFlag := surfaceCommand(root, "operation", "bind").Flags().Lookup("preference")
	operationPreference := surfaceInt64Value(0)
	preferenceFlag.Value = &operationPreference
	preferenceFlag.DefValue = "0"
	preferenceFlag.Usage = "signed integer author preference; higher is preferred"
	operationBind := surfaceCommand(root, "operation", "bind")
	operationBind.Long = `Convenience path for attaching a source selector to an existing
operation. Missing values are chosen interactively. This path derives a
binding key from the operation and source and can re-point that binding with
--force. Use "ob binding add <obi> <key>" when choosing an explicit key or
creating multiple bindings through the same source.`
	operationBind.Example = `  ob operation bind api.obi.json
  ob operation bind api.obi.json listPets api '#/paths/~1pets/get'`
	merge := surfaceCommand(root, "merge")
	_ = merge.Flags().MarkHidden("out") // Superseded by the proposed -o rule.
	merge.Long = `Merge changes from a source OBI into a target OBI. The target is
edited in place by default; -o writes the resulting OBI to another path.
The merge report goes to stdout, or to stderr when stdout carries an OBI.`
	merge.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
			return err
		}
		if cmd.Flags().Changed("out") {
			return fmt.Errorf("--out is superseded by -o in this proposal")
		}
		return nil
	}
	codegen := surfaceCommand(root, "codegen")
	codegen.Flags().Bool("envelope", false, "include the language and code in a result envelope")
	codegen.Long = `Generate typed invoker code from an OBI or a source ob can
synthesize. The default result is raw source code. -F changes its encoding;
--envelope selects an object containing both language and code. -o saves the
selected result. Supported languages: typescript and go.`
	codegen.Example = `  ob codegen api.obi.json --lang typescript -o invoker.ts
  ob codegen api.obi.json --lang go --envelope -F json`
	codegen.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		language, _ := cmd.Flags().GetString("lang")
		if language == "" {
			return fmt.Errorf("--lang is required (typescript or go)")
		}
		return nil
	}
	contextGet := surfaceCommand(root, "context", "get")
	contextGet.Short = "Get context for a target URL (secrets masked by default)"
	contextGet.Long = `Get the context selected for a target URL. All formats
mask secret values by default. --reveal-secrets explicitly includes stored
secrets in the selected result, whether rendered as text, JSON, or YAML.
For URL lookup, an exact stored key wins, then the most specific parent path,
then the origin. A key at /warehouse can also apply to /warehouse/items.
Non-URL targets match exactly. The selected value does not identify which
stored key matched; use "ob context list" to inspect stored keys.`
	contextGet.Flags().Bool("reveal-secrets", false, "include stored secret values in the result")
	contextGet.Example = `  ob context get https://api.example.com -F json
  ob context get https://api.example.com --reveal-secrets -F json`
	contextSet := surfaceCommand(root, "context", "set")
	contextSet.Long = `URL scope: an exact stored key wins, then the most specific
parent path, then the origin. A key at /warehouse may also apply to
/warehouse/items. Non-URL targets match exactly. Use a specific target URL
when a credential should have a narrow scope.

` + contextSet.Long
	contextSet.Long = strings.ReplaceAll(contextSet.Long,
		"  ob context set https://api.github.com --bearer-token ghp_xxx\n", "")
	contextSet.Long = strings.ReplaceAll(contextSet.Long,
		"ob context set https://api.stripe.com/openapi.json --api-key sk_live_xxx",
		"ob context set https://api.stripe.com/openapi.json --api-key -")
	contextSet.Long = strings.ReplaceAll(contextSet.Long,
		"\n  ob context set https://api.github.com --from-curl 'curl -H \"Authorization: Bearer ghp_xxx\" https://api.github.com'", "")
	contextSet.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --bearer") {
			return fmt.Errorf("%w; use --bearer-token - to read a token from stdin", err)
		}
		return err
	})
	contextGroup := surfaceCommand(root, "context")
	contextGroup.Long = strings.ReplaceAll(contextGroup.Long,
		"ob context set https://api.stripe.com/openapi.json --bearer-token\n",
		"ob context set https://api.stripe.com/openapi.json --bearer-token -\n")
	delegateRegister := surfaceCommand(root, "delegate", "register")
	delegateRegister.Long = strings.ReplaceAll(delegateRegister.Long,
		"-F json | jq .interface | ob delegate register",
		"-F json | ob delegate register")
	delegateRequirements := surfaceCommand(root, "delegate", "requirements")
	delegateRequirements.Short = "Show the single accepted interface for a delegate role"
	delegateRequirements.Long = `Print the single interface accepted for a delegate role.
This is ob's enrollment policy, not a Core dependency rule. When the role
allows multiple alternatives, use "ob delegate roles" instead of reducing
the set to one interface. -o saves canonical JSON; -F yaml is a stdout-only
display view.

Examples:
  ob delegate requirements invoke -o required.obi.json
  ob compat required.obi.json my-tool.obi.json`
	inspect := surfaceCommand(root, "inspect")
	inspect.Long = strings.ReplaceAll(inspect.Long,
		`"location":"api.yaml"`, `"location":"https://example.test/api.yaml"`)
	bindingInvoke := surfaceCommand(root, "binding", "invoke")
	bindingInvoke.Flags().Lookup("input").Usage = "binding input JSON value when <obi> and <binding> are given"
	bindingInvoke.Flags().String("request", "", "complete BindingInvocationInput JSON envelope for the machine lane")
	bindingInvoke.Args = func(cmd *cobra.Command, args []string) error {
		request := cmd.Flags().Changed("request")
		input := cmd.Flags().Changed("input")
		switch len(args) {
		case 0:
			if !request || input {
				return fmt.Errorf("the no-argument machine lane requires --request and does not accept --input")
			}
		case 2:
			if request {
				return fmt.Errorf("--request takes no positional arguments; use --input for a binding value")
			}
		default:
			return fmt.Errorf("pass <obi> <binding> with optional --input, or --request with no positional arguments")
		}
		return nil
	}
	bindingInvoke.Long = `Invoke a binding directly, below the operation layer.

With an OBI and binding key, ob resolves that binding and returns its decoded
source result. The binding's input transform still applies. This diagnostic
path does not apply ob's operation-level output transform or schema checks.
Those are ob invocation policies; OpenBindings Core does not define an
invoker or require validation on invocation.

With no positional arguments, --request carries a BindingInvocationInput
envelope and ob writes one JSON result envelope to stdout. This is the
machine lane of ob's binding-invoker interface.

Output contract: the positional form may save its value with -o or render it
with -F. The no-argument machine form requires --request and rejects -o and -F.`
	bindingInvoke.Example = `  ob binding invoke orders.obi.json listOrders.api --input '{"limit":10}'
  ob binding invoke --request '{"source":{"bindingSpec":"example.custom@1","location":"https://example.test/live"},"selector":"orders"}'`
	start := surfaceCommand(root, "start")
	start.Short = "Start ob's local management API and workbench"
	start.Long += "\n\nThis foreground server does not accept -o or -F."
	hideSurfaceInheritedFlagsInHelp(start)
	mcp := surfaceCommand(root, "mcp")
	mcp.Long += "\n\nThis foreground server does not accept -o or -F."
	hideSurfaceInheritedFlagsInHelp(mcp)
	demo := surfaceCommand(root, "demo")
	demo.Short = "Start a demo of one OBI across six protocols"
	demo.Long += "\n\nThis foreground server does not accept -o or -F."
	hideSurfaceInheritedFlagsInHelp(demo)
	compat := surfaceCommand(root, "compat")
	compat.Short = "Check whether one OBI is compatible with another under ob policy"
	compat.Long = strings.ReplaceAll(compat.Long,
		"compatible per the OpenBindings comparison convention:",
		"compatible under ob's comparison policy (not a core spec rule):")
	compat.Long = strings.ReplaceAll(compat.Long, "produce a conformance report.", "produce a compatibility report.")
	compat.Long = strings.ReplaceAll(compat.Long, "Exit code 0 if the summary verdict is compatible, 1 otherwise.",
		"Exit 0 for compatible, 1 for incompatible, 2 for an error, and 3 when compatibility is undetermined.")
	status := surfaceCommand(root, "status")
	status.Long = strings.ReplaceAll(status.Long,
		"Use --exit-code to exit non-zero when any source has drift (a CI gate).",
		"Use --exit-code as a CI gate: exit 0 when no source has drift, 1 when drift\nis present, and 2 for a usage or execution error.")
	status.Long += `

For the structural OBI changes behind drift, use "ob diff <obi> --from-sources".
Status gives a per-source report and is the preview of "ob source pull".`
	diff := surfaceCommand(root, "diff")
	diff.Short = "Show structural differences between OBIs or against sources"
	diff.Long += `

For a per-source drift report or a CI drift gate, use "ob status <obi>".`
	diff.Args = func(cmd *cobra.Command, args []string) error {
		if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
			return err
		}
		fromSources, _ := cmd.Flags().GetBool("from-sources")
		only, _ := cmd.Flags().GetString("only")
		return validateSourceModeArgs(args, fromSources, only)
	}
	diff.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --sources") {
			return fmt.Errorf("%w; use --from-sources to compare an OBI with its registered sources", err)
		}
		if strings.Contains(err.Error(), "unknown flag: --tracked") {
			return fmt.Errorf("%w; use --from-sources to compare an OBI with its registered sources", err)
		}
		return err
	})
	conform := surfaceCommand(root, "conform")
	conform.Long = strings.ReplaceAll(conform.Long,
		"Scaffold or update operations in a target OBI so it corresponds to another interface.",
		"Scaffold or update operations in a target OBI to claim correspondence with another interface.")
	conform.Long = strings.ReplaceAll(conform.Long,
		"If present but incompatible: offers to replace the schema, declaring the\n    correspondence",
		"If incompatible under ob's policy: offers a schema change, declaring\n    the correspondence")
	conform.Long = strings.ReplaceAll(conform.Long,
		"Correspondence is expressed purely through the operation key+alias namespace\n(spec OBI-T-12): an operation corresponds to a contract operation by carrying\nits name as the key or an alias.",
		"An operation claims correspondence with a contract operation by carrying\nits name as a key or alias (OpenBindings §5.1). This assertion alone does not\nestablish compatibility or substitutability.")
	validate := surfaceCommand(root, "validate")
	validate.Long = strings.ReplaceAll(validate.Long,
		"A document declaring a too-new OpenBindings version (a higher major, or\nwhile pre-1.0 a higher minor, than this build supports) is refused in every\nmode, per OBI-T-04.",
		"An unsupported declared OpenBindings version is refused in every mode,\nper OBI-T-04. Support is explicit, not inferred from version ordering.")
	validate.Long = strings.ReplaceAll(validate.Long,
		"With --strict, additionally rejects unknown (non-x-) fields.",
		"With --strict, additionally applies ob's lint policy against unknown (non-x-) fields; a lint failure is distinct from core OBI non-conformance.")
	validate.Long = strings.ReplaceAll(validate.Long, "Exit code 0 if valid, 1 if invalid or an error occurred.",
		"Exit 0 for conformant, 1 for a negative result (non-conformant or requested lint failure), 2 for an error, and 3 when conformance is undetermined. Reports distinguish these outcomes.")
	purify := surfaceCommand(root, "purify")
	purify.Use = "strip-ob-metadata <obi>"
	purify.Aliases = []string{"purify"}
	purify.Short = "Strip ob authoring metadata for distribution"
	purify.Long = `Strip x-ob authoring metadata for distribution. x- extensions
are valid under the spec; this is an optional packaging choice, not a
conformance repair. Other extension fields are preserved. --check writes
nothing, rejects -o, and exits 0 when no x-ob fields are present, 1 when
stripping would change the document, and 2 on an error.`
	purify.Flags().Lookup("check").Usage = "report x-ob locations without writing (exit 1 if any are present)"
	purify.Example = `  ob strip-ob-metadata api.obi.json -o dist/api.obi.json
  ob strip-ob-metadata api.obi.json --check`
	resolve.Long = `Resolve an OBI from a URL or host. The default output is its
complete JSON document; -F yaml renders a stdout-only YAML view. -o saves
canonical JSON. --with-origin selects an envelope containing both the
interface and discovery provenance; saving it with -o writes a JSON envelope,
not an OBI artifact.`
	resolve.Example = `  ob resolve example.com -o service.obi.json
  ob resolve https://example.com -F yaml
  ob resolve example.com --with-origin -F json`

	// "describe" currently describes ob itself, while "show" describes an
	// OBI. "about" makes that distinction visible on the first help screen.
	about := surfaceCommand(root, "describe")
	about.Use = "about"
	about.Aliases = []string{"describe"}
	about.Short = "Show ob's identity, version, and supported spec range"
	about.Long = about.Short
	about.Long += `

For installed binding-specification support, use "ob binding-specs check <id>".`
	rootInvoke := surfaceCommand(root, "invoke")
	rootInvoke.SetFlagErrorFunc(surfaceInvokeFlagError)
	rootInvoke.Long += `

To invoke one binding with a complete BindingInvocationInput request envelope,
use "ob binding invoke --request" instead.`
	rootInvoke.Long += `

Proposed exits: 0 for complete success, 1 for an unsuccessful invocation,
2 for usage or execution failure, and 3 for an incomplete bounded envelope.
A stream may have emitted values before a later failure; diagnostics go to
stderr and the exit code remains nonzero.`
	synthesize := surfaceCommand(root, "synthesize")
	synthesize.Long = strings.ReplaceAll(synthesize.Long,
		"so the document is conformant (OBI-D-05) and works from\nanywhere",
		"rather than putting a relative file path in spec `location` (forbidden\nby OBI-D-05). The OBI can then be used from anywhere")
	synthesize.Long += `

An OBI saved with -o is canonical JSON. -F yaml is a stdout-only display view.`
	synthesize.Long = strings.ReplaceAll(synthesize.Long,
		`"location":"api.yaml"`, `"location":"https://example.test/api.yaml"`)
	bindingSpecs := surfaceCommand(root, "binding-specs")
	bindingSpecs.Long = `List or check binding specifications this ob installation can use.
Identifiers are exact and opaque. This is a local capability report, not a
Core registry or approval of any publisher's binding specification.`
	bindingSpecCheck := surfaceCommand(root, "binding-specs", "check")
	bindingSpecCheck.Short = "Check ob's support for exact binding-specification identifiers"
	bindingSpecCheck.Long = `Check this installation's support for one or more exact, opaque
binding-specification identifiers. Text prints yes/no for each identifier;
-F json returns verdict objects for scripts. An unsupported identifier is a
negative result in the report. The proposed exit code is 1 if any identifier
is unsupported, 0 if all are supported, and 2 on a usage or execution error.`
	bindingSpecCheck.Example = `  ob binding-specs check vendor.widgets@2
  ob binding-specs check vendor.widgets@2 -F json`
	operationAlias := surfaceCommand(root, "operation", "alias")
	operationAlias.Long = `Manage author-asserted correspondence aliases on an operation.
The operation key and aliases form one flat, document-unique namespace.
An adopted name claims correspondence with another operation; the claim
alone does not establish compatibility or substitutability (OpenBindings §5.1).`
	operationAliasList := surfaceCommand(root, "operation", "alias", "list")
	operationAliasList.Short = "List operations' author-asserted correspondence names"
	operationAliasList.Long = `List operation keys and aliases that claim correspondence
with other operation names. With no operation argument, list all operations
carrying aliases; pass an operation to scope the report. This reports names,
not verified compatibility.`
	operationAliasList.Example = `  ob operation alias list api.obi.json
  ob operation alias list api.obi.json listPets`

	// Keep historical names that need a migration path, but present one
	// canonical spelling for each proposed command. Routine abbreviations
	// such as src, op, ls, and rm add paths without adding capability.
	canonicalizeSurfaceHelp(root)
	clearSurfaceAliases(root)
	purify.Aliases = []string{"purify"}
	about.Aliases = []string{"describe"}

	disableSurfaceHandlers(root, &sampleOutput)
	return root
}

func surfaceRootArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	unknown := args[0]
	var target, purpose string
	switch strings.ToLower(unknown) {
	case "obi", "interface":
		return fmt.Errorf("unknown command %q for %q; OBI tasks are direct (try %q or %q); run %q for all commands",
			unknown, cmd.CommandPath(), "ob new", "ob show", "ob --help")
	case "create":
		target, purpose = "ob new", "create an OBI"
	case "export":
		target, purpose = "ob strip-ob-metadata", "make a distribution copy"
	case "auth":
		set := surfacePathForContract(cmd, "ob context set")
		get := surfacePathForContract(cmd, "ob context get")
		return fmt.Errorf("unknown command %q for %q; use %q to set endpoint credentials or %q to inspect selected context",
			unknown, cmd.CommandPath(), set, get)
	case "spec":
		target, purpose = "ob binding-specs check", "check binding-specification support"
	case "binding-spec", "binding-specs", "specs":
		target, purpose = "ob binding-specs check", "check binding-specification support"
	case "bindings":
		binding := surfacePathForContract(cmd, "ob binding add")
		check := surfacePathForContract(cmd, "ob binding-specs check")
		binding = strings.TrimSuffix(binding, " add")
		if binding == "" {
			binding = "ob binding"
		}
		return fmt.Errorf("unknown command %q for %q; use %q for OBI bindings or %q to check installed binding-specification support",
			unknown, cmd.CommandPath(), binding, check)
	case "compare", "replace":
		diff := surfacePathForContract(cmd, "ob diff")
		compat := surfacePathForContract(cmd, "ob compat")
		return fmt.Errorf("unknown command %q for %q; use %q for structural differences or %q for replacement compatibility",
			unknown, cmd.CommandPath(), diff, compat)
	case "consumption-point", "consumption", "consume":
		target, purpose = "ob dependency add", "declare a consumption point"
	case "policy":
		target, purpose = "ob compat", "check replacement compatibility under ob policy"
	case "drift":
		target, purpose = "ob status", "check source drift"
	}
	if target != "" {
		if path := surfacePathForContract(cmd, target); path != "" {
			target = path
		}
		return fmt.Errorf("unknown command %q for %q; use %q to %s", unknown, cmd.CommandPath(), target, purpose)
	}
	if suggestions := cmd.SuggestionsFor(unknown); len(suggestions) > 0 {
		return fmt.Errorf("unknown command %q for %q; did you mean %q? Run %q for all commands",
			unknown, cmd.CommandPath(), "ob "+suggestions[0], "ob --help")
	}
	return fmt.Errorf("unknown command %q for %q; run %q to find a task path", unknown, cmd.CommandPath(), "ob --help")
}

func surfaceGroupArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	unknown := args[0]
	if surfaceContractID(cmd) == "ob binding" && (unknown == "supports" || unknown == "support" || unknown == "check") {
		return fmt.Errorf("unknown command %q for %q; use %q to check installed binding-specification support",
			unknown, cmd.CommandPath(), surfacePathForContract(cmd.Root(), "ob binding-specs check"))
	}
	if surfaceContractID(cmd) == "ob source" && unknown == "attach" {
		return fmt.Errorf("unknown command %q for %q; use %q to register a source (add --pull also derives operations)",
			unknown, cmd.CommandPath(), surfacePathForContract(cmd.Root(), "ob source add"))
	}
	if surfaceContractID(cmd) == "ob source" && unknown == "status" {
		return fmt.Errorf("unknown command %q for %q; use %q for a per-source drift report",
			unknown, cmd.CommandPath(), surfacePathForContract(cmd.Root(), "ob status"))
	}
	if surfaceContractID(cmd) == "ob context" && unknown == "bearer" {
		return fmt.Errorf("unknown command %q for %q; use %q with --bearer-token to set a URL-scoped credential",
			unknown, cmd.CommandPath(), surfacePathForContract(cmd.Root(), "ob context set"))
	}
	if surfaceContractID(cmd) == "ob context" && unknown == "resolve" {
		return fmt.Errorf("unknown command %q for %q; use %q to inspect selected context",
			unknown, cmd.CommandPath(), surfacePathForContract(cmd.Root(), "ob context get"))
	}
	if suggestions := cmd.SuggestionsFor(unknown); len(suggestions) > 0 {
		return fmt.Errorf("unknown command %q for %q; did you mean %q? Run %q for all subcommands",
			unknown, cmd.CommandPath(), cmd.CommandPath()+" "+suggestions[0], cmd.CommandPath()+" --help")
	}
	return fmt.Errorf("unknown command %q for %q; run %q for available subcommands",
		unknown, cmd.CommandPath(), cmd.CommandPath()+" --help")
}

func surfaceInvokeFlagError(cmd *cobra.Command, err error) error {
	if strings.Contains(err.Error(), "unknown flag: --request") {
		return fmt.Errorf("%w; use %q for a complete binding machine request",
			err, surfacePathForContract(cmd.Root(), "ob binding invoke")+" --request")
	}
	return err
}

// SurfacePreflightArgs checks command paths before Cobra parses flags or
// short-circuits on --help. It keeps invalid paths from looking successful.
// Operational arguments and flags remain Cobra's responsibility.
func SurfacePreflightArgs(root *cobra.Command, args []string) error {
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
		if !current.HasAvailableSubCommands() {
			return nil
		}
		if current == root && word == "help" {
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
			if current == root {
				return surfaceRootArgs(root, []string{word})
			}
			return surfaceGroupArgs(current, []string{word})
		}
		current = next
	}
	return nil
}

func surfacePathForContract(root *cobra.Command, id string) string {
	var visit func(*cobra.Command) string
	visit = func(cmd *cobra.Command) string {
		if surfaceContractID(cmd) == id {
			return cmd.CommandPath()
		}
		for _, child := range cmd.Commands() {
			if path := visit(child); path != "" {
				return path
			}
		}
		return ""
	}
	return visit(root)
}

func clearSurfaceAliases(cmd *cobra.Command) {
	cmd.Aliases = nil
	for _, child := range cmd.Commands() {
		clearSurfaceAliases(child)
	}
}

func canonicalizeSurfaceHelp(cmd *cobra.Command) {
	replacements := [][2]string{
		{" (alias 'ob env')", ""},
		{"ob op ", "ob operation "},
		{"ob src ", "ob source "},
		{"ob ctx ", "ob context "},
		{"ob purify", "ob strip-ob-metadata"},
		{"ob operation alias rm", "ob operation alias remove"},
		{"ob operation alias ls", "ob operation alias list"},
		{"ob operation rm", "ob operation remove"},
		{"ob operation mv", "ob operation rename"},
		{"ob operation ls", "ob operation list"},
		{"ob source rm", "ob source remove"},
		{"ob source ls", "ob source list"},
		{"ob context rm", "ob context remove"},
		{"ob context ls", "ob context list"},
		{"ob delegate rm", "ob delegate unregister"},
		{"ob delegate ls", "ob delegate list"},
		{"ob binding ls", "ob binding list"},
	}
	for _, replacement := range replacements {
		cmd.Long = strings.ReplaceAll(cmd.Long, replacement[0], replacement[1])
		cmd.Example = strings.ReplaceAll(cmd.Example, replacement[0], replacement[1])
	}
	cmd.Long = regexp.MustCompile(`ob env\b`).ReplaceAllString(cmd.Long, "ob environment")
	cmd.Example = regexp.MustCompile(`ob env\b`).ReplaceAllString(cmd.Example, "ob environment")
	for _, child := range cmd.Commands() {
		canonicalizeSurfaceHelp(child)
	}
}

func newSurfaceDependencyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "dependency", Short: "Manage named operation dependencies",
		Long: `A dependency names where the described component consumes an operation.
It may constrain acceptable binding specifications, but does not name a
provider or concrete target. An unsatisfied dependency does not invalidate an
OBI.`,
		GroupID: "authoring",
	}
	markCommandGroup(cmd)
	add := &cobra.Command{
		Use: "add <obi> <key>", Short: "Declare a named consumption point",
		Long: `Add a dependency under the given key. --operation names an existing
operation key. Repeat --binding-spec to allow any one of those exact binding
specification identifiers; omit it to leave the family unconstrained.`,
		Example: `  ob dependency add app.obi.json billing --operation chargeCard
  ob dependency add app.obi.json search --operation query --binding-spec openbindings.mcp@1`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return err
			}
			if err := surfaceValidateKey("dependency", args[1]); err != nil {
				return err
			}
			operation, _ := cmd.Flags().GetString("operation")
			if err := surfaceValidateKey("operation", operation); err != nil {
				return err
			}
			ids, _ := cmd.Flags().GetStringArray("binding-spec")
			seen := make(map[string]bool, len(ids))
			for _, id := range ids {
				if id == "" || seen[id] {
					return fmt.Errorf("--binding-spec entries must be non-empty and unique")
				}
				seen[id] = true
			}
			return nil
		},
	}
	add.Flags().String("operation", "", "operation key consumed at this point (required)")
	add.Flags().StringArray("binding-spec", nil, "acceptable binding specification identifier (repeatable; any-of)")
	add.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --accept") {
			return fmt.Errorf("%w; repeat --binding-spec for each acceptable identifier", err)
		}
		return err
	})
	_ = add.MarkFlagRequired("operation")
	cmd.AddCommand(add,
		&cobra.Command{Use: "list <obi>", Short: "List an OBI's dependencies", Args: cobra.ExactArgs(1)},
		&cobra.Command{Use: "remove <obi> <key>", Short: "Remove a named dependency", Args: cobra.ExactArgs(2)},
	)
	show := &cobra.Command{Use: "show <obi> <key>", Short: "Show one dependency", Args: cobra.ExactArgs(2)}
	show.Flags().Bool("full", false, "show the complete stored value instead of a summary")
	cmd.AddCommand(show)
	return cmd
}

func newSurfaceBindingAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "add <obi> <key>", Short: "Add a binding with an explicit key",
		Long: `Add one named realization of an existing operation through an existing
source. The key is chosen by the author; multiple bindings may use the same
operation and source. --selector is interpreted by the source's binding
specification. Use "ob patch" for fields not represented by these flags.`,
		Example: `  ob binding add api.obi.json listPets.http --operation listPets --source api --selector '#/paths/~1pets/get'
  ob binding add api.obi.json listPets.cached --operation listPets --source api --selector '#/paths/~1cached-pets/get' --preference 10`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return err
			}
			if err := surfaceValidateKey("binding", args[1]); err != nil {
				return err
			}
			for _, name := range []string{"operation", "source"} {
				value, _ := cmd.Flags().GetString(name)
				if err := surfaceValidateKey(name, value); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().String("operation", "", "existing operation key (required)")
	cmd.Flags().String("source", "", "existing source key (required)")
	cmd.Flags().String("selector", "", "binding-specification-defined target selector")
	preference := surfaceInt64Value(0)
	cmd.Flags().Var(&preference, "preference", "signed integer author preference; higher is preferred")
	cmd.Flags().String("description", "", "human-readable binding description")
	cmd.Flags().Bool("deprecated", false, "mark this binding as deprecated")
	cmd.Flags().String("input-transform", "", "inline JSONata input transform")
	cmd.Flags().String("output-transform", "", "inline JSONata output transform")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --accept") {
			return fmt.Errorf("%w; use %q with repeated --binding-spec flags to declare a consumption point",
				err, surfacePathForContract(cmd.Root(), "ob dependency add"))
		}
		return err
	})
	_ = cmd.MarkFlagRequired("operation")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}

func newSurfaceBindingRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use: "remove <obi> <key>", Short: "Remove one binding by its key",
		Args: cobra.ExactArgs(2),
	}
}

func newSurfacePatchCmd() *cobra.Command {
	return &cobra.Command{
		Use: "patch <obi> <patch.json|->", Short: "Apply a JSON Patch to any OBI field",
		Long: `Apply an RFC 6902 JSON Patch to an OBI, then validate the resulting
document. This lossless escape hatch covers extensions and less common fields
without adding a dedicated command for every document property. Use - as the
patch path to read patch JSON from stdin. The OBI is edited in place by default;
-o writes the resulting JSON OBI to another path.`,
		Example: `  ob patch api.obi.json changes.patch.json
  ob patch api.obi.json - -o revised.obi.json`,
		Args: cobra.ExactArgs(2), GroupID: "authoring",
	}
}

func newSurfaceInvokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "invoke <obi> [operation]", Short: "Invoke an OBI operation",
		Long: `Invoke an OBI operation, selecting a binding explicitly or by ob's
documented selection policy. The default output is one JSON value per event.
--input supplies one JSON value; --input-stream supplies newline-delimited
JSON values where the selected binding supports ongoing input. The selected
binding determines the interaction pattern. --context and --configuration
apply to this call only. --envelope collects
output into one JSON object, capped by --max-events and --timeout; the
envelope reports whether collection was truncated. Operation-level invocation
is also available as "ob operation invoke".`,
		Example: `  ob invoke api.obi.json listPets --input '{"limit":10}'
  ob invoke api.obi.json listPets --context @context.json --binding listPets.http
  ob invoke chat.obi.json send --input-stream -`,
		Args: surfaceInvokeArgs, GroupID: "setup",
	}
	cmd.Flags().String("binding", "", "binding key to invoke (operation derived from the entry)")
	cmd.Flags().StringArray("select-binding", nil, "ordered binding choice for this and nested operations (repeatable)")
	cmd.Flags().String("input", "", "operation input as JSON, @file, or - for stdin")
	cmd.Flags().String("input-stream", "", "input values as newline-delimited JSON from @file or - (stdin)")
	cmd.Flags().String("context", "", "context for this call as JSON, @file, or - for stdin")
	cmd.Flags().String("configuration", "", "binding-spec configuration as JSON, @file, or - for stdin")
	cmd.Flags().Bool("envelope", false, "collect output into one bounded JSON envelope")
	cmd.Flags().Int("max-events", 1000, "maximum events to collect with --envelope")
	cmd.Flags().Duration("timeout", 30*time.Second, "maximum collection time with --envelope")
	cmd.Flags().BoolP("verbose", "v", false, "show binding choice and timing on stderr")
	return cmd
}

type surfaceInt64Value int64

func (v *surfaceInt64Value) Set(s string) error {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < -9007199254740991 || n > 9007199254740991 {
		return fmt.Errorf("preference must be an integer from -9007199254740991 through 9007199254740991")
	}
	*v = surfaceInt64Value(n)
	return nil
}

func (v *surfaceInt64Value) String() string { return strconv.FormatInt(int64(*v), 10) }
func (v *surfaceInt64Value) Type() string   { return "integer" }

var surfaceKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func surfaceValidateKey(kind, key string) error {
	if !surfaceKeyPattern.MatchString(key) {
		return fmt.Errorf("%s key %q must match [A-Za-z0-9_][A-Za-z0-9_.-]*", kind, key)
	}
	return nil
}

func surfaceInvokeArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s requires an OBI argument; to invoke one binding with a complete BindingInvocationInput envelope, use %q",
			cmd.CommandPath(), surfacePathForContract(cmd.Root(), "ob binding invoke")+" --request")
	}
	if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
		return err
	}
	stdinConsumers := []string{}
	if args[0] == "-" {
		stdinConsumers = append(stdinConsumers, "OBI")
	}
	input, _ := cmd.Flags().GetString("input")
	inputStream, _ := cmd.Flags().GetString("input-stream")
	if cmd.Flags().Changed("input") && cmd.Flags().Changed("input-stream") {
		return fmt.Errorf("choose either --input or --input-stream, not both")
	}
	if cmd.Flags().Changed("input-stream") && inputStream != "-" && !strings.HasPrefix(inputStream, "@") {
		return fmt.Errorf("--input-stream expects @file or - for stdin")
	}
	for _, name := range []string{"context", "configuration"} {
		value, _ := cmd.Flags().GetString(name)
		if value == "-" {
			stdinConsumers = append(stdinConsumers, "--"+name)
		}
	}
	if input == "-" {
		stdinConsumers = append(stdinConsumers, "--input")
	}
	if inputStream == "-" {
		stdinConsumers = append(stdinConsumers, "--input-stream")
	}
	if len(stdinConsumers) > 1 {
		return fmt.Errorf("only one of %s may read stdin", strings.Join(stdinConsumers, ", "))
	}
	envelope, _ := cmd.Flags().GetBool("envelope")
	if !envelope && (cmd.Flags().Changed("max-events") || cmd.Flags().Changed("timeout")) {
		return fmt.Errorf("--max-events and --timeout apply only with --envelope")
	}
	maxEvents, _ := cmd.Flags().GetInt("max-events")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	if maxEvents <= 0 || timeout <= 0 {
		return fmt.Errorf("--max-events and --timeout must be positive")
	}
	return nil
}

func hideSurfaceInheritedFlagsInHelp(cmd *cobra.Command) {
	const inheritedBlock = `{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}`
	cmd.SetUsageTemplate(strings.Replace(cmd.UsageTemplate(), inheritedBlock, "", 1))
}

func surfaceHelpOnlyFormat(cmd *cobra.Command, explanation string) {
	const inheritedBlock = `{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}`
	replacement := "\n\nResult Flags:\n  -F, --format string   " + explanation
	cmd.SetUsageTemplate(strings.Replace(cmd.UsageTemplate(), inheritedBlock, replacement, 1))
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

func disableSurfaceHandlers(command *cobra.Command, sampleOutput *bool) {
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
		contractID := command.CommandPath()
		command.Annotations = map[string]string{
			"surface-preview":     "placeholder",
			"surface-contract-id": contractID,
		}
		switch surfaceContracts[contractID] {
		case surfaceEditConfig:
			surfaceHelpOnlyFormat(command, "render the configuration summary as text, json, or yaml")
		case surfaceStream:
			surfaceHelpOnlyFormat(command, "json events; text or yaml requires --envelope")
		}
		command.RunE = func(cmd *cobra.Command, args []string) error {
			if err := enforceSurfaceOutputContract(cmd, args); err != nil {
				return err
			}
			if *sampleOutput {
				return surfaceSampleOutput(cmd, args)
			}
			return fmt.Errorf("%s is a command-surface placeholder; no operation ran", cmd.CommandPath())
		}
	} else {
		if command.Parent() != nil {
			command.Args = surfaceGroupArgs
		}
		command.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
	for _, child := range command.Commands() {
		disableSurfaceHandlers(child, sampleOutput)
	}
}
