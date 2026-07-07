package cmd

import (
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var (
		name        string
		version     string
		description string
		obVersion   string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "new <path>",
		Short: "Create an empty OpenBindings interface document",
		Long: `Create an empty OpenBindings interface document — only the core fields
(openbindings, name, version, description) and an empty operations map.

This is the authorship entry point. Populate the new interface with
'ob operation add' (hand-authored ops), 'ob source add' + 'ob source pull'
(derive from a binding source), and 'ob operation bind' (wire an op to a
source). To satisfy a published interface, see 'ob operation alias add'.

Pass '-' as <path> to write the new document to stdout instead of a file
(the summary moves to stderr), ready to pipe into further edits.

Examples:
  ob new interface.obi.json --name "Acme API" --version 0.1.0
  ob new svc.obi.json --name svc --version 1.0.0 --description "My service"
  ob new - --name svc | ob operation add - greet`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.NewInterface(app.NewInterfaceInput{
				Path:         args[0],
				Name:         name,
				Version:      version,
				Description:  description,
				OpenBindings: obVersion,
				Force:        force,
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("create interface: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "interface name")
	cmd.Flags().StringVar(&version, "version", "", "interface version")
	cmd.Flags().StringVar(&description, "description", "", "interface description")
	cmd.Flags().StringVar(&obVersion, "openbindings", "", "target OpenBindings spec version (default: latest tested)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")

	return cmd
}

func newMetaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "meta",
		Short: "Manage interface-level metadata",
		Long: `Manage an interface's top-level metadata (name, version, description).

Note: homepage/repository/maintainer are software-descriptor fields
(see 'ob operation alias add ... openbindings.software-descriptor.describe'),
not interface metadata.`,
	}
	cmd.AddCommand(newMetaSetCmd())
	return cmd
}

func newMetaSetCmd() *cobra.Command {
	var (
		name        string
		version     string
		description string
	)

	cmd := &cobra.Command{
		Use:   "set <obi-path>",
		Short: "Edit interface metadata (name, version, description)",
		Long: `Edit an interface's top-level metadata. Only the flags you pass are
changed.

Pass '-' as <obi-path> to read the document from stdin and write the
modified document to stdout (the summary moves to stderr).

Examples:
  ob meta set interface.obi.json --version 0.2.0
  ob meta set interface.obi.json --name "Acme API" --description "..."`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in := app.MetaSetInput{Path: args[0]}
			if cmd.Flags().Changed("name") {
				in.Name = &name
			}
			if cmd.Flags().Changed("version") {
				in.Version = &version
			}
			if cmd.Flags().Changed("description") {
				in.Description = &description
			}
			result, err := app.MetaSet(in)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("set metadata: %v", err), ToStderr: true}
			}
			return outputEditResult(cmd, args[0], result)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "set the interface name")
	cmd.Flags().StringVar(&version, "version", "", "set the interface version")
	cmd.Flags().StringVar(&description, "description", "", "set the interface description")

	return cmd
}
