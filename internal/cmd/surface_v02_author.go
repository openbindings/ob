package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func v02NewCommand() *cobra.Command {
	cmd := v02Leaf("new", "new", "Create an empty 0.2 OBI", `Create an empty OpenBindings 0.2 JSON document. Print it to stdout or
save canonical JSON with -o. The document has an operations map and may have
human-readable name, version label, and description. -F yaml is a display
view on stdout, not an OBI serialization.`, func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("new takes no positional path; use -o <path> to save the OBI")
		}
		force, _ := cmd.Flags().GetBool("force")
		if force && !cmd.Root().PersistentFlags().Changed("output") {
			return fmt.Errorf("--force requires -o <path>")
		}
		return nil
	})
	cmd.GroupID = "start"
	cmd.Flags().String("name", "", "human-readable name (not a document identity)")
	cmd.Flags().String("version", "", "opaque, non-empty interface-version label")
	cmd.Flags().String("description", "", "human-readable description")
	cmd.Flags().Bool("force", false, "overwrite the file named by -o when implemented")
	return cmd
}

func v02InitCommand() *cobra.Command {
	cmd := v02Leaf("new", "init [<obi|->]", "Create an empty 0.2 OBI at a path or stdout", `Create a minimal OpenBindings 0.2 JSON document. Supply a positional
path or -o to save it, or omit both (or use -) for stdout. A positional
path and -o cannot be combined. --interface-version is the
opaque author label in the OBI's version member, not the OpenBindings
specification version. This command always creates a 0.2.0 document.
--version is deliberately rejected because it confuses those meanings.
No path is read or written in this preview.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
			return err
		}
		if cmd.Flags().Changed("version") {
			return fmt.Errorf("--version is ambiguous here; the OpenBindings spec is fixed at 0.2.0, and the OBI label uses --interface-version")
		}
		output, _ := cmd.Flags().GetString("output")
		if len(args) == 1 && cmd.Root().PersistentFlags().Changed("output") {
			return fmt.Errorf("choose a positional destination or -o, not both")
		}
		destination := "-"
		if len(args) == 1 {
			destination = args[0]
		} else if cmd.Root().PersistentFlags().Changed("output") {
			destination = output
		}
		if destination == "" {
			return fmt.Errorf("-o needs a non-empty destination or - for stdout")
		}
		force, _ := cmd.Flags().GetBool("force")
		if force && destination == "-" {
			return fmt.Errorf("--force applies to a file destination, not stdout (-)")
		}
		format, _ := cmd.Flags().GetString("format")
		if format == "yaml" && destination != "-" {
			return fmt.Errorf("-F yaml is a stdout view; use - as the destination or omit -F yaml")
		}
		if cmd.Flags().Changed("interface-version") {
			value, _ := cmd.Flags().GetString("interface-version")
			if value == "" {
				return fmt.Errorf("--interface-version must be non-empty when present")
			}
		}
		return nil
	})
	cmd.GroupID = "start"
	cmd.Flags().String("name", "", "human-readable name (not document identity)")
	cmd.Flags().String("interface-version", "", "opaque, non-empty OBI version label")
	cmd.Flags().String("version", "", "ambiguous spelling; use --interface-version for the OBI label")
	_ = cmd.Flags().MarkHidden("version")
	cmd.Flags().String("description", "", "human-readable description")
	cmd.Flags().Bool("force", false, "overwrite the destination when implemented")
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), cmd.Long)
		fmt.Fprintf(cmd.OutOrStdout(), "\nUsage:\n  %s\n", cmd.UseLine())
		fmt.Fprintf(cmd.OutOrStdout(), "\nExamples:\n%s\n", cmd.Example)
		fmt.Fprintf(cmd.OutOrStdout(), "\nFlags:\n%s", cmd.NonInheritedFlags().FlagUsages())
		fmt.Fprintln(cmd.OutOrStdout(), "\nOutput:\n  -o, --output string   alternative destination; omit for stdout\n  -F, --format string   json or yaml; yaml requires stdout")
	})
	return cmd
}

func v02ShowCommand() *cobra.Command {
	cmd := v02Leaf("show", "show <obi>", "Show an OBI summary or complete JSON document", `Show a document summary by default. --full selects the complete stored
OBI. JSON is the normative OBI representation; -F yaml is only a display
view. Unknown kinds may be shown without interpreting their content.`, cobra.ExactArgs(1))
	cmd.GroupID = "start"
	cmd.Flags().Bool("full", false, "show the complete stored OBI")
	return cmd
}

func v02ValidateCommand() *cobra.Command {
	cmd := v02Leaf("validate", "validate <obi>", "Validate a document against OpenBindings 0.2", `Check the applicable 0.2 document rules and report what was established.
Unknown non-x- fields in Core-defined objects violate OBI-D-02. An unknown
source kind does not make a document non-conformant. After the document is
obtained, deciding document conformance never requires network retrieval.

The overall conclusion is conformant only after every applicable rule was
checked without a violation; non-conformant when any violation is known; or
conformance undetermined when no violation is known but some rules remain
inconclusive. A declared unsupported specification version is refused, not
judged under a different version. The report identifies rule-level evidence
and distinguishes version refusal from non-conformance. This preview does
not validate a supplied locator.`, cobra.ExactArgs(1))
	cmd.GroupID = "start"
	cmd.Flags().Bool("quiet", false, "suppress result output while retaining the proposed verdict exit")
	return cmd
}

func v02PatchCommand() *cobra.Command {
	cmd := v02Leaf("patch", "patch <obi> <patch.json|->", "Apply a JSON Patch to any OBI field", `Apply an RFC 6902 JSON Patch, then validate the resulting OBI under its
declared specification version. This is a lossless path for less common Core
fields and x- extensions. A patch supplied as - would read stdin in an
implemented CLI.`, cobra.ExactArgs(2))
	cmd.GroupID = "author"
	return cmd
}

func v02DiffCommand() *cobra.Command {
	cmd := v02Leaf("diff", "diff <before.obi.json> <after.obi.json>", "Report exact OBI document changes", `Compare two stored OBI JSON documents structurally. The result lists
changed JSON paths and values; it makes no compatibility or semantic
equivalence claim. Text is the default report, and -F json gives a machine
report. This is an ob document utility, not an OpenBindings Core rule.`, cobra.ExactArgs(2))
	cmd.GroupID = "start"
	return cmd
}

func v02SourceCommands() []*cobra.Command {
	add := v02Leaf("source.add", "add <obi> <key>", "Add a source with an exact kind", `Add a named source. --kind is the exact, opaque, non-empty string stored
in source.kind; it is not a locator and does not require installed support.
--content supplies any JSON value read under that kind. With no --content,
the member is absent; --content null stores an explicit JSON null. The Core
assigns no universal meaning to the content or to an address inside it.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		if err := v02CoreName("source key", args[1]); err != nil {
			return err
		}
		if err := v02RequiredFlag(cmd, "kind"); err != nil {
			return err
		}
		kind, _ := cmd.Flags().GetString("kind")
		if kind == "" {
			return fmt.Errorf("--kind must be a non-empty exact string")
		}
		return nil
	})
	add.Flags().String("kind", "", "exact source kind (required; arbitrary non-empty string)")
	add.Flags().String("content", "", "one JSON value, @file, or -; omitted member if flag is absent")
	add.Flags().String("description", "", "human-readable source description")

	show := v02Leaf("source.show", "show <obi> <key>", "Show a source and its exact kind", `Show a source without requiring installed support for its kind. --full
shows the complete stored source object, preserving absent versus null
content.`, cobra.ExactArgs(2))
	show.Flags().Bool("full", false, "show the complete stored source")
	list := v02Leaf("source.list", "list <obi>", "List source keys and kinds", `List source keys and exact kinds without interpreting source content.`, cobra.ExactArgs(1))
	remove := v02Leaf("source.remove", "remove <obi> <key>", "Remove a source", `Remove a source by key; a real implementation must account for bindings
that reference it before writing a resulting document.`, cobra.ExactArgs(2))
	importSource := v02Leaf("source.import", "import <obi> <key> <input>", "Construct a source through an installed kind handler", `Ask the locally installed handler for --kind to make a source from an
ob acquisition locator. This is an optional tool workflow, not a Core
interpretation of source.content. The handler determines the resulting
content and may reject the input. --kind is stored exactly. Generic source
add works without this capability.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(3)(cmd, args); err != nil {
			return err
		}
		if err := v02CoreName("source key", args[1]); err != nil {
			return err
		}
		if err := v02RequiredFlag(cmd, "kind"); err != nil {
			return err
		}
		kind, _ := cmd.Flags().GetString("kind")
		if kind == "" {
			return fmt.Errorf("--kind must be a non-empty exact string")
		}
		return nil
	})
	importSource.Flags().String("kind", "", "exact kind whose installed import handler should read <input> (required)")
	inspect := v02Leaf("source.inspect", "inspect <obi> <key>", "Inspect through an installed kind handler", `Ask an installed handler to interpret a source under its exact kind.
The report is handler-defined; there is no Core target list or artifact
shape. Use source show --full to inspect the exact stored OBI object.`, cobra.ExactArgs(2))
	pull := v02Leaf("source.pull", "pull <obi> <key>", "Refresh source content through its kind handler", `Ask an installed handler to refresh this source's content. This ob
workflow does not assume content contains a URL, a file path, or any
particular JSON type. Unsupported pull is a local capability error, not a
document-conformance result.`, cobra.ExactArgs(2))
	synthesize := v02Leaf("source.synthesize", "synthesize <obi> <key>", "Propose operation and binding entries from a source", `Ask an installed kind handler to propose operation/binding entries.
The default result is an RFC 6902 patch for review; --apply proposes
writing the resulting JSON OBI. Inference is handler-specific and does not
prove that a target honors the operation contract.`, cobra.ExactArgs(2))
	synthesize.Flags().Bool("apply", false, "apply the proposed patch to the OBI when implemented")
	return []*cobra.Command{add, show, list, remove, importSource, inspect, pull, synthesize}
}

func v02BindingCommands() []*cobra.Command {
	add := v02Leaf("binding.add", "add <obi> <key>", "Add a named realization of an operation", `A binding links one operation key to one source key. Multiple bindings
may realize the same operation, including through the same source. Optional
--content is any JSON value read under the source's kind; no universal
selector, target address, or transform field exists in Core. Omission and
explicit JSON null are distinct. --preference is an author signal, not an
automatic selection algorithm.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		if err := v02CoreName("binding key", args[1]); err != nil {
			return err
		}
		for _, name := range []string{"operation", "source"} {
			if err := v02RequiredFlag(cmd, name); err != nil {
				return err
			}
			value, _ := cmd.Flags().GetString(name)
			if err := v02CoreName(name+" key", value); err != nil {
				return err
			}
		}
		return v02Preference(cmd)
	})
	add.Flags().String("operation", "", "existing operation key (required; an alias is not a local reference)")
	add.Flags().String("source", "", "existing source key (required)")
	add.Flags().String("content", "", "one JSON value, @file, or -; omitted member if flag is absent")
	add.Flags().String("preference", "", "signed author preference; higher is stronger; no selection rule implied")
	add.Flags().String("description", "", "human-readable binding description")
	add.Flags().Bool("deprecated", false, "recommend migration away from this binding")
	show := v02Leaf("binding.show", "show <obi> <key>", "Show one binding", `Show the operation and source keys and, with --full, the complete
binding object. Content is displayed without universal interpretation.`, cobra.ExactArgs(2))
	show.Flags().Bool("full", false, "show the complete stored binding")
	list := v02Leaf("binding.list", "list <obi>", "List bindings", `List binding keys and their operation/source relationships.`, cobra.ExactArgs(1))
	remove := v02Leaf("binding.remove", "remove <obi> <key>", "Remove one binding", `Remove one binding by its independent key.`, cobra.ExactArgs(2))
	invoke := v02Leaf("binding.invoke", "invoke <obi> <binding-key>", "Invoke one exact binding through its installed kind handler", `Invoke the named binding exactly; no automatic binding selection occurs.
--input supplies one caller-facing JSON value (including null); omission
supplies none. --input-stream - proposes NDJSON stdin, only where the kind
handler supports stream input. The handler determines interaction and target
mechanics. Output is an ob NDJSON event-envelope stream, never raw Core
output. Diagnostics go to stderr. -o and -F do not apply; redirect stdout.
--context selects local ob configuration, not a document reference. This
preview never invokes a target or reveals context secrets.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		return v02CoreName("binding key", args[1])
	})
	invoke.Flags().String("input", "", "one caller-facing JSON value, @file, or -; omitted means no value")
	invoke.Flags().String("input-stream", "", "NDJSON input stream from - stdin, when the kind supports it")
	invoke.Flags().String("context", "", "local ob context name (never a Core document reference)")
	invoke.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), cmd.Long)
		fmt.Fprintf(cmd.OutOrStdout(), "\nUsage:\n  %s\n", cmd.UseLine())
		fmt.Fprintf(cmd.OutOrStdout(), "\nExamples:\n%s\n", cmd.Example)
		fmt.Fprintf(cmd.OutOrStdout(), "\nFlags:\n%s", cmd.NonInheritedFlags().FlagUsages())
	})
	return []*cobra.Command{add, show, list, remove, invoke}
}

func v02DependencyCommands() []*cobra.Command {
	add := v02Leaf("dependency.add", "add <obi> <key>", "Declare a named consumption point", `A dependency names where this component consumes a realization of a
local operation. --operation references its key, not an alias. Repeat --kind
to declare an exact, unordered any-of constraint; omit --kind for no kind
constraint. A listed kind need not be installed. The dependency names no
provider or target and says nothing about readiness if unsatisfied.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		if err := v02CoreName("dependency key", args[1]); err != nil {
			return err
		}
		if err := v02RequiredFlag(cmd, "operation"); err != nil {
			return err
		}
		op, _ := cmd.Flags().GetString("operation")
		if err := v02CoreName("operation key", op); err != nil {
			return err
		}
		kinds, _ := cmd.Flags().GetStringArray("kind")
		seen := map[string]bool{}
		for _, kind := range kinds {
			if kind == "" || seen[kind] {
				return fmt.Errorf("--kind values must be non-empty and unique exact strings")
			}
			seen[kind] = true
		}
		return nil
	})
	add.Flags().String("operation", "", "existing operation key consumed at this point (required)")
	add.Flags().StringArray("kind", nil, "acceptable exact kind (repeatable; any-of)")
	show := v02Leaf("dependency.show", "show <obi> <key>", "Show one consumption point", `Show the local operation key and exact kind constraint. With --full,
show the complete stored dependency object.`, cobra.ExactArgs(2))
	show.Flags().Bool("full", false, "show the complete stored dependency")
	list := v02Leaf("dependency.list", "list <obi>", "List named consumption points", `List dependency keys and their local operation keys.`, cobra.ExactArgs(1))
	remove := v02Leaf("dependency.remove", "remove <obi> <key>", "Remove one consumption point", `Remove a dependency by its local key.`, cobra.ExactArgs(2))
	return []*cobra.Command{add, show, list, remove}
}

