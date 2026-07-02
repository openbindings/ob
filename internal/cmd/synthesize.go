package cmd

import (
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newSynthesizeCmd() *cobra.Command {
	var (
		name        string
		version     string
		description string
		obVersion   string
	)

	cmd := &cobra.Command{
		Use:   "synthesize [source]...",
		Short: "Synthesize an interface from binding sources",
		Long: `Synthesize an OpenBindings interface from binding source artifacts.
Given one or more sources (e.g. an OpenAPI spec, a CLI usage spec), extracts
operations, schemas, sources, and bindings into a new OBI document; with no
sources, produces an empty interface to author by hand (see also 'ob new').

Sources use the same syntax as 'ob inspect' and 'ob source add':
[format:]path[?option&option...], with options name=, outputLocation=,
description=, and embed.

The document is written to stdout, or to a file with -o. To keep an OBI in
sync with its sources over time, prefer 'ob source add' + 'ob source pull';
synthesize is the one-shot derivation. It is also the operation a
synthesize-capable delegate performs for formats ob does not natively
support.

Examples:
  ob synthesize openapi.json -o api.obi.json
  ob synthesize usage@2.0.0:./cli.kdl?name=cli --name "Acme CLI"
  ob synthesize api.yaml?embed --name "Acme API" --version 1.0.0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := app.SynthesizeInterfaceInput{
				OpenBindingsVersion: obVersion,
				Name:                name,
				Version:             version,
				Description:         description,
			}
			for _, s := range args {
				src, err := app.ParseSource(s)
				if err != nil {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("source %q: %v", s, err), ToStderr: true}
				}
				if src.Format == "" {
					detected, derr := app.DetectSourceFormat(src.Location)
					if derr != nil {
						return app.ExitResult{Code: 2, Message: derr.Error(), ToStderr: true}
					}
					src.Format = detected
				}
				input.Sources = append(input.Sources, src)
			}

			iface, err := app.SynthesizeInterface(input)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("synthesize interface: %v", err), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(iface, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "interface name")
	cmd.Flags().StringVar(&version, "version", "", "interface version")
	cmd.Flags().StringVar(&description, "description", "", "interface description")
	cmd.Flags().StringVar(&obVersion, "openbindings", "", "target OpenBindings spec version (default: latest tested)")

	return cmd
}
