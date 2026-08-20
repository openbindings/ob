package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newOperationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "operation",
		Aliases: []string{"op", "operations"},
		Short:   "Manage and invoke operations on an OBI",
		Long: `Manage and invoke operations on an OpenBindings interface document.

Operations define the abstract methods and events that an interface
exposes. Use subcommands to list, rename, remove, or invoke operations.`,
	}
	markCommandGroup(cmd)

	cmd.AddCommand(
		newOperationListCmd(),
		newOperationInvokeCmd(),
		newOperationPrepareCmd(),
		newOperationAddCmd(),
		newOperationSetCmd(),
		newOperationDetachCmd(),
		newOperationBindCmd(),
		newOperationUnbindCmd(),
		newOperationAliasCmd(),
		newOperationCodegenNameCmd(),
		newOperationOutputSchemaCmd(),
		newOperationRenameCmd(),
		newOperationRemoveCmd(),
	)

	return cmd
}

func newOperationOutputSchemaCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "output-schema <obi-path> <operation> [schema]",
		Short: "Set an operation's output schema when its source can't declare one",
		Long: `Set the output schema for an operation.

When a source cannot declare an operation's output shape, synthesis records
a permissive placeholder — {"type":"string"}, marked in x-ob as a "floor" —
so the derived contract never claims to know more than it does. Once you
know the real shape, set it here: the schema is written into the operation's
output and remembered in x-ob so it survives 'ob source pull' (re-applied
onto each fresh derivation). If a later pull finds that the SOURCE now
declares its own real output schema, that source schema wins and your
override is dropped with a warning on the next pull.

The schema is inline JSON, @file, or - (stdin). Use --clear to remove the
override (the value you set stays until the next pull re-derives it).
'ob purify' strips the override marker and the floor placeholder alike.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op output-schema interface.json listPets @pets-schema.json
  ob op output-schema interface.json listPets '{"type":"array","items":{"type":"object"}}'
  ob op output-schema interface.json listPets --clear`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var schema openbindings.JSONSchema
			switch {
			case clear:
				// nil schema clears the election.
			case len(args) == 3:
				parsed, err := readSchemaArg(args[2], "schema")
				if err != nil {
					return err
				}
				schema = openbindings.JSONSchema(parsed)
			default:
				return app.ExitResult{Code: 2, Message: "provide a schema (inline JSON, @file, or -), or pass --clear", ToStderr: true}
			}
			result, err := app.OperationSetOutputSchema(args[0], args[1], schema)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("elect output schema: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the output-schema override")
	return cmd
}

