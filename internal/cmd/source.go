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
operations — use 'ob sync' for that.`,
	}

	cmd.AddCommand(
		newSourceAddCmd(),
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
'ob sync' knows which delegate to use later.

The source path is stored relative to the OBI file's directory, so the
reference works regardless of where you run commands from.

This does NOT derive operations or create bindings — it only registers
the source reference. Use 'ob sync' afterward to derive operations
and bindings from the source.

The --resolve flag controls how the source is stored in the OBI:
  location  (default) Store a path/URI in the spec 'location' field.
  content   Read the source and embed its content in the spec 'content' field.

When --resolve=location and --uri is provided, the spec location field
uses the given URI instead of the local path.

Examples:
  ob source add my.obi.json openapi.json
  ob source add my.obi.json ./api.yaml --key restApi
  ob source add my.obi.json openapi@3.1:./api.yaml
  ob source add my.obi.json openapi.json --delegate ob
  ob source add my.obi.json usage@2.13.1:./cli.kdl --resolve content
  ob source add my.obi.json openapi@3.1:./api.yaml --uri https://cdn.example.com/api.yaml`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obiPath := args[0]

			src, err := app.ParseSource(args[1])
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}

			delegateID := delegateArg

			if src.Format == "" {
				claim, claimErr := selectDelegate(cmd, src.Location, delegateArg, yes)
				if claimErr != nil {
					return claimErr
				}
				src.Format = claim.FormatToken
				delegateID = claim.DelegateID
				fmt.Fprintf(cmd.ErrOrStderr(), "detected format: %s (via %s)\n", claim.FormatToken, claim.DelegateName)
			} else if delegateID == "" {
				claim, claimErr := selectDelegate(cmd, src.Location, delegateArg, yes)
				if claimErr != nil {
					return claimErr
				}
				delegateID = claim.DelegateID
			}

			sourceKey := key
			if sourceKey == "" {
				derived := app.DeriveSourceKey(app.CreateInterfaceSource{
					Format:   src.Format,
					Location: src.Location,
				}, 0)
				sourceKey, err = promptSourceName(derived, yes)
				if err != nil {
					return err
				}
			}

			result, err := app.SourceAdd(app.SourceAddInput{
				OBIPath:  obiPath,
				Format:   src.Format,
				Location: src.Location,
				Key:      sourceKey,
				Resolve:  resolveArg,
				URI:      uriArg,
				Delegate: delegateID,
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&key, "key", "", "explicit source key (default: derived from format and path)")
	cmd.Flags().StringVar(&resolveArg, "resolve", "", "resolution mode: location (default) or content")
	cmd.Flags().StringVar(&uriArg, "uri", "", "explicit published URI for location mode")
	cmd.Flags().StringVar(&delegateArg, "delegate", "", "delegate to use for this source (skips detection)")
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
			Code:    1,
			Message: "no delegates can handle this source",
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
			c.DelegateID, c.FormatToken, c.OperationCount, c.BindingCount)
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

This removes only the source entry. Operations and bindings that reference
this source are preserved — the user decides what to do with them.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.SourceRemove(args[0], args[1])
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result, format, outputPath)
		},
	}

	return cmd
}
