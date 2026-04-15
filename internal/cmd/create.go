package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/openbindings-go"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newCreateCmd() *cobra.Command {
	var (
		toVersion   string
		name        string
		version     string
		description string
		yes         bool
	)

	cmd := &cobra.Command{
		Use:   "create [sources...]",
		Short: "Create an OpenBindings interface from binding source artifacts",
		Long: `Create an OpenBindings interface from one or more binding source artifacts.

Sources can be bare file paths (format auto-detected) or explicit
format:path references.

Source format: [format:]path[?option&option...]

Options (after ? delimiter):
  name=X             Key name in sources
  outputLocation=Y   Location to use in output (instead of input path)
  description=Z      Description for this binding source
  embed              Embed content inline (JSON/YAML only)

Examples:
  ob create                                      # Interactive if TTY
  ob create --yes                                # Accept defaults, no prompts
  ob create openapi.json                         # Auto-detect format
  ob create usage@2.13.1:./cli.kdl               # Explicit format
  ob create "usage@2.13.1:./cli.kdl?name=cli"    # With custom source name
  ob create openapi.json asyncapi.yaml           # Multiple sources`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var sources []app.CreateInterfaceSource
			for _, arg := range args {
				src, err := app.ParseSource(arg)
				if err != nil {
					return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
				}
				if src.Format == "" {
					claim, claimErr := selectDelegate(cmd, src.Location, "", yes)
					if claimErr != nil {
						return claimErr
					}
					src.Delegate = claim.DelegateID
					src.Format = claim.FormatToken
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "detected format: %s (via %s)\n", claim.FormatToken, claim.DelegateName)
				} else {
					src.Delegate = "ob"
				}
				if src.Name == "" {
					derived := app.DeriveSourceKey(src, len(sources))
					sourceName, nameErr := promptSourceName(derived, yes)
					if nameErr != nil {
						return nameErr
					}
					src.Name = sourceName
				}
				sources = append(sources, src)
			}

			input := app.CreateInterfaceInput{
				OpenBindingsVersion: toVersion,
				Sources:             sources,
				Name:                name,
				Version:             version,
				Description:         description,
			}

			iface, err := app.CreateInterface(input)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)

			isTTY := term.IsTerminal(int(os.Stdin.Fd()))
			if isTTY && !yes && iface != nil {
				updated, err := promptInterfaceMetadata(*iface)
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				iface = &updated
			}

			if outputPath != "" && iface != nil {
				if err := app.WriteInterfaceToPath(outputPath, iface, format); err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				return app.ExitResult{Code: 0, Message: "Wrote " + outputPath, ToStderr: false}
			}

			return app.OutputResultText(iface, format, outputPath, func() string {
				return app.RenderInterface(iface)
			})
		},
	}

	cmd.Flags().StringVar(&toVersion, "to", "", "target OpenBindings version (e.g., 0.1.0)")
	cmd.Flags().StringVar(&name, "name", "", "interface name override")
	cmd.Flags().StringVar(&version, "version", "", "interface version override")
	cmd.Flags().StringVar(&description, "description", "", "interface description override")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept defaults without prompting")

	return cmd
}

// promptInterfaceMetadata prompts the user to confirm or edit interface metadata.
func promptInterfaceMetadata(iface openbindings.Interface) (openbindings.Interface, error) {
	name := iface.Name
	version := iface.Version
	description := iface.Description

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Name").
				Description("Human-readable name").
				Value(&name).
				Placeholder(app.DefaultInterfaceName),

			huh.NewInput().
				Title("Version").
				Description("Interface version (e.g., 1.0.0)").
				Value(&version).
				Placeholder(""),

			huh.NewText().
				Title("Description").
				Description("What does this interface do?").
				Value(&description).
				Placeholder("").
				CharLimit(500),
		),
	)

	if err := form.Run(); err != nil {
		return iface, err
	}

	iface.Name = name
	iface.Version = version
	iface.Description = description

	return iface, nil
}
