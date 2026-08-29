package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/spf13/cobra"
)

func newInspectCmd() *cobra.Command {
	var inputJSON string

	cmd := &cobra.Command{
		Use:   "inspect [source]",
		Short: "Inspect a binding source and list its bindable targets",
		Long: `Inspect a binding source artifact and report the bindable targets it
contains, without creating an interface. Use it to preview the operations
that "ob source pull" would derive from a spec. It is also the operation an
inspect-capable delegate performs for formats ob does not natively support.

Source format: [format:]path[?option...]  (same form as "ob source add")

Pass '-' as the path to read the artifact from stdin (e.g.
openbindings.openapi-3.1@1:-).

Machine callers pass the operation's wire input wholesale instead:
--input takes an InspectSourceInput as a JSON string (exclusive with the
<source> argument; the source's format must be explicit).

Examples:
  ob inspect openapi.json
  ob inspect openbindings.usage@1:./cli.kdl
  ob inspect https://api.example.com/openapi.json
  curl -s https://api.example.com/openapi.json | ob inspect openbindings.openapi-3.1@1:-
  ob inspect --input '{"source":{"bindingSpec":"openbindings.openapi-3.1@1","location":"api.yaml"}}'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var source *openbindings.Source
			fromStdin := false

			switch {
			case inputJSON != "":
				if len(args) > 0 {
					return app.ExitResult{Code: 2, Message: "--input is exclusive with the <source> argument", ToStderr: true}
				}
				var wire struct {
					Source *openbindings.Source `json:"source"`
				}
				if err := json.Unmarshal([]byte(inputJSON), &wire); err != nil {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("parse --input: %v", err), ToStderr: true}
				}
				if wire.Source == nil {
					return app.ExitResult{Code: 2, Message: "--input: source is required", ToStderr: true}
				}
				source = wire.Source
			case len(args) == 1:
				parsed, err := app.ParseSource(args[0])
				if err != nil {
					return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
				}
				// The stdin lane: a location of `-` reads the artifact from
				// stdin (the same filter convention 'ob synthesize' honors).
				// What arrives is content, not a location.
				if parsed.Location == app.StdinLocator {
					fromStdin = true
					data, rerr := io.ReadAll(cmd.InOrStdin())
					if rerr != nil {
						return app.ExitResult{Code: 2, Message: fmt.Sprintf("read stdin: %v", rerr), ToStderr: true}
					}
					spec, content, cerr := app.StdinSourceContent(parsed.BindingSpec, data)
					if cerr != nil {
						return app.ExitResult{Code: 2, Message: cerr.Error(), ToStderr: true}
					}
					source = &openbindings.Source{
						BindingSpec: spec,
						Content:     content,
						Description: parsed.Description,
					}
					break
				}
				// USAGE-P-02: an operator-typed exec address is the explicit
				// authorization; record it durably.
				if strings.HasPrefix(parsed.Location, "exec:") {
					if aerr := app.RecordAuthorizedExec(parsed.Location); aerr == nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "authorized exec address %q (recorded in environment config)\n", parsed.Location)
					}
				}
				if parsed.BindingSpec == "" {
					detected, derr := app.DetectSourceFormat(parsed.Location)
					if derr != nil {
						return app.ExitResult{Code: 2, Message: derr.Error(), ToStderr: true}
					}
					parsed.BindingSpec = detected
				}
				source = &openbindings.Source{
					BindingSpec: parsed.BindingSpec,
					Location:    parsed.Location,
					Description: parsed.Description,
				}
			default:
				return app.ExitResult{Code: 2, Message: "provide a <source> argument or --input", ToStderr: true}
			}

			inspection, err := app.InspectSource(context.Background(), source)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			if inputJSON != "" && format == "" {
				// Machine lane: wire input in, wire-shaped output out.
				format = "json"
			}
			return app.OutputResultText(inspection, format, outputPath, func() string {
				location := source.Location
				if fromStdin {
					location = "<stdin>"
				}
				return app.RenderSourceInspection(source.BindingSpec, location, inspection)
			})
		},
	}

	cmd.Flags().StringVar(&inputJSON, "input", "", "InspectSourceInput as a JSON string (machine lane)")

	return cmd
}