func v02OperationCommands(idempotentValue bool) []*cobra.Command {
	add := v02Leaf("operation.add", "add <obi> <key>", "Add a neutral, per-value operation contract", `Add an operation independent of bindings or dependencies. Optional
input/output schemas govern each caller-facing value, not the number or
lifecycle of values. A schema may be an object or boolean; omit its flag to
leave that boundary unspecified. An alias shares one flat exact-name space
with operation keys. Idempotency is an author-attested effect claim, not
automatic permission to retry or cache.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		if err := v02CoreName("operation key", args[1]); err != nil {
			return err
		}
		aliases, _ := cmd.Flags().GetStringArray("alias")
		seen := map[string]bool{args[1]: true}
		for _, alias := range aliases {
			if err := v02CoreName("alias", alias); err != nil {
				return err
			}
			if seen[alias] {
				return fmt.Errorf("--alias values must be unique and differ from the operation key")
			}
			seen[alias] = true
		}
		if idempotentValue && cmd.Flags().Changed("idempotent") {
			value, _ := cmd.Flags().GetString("idempotent")
			if value != "true" && value != "false" {
				return fmt.Errorf("--idempotent must be true or false")
			}
		}
		return nil
	})
	add.Flags().String("description", "", "human-readable operation description")
	add.Flags().Bool("deprecated", false, "recommend migration away from this operation")
	add.Flags().StringArray("tag", nil, "documentation tag (repeatable)")
	add.Flags().StringArray("alias", nil, "additional exact operation name (repeatable)")
	if idempotentValue {
		add.Flags().String("idempotent", "", "author-attested intended-effect claim: true or false")
	} else {
		add.Flags().Bool("idempotent", false, "author-attested intended-effect claim; use --idempotent=false to deny")
	}
	add.Flags().String("input-schema", "", "per-value input JSON Schema: object/boolean, @file, or -")
	add.Flags().String("output-schema", "", "per-value successful output JSON Schema: object/boolean, @file, or -")
	show := v02Leaf("operation.show", "show <obi> <key-or-alias>", "Show one operation contract", `Look up an operation key or exact alias. --full shows the complete
stored operation value. Name resolution treats keys and aliases equally;
bindings and dependencies still reference the operation key.`, cobra.ExactArgs(2))
	show.Flags().Bool("full", false, "show the complete stored operation")
	list := v02Leaf("operation.list", "list <obi>", "List operations and aliases", `List operation keys and their exact aliases.`, cobra.ExactArgs(1))
	remove := v02Leaf("operation.remove", "remove <obi> <key>", "Remove an operation", `Remove an operation by key; an implementation must account for bindings
and dependencies that reference it.`, cobra.ExactArgs(2))
	return []*cobra.Command{add, show, list, remove}
}

func v02SchemaCommands() []*cobra.Command {
	add := v02Leaf("schema.add", "add <obi> <key>", "Add a reusable JSON Schema", `Add a named JSON Schema 2020-12 object or boolean. Operations may refer
to it by an exact same-document $ref such as {"$ref":"#/schemas/Item"}.
--value accepts one schema value inline, from @file, or from stdin.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		if err := v02CoreName("schema key", args[1]); err != nil {
			return err
		}
		return v02RequiredFlag(cmd, "value")
	})
	add.Flags().String("value", "", "JSON Schema object or boolean, @file, or - (required)")
	show := v02Leaf("schema.show", "show <obi> <key>", "Show one reusable schema", `Show the exact stored JSON Schema, including boolean form.`, cobra.ExactArgs(2))
	list := v02Leaf("schema.list", "list <obi>", "List reusable schema keys", `List names in the document's schemas map.`, cobra.ExactArgs(1))
	remove := v02Leaf("schema.remove", "remove <obi> <key>", "Remove a reusable schema", `Remove a schema by key after checking local references.`, cobra.ExactArgs(2))
	return []*cobra.Command{add, show, list, remove}
}