func newOperationInvokeCmd() *cobra.Command {
	var bindingKey string
	var inputArg string
	var verbose bool
	var selection []string

	cmd := &cobra.Command{
		Use:   "invoke <obi> [operation]",
		Short: "Invoke an operation via a binding",
		Long: `Invoke an operation from an OpenBindings interface.

Every operation is a stream. One JSON value per event is printed to
stdout. Unary operations produce one line and exit. Streaming
operations produce lines until the stream closes or Ctrl-C.

The operation key is a positional argument. A sole invocable binding
is used automatically; if several are available, choose one explicitly.

Alternatively, use --binding to select a specific binding directly;
the operation is derived from the binding entry.

Use repeatable --select-binding flags to supply an ordered caller choice
when invoking by operation. The list also reaches nested operation-graph
calls, so one invocation can choose a binding for each referenced operation.

Context (credentials, headers, etc.) is automatically resolved from
the target URL. Use 'ob context set <url>' to configure context. When
the governing binding specification exposes named interpretation points
(a GraphQL document, a server choice), those answers are context too:
invocation raises CONTEXT_REQUIRED naming the point, and the standing
context store's configuration for the target satisfies it.

--input accepts inline JSON, @file (read from a file), or - (read from
stdin) — so credentials never sit on ob's own argv.

Use -v/--verbose to emit the binding key, duration, and any displaced
output-schema overrides on stderr.

Examples:
  ob op invoke interface.json listPets --input '{"limit":10}'
  ob op invoke interface.json echo
  ob op invoke interface.json --binding viewer.graphql
  ob op invoke interface.json placeAndTrack \
    --select-binding placeOrder.rest --select-binding orderUpdates.grpc
  ob op invoke interface.json --binding listPets.openapi --input '{"limit":10}'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obiFile := args[0]

			var operationKey string
			if len(args) == 2 {
				operationKey = args[1]
			}

			if operationKey == "" && bindingKey == "" {
				return app.ExitResult{Code: 2, Message: "provide an operation key or use --binding", ToStderr: true}
			}
			if operationKey != "" && bindingKey != "" {
				return app.ExitResult{Code: 2, Message: "operation key and --binding are mutually exclusive", ToStderr: true}
			}
			if conflict := invocationStdinConflict(obiFile, inputArg, ""); conflict != "" {
				return app.ExitResult{Code: 2, Message: conflict, ToStderr: true}
			}

			input, ierr := readInvokeInput(inputArg)
			if ierr != nil {
				return app.ExitResult{Code: 2, Message: ierr.Error(), ToStderr: true}
			}

			config := &app.InvokeConfig{Selection: append([]string(nil), selection...)}

			// The invoke lane is a streaming Unix filter: output rides stdout as
			// one JSON value per event. -o (write-to-file) has no place on a
			// stream — redirect with the shell instead. -F selects the output
			// shape: json = the aggregate machine envelope, unset = streaming
			// JSON lines; any other value is not something this lane produces.
			if err := refuseUnhonoredOutputFlags(cmd, "operation invoke", "output"); err != nil {
				return err
			}
			format, _ := getOutputFlags(cmd)
			switch format {
			case "", "json":
			default:
				return app.ExitResult{Code: 2, Message: fmt.Sprintf(
					"operation invoke supports -F json (aggregate envelope) or no -F (streaming JSON lines); %q is not a supported format", format), ToStderr: true}
			}
			jsonEnvelope := format == "json"

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			run, err := app.InvokeOBIOperationConfigured(ctx, obiFile, operationKey, bindingKey, input, config)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("invoke %s in %s: %v", operationKey, obiFile, err), ToStderr: true}
			}

			if verbose {
				fmt.Fprintf(os.Stderr, "binding: %s\n", run.BindingKey)
			}
			if run.DisplacedWarning != "" {
				fmt.Fprintf(os.Stderr, "warning: %s\n", run.DisplacedWarning)
				if verbose {
					for _, d := range run.DisplacedDetail {
						fmt.Fprintf(os.Stderr, "  displaced: %s\n", d)
					}
				}
			}

			start := time.Now()
			if jsonEnvelope {
				return renderInvokeJSON(os.Stdout, run)
			}

			enc := json.NewEncoder(os.Stdout)
			hadError := false
			for ev := range run.Events {
				if ev.Error != nil {
					renderInvokeError(os.Stderr, ev.Error)
					hadError = true
					continue
				}
				if err := enc.Encode(ev.Output); err != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("write error: %v", err), ToStderr: true}
				}
			}

			if verbose {
				fmt.Fprintf(os.Stderr, "duration: %dms\n", time.Since(start).Milliseconds())
			}

			if hadError {
				return app.ExitResult{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&bindingKey, "binding", "", "binding key to invoke (operation is derived from the entry)")
	cmd.Flags().StringArrayVar(&selection, "select-binding", nil, "ordered binding choice for this and nested operations (repeatable)")
	cmd.Flags().StringVar(&inputArg, "input", "", "operation input: inline JSON, @file, or - (stdin)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show binding key, duration, and displaced output-schema overrides on stderr")

	return cmd
}

// renderInvokeJSON drains the invocation into one protocol-blind machine
// envelope. Prior outputs remain present when completion is unsuccessful.
func renderInvokeJSON(w io.Writer, run *app.ConfiguredInvocation) error {
	outputs := []any{}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	for ev := range run.Events {
		if ev.Error != nil {
			errorValue := map[string]any{"code": ev.Error.Code}
			if ev.Error.HasData() {
				errorValue["data"] = ev.Error.Data
			}
			envelope := map[string]any{"outputs": outputs, "error": errorValue}
			_ = enc.Encode(envelope)
			return app.ExitResult{Code: 1}
		}
		outputs = append(outputs, ev.Output)
	}

	envelope := map[string]any{"outputs": outputs}
	if err := enc.Encode(envelope); err != nil {
		return app.ExitResult{Code: 1, Message: fmt.Sprintf("write error: %v", err), ToStderr: true}
	}
	return nil
}

// renderInvokeError writes a terminal invocation error for humans: the code,
// any application-authored data, and — for CONTEXT_REQUIRED — the full
// challenge plus a copy-pasteable remedy, so the auth loop closes from the
// error itself instead of from the docs.
func renderInvokeError(w io.Writer, ierr *invoke.InvocationError) {
	fmt.Fprintf(w, "error: %s\n", ierr.Code)
	if details := invoke.ContextRequiredFrom(ierr); details != nil {
		fmt.Fprintln(w, app.RenderContextRequirements(details))
		if hint := contextSetHint(details); hint != "" {
			fmt.Fprintf(w, "  satisfy it with: %s\n", hint)
		}
		return
	}
	if ierr.HasData() {
		fmt.Fprint(w, "  data: ")
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(ierr.Data)
	}
}

// contextSetHint maps the challenge's first requirement to the ob context
// flag that satisfies it.
func contextSetHint(d *invoke.ContextRequiredDetails) string {
	if d.Target == "" || len(d.Alternatives) == 0 || len(d.Alternatives[0].Requirements) == 0 {
		return ""
	}
	switch d.Alternatives[0].Requirements[0].Type {
	case "auth.bearer":
		return fmt.Sprintf("ob context set %s --bearer-token <token>", d.Target)
	case "auth.apiKey":
		return fmt.Sprintf("ob context set %s --api-key <key>", d.Target)
	case "auth.basic":
		return fmt.Sprintf("ob context set %s --basic <user:pass>", d.Target)
	case "auth.oauth2":
		return fmt.Sprintf("ob context set %s --bearer-token <access-token>", d.Target)
	default:
		return fmt.Sprintf("ob context set %s --help", d.Target)
	}
}

// readInvokeInput resolves the --input house grammar: "" is no input,
// "@file" reads a file, "-" reads stdin, and anything else is inline JSON.
// Credentials never sit on ob's own argv this way.
func readInvokeInput(arg string) (any, error) {
	if arg == "" {
		return nil, nil
	}
	var raw []byte
	switch {
	case arg == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read --input from stdin: %w", err)
		}
		raw = b
	case strings.HasPrefix(arg, "@"):
		b, err := os.ReadFile(arg[1:])
		if err != nil {
			return nil, fmt.Errorf("read --input file: %w", err)
		}
		raw = b
	default:
		raw = []byte(arg)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("invalid --input JSON: %w", err)
	}
	return v, nil
}

func invocationStdinConflict(obiFile, inputArg, configurationArg string) string {
	var consumers []string
	if obiFile == "-" {
		consumers = append(consumers, "OBI")
	}
	if inputArg == "-" {
		consumers = append(consumers, "--input")
	}
	if configurationArg == "-" {
		consumers = append(consumers, "--configuration")
	}
	if len(consumers) < 2 {
		return ""
	}
	return strings.Join(consumers, " and ") + " cannot both consume stdin"
}

// readInvokeConfiguration resolves --configuration using the same house
// grammar as --input, but requires a JSON object because binding
// specifications name their interpretation points as object members.
func readInvokeConfiguration(arg string) (map[string]any, error) {
	return readJSONObjectArg(arg, "--configuration")
}

func readJSONObjectArg(arg, flagName string) (map[string]any, error) {
	if arg == "" {
		return nil, nil
	}
	var raw []byte
	switch {
	case arg == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read %s from stdin: %w", flagName, err)
		}
		raw = b
	case strings.HasPrefix(arg, "@"):
		b, err := os.ReadFile(arg[1:])
		if err != nil {
			return nil, fmt.Errorf("read %s file: %w", flagName, err)
		}
		raw = b
	default:
		raw = []byte(arg)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var configuration map[string]any
	if err := json.Unmarshal(raw, &configuration); err != nil {
		return nil, fmt.Errorf("invalid %s JSON object: %w", flagName, err)
	}
	if configuration == nil {
		return nil, fmt.Errorf("%s must be a JSON object", flagName)
	}
	return configuration, nil
}

func newOperationPrepareCmd() *cobra.Command {
	var bindingKey string
	var selection []string
	var configurationArg string

	cmd := &cobra.Command{
		Use:   "prepare <obi> [operation]",
		Short: "Preflight an operation's required context without invoking it",
		Long: `Report the context invoking an operation would require, without invoking
it or causing any side effect.

Resolves the operation (or, with --binding, a specific binding) to a
concrete binding and reports its context requirements, or reports none
when they cannot be determined without invoking. Repeat --select-binding
to supply the same ordered caller choice accepted by invocation. Use
--configuration (inline JSON, @file, or - for stdin) when binding-spec
interpretation points are already known. This is advisory:
the reactive CONTEXT_REQUIRED error from 'ob op invoke' is authoritative.

Examples:
  ob op prepare interface.json createOrder
  ob op prepare interface.json --binding createOrder.openapi
  ob op prepare interface.json --binding viewer.graphql \
    --configuration '{"document":"query { viewer { id } }"}'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obiFile := args[0]

			var operationKey string
			if len(args) == 2 {
				operationKey = args[1]
			}

			if operationKey == "" && bindingKey == "" {
				return app.ExitResult{Code: 2, Message: "provide an operation key or use --binding", ToStderr: true}
			}
			if operationKey != "" && bindingKey != "" {
				return app.ExitResult{Code: 2, Message: "operation key and --binding are mutually exclusive", ToStderr: true}
			}
			if conflict := invocationStdinConflict(obiFile, "", configurationArg); conflict != "" {
				return app.ExitResult{Code: 2, Message: conflict, ToStderr: true}
			}

			configuration, configErr := readInvokeConfiguration(configurationArg)
			if configErr != nil {
				return app.ExitResult{Code: 2, Message: configErr.Error(), ToStderr: true}
			}
			config := &app.InvokeConfig{
				Selection:     append([]string(nil), selection...),
				Configuration: configuration,
			}
			details, err := app.PrepareOperation(context.Background(), obiFile, operationKey, bindingKey, config.Context())
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("prepare %s in %s: %v", operationKey, obiFile, err), ToStderr: true}
			}

			// Wire shape per the contract: the details themselves, or null —
			// no envelope (matching `ob binding prepare`).
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(details, format, outputPath, func() string {
				return app.RenderContextRequirements(details)
			})
		},
	}

	cmd.Flags().StringVar(&bindingKey, "binding", "", "binding key to preflight (operation is derived from the entry)")
	cmd.Flags().StringArrayVar(&selection, "select-binding", nil, "ordered binding choice (repeatable)")
	cmd.Flags().StringVar(&configurationArg, "configuration", "", "binding-spec configuration object as JSON, @file, or - for stdin")

	return cmd
}

