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
		newOperationRenameCmd(),
		newOperationRemoveCmd(),
	)

	return cmd
}

func newOperationInvokeCmd() *cobra.Command {
	var bindingKey string
	var inputJSON string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "invoke <obi-path> [operation]",
		Short: "Invoke an operation via a binding",
		Long: `Invoke an operation from an OpenBindings interface.

Every operation is a stream. One JSON value per event is printed to
stdout. Unary operations produce one line and exit. Streaming
operations produce lines until the stream closes or Ctrl-C.

The operation key is a positional argument. The most-preferred
binding for that operation is used automatically.

Alternatively, use --binding to select a specific binding directly;
the operation is derived from the binding entry.

Context (credentials, headers, etc.) is automatically resolved from
the target URL. Use 'ob context set <url>' to configure context.

Use -v/--verbose to emit binding key and total duration on stderr.

Examples:
  ob op invoke interface.json listPets --input '{"limit":10}'
  ob op invoke interface.json echo
  ob op invoke interface.json --binding listPets.openapi --input '{"limit":10}'
  ob op invoke interface.json listPets -v`,
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

			var input any
			if inputJSON != "" {
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("invalid --input JSON: %v", err), ToStderr: true}
				}
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			ch, err := app.InvokeOBIOperation(ctx, obiFile, operationKey, bindingKey, input)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("invoke %s in %s: %v", operationKey, obiFile, err), ToStderr: true}
			}

			if verbose {
				fmt.Fprintf(os.Stderr, "operation: %s\n", operationKey)
			}

			start := time.Now()
			enc := json.NewEncoder(os.Stdout)
			hadError := false
			for ev := range ch {
				if ev.Error != nil {
					fmt.Fprintf(os.Stderr, "error: %s\n", ev.Error.Message)
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
	cmd.Flags().StringVar(&inputJSON, "input", "", "operation input as JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show binding key and duration on stderr")

	return cmd
}

func newOperationPrepareCmd() *cobra.Command {
	var bindingKey string

	cmd := &cobra.Command{
		Use:   "prepare <obi-path> [operation]",
		Short: "Preflight an operation's required context without invoking it",
		Long: `Report the context invoking an operation would require, without invoking
it or causing any side effect.

Resolves the operation (or, with --binding, a specific binding) to a
concrete binding and reports its context requirements, or reports none
when they cannot be determined without invoking. This is advisory: the
reactive CONTEXT_REQUIRED error from 'ob op invoke' is authoritative.

Examples:
  ob op prepare interface.json createOrder
  ob op prepare interface.json --binding createOrder.openapi`,
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

			details, err := app.PrepareOperation(context.Background(), obiFile, operationKey, bindingKey, nil)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("prepare %s in %s: %v", operationKey, obiFile, err), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(app.PrepareOperationOutput{Details: details}, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&bindingKey, "binding", "", "binding key to preflight (operation is derived from the entry)")

	return cmd
}

func newOperationListCmd() *cobra.Command {
	var tagFilter string

	cmd := &cobra.Command{
		Use:     "list <obi-path>",
		Aliases: []string{"ls"},
		Short:   "List operations on an OBI",
		Long: `List all operations defined in an OpenBindings interface document.

Shows each operation's key, tags, managed status, and binding count.

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
description, satisfaction aliases, tags, and schemas. The operation is
added without any bindings — bind it to a source with 'ob operation bind'.

Schema flags accept inline JSON, '@path' to read a file, or '-' for stdin.

Examples:
  ob op add interface.json createUser --description "Create a new user"
  ob op add interface.json get --input-schema @get-input.json --output-schema @get-output.json
  ob op add interface.json get --alias openbindings.kv-store.get
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
			if idempotent != "" {
				val := idempotent == "true"
				addInput.Idempotent = &val
			}

			result, err := app.OperationAdd(addInput)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("add operation: %v", err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "operation description")
	cmd.Flags().StringArrayVar(&aliases, "alias", nil, "satisfaction alias: another interface's operation key this satisfies (repeatable)")
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
		Short:   "Manage an operation's satisfaction aliases",
		Long: `Manage the satisfaction aliases on an operation.

An alias is another interface's operation key that this operation also
answers to. Key and aliases form one flat, document-unique namespace
(OBI-T-12): an operation satisfies a published interface by carrying that
interface's operation key as an alias.`,
	}

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
		Short: "Add satisfaction alias(es) to an operation",
		Long: `Add one or more satisfaction aliases to an operation.

The operation may be referenced by its key or any existing identifier.
Each alias must be free in the document's flat key+alias namespace.

Examples:
  ob op alias add interface.json acme.cache.fetch openbindings.kv-store.get
  ob op alias add interface.json describe openbindings.software-descriptor.describe`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationAliasAdd(args[0], args[1], args[2:])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("add alias: %v", err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	return cmd
}

func newOperationAliasRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <obi-path> <operation> <alias>...",
		Aliases: []string{"rm"},
		Short:   "Remove satisfaction alias(es) from an operation",
		Long: `Remove one or more satisfaction aliases from an operation.

Examples:
  ob op alias rm interface.json acme.cache.fetch openbindings.kv-store.get`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationAliasRemove(args[0], args[1], args[2:])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("remove alias: %v", err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	return cmd
}

func newOperationAliasListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list <obi-path> [operation]",
		Aliases: []string{"ls"},
		Short:   "Show the satisfaction map (which ops satisfy which interfaces)",
		Long: `List the satisfaction aliases in an interface — each operation and the
interface operations it satisfies. With no operation argument, shows every
operation that carries aliases (a quick "what does this OBI satisfy?").
Scoped to one operation when given.

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
			if idempotent != "" {
				v := idempotent == "true"
				setInput.Idempotent = &v
			}
			if deprecated != "" {
				v := deprecated == "true"
				setInput.Deprecated = &v
			}
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
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
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

Examples:
  ob op detach interface.json getA`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationDetach(args[0], args[1])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("detach operation: %v", err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
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
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	cmd.Flags().BoolVar(&transformStub, "transform-stub", false, "scaffold identity transform stub(s) on shape mismatch")
	cmd.Flags().StringVar(&inputTransform, "input-transform", "", "inline JSONata transforming operation input to binding input")
	cmd.Flags().StringVar(&outputTransform, "output-transform", "", "inline JSONata transforming binding output to operation output")
	cmd.Flags().Float64Var(&preference, "preference", 0, "binding selection preference (higher = more preferred)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing binding for this operation+source (re-point)")
	return cmd
}

func newOperationUnbindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unbind <obi-path> <operation> <source>",
		Short: "Remove a binding from an operation (keep the operation)",
		Long: `Remove the binding connecting an operation to a source, leaving the
operation in place. Useful for re-pointing to a different backend.

Examples:
  ob op unbind interface.json greet openapi`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationUnbind(args[0], args[1], args[2])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("unbind operation: %v", err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
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
		if len(ops.Operations) == 0 {
			return "", "", "", fmt.Errorf("no operations to bind; add one with 'ob operation add'")
		}
		opts := make([]huh.Option[string], len(ops.Operations))
		for i, e := range ops.Operations {
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
		if len(srcs.Sources) == 0 {
			return "", "", "", fmt.Errorf("no sources registered; add one with 'ob source add'")
		}
		opts := make([]huh.Option[string], len(srcs.Sources))
		for i, e := range srcs.Sources {
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

func newOperationRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rename <obi-path> <old-key> <new-key>",
		Aliases: []string{"mv"},
		Short:   "Rename an operation and update all references",
		Long: `Rename an operation key throughout an OpenBindings interface document.

Updates the operation key, all binding 'operation' fields that reference
it, and binding keys that follow the <operation>.<source> convention.

Examples:
  ob operation rename interface.json hello greet
  ob op rename interface.json config.set settings.update`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.OperationRename(args[0], args[1], args[2])
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("rename %s → %s in %s: %v", args[1], args[2], args[0], err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
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

For managed operations (those with x-ob metadata from sync), a warning
is shown because the next 'ob sync' will recreate them. Use --force to
suppress the warning, or 'ob source remove' to stop syncing from the
source entirely.

Examples:
  ob operation remove interface.json hello
  ob op remove interface.json config.set config.get
  ob op remove interface.json hello --force`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obiPath := args[0]
			keys := args[1:]

			// Warn about managed operations unless --force.
			if !force {
				if warning := checkManagedOps(obiPath, keys); warning != "" {
					return app.ExitResult{Code: 1, Message: warning, ToStderr: true}
				}
			}

			result, err := app.OperationRemove(obiPath, keys)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("remove %v from %s: %v", keys, obiPath, err), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "remove managed operations without warning")

	return cmd
}

// checkManagedOps loads the OBI and returns a warning string if any of the
// given operation keys are managed (have x-ob metadata). Returns "" if none are managed.
func checkManagedOps(obiPath string, keys []string) string {
	result, err := app.OperationList(obiPath, "")
	if err != nil {
		return "" // let the actual remove call surface the error
	}

	managed := map[string]bool{}
	for _, op := range result.Operations {
		if app.HasXOB(op.Operation.LosslessFields) {
			managed[op.Key] = true
		}
	}

	var warn []string
	for _, key := range keys {
		if managed[key] {
			warn = append(warn, key)
		}
	}

	if len(warn) == 0 {
		return ""
	}

	return "managed operations (will be recreated by sync): " +
		joinKeys(warn) +
		"\nuse --force to remove anyway, or 'ob source remove' to stop syncing"
}

func joinKeys(keys []string) string {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = "\"" + k + "\""
	}
	return strings.Join(quoted, ", ")
}