func v02KindCommand() *cobra.Command {
	group := v02Group("kind", "Inspect installed abilities for exact OBI kinds")
	group.GroupID = "operate"
	group.Long = `A kind is an exact opaque source token. These commands report only
what this ob installation can do with one. They do not define a registry,
validate a kind's existence, or affect Core document conformance.`
	list := v02Leaf("kind.list", "list", "List locally supported kind abilities", `List exact kinds for which this installation declares an inspect,
import, pull, synthesize, or invoke capability. This is local metadata, not a global
catalog or a Core validity check.`, cobra.NoArgs)
	check := v02Leaf("kind.check", "check <kind>", "Check this installation's ability to act on a kind", `Report whether this installation can perform a named action for the
exact kind string. A negative answer is a local capability result; an OBI
carrying that kind may still be conformant. Without --action, show all known
actions for the kind. The action tokens mean: inspect asks a
handler for a report; import creates a source from an input; pull refreshes
stored source content; synthesize proposes entries; invoke acts on a binding.`, func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		if args[0] == "" {
			return fmt.Errorf("kind must be a non-empty exact string")
		}
		capability, _ := cmd.Flags().GetString("action")
		if capability != "" && capability != "inspect" && capability != "import" && capability != "pull" && capability != "synthesize" && capability != "invoke" {
			return fmt.Errorf("--action must be inspect, import, pull, synthesize, or invoke (pull refreshes stored content)")
		}
		return nil
	})
	check.Flags().String("action", "", "installed action to check: inspect, import, pull, synthesize, or invoke")
	group.AddCommand(list, check)
	return group
}

func v02DocPrefix(path string) string { return strings.TrimPrefix(path, "ob ") }
