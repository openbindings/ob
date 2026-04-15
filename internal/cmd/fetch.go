package cmd

import (
	"net/url"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch <url-or-host>",
		Short: "Download an OpenBindings interface from a URL or host",
		Long: `Fetch an OpenBindings interface document from a server.

The argument can be a full URL or a host (e.g. localhost:8080).
If no scheme is given, http is used. If the direct URL does not
return an OBI, the tool tries /.well-known/openbindings.

Use -o/--output to set the output file. If omitted, the filename
is derived from the host (e.g. localhost:8080 → localhost_8080.obi.json).

Examples:
  ob fetch localhost:8080
  ob fetch localhost:8080 -o blend.obi.json
  ob fetch https://api.example.com
  ob fetch https://api.example.com -o myapi.obi.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			urlOrHost := strings.TrimSpace(args[0])
			_, outputPath := getOutputFlags(cmd)
			if outputPath == "" {
				outputPath = defaultFetchOutputPath(urlOrHost)
			}

			doc, err := app.FetchOBI(urlOrHost)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}

			if err := app.AtomicWriteFile(outputPath, doc, app.FilePerm); err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			return app.ExitResult{Code: 0, Message: "Wrote " + outputPath, ToStderr: false}
		},
	}
	return cmd
}

// defaultFetchOutputPath returns a safe filename from a URL or host for use as the default -o path.
func defaultFetchOutputPath(urlOrHost string) string {
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
