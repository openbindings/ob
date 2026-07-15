package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "source",
		Aliases: []string{"src", "sources"},
		Short:   "Manage source references on an OBI",
		Long: `Manage binding source references on an OpenBindings interface document.

Sources are registered references to binding specification artifacts
(e.g., OpenAPI specs, usage specs). Adding a source does not derive
operations — use 'ob source pull' for that.`,
	}

	cmd.AddCommand(
		newSourceAddCmd(),
		newSourcePullCmd(),
		newSourceListCmd(),
		newSourceRemoveCmd(),
	)

	return cmd
}

func newSourceAddCmd() *cobra.Command {
	var (
		key         string
		resolveArg  string
		uriArg      string
		delegateArg string
		description string
		yes         bool
	)

	cmd := &cobra.Command{
		Use:   "add <obi-path> <source>",
		Short: "Register a source reference on an OBI",
		Long: `Register a binding source reference on an OpenBindings interface document.

The source can be a bare file path or an explicit format:path. When a
bare path is given, the format is auto-detected by trying each
registered delegate.

When multiple delegates can handle the source, you are prompted to
choose which delegate to use. Use --delegate to select non-interactively,
or --yes to accept the first capable delegate.

The delegate choice is stored in the source's x-ob metadata so that
'ob source pull' knows which delegate to use later.

A LOCAL FILE artifact is embedded by default: its content rides the
spec 'content' field so the document is conformant (OBI-D-05) and works
from anywhere, and the local path is recorded in x-ob metadata as the
pull path 'ob source pull' refreshes from. URLs and live addresses
(host:port, MCP endpoints) are stored as the spec 'location'.

This does NOT derive operations or create bindings — it only registers
the source reference. Use 'ob source pull' afterward to derive operations
and bindings from the source.

The --resolve flag overrides the default storage mode:
  content   Embed the artifact in the spec 'content' field (the default
            for local files; on a URL, fetches and pins the artifact).
  location  Store a path/URI in the spec 'location' field. A local path
            stored this way is NOT publishable (OBI-D-05); pair it with
            --uri to record the published URL.

--uri sets the spec location field. In location mode it replaces the
local path (the published URL). In content mode it PAIRS with the
embedded artifact (spec §6.4): the artifact's canonical origin for
document formats, or the service's dial address for service-addressed
formats — e.g. pin a .proto and carry its server:

  ob source add my.obi.json grpc:./svc.proto --resolve content --uri api.example.com:443

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob source add my.obi.json openapi.json
  ob source add my.obi.json ./api.yaml --key restApi
  ob source add my.obi.json openapi@3.1:./api.yaml
  ob source add my.obi.json openapi.json --delegate ob
  ob source add my.obi.json 'openapi@3.1:https://example.com/openapi.json?embed'
  ob source add my.obi.json openapi@3.1:./api.yaml --uri https://cdn.example.com/api.yaml`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obiPath := args[0]

			src, err := app.ParseSource(args[1])
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}

			delegateID := delegateArg

			if src.BindingSpec == "" {
				claim, claimErr := selectDelegate(cmd, src.Location, delegateArg, yes)
				if claimErr != nil {
					return claimErr
				}
				src.BindingSpec = claim.BindingSpec
				delegateID = claim.DelegateID
				fmt.Fprintf(cmd.ErrOrStderr(), "detected format: %s (via %s)\n", claim.BindingSpec, claim.DelegateName)
			} else if delegateID == "" {
				// The format is explicit: route by the token through the
				// delegate registry. Probe-detection is for format-less adds
				// only (a probe would try every synthesizer against the
				// location — including ones that dial it as an endpoint).
				resolved, resErr := app.ResolveDelegateForBindingSpec(src.BindingSpec)
				if resErr != nil {
					return resErr
				}
				if resolved.Builtin {
					delegateID = "ob"
				} else {
					delegateID = resolved.Location
				}
			}

			// Source-string options are honored, never silently dropped: the
			// synthesize/inspect/source-add source syntax is one grammar.
			resolve := resolveArg
			if src.Embed {
				if resolveArg == app.ResolveModeLocation {
					return app.ExitResult{Code: 2, Message: "?embed contradicts --resolve location", ToStderr: true}
				}
				resolve = app.ResolveModeContent
			}
			uri := uriArg
			if src.OutputLocation != "" {
				if uriArg != "" && uriArg != src.OutputLocation {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf(
						"?outputLocation=%s contradicts --uri %s", src.OutputLocation, uriArg), ToStderr: true}
				}
				uri = src.OutputLocation
			}
			desc := description
			if desc == "" {
				desc = src.Description
			}

			sourceKey := key
			if sourceKey == "" && src.Name != "" {
				sourceKey = src.Name
			}
			if sourceKey == "" {
				derived := app.DeriveSourceKey(app.SynthesizeInterfaceSource{
					BindingSpec: src.BindingSpec,
					Location:    src.Location,
				}, 0)
				sourceKey, err = promptSourceName(derived, yes)
				if err != nil {
					return err
				}
			}

			// USAGE-P-02: the operator typed this exec address — that is the
			// explicit authorization; record it durably so later
			// dereferences (pull, invoke) are authorized too.
			if strings.HasPrefix(src.Location, "exec:") {
				if aerr := app.RecordAuthorizedExec(src.Location); aerr == nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "authorized exec address %q (recorded in environment config)\n", src.Location)
				}
			}
			result, err := app.SourceAdd(app.SourceAddInput{
				OBIPath:     obiPath,
				Format:      src.BindingSpec,
				Location:    src.Location,
				Key:         sourceKey,
				Resolve:     resolve,
				URI:         uri,
				Delegate:    delegateID,
				Description: desc,
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			return outputEditResult(cmd, obiPath, result)
		},
	}

	cmd.Flags().StringVar(&key, "key", "", "explicit source key (default: derived from format and path)")
	cmd.Flags().StringVar(&resolveArg, "resolve", "", "resolution mode: location (default) or content")
	cmd.Flags().StringVar(&uriArg, "uri", "", "explicit published URI for location mode")
	cmd.Flags().StringVar(&delegateArg, "delegate", "", "delegate to use for this source (skips detection)")
	cmd.Flags().StringVar(&description, "description", "", "human-readable description for this source")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept first capable delegate without prompting")

	return cmd
}

