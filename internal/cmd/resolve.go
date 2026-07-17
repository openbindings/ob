package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/spf13/cobra"
)

func newResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <address>",
		Short: "Resolve an OpenBindings interface from a URL or host",
		Long: `Resolve an OpenBindings interface document from a server.

The address can be a full URL or a host (e.g. localhost:8080).
If no scheme is given, http is used. If the direct URL does not
return an OBI, the tool tries /.well-known/openbindings, and failing
that, synthesizes an interface from the raw spec it finds.

By default the resolved document is written to a file: use -o/--output to
set it, or the filename is derived from the host (e.g. localhost:8080 →
localhost_8080.obi.json).

With -F json (or yaml), no interface file is written: the resolved
interface and what it was synthesized from are printed to stdout instead
(as {"interface": ..., "synthesizedFrom": ...}), and -o writes that JSON.

Examples:
  ob resolve localhost:8080
  ob resolve localhost:8080 -o blend.obi.json
  ob resolve https://api.example.com
  ob resolve https://api.example.com -F json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := strings.TrimSpace(args[0])
			format, outputPath := getOutputFlags(cmd)

			doc, synthesizedFrom, err := app.ResolveOBI(address)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			if format != "" && format != "text" {
				// Wire lane: emit the ResolveInterfaceOutput envelope instead
				// of writing an interface file.
				var iface openbindings.Interface
				if err := json.Unmarshal(doc, &iface); err != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("parse resolved interface: %v", err), ToStderr: true}
				}
				out := app.ResolveInterfaceOutput{Interface: &iface, SynthesizedFrom: synthesizedFrom}
				return app.OutputResult(out, format, outputPath)
			}

			if outputPath == "" {
				outputPath = defaultResolveOutputPath(address)
			}
			if err := app.AtomicWriteFile(outputPath, doc, app.FilePerm); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			msg := "Wrote " + outputPath
			if synthesizedFrom != "" {
				msg = fmt.Sprintf("Synthesized interface from %s\n%s", synthesizedFrom, msg)
			}
			return app.ExitResult{Code: 0, Message: msg, ToStderr: false}
		},
	}
	return cmd
}

// defaultResolveOutputPath returns a safe filename from a URL or host for use as the default -o path.
func defaultResolveOutputPath(urlOrHost string) string {
	u := app.NormalizeURL(urlOrHost)
	if u == "" {
		return "openbindings.obi.json"
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return "openbindings.obi.json"
	}
	host := strings.ReplaceAll(parsed.Host, ":", "_")
	// Avoid path traversal or empty host
	if host == "" || strings.Contains(host, "/") {
		return "openbindings.obi.json"
	}
	return host + ".obi.json"
}