func newOperationListCmd() *cobra.Command {
	var tagFilter string

	cmd := &cobra.Command{
		Use:     "list <obi>",
		Aliases: []string{"ls"},
		Short:   "List operations on an OBI",
		Long: `List all operations defined in an OpenBindings interface document.

Shows each operation's key, tags, ownership (source-owned operations are
re-derived by 'ob source pull'; the rest are hand-authored), and binding
count.

Examples:
  ob operation list interface.json
  ob op list interface.json --tag admin
  ob op list interface.json -F json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationList(args[0], tagFilter)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("list operations in %s: %v", args[0], err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&tagFilter, "tag", "", "filter operations by tag")

	return cmd
}

func newOperationAddCmd() *cobra.Command {
	var (
		description string
		aliases     []string
		tags        []string
		inputJSON   string
		outputJSON  string
		idempotent  string
	)

	cmd := &cobra.Command{
		Use:   "add <obi-path> <key>",
		Short: "Add a new operation to an OBI",
		Long: `Add a new operation to an OpenBindings interface document.

Creates a bare operation with the given key. Use flags to set
description, correspondence aliases, tags, and schemas. The operation is
added without any bindings — bind it to a source with 'ob operation bind'.

Schema flags accept inline JSON, '@path' to read a file, or '-' for stdin.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op add interface.json createUser --description "Create a new user"
  ob op add interface.json get --input-schema @get-input.json --output-schema @get-output.json
  ob op add interface.json get --alias openbindings.document-store.get
  ob op add interface.json listUsers --tag admin --tag readonly --idempotent true`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			addInput := app.OperationAddInput{
				OBIPath:     args[0],
				Key:         args[1],
				Aliases:     aliases,
				Description: description,
				Tags:        tags,
			}

			if inputJSON != "" {
				schema, err := readSchemaArg(inputJSON, "--input-schema")
				if err != nil {
					return err
				}
				addInput.Input = schema
			}
			if outputJSON != "" {
				schema, err := readSchemaArg(outputJSON, "--output-schema")
				if err != nil {
					return err
				}
				addInput.Output = schema
			}
			idem, err := parseBoolFlag("idempotent", idempotent)
			if err != nil {
				return err
			}
			addInput.Idempotent = idem

			result, err := app.OperationAdd(addInput)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("add operation: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "operation description")
	cmd.Flags().StringArrayVar(&aliases, "alias", nil, "correspondence alias: another interface's operation key this operation also answers to (repeatable)")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "operation tag (repeatable)")
	cmd.Flags().StringVar(&inputJSON, "input-schema", "", "input schema: inline JSON, @file, or - for stdin")
	cmd.Flags().StringVar(&outputJSON, "output-schema", "", "output schema: inline JSON, @file, or - for stdin")
	cmd.Flags().StringVar(&idempotent, "idempotent", "", "whether the operation is idempotent (true/false)")

	return cmd
}

// readSchemaArg resolves a schema flag value into a parsed JSON object. The
// value is inline JSON, "@path" to read a file, or "-" to read stdin. The
// flagName is used only for error messages.
func readSchemaArg(val, flagName string) (map[string]any, error) {
	raw := val
	switch {
	case val == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, app.ExitResult{Code: 2, Message: fmt.Sprintf("%s: read stdin: %v", flagName, err), ToStderr: true}
		}
		raw = string(data)
	case strings.HasPrefix(val, "@"):
		data, err := os.ReadFile(val[1:])
		if err != nil {
			return nil, app.ExitResult{Code: 2, Message: fmt.Sprintf("%s: %v", flagName, err), ToStderr: true}
		}
		raw = string(data)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil, app.ExitResult{Code: 2, Message: fmt.Sprintf("invalid %s JSON: %v", flagName, err), ToStderr: true}
	}
	return schema, nil
}

func newOperationAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alias",
		Aliases: []string{"aliases"},
		Short:   "Manage an operation's correspondence aliases",
		Long: `Manage the correspondence aliases on an operation.

An alias is another interface's operation key that this operation also
answers to. Key and aliases form one flat, document-unique namespace
(OBI-T-12): an operation corresponds to a published interface by carrying
that interface's operation key as an alias.`,
	}
	markCommandGroup(cmd)

	cmd.AddCommand(
		newOperationAliasAddCmd(),
		newOperationAliasRemoveCmd(),
		newOperationAliasListCmd(),
	)

	return cmd
}

func newOperationAliasAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <obi-path> <operation> <alias>...",
		Short: "Add correspondence alias(es) to an operation",
		Long: `Add one or more correspondence aliases to an operation.

The operation may be referenced by its key or any existing identifier.
Each alias must be free in the document's flat key+alias namespace.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op alias add interface.json acme.cache.fetch openbindings.document-store.get
  ob op alias add interface.json describe openbindings.software-descriptor.describe`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationAliasAdd(args[0], args[1], args[2:])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("add alias: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}
	return cmd
}

func newOperationAliasRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <obi-path> <operation> <alias>...",
		Aliases: []string{"rm"},
		Short:   "Remove correspondence alias(es) from an operation",
		Long: `Remove one or more correspondence aliases from an operation.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op alias rm interface.json acme.cache.fetch openbindings.document-store.get`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationAliasRemove(args[0], args[1], args[2:])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("remove alias: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}
	return cmd
}

func newOperationAliasListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list <obi> [operation]",
		Aliases: []string{"ls"},
		Short:   "Show the correspondence map (which ops correspond to which interfaces)",
		Long: `List the correspondence aliases in an interface — each operation and the
interface operations it corresponds to. With no operation argument, shows
every operation that carries aliases (a quick "what does this OBI correspond
to?"). Scoped to one operation when given.

Examples:
  ob op alias list interface.json
  ob op alias ls interface.json acme.cache.fetch`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var op string
			if len(args) == 2 {
				op = args[1]
			}
			result, err := app.OperationAliasList(args[0], op)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("list aliases in %s: %v", args[0], err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	return cmd
}

