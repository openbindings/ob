package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

func newResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <url-or-host>",
		Short: "Resolve an OpenBindings interface from a URL or host",
		Long: `Resolve an OpenBindings interface document from a server.

The argument can be a full URL or a host (e.g. localhost:8080).
If no scheme is given, http is used. If the direct URL does not
return an OBI, the tool tries /.well-known/openbindings, and failing
that, synthesizes an interface from the raw spec it finds.

Use -o/--output to set the output file. If omitted, the filename
is derived from the host (e.g. localhost:8080 → localhost_8080.obi.json).

Examples:
  ob resolve localhost:8080
  ob resolve localhost:8080 -o blend.obi.json
  ob resolve https://api.example.com
  ob resolve https://api.example.com -o myapi.obi.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			urlOrHost := strings.TrimSpace(args[0])
			_, outputPath := getOutputFlags(cmd)
			if outputPath == "" {
				outputPath = defaultResolveOutputPath(urlOrHost)
			}

			doc, synthesizedFrom, err := app.ResolveOBI(urlOrHost)
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
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
