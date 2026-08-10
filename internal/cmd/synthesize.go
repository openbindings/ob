package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

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

Pass '-' as the path to read the artifact from stdin (e.g.
openbindings.openapi@5:-). A stdin artifact is content, not a location:
it embeds in the document exactly like a wire-supplied content source,
with no pull path recorded (outputLocation= still sets the spec-level
location, as on every lane). At most one source may read from stdin.

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
  ob synthesize openbindings.usage@1:./cli.kdl?name=cli --name "Acme CLI"
  ob synthesize api.yaml?embed --name "Acme API" --version 1.0.0
  curl -s https://api.example.com/openapi.json | ob synthesize openbindings.openapi@5:- -o api.obi.json
  ob synthesize --input '{"sources":[{"bindingSpec":"openbindings.openapi@5","location":"api.yaml"}]}'`,
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
				stdinUsed := false
				for _, s := range args {
					src, err := app.ParseSource(s)
					if err != nil {
						return app.ExitResult{Code: 2, Message: fmt.Sprintf("source %q: %v", s, err), ToStderr: true}
					}
					// The stdin lane: a location of `-` reads the artifact
					// from stdin (the filter convention the editing family's
					// <obi-path> already honors). What arrives is content,
					// not a location — the source entry carries it inline
					// exactly like a wire-supplied content source, with no
					// pull path recorded.
					if src.Location == app.StdinLocator {
						if stdinUsed {
							return app.ExitResult{Code: 2, Message: fmt.Sprintf("source %q: stdin (-) can supply at most one source", s), ToStderr: true}
						}
						stdinUsed = true
						data, rerr := io.ReadAll(cmd.InOrStdin())
						if rerr != nil {
							return app.ExitResult{Code: 2, Message: fmt.Sprintf("source %q: read stdin: %v", s, rerr), ToStderr: true}
						}
						spec, content, cerr := app.StdinSourceContent(src.BindingSpec, data)
						if cerr != nil {
							return app.ExitResult{Code: 2, Message: fmt.Sprintf("source %q: %v", s, cerr), ToStderr: true}
						}
						src.BindingSpec = spec
						src.Content = content
						src.Location = ""
						input.Sources = append(input.Sources, src)
						continue
					}
					// USAGE-P-02: an operator-typed exec address is the
					// explicit authorization; record it durably.
					if strings.HasPrefix(src.Location, "exec:") {
						if aerr := app.RecordAuthorizedExec(src.Location); aerr == nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "authorized exec address %q (recorded in environment config)\n", src.Location)
						}
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