func newOperationSetCmd() *cobra.Command {
	var (
		description string
		idempotent  string
		deprecated  string
		inputJSON   string
		outputJSON  string
		addTags     []string
		removeTags  []string
		own         bool
	)
	cmd := &cobra.Command{
		Use:   "set <obi-path> <operation>",
		Short: "Edit an existing operation",
		Long: `Edit fields of an existing operation: description, idempotency,
deprecation, schemas, and tags.

A source-owned operation (derived via 'ob source pull') can only be
edited with --own, which detaches it first — otherwise the edit would be
overwritten by the next pull.

Schema flags accept inline JSON, '@path' to read a file, or '-' for stdin.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op set interface.json greet --description "Greet a user"
  ob op set interface.json greet --deprecated true
  ob op set interface.json getA --own --output-schema @new-output.json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			setInput := app.OperationSetInput{
				OBIPath:    args[0],
				Op:         args[1],
				AddTags:    addTags,
				RemoveTags: removeTags,
				Own:        own,
			}
			if cmd.Flags().Changed("description") {
				setInput.Description = &description
			}
			idem, err := parseBoolFlag("idempotent", idempotent)
			if err != nil {
				return err
			}
			setInput.Idempotent = idem
			dep, err := parseBoolFlag("deprecated", deprecated)
			if err != nil {
				return err
			}
			setInput.Deprecated = dep
			if inputJSON != "" {
				schema, err := readSchemaArg(inputJSON, "--input-schema")
				if err != nil {
					return err
				}
				setInput.Input = schema
			}
			if outputJSON != "" {
				schema, err := readSchemaArg(outputJSON, "--output-schema")
				if err != nil {
					return err
				}
				setInput.Output = schema
			}
			result, err := app.OperationSet(setInput)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("set operation: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "set the operation description")
	cmd.Flags().StringVar(&idempotent, "idempotent", "", "set whether the operation is idempotent (true/false)")
	cmd.Flags().StringVar(&deprecated, "deprecated", "", "set whether the operation is deprecated (true/false)")
	cmd.Flags().StringVar(&inputJSON, "input-schema", "", "set input schema: inline JSON, @file, or - for stdin")
	cmd.Flags().StringVar(&outputJSON, "output-schema", "", "set output schema: inline JSON, @file, or - for stdin")
	cmd.Flags().StringArrayVar(&addTags, "add-tag", nil, "add a tag (repeatable)")
	cmd.Flags().StringArrayVar(&removeTags, "remove-tag", nil, "remove a tag (repeatable)")
	cmd.Flags().BoolVar(&own, "own", false, "take ownership of a source-owned operation (detach) before editing")
	return cmd
}

func newOperationDetachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detach <obi-path> <operation>",
		Short: "Convert a source-owned operation to hand-authored",
		Long: `Detach a source-owned operation so 'ob source pull' no longer overwrites
its schema. The operation becomes hand-authored ("you own it now").

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op detach interface.json getA`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationDetach(args[0], args[1])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("detach operation: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}
	return cmd
}

func newOperationBindCmd() *cobra.Command {
	var (
		transformStub   bool
		inputTransform  string
		outputTransform string
		preference      float64
		force           bool
	)
	cmd := &cobra.Command{
		Use:   "bind <obi-path> [operation] [source] [ref]",
		Short: "Attach a source ref to an existing operation",
		Long: `Attach a registered source's ref to an existing operation — the
contract-keyed wire-up. The operation keeps its key; the source's wire
identifier lives in the binding's ref.

When operation, source, or ref are omitted, you are prompted to pick them
(operation → source → ref). When all are given, it runs without prompts.

On a shape mismatch between the operation and the ref, it warns; with
--transform-stub it scaffolds identity transform stubs ($) for you to
complete. Use --force to re-point an existing binding.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op bind interface.json                              # interactive picker
  ob op bind interface.json greet openapi getGreeting
  ob op bind interface.json fetch openapi readEntry --transform-stub`,
		Args: cobra.RangeArgs(1, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			obiPath := args[0]
			var op, source, ref string
			if len(args) > 1 {
				op = args[1]
			}
			if len(args) > 2 {
				source = args[2]
			}
			if len(args) > 3 {
				ref = args[3]
			}
			if op == "" || source == "" || ref == "" {
				var err error
				op, source, ref, err = pickBindArgs(obiPath, op, source, ref)
				if err != nil {
					return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
				}
			}
			bindInput := app.OperationBindInput{
				OBIPath:         obiPath,
				Op:              op,
				Source:          source,
				Ref:             ref,
				TransformStub:   transformStub,
				InputTransform:  inputTransform,
				OutputTransform: outputTransform,
				Force:           force,
			}
			if cmd.Flags().Changed("preference") {
				bindInput.Preference = &preference
			}
			result, err := app.OperationBind(bindInput)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("bind operation: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, obiPath, result)
		},
	}
	cmd.Flags().BoolVar(&transformStub, "transform-stub", false, "scaffold identity transform stub(s) on shape mismatch")
	cmd.Flags().StringVar(&inputTransform, "input-transform", "", "inline JSONata transforming operation input to binding input")
	cmd.Flags().StringVar(&outputTransform, "output-transform", "", "inline JSONata transforming binding output to operation output")
	cmd.Flags().Float64Var(&preference, "preference", 0, "author preference signal (higher = more preferred; does not select automatically)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing binding for this operation+source (re-point)")
	return cmd
}

func newOperationUnbindCmd() *cobra.Command {
	var binding string
	cmd := &cobra.Command{
		Use:   "unbind <obi-path> [operation] [source]",
		Short: "Remove a binding from an operation (keep the operation)",
		Long: `Remove the binding connecting an operation to a source, leaving the
operation in place. Useful for re-pointing to a different backend.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op unbind interface.json greet openapi
  ob op unbind interface.json --binding custom-binding-key`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && binding != "" {
				return nil
			}
			if len(args) == 3 && binding == "" {
				return nil
			}
			return fmt.Errorf("provide either <operation> <source> or --binding <key>")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				result app.OperationUnbindOutput
				err    error
			)
			if binding != "" {
				result, err = app.OperationUnbindBinding(args[0], binding)
			} else {
				result, err = app.OperationUnbind(args[0], args[1], args[2])
			}
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("unbind operation: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}
	cmd.Flags().StringVar(&binding, "binding", "", "exact binding key to remove")
	return cmd
}

// pickBindArgs interactively fills any missing operation/source/ref for
// `operation bind`. It requires a TTY; on a non-interactive stdin it returns an
// error directing the caller to supply all three positionally.
func pickBindArgs(obiPath, op, source, ref string) (string, string, string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", "", "", fmt.Errorf("operation, source, and ref are required (no TTY for interactive selection)")
	}
	if op == "" {
		ops, err := app.OperationList(obiPath, "")
		if err != nil {
			return "", "", "", err
		}
		if len(ops) == 0 {
			return "", "", "", fmt.Errorf("no operations to bind; add one with 'ob operation add'")
		}
		opts := make([]huh.Option[string], len(ops))
		for i, e := range ops {
			opts[i] = huh.NewOption(e.Key, e.Key)
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Operation").Options(opts...).Value(&op),
		)).Run(); err != nil {
			return "", "", "", err
		}
	}
	if source == "" {
		srcs, err := app.SourceList(obiPath)
		if err != nil {
			return "", "", "", err
		}
		if len(srcs) == 0 {
			return "", "", "", fmt.Errorf("no sources registered; add one with 'ob source add'")
		}
		opts := make([]huh.Option[string], len(srcs))
		for i, e := range srcs {
			opts[i] = huh.NewOption(e.Key, e.Key)
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Source").Options(opts...).Value(&source),
		)).Run(); err != nil {
			return "", "", "", err
		}
	}
	if ref == "" {
		refs, err := app.SourceRefs(obiPath, source)
		if err != nil {
			return "", "", "", err
		}
		if len(refs) == 0 {
			return "", "", "", fmt.Errorf("source %q exposes no bindable refs", source)
		}
		opts := make([]huh.Option[string], len(refs))
		for i, r := range refs {
			opts[i] = huh.NewOption(r, r)
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Ref").Options(opts...).Value(&ref),
		)).Run(); err != nil {
			return "", "", "", err
		}
	}
	return op, source, ref, nil
}

func newOperationCodegenNameCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "codegen-name <obi-path> <operation> [name]",
		Short: "Set the symbol name 'ob codegen' emits for an operation",
		Long: `Set (or clear) the codegen-name override on an operation.

By default 'ob codegen' derives a symbol name from the full operation key, so a
namespace-prefixed key like "openbindings.binding-invoker.invokeBinding" emits
the verbose OpenbindingsBindingInvokerInvokeBinding. Set an override to emit a
friendlier name (e.g. "invokeBinding" becomes InvokeBinding in Go, invokeBinding
in TS) without changing the operation key itself — bindings and the wire still
reference the key verbatim.

The override is stored in the operation's x-ob metadata. It survives
'ob source pull', and is stripped by 'ob purify' along with the rest of x-ob.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob op codegen-name interface.json openbindings.binding-invoker.invokeBinding invokeBinding
  ob op codegen-name interface.json invokeBinding --clear`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 3 {
				name = args[2]
			}
			switch {
			case clear:
				name = ""
			case name == "":
				return app.ExitResult{Code: 2, Message: "provide a name, or pass --clear to remove the override", ToStderr: true}
			}
			result, err := app.OperationSetCodegenName(args[0], args[1], name)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("set codegen name: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the codegen-name override")
	return cmd
}

func newOperationRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rename <obi-path> <old-key> <new-key>",
		Aliases: []string{"mv"},
		Short:   "Rename an operation and update all references",
		Long: `Rename an operation key throughout an OpenBindings interface document.

Updates the operation key, all binding 'operation' fields that reference
it, and binding keys that follow the <operation>.<source> convention.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob operation rename interface.json hello greet
  ob op rename interface.json config.set settings.update`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationRename(args[0], args[1], args[2])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("rename %s → %s in %s: %v", args[1], args[2], args[0], err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}

	return cmd
}

func newOperationRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "remove <obi-path> <key>...",
		Aliases: []string{"rm"},
		Short:   "Remove operations and their bindings from an OBI",
		Long: `Remove one or more operations from an OpenBindings interface document.

All bindings that reference the removed operations are also deleted.

For source-owned operations (derived from a registered source), a warning
is shown because the next 'ob source pull' will re-derive them. Use --force
to suppress the warning, or 'ob source remove' to stop deriving from the
source entirely.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob operation remove interface.json hello
  ob op remove interface.json config.set config.get
  ob op remove interface.json hello --force`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obiPath := args[0]
			keys := args[1:]

			result, err := app.OperationRemove(obiPath, keys, force)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("remove %v from %s: %v", keys, obiPath, err), ToStderr: true}
			}
			return outputEditResult(cmd, obiPath, result)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "remove source-owned operations without warning")

	return cmd
}
