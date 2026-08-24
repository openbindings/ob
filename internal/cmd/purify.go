package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	var check bool

	cmd := &cobra.Command{
		Use:   "purify <obi-path>",
		Short: "Strip x-ob vendor metadata, yielding a spec-only interface",
		Long: `Strip x-ob vendor metadata from an interface, yielding a spec-only
document suitable for publishing or handing to another tool.

purify is implemented as an operation-graph (a single JSONata transform), so it
runs through the same graph machinery ob exposes to everyone else. It produces
the same result as ` + "`ob source pull --pure`" + `.

With --check, nothing is written: the command exits 0 when the document is
already pure and 1 with the x-ob locations otherwise — the mechanical purity
gate a registry or CI pre-publish step needs (gofmt -l style).`,
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

			if check {
				paths := app.FindXOBPaths(doc)
				if len(paths) == 0 {
					return app.ExitResult{Code: 0, Message: "pure (no x-ob metadata)"}
				}
				return app.ExitResult{Code: 1, Message: fmt.Sprintf(
					"not pure — x-ob metadata at %d location(s):\n  %s",
					len(paths), strings.Join(paths, "\n  ")), ToStderr: true}
			}

			result := app.InvokeOperationWithContext(context.Background(), app.InvocationInput{
				Source:   app.InvokeSource{BindingSpec: "openbindings.operation-graph@1", Content: json.RawMessage(purifyGraph)},
				Selector: "#/graphs/purify",
				Input:    doc,
			})
			if result.Error != nil {
				return app.ExitResult{Code: 1, Message: result.Error.Message, ToStderr: true}
			}

			// The strip is mechanical; conformance is not. Surface residual
			// spec problems (a relative location, say) so "purify then
			// publish" cannot silently ship an invalid document.
			if problems := app.ValidateDocumentValue(result.Output); len(problems) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: purified document is still not conformant (%d problem(s)); run 'ob validate' for details\n",
					len(problems))
			}

			format, outputPath := getOutputFlags(cmd)
			// The document channel writes through the shared canonical
			// serializer (same bytes regardless of which command wrote them).
			return app.OutputDocument(result.Output, format, outputPath)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "check purity without writing: exit 1 and list x-ob locations if anything would be stripped")
	return cmd
}
