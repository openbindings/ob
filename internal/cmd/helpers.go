package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/spf13/cobra"
)

// markCommandGroup configures a pure command-group parent (one with
// subcommands and no action of its own): a bare invocation prints help, and an
// unrecognized subcommand errors with cobra's standard "unknown command"
// message instead of silently falling through to help with exit 0. Cobra
// short-circuits a non-runnable command straight to help before it validates
// args, so the group must be runnable (RunE = help) for NoArgs to reject the
// stray token.
func markCommandGroup(c *cobra.Command) {
	c.Args = cobra.NoArgs
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}
}

// getOutputFlags returns the global --format and -o/--output (path) from the root command.
// -o/--output = output path (file to write). --format/-F = output format (json|yaml|text|quiet).
func getOutputFlags(c *cobra.Command) (format string, outputPath string) {
	format, _ = c.Root().PersistentFlags().GetString("format")
	outputPath, _ = c.Root().PersistentFlags().GetString("output")
	return format, outputPath
}

// refuseUnhonoredOutputFlags refuses the global output flags a command's lane
// cannot honor. -o/-F are root-persistent and thus inherited by every command,
// but some lanes (long-running servers, the streaming invoke filter) do not feed
// their result through OutputResult and so cannot honor them. Rather than accept
// and silently drop such a flag, a lane calls this first: a flag the user passed
// is never silently ignored. names lists the persistent flag names to refuse
// ("output", "format"); lane names the command for the message.
func refuseUnhonoredOutputFlags(cmd *cobra.Command, lane string, names ...string) error {
	persistent := cmd.Root().PersistentFlags()
	var offending []string
	for _, name := range names {
		if !persistent.Changed(name) {
			continue
		}
		label := "--" + name
		if f := persistent.Lookup(name); f != nil && f.Shorthand != "" {
			label = "-" + f.Shorthand + "/--" + name
		}
		offending = append(offending, label)
	}
	if len(offending) == 0 {
		return nil
	}
	return app.ExitResult{
		Code:     2,
		Message:  fmt.Sprintf("%s does not honor %s", lane, strings.Join(offending, ", ")),
		ToStderr: true,
	}
}

// outputEditResult prints an editing command's summary. On the filter lane —
// the command's document argument is `-`, so the modified document rides
// stdout — the summary moves to stderr (sed semantics); otherwise it behaves
// exactly like OutputResult.
//
// On editing commands -o redirects the SUMMARY, not the document (the
// document is edited in place). Synthesize/purify/codegen train the opposite
// muscle memory (-o = the document there), so `-o <the-obi-being-edited>`
// arrives regularly and would overwrite the just-edited document with a
// summary envelope — refuse it instead of destroying data.
func outputEditResult(cmd *cobra.Command, docArg string, result any) error {
	format, outputPath := getOutputFlags(cmd)
	if docArg == app.StdinLocator {
		return app.OutputResultStderr(result, format, outputPath)
	}
	if outputPath != "" && sameFilePath(outputPath, docArg) {
		return app.ExitResult{
			Code: 2,
			Message: fmt.Sprintf(
				"%s was edited in place; refusing -o %s: it names the edited document, and -o on an editing command redirects only the command summary",
				docArg, outputPath),
			ToStderr: true,
		}
	}
	return app.OutputResult(result, format, outputPath)
}

// sameFilePath reports whether two paths name the same file, robust to
// spelling differences (./x vs x, relative vs absolute, symlinks when both
// exist).
func sameFilePath(a, b string) bool {
	if ai, aerr := os.Stat(a); aerr == nil {
		if bi, berr := os.Stat(b); berr == nil {
			return os.SameFile(ai, bi)
		}
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
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
