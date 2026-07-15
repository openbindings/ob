package cmd

import (
	"encoding/json"
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
		inputJSON   string
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

A LOCAL FILE artifact is embedded by default: its content rides the spec
'content' field so the document is conformant (OBI-D-05) and works from
anywhere, and the local path is recorded in x-ob metadata as the pull
path. Pass outputLocation= to point at the published URL instead, or
?embed on a URL to fetch and pin a remote artifact.

The document is written to stdout, or to a file with -o. To keep an OBI in
sync with its sources over time, prefer 'ob source add' + 'ob source pull';
synthesize is the one-shot derivation. It is also the operation a
synthesize-capable delegate performs for formats ob does not natively
support.

Machine callers pass the operation's wire input wholesale instead:
--input takes a SynthesizeInterfaceInput as a JSON string (exclusive with
source arguments and metadata flags; formats must be explicit).

Examples:
  ob synthesize openapi.json -o api.obi.json
  ob synthesize usage@2.0.0:./cli.kdl?name=cli --name "Acme CLI"
  ob synthesize api.yaml?embed --name "Acme API" --version 1.0.0
  ob synthesize --input '{"sources":[{"bindingSpec":"openbindings.openapi@1","location":"api.yaml"}]}'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var input app.SynthesizeInterfaceInput

			if inputJSON != "" {
				if len(args) > 0 || name != "" || version != "" || description != "" || obVersion != "" {
					return app.ExitResult{Code: 2, Message: "--input is exclusive with source arguments and metadata flags", ToStderr: true}
				}
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("parse --input: %v", err), ToStderr: true}
				}
			} else {
				input = app.SynthesizeInterfaceInput{
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
					if src.BindingSpec == "" {
						detected, derr := app.DetectSourceFormat(src.Location)
						if derr != nil {
							return app.ExitResult{Code: 2, Message: derr.Error(), ToStderr: true}
						}
						src.BindingSpec = detected
					}
					input.Sources = append(input.Sources, src)
				}
			}

			iface, err := app.SynthesizeInterface(input)
			if err != nil {
				return app.ExitResult{Code: 1, Message: fmt.Sprintf("synthesize interface: %v", err), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			if inputJSON != "" && format == "" {
				// Machine lane: wire input in, wire-shaped output out.
				format = "json"
			}
			// The document channel writes through the one canonical
			// serializer every document-writing command shares, so a
			// synthesize followed by a no-op pull is byte-identical.
			return app.OutputDocument(iface, format, outputPath)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "interface name")
	cmd.Flags().StringVar(&version, "version", "", "interface version")
	cmd.Flags().StringVar(&description, "description", "", "interface description")
	cmd.Flags().StringVar(&obVersion, "spec-version", "", "target OpenBindings spec version (default: latest tested)")
	cmd.Flags().StringVar(&inputJSON, "input", "", "SynthesizeInterfaceInput as a JSON string (machine lane)")

	return cmd
}
