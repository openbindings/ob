package cmd

import (
	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

// getOutputFlags returns the global --format and -o/--output (path) from the root command.
// -o/--output = output path (file to write). --format/-F = output format (json|yaml|text|quiet).
func getOutputFlags(c *cobra.Command) (format string, outputPath string) {
	format, _ = c.Root().PersistentFlags().GetString("format")
	outputPath, _ = c.Root().PersistentFlags().GetString("output")
	return format, outputPath
}

// validateSourceModeArgs validates the mutual exclusion between positional OBI args
// and --from-sources / --only flags used by diff and merge commands.
func validateSourceModeArgs(args []string, fromSources bool, onlySource string) error {
	if len(args) == 2 && fromSources {
		return app.ExitResult{
			Code:     2,
			Message:  "cannot use both a positional OBI argument and --from-sources",
			ToStderr: true,
		}
	}
	if len(args) < 2 && !fromSources {
		return app.ExitResult{
			Code:     2,
			Message:  "either provide two OBI arguments or use --from-sources",
			ToStderr: true,
		}
	}
	if onlySource != "" && !fromSources {
		return app.ExitResult{
			Code:     2,
			Message:  "--only requires --from-sources",
			ToStderr: true,
		}
	}
	return nil
}
