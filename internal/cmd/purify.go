package cmd

import (
	"context"
	"encoding/json"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

// purifyGraph implements purifyInterface as a transform-only operation-graph:
// a single JSONata node that deletes x-ob at the four structural levels
// (document root, each source, each operation, each binding) AND recursively
// within the schema bodies (each operation's input/output and the shared
// schemas section, at any depth), so the synthesis floor-stamp
// ({"x-ob":{"floor":...}} inside an output schema) strips too — matching
// app.StripAllXOB. The recursive $strip lambda removes every x-ob key it
// finds under those schema roots. Running ob purify therefore exercises ob's
// own operation-graph processor rather than calling the Go stripper directly.
const purifyGraph = `{"graphs":{"purify":{"openbindings.operation-graph":"0.2.0","nodes":{"in":{"type":"input"},"strip":{"type":"transform","transform":"($strip := function($v) {($type($v) = \"object\" ? $merge($each($sift($v, function($val, $k) { $k != \"x-ob\" }), function($val, $k) { {$k: $strip($val)} })) : $type($v) = \"array\" ? [$map($v, function($e) { $strip($e) })] : $v)}; $doc := $ ~> |$|{},[\"x-ob\"]| ~> |sources.*|{},[\"x-ob\"]| ~> |bindings.*|{},[\"x-ob\"]|; $doc := $doc ~> |operations.*|{\"input\": $exists(input) ? $strip(input), \"output\": $exists(output) ? $strip(output)},[\"x-ob\"]|; $exists($doc.schemas) ? ($doc ~> |$|{\"schemas\": $strip(schemas)}|) : $doc)"},"out":{"type":"output"}},"edges":[{"from":"in","to":"strip"},{"from":"strip","to":"out"}]}}}`

func newPurifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purify <obi-path>",
		Short: "Strip x-ob vendor metadata, yielding a spec-only interface",
		Long: `Strip x-ob vendor metadata from an interface, yielding a spec-only
document suitable for publishing or handing to another tool.

purify is implemented as an operation-graph (a single JSONata transform), so it
runs through the same graph machinery ob exposes to everyone else. It produces
the same result as ` + "`ob source pull --pure`" + `.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := app.ReadDocumentBytes(args[0])
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			var doc any
			if err := json.Unmarshal(data, &doc); err != nil {
				return app.ExitResult{Code: 1, Message: "parse interface: " + err.Error(), ToStderr: true}
			}

			result := app.InvokeOperationWithContext(context.Background(), app.InvokeOperationInput{
				Source: app.InvokeSource{Format: "openbindings.operation-graph@0.2.0", Content: purifyGraph},
				Ref:    "#/graphs/purify",
				Input:  doc,
			})
			if result.Error != nil {
				return app.ExitResult{Code: 1, Message: result.Error.Message, ToStderr: true}
			}

			format, outputPath := getOutputFlags(cmd)
			return app.OutputResult(result.Output, format, outputPath)
		},
	}
	return cmd
}