// selectDelegate discovers capable delegates for a source and either
// auto-selects or prompts the user to choose one.
func selectDelegate(cmd *cobra.Command, location, delegateArg string, yes bool) (app.DelegateClaim, error) {
	claims, err := withSpinner(cmd.ErrOrStderr(), "Checking delegates…", func() ([]app.DelegateClaim, error) {
		return app.DetectSourceCandidates(location)
	})
	if err != nil {
		return app.DelegateClaim{}, app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}

	// If --delegate was specified, find that specific one.
	if delegateArg != "" {
		for _, c := range claims {
			if c.DelegateName == delegateArg || c.DelegateID == delegateArg {
				return c, nil
			}
		}
		return app.DelegateClaim{}, app.ExitResult{
			Code:     1,
			Message:  fmt.Sprintf("delegate %q is not capable of handling this source; capable: %s", delegateArg, claimNames(claims)),
			ToStderr: true,
		}
	}

	if len(claims) == 0 {
		return app.DelegateClaim{}, app.ExitResult{
			Code:     1,
			Message:  "no delegates can handle this source",
			ToStderr: true,
		}
	}

	// --yes or non-TTY — auto-select first capable delegate.
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))
	if yes || !isTTY {
		return claims[0], nil
	}

	// Interactive: always prompt, even with a single candidate.
	return promptDelegateSelection(claims)
}

func promptDelegateSelection(claims []app.DelegateClaim) (app.DelegateClaim, error) {
	options := make([]huh.Option[int], len(claims))
	for i, c := range claims {
		label := fmt.Sprintf("%s — %s (%d ops, %d bindings)",
			c.DelegateID, c.BindingSpec, c.OperationCount, c.BindingCount)
		options[i] = huh.NewOption(label, i)
	}

	var title string
	if len(claims) == 1 {
		title = "One delegate can handle this source"
	} else {
		title = fmt.Sprintf("%d delegates can handle this source", len(claims))
	}

	var selected int
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(title).
				Description("Which delegate should handle this source?").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return app.DelegateClaim{}, app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}

	return claims[selected], nil
}

// promptSourceName asks the user to confirm or edit the source key name.
// In non-interactive mode (--yes or non-TTY), returns the default.
func promptSourceName(defaultName string, yes bool) (string, error) {
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))
	if yes || !isTTY {
		return defaultName, nil
	}

	name := defaultName
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Source name").
				Description("A short identifier for this source in the OBI").
				Value(&name).
				Placeholder(defaultName),
		),
	)

	if err := form.Run(); err != nil {
		return "", app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
	}
	if name == "" {
		name = defaultName
	}
	return name, nil
}

func claimNames(claims []app.DelegateClaim) string {
	names := make([]string, len(claims))
	for i, c := range claims {
		names[i] = c.DelegateName
	}
	return strings.Join(names, ", ")
}

func newSourcePullCmd() *cobra.Command {
	var pure bool

	cmd := &cobra.Command{
		Use:   "pull <obi-path> [source-key]...",
		Short: "Derive operations and bindings from registered sources",
		Long: `Derive operations and bindings from a registered source into the OBI.

On first run this creates the source's operations and bindings; on later
runs it overwrites the source-owned objects and prunes ones the source no
longer emits. Hand-authored operations and bindings are never touched.

Unlike a three-way merge, pull does not reconcile local edits — the source
is authoritative for what it owns. To fold upstream changes into a
hand-edited operation, use 'ob merge --from-sources'.

To preview what pull would apply without writing, use 'ob status <obi-path>'.

With no source keys, every registered source is pulled.

Use --pure with -o to write a clean, spec-only copy (x-ob metadata
stripped) suitable for publishing.

Pass '-' as <obi-path> to read the document from stdin and write the
pulled document to stdout (the change log moves to stderr). Relative
x-ob pull paths then resolve against the current directory.

Examples:
  ob source pull interface.json
  ob source pull interface.json openapi
  ob source pull interface.json -o dist/interface.json --pure
  ob source add interface.json openapi.json && ob source pull interface.json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, outputPath := getOutputFlags(cmd)
			result, err := app.SourcePull(app.SourcePullInput{
				OBIPath:    args[0],
				SourceKeys: args[1:],
				OutputPath: outputPath,
				Format:     format,
				Pure:       pure,
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("pull sources in %s: %v", args[0], err), ToStderr: true}
			}
			// -o names the document's destination (SourcePull wrote it there);
			// the summary goes to the terminal, never to that same path. With
			// `-` and no -o the document rode stdout, so the summary moves to
			// stderr (the filter lane). An incomplete pull (a tracked source
			// could not be read or derived) exits non-zero: "Pull complete"
			// over 0/N failures is how stale documents pass CI.
			exitCode := 0
			if len(result.Failed) > 0 {
				exitCode = 1
			}
			if args[0] == app.StdinLocator && outputPath == "" {
				return app.OutputResultStderrWithCode(result, format, "", exitCode)
			}
			return app.OutputResultWithCode(result, format, "", exitCode)
		},
	}

	cmd.Flags().BoolVar(&pure, "pure", false, "strip x-ob metadata from the output (publish-clean); requires -o")
	return cmd
}

func newSourceListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list <obi-path>",
		Aliases: []string{"ls"},
		Short:   "List source references on an OBI",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.SourceList(args[0])
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}
	return cmd
}

func newSourceRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <obi-path> <key>",
		Aliases: []string{"rm"},
		Short:   "Remove a source reference from an OBI",
		Long: `Remove a binding source reference from an OpenBindings interface document.

The source entry and the bindings that reference it are removed. Operations
are always preserved — ones left with no bindings at all are listed in a
warning so you can decide whether to keep, rebind, or remove them.

Removing a source that is not registered succeeds with nothing removed.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.SourceRemove(args[0], args[1])
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			return outputEditResult(cmd, args[0], result)
		},
	}

	return cmd
}
