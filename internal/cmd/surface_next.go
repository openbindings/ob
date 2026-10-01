package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pflagFlag = pflag.Flag

// NewNextSurfaceRoot is the proposed ob command surface for OpenBindings 0.2.
// It is a preview: every command answers from a built-in sample document and
// pretend installation, and nothing is read, written, or called.
//
// Variants show the alternatives to decisions that are still open:
//
//	filter-edits    edits print the resulting document instead of editing in place
//	binding-invoke  invoke takes an exact binding key and prints event envelopes
//	adopt           adds the dropped adopt command, for comparison
func NewNextSurfaceRoot(variant string) *cobra.Command {
	switch variant {
	case "filter-edits", "binding-invoke", "adopt":
	default:
		variant = ""
	}
	root := &cobra.Command{
		Use:   "ob",
		Short: "Work with OpenBindings interface documents",
		Long: `ob works with OpenBindings interface documents (OBIs): create and edit
them, check them, compare them with shared contracts, and use them to call
the services they describe.

Exit status, for every command:
  0    done, or yes
  1    failed, or no
  2    usage error
  3    refused; nothing was written or sent
  4    no verdict (a check could not decide)
  130  cancelled

PREVIEW: this build shows the proposed command surface. Commands answer from
a built-in sample document (a Task Manager API); nothing is read, written,
or called. ob invoke reads the input values you give it.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations:   map[string]string{"variant": variant},
		Version:       "0.2-preview (OpenBindings 0.2)",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return nxUnknown(cmd, args[0])
			}
			return nil
		},
		RunE: nxRootRun,
	}
	root.SetVersionTemplate("ob {{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown flag: --json") {
			return fmt.Errorf("%v; did you mean -F json?", err)
		}
		return err
	})
	root.Flags().Bool("openbindings", false, "print ob's own OBI and exit")
	root.Flags().Bool("usage-spec", false, "print ob's usage spec (usage.kdl) and exit")
	root.Flags().Bool("agent-primer", false, "print the OpenBindings primer for AI agents and exit")

	for _, g := range []*cobra.Group{
		{ID: "docs", Title: "Documents"},
		{ID: "parts", Title: "Parts of a document"},
		{ID: "sources", Title: "Sources"},
		{ID: "use", Title: "Using a service"},
		{ID: "serve", Title: "Serving"},
		{ID: "extend", Title: "Kinds and delegates"},
	} {
		root.AddGroup(g)
	}
	nxAdd(root, "docs", nxInitCmd(), nxShowCmd(), nxSetCmd(), nxValidateCmd(), nxDiffCmd(), nxCompatCmd(), nxFmtCmd(), nxPatchCmd(), nxMergeCmd())
	if variant == "adopt" {
		nxAdd(root, "docs", nxAdoptCmd())
	}
	nxAdd(root, "parts", nxOperationCmd(), nxSourceCmd(), nxBindingCmd(), nxDependencyCmd(), nxSchemaCmd())
	nxAdd(root, "sources", nxSynthesizeCmd(), nxStatusCmd())
	nxAdd(root, "use", nxFetchCmd(), nxInvokeCmd(variant), nxContextCmd(), nxCodegenCmd())
	nxAdd(root, "serve", nxStartCmd(), nxMCPCmd())
	nxAdd(root, "extend", nxKindCmd(), nxDelegateCmd())
	nxApplyEditMode(root, variant)
	root.Example = `  ob init tasks.obi.json --name "Task Manager"
  ob operation add tasks.obi.json createTask --input-schema '{"type":"object"}'
  ob source add tasks.obi.json httpApi --kind example.openapi@1 --content '{"location":"https://api.example.com/openapi.json"}'
  ob binding add tasks.obi.json createTask.http --operation createTask --source httpApi --content '{"target":"#/paths/~1tasks/post"}'
  ob validate tasks.obi.json
  ob invoke tasks.obi.json completeTask --input '{"id":"t_1"}'`
	if variant == "binding-invoke" {
		root.Example = strings.Replace(root.Example, "invoke tasks.obi.json completeTask ", "invoke tasks.obi.json completeTask.http ", 1)
	}
	return root
}

func nxAdd(root *cobra.Command, group string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = group
		root.AddCommand(c)
	}
}

func nxRootRun(cmd *cobra.Command, args []string) error {
	for _, flag := range []string{"openbindings", "usage-spec", "agent-primer"} {
		if on, _ := cmd.Flags().GetBool(flag); on {
			return nxRun(cmd, args, func(c *nxCtx) error {
				switch flag {
				case "openbindings":
					c.println(`{
  "openbindings": "0.2.0",
  "name": "ob",
  "description": "The OpenBindings CLI.",
  "operations": {
    "validateDocument": { "…": "…" },
    "invokeOperation": { "…": "…" }
  },
  "sources": { "cli": { "kind": "openbindings.usage@1", "…": "…" } },
  "bindings": { "…": "…" }
}`)
				case "usage-spec":
					c.println(`name "ob"
bin "ob"
about "Work with OpenBindings interface documents"
cmd "init" help="Create a new OBI" { … }
cmd "show" help="Show a document" { … }
…`)
				default:
					c.println("# OpenBindings for agents\n\nAn OBI declares each operation once, as a protocol-independent contract, …")
				}
				c.note("(abbreviated in the preview)")
				return nil
			})
		}
	}
	return cmd.Help()
}

// nxCtx collects a preview's output. Every command prints the preview banner
// on stderr, then any notes, then its stdout.
type nxCtx struct {
	cmd    *cobra.Command
	args   []string
	out    strings.Builder
	notes  []string
	banner string
	isLive bool
}

const nxBanner = "ob preview: sample output; nothing was read, written, or called"

// live switches to printing as output happens, for commands that stream.
func (c *nxCtx) live() {
	if c.isLive {
		return
	}
	c.isLive = true
	stderr := c.cmd.ErrOrStderr()
	fmt.Fprintln(stderr, c.bannerText())
	for _, n := range c.notes {
		fmt.Fprintln(stderr, n)
	}
	c.notes = nil
	fmt.Fprint(c.cmd.OutOrStdout(), c.out.String())
	c.out.Reset()
}

func (c *nxCtx) bannerText() string {
	if c.banner != "" {
		return c.banner
	}
	return nxBanner
}

func (c *nxCtx) variant() string { return c.cmd.Root().Annotations["variant"] }
func (c *nxCtx) println(s string) {
	if c.isLive {
		fmt.Fprintln(c.cmd.OutOrStdout(), s)
		return
	}
	c.out.WriteString(s + "\n")
}
func (c *nxCtx) printf(f string, a ...any) { fmt.Fprintf(&c.out, f, a...) }
func (c *nxCtx) note(s string) {
	if c.isLive {
		fmt.Fprintln(c.cmd.ErrOrStderr(), s)
		return
	}
	c.notes = append(c.notes, s)
}
func (c *nxCtx) str(flag string) string {
	v, _ := c.cmd.Flags().GetString(flag)
	return v
}
func (c *nxCtx) strs(flag string) []string {
	v, _ := c.cmd.Flags().GetStringArray(flag)
	return v
}
func (c *nxCtx) on(flag string) bool {
	v, _ := c.cmd.Flags().GetBool(flag)
	return v
}
func (c *nxCtx) set(flag string) bool {
	f := c.cmd.Flags().Lookup(flag)
	return f != nil && f.Changed
}

func (c *nxCtx) table(header string, rows [][]string) {
	w := tabwriter.NewWriter(&c.out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, header)
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
}

// nxExit carries an exit status other than 2 for a result that is not a
// usage error: a refusal, a failed check, a "no" answer.
type nxExit struct {
	code int
	msg  string
}

func (e *nxExit) Error() string { return e.msg }
func (e *nxExit) ExitCode() int { return e.code }

func nxFail(code int, format string, a ...any) error {
	return &nxExit{code: code, msg: fmt.Sprintf(format, a...)}
}

// nxRefuse is a request ob declined, with nothing written or sent.
func nxRefuse(format string, a ...any) error { return nxFail(3, format, a...) }

// Commands that write a file or stored state without being document edits.
// What they cannot do is a refusal rather than a failure.
var nxWritingCommands = map[string]bool{
	"init": true, "fetch": true, "synthesize": true, "codegen": true, "fmt": true,
	"context.set": true, "context.remove": true,
	"delegate.register": true, "delegate.prefer": true, "delegate.unregister": true,
}

func (c *nxCtx) writes() bool {
	return c.cmd.Annotations["edits"] == "true" || c.cmd.Annotations["writes"] == "true"
}

// missing reports a name that is not there: a refusal for a command that
// would have changed something, a failure for one that only reads.
func (c *nxCtx) missing(format string, a ...any) error {
	if c.writes() {
		return nxRefuse(format, a...)
	}
	return nxFail(1, format, a...)
}

func nxLeaf(id, use, short, long, example string, args cobra.PositionalArgs, run func(*nxCtx) error) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Long: long, Example: example, Args: args,
		Annotations: map[string]string{"surface-id": id, "writes": fmt.Sprint(nxWritingCommands[id])},
		RunE: func(cmd *cobra.Command, a []string) error {
			return nxRun(cmd, a, run)
		},
	}
}

func nxRun(cmd *cobra.Command, args []string, run func(*nxCtx) error) error {
	if err := nxCheckStdin(cmd, args); err != nil {
		return err
	}
	if f := cmd.Flags().Lookup("format"); f != nil {
		allowed := strings.Split(f.Annotations["allowed"][0], ",")
		value := f.Value.String()
		ok := false
		for _, a := range allowed {
			ok = ok || a == value
		}
		if !ok {
			return fmt.Errorf("-F must be one of: %s", strings.Join(allowed, ", "))
		}
	}
	c := &nxCtx{cmd: cmd, args: args}
	err := run(c)
	var usage *nxUsage
	if err != nil && asUsage(err, &usage) {
		return usage.err
	}
	stderr := cmd.ErrOrStderr()
	if !c.isLive {
		fmt.Fprintln(stderr, c.bannerText())
	}
	for _, n := range c.notes {
		fmt.Fprintln(stderr, n)
	}
	fmt.Fprint(cmd.OutOrStdout(), c.out.String())
	return err
}

// nxUsage marks an error found before any work, printed without the preview
// banner, like any argument error.
type nxUsage struct{ err error }

func (u *nxUsage) Error() string { return u.err.Error() }
func nxUsageErr(format string, a ...any) error {
	return &nxUsage{fmt.Errorf(format, a...)}
}
func asUsage(err error, target **nxUsage) bool {
	u, ok := err.(*nxUsage)
	if ok {
		*target = u
	}
	return ok
}

func nxCheckStdin(cmd *cobra.Command, args []string) error {
	n := 0
	for _, a := range args {
		if a == "-" {
			n++
		}
	}
	cmd.Flags().Visit(func(f *pflagFlag) {
		if f.Value.Type() == "string" && f.Value.String() == "-" {
			n++
		}
	})
	if n > 1 {
		return fmt.Errorf("stdin (-) can supply only one input per command")
	}
	return nil
}

// nxFormat adds -F with the renderings a command offers; text first.
func nxFormat(cmd *cobra.Command, allowed ...string) {
	cmd.Flags().StringP("format", "F", allowed[0], "output format: "+strings.Join(allowed, ", "))
	_ = cmd.Flags().SetAnnotation("format", "allowed", []string{strings.Join(allowed, ",")})
}

func (c *nxCtx) format() string { return c.str("format") }

// nxEditable marks a command that changes a document in place.
func nxEditable(cmd *cobra.Command) *cobra.Command {
	cmd.Annotations["edits"] = "true"
	cmd.Flags().Bool("dry-run", false, "show the change without writing it")
	return cmd
}

// nxApplyEditMode rewrites edit commands for the filter-edits variant: they
// print the resulting document and never touch the file.
func nxApplyEditMode(root *cobra.Command, variant string) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Annotations["edits"] == "true" {
			if variant == "filter-edits" {
				_ = cmd.Flags().MarkHidden("dry-run")
				cmd.Flags().StringP("out", "o", "", "write the resulting document to a file instead of stdout")
				cmd.Long += "\n\nPrints the resulting document to stdout; <obi> is not changed. Use -o to\nsave the result to a file."
			} else {
				cmd.Long += "\n\nChanges <obi> in place. Give - as <obi> to read a document from stdin and\nprint the result instead, or use --dry-run to see the change without\nwriting it."
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

// nxDoc returns the sample document standing in for the <obi> argument.
func (c *nxCtx) doc(arg string) *nxObj {
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		c.note(fmt.Sprintf("(preview: %s was not fetched; answering from the sample document)", arg))
	}
	return nxFixture()
}

// wrote reports an edit the way ob would, and shows the change.
func (c *nxCtx) wrote(path string, before, after *nxObj, summary string) error {
	if nxIsURL(path) {
		return nxUsageErr("ob edits a document on this machine; save it first with ob fetch %s -o <file>", path)
	}
	if nxCompact(before) == nxCompact(after) {
		c.note(path + ": no change")
		return nil
	}
	if added := nxNewViolations(before, after); len(added) > 0 {
		return nxRefuse("refused: this change would make %s non-conformant, so nothing was written:\n  %s", path, strings.Join(added, "\n  "))
	}
	if c.variant() == "filter-edits" {
		if dest := c.str("out"); dest != "" {
			c.note("Wrote " + dest)
			c.note("(preview) the document that would be written:")
		}
		c.println(nxPretty(after))
		return nil
	}
	if path == "-" {
		c.println(nxPretty(after))
		return nil
	}
	diff := nxDiff(nxPretty(before), nxPretty(after))
	summary = strings.ToLower(summary[:1]) + summary[1:]
	if c.on("dry-run") {
		c.println(path + " (dry run, nothing written): " + summary)
		for _, l := range diff {
			c.println(l)
		}
		return nil
	}
	c.note(path + ": " + summary)
	c.note("(preview) the change that would be written:")
	for _, l := range diff {
		c.println(l)
	}
	return nil
}

// nxValue reads a JSON value flag. The preview never reads @file or stdin;
// it substitutes a marked placeholder.
func (c *nxCtx) value(flag string) (any, bool, error) {
	if !c.set(flag) {
		return nil, false, nil
	}
	raw := c.str(flag)
	if raw == "-" || strings.HasPrefix(raw, "@") {
		if raw == "@" {
			return nil, true, nxUsageErr("--%s: @ must be followed by a file path", flag)
		}
		from := "stdin"
		if raw != "-" {
			from = raw[1:]
		}
		c.note(fmt.Sprintf("(preview: %s was not read; using a placeholder)", from))
		obj := nxNewObj()
		obj.Set("$comment", "value from "+from)
		return obj, true, nil
	}
	v, err := nxParse(raw)
	if err != nil {
		return nil, true, nxUsageErr("--%s: expected one JSON value, @file, or - for stdin", flag)
	}
	return v, true, nil
}

// creating refuses to replace an existing file unless --force is given.
func (c *nxCtx) creating(path string) error {
	if path == "" || path == "-" || c.on("force") {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nxRefuse("%s already exists, so nothing was written; --force replaces it", path)
	}
	return nil
}

// object reads a value flag that must be a JSON object, such as a context.
func (c *nxCtx) object(flag string) (*nxObj, bool, error) {
	v, ok, err := c.value(flag)
	if err != nil || !ok {
		return nil, ok, err
	}
	obj, isObj := v.(*nxObj)
	if !isObj {
		return nil, true, nxUsageErr("--%s must be a JSON object", flag)
	}
	return obj, true, nil
}

func (c *nxCtx) schema(flag string) (any, bool, error) {
	v, ok, err := c.value(flag)
	if err != nil || !ok {
		return v, ok, err
	}
	switch v.(type) {
	case *nxObj, bool:
		return v, true, nil
	}
	return nil, true, nxUsageErr("--%s must be a JSON Schema: an object or a boolean", flag)
}

func nxName(what, value string) error {
	if !v02NamePattern.MatchString(value) {
		return nxUsageErr("%s %q is not a valid name: use letters, digits, and _ . - (starting with a letter, digit, or _)", what, value)
	}
	return nil
}

func nxGroupCmd(use, short, long string, children ...*cobra.Command) *cobra.Command {
	g := &cobra.Command{
		Use: use, Short: short, Long: long,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return nxUnknown(cmd, args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	g.AddCommand(children...)
	return g
}

var nxRootHints = map[string]string{
	"new": "init", "create": "init",
	"get": "show", "view": "show", "cat": "show", "print": "show", "read": "show", "describe": "show",
	"check": "validate", "lint": "validate", "verify": "validate",
	"compare": "diff (changes) or ob compat (contract fit)",
	"format":  "fmt", "apply": "patch",
	"operations": "operation", "ops": "operation", "op": "operation",
	"sources": "source", "bindings": "binding",
	"dependencies": "dependency", "deps": "dependency", "dep": "dependency",
	"schemas":  "schema",
	"generate": "synthesize (a document from an artifact) or ob codegen (a client)",
	"gen":      "codegen", "client": "codegen", "sdk": "codegen",
	"call": "invoke", "run": "invoke", "exec": "invoke", "execute": "invoke", "request": "invoke",
	"resolve": "fetch", "discover": "fetch", "download": "fetch",
	"pull": "source pull", "sync": "source pull", "import": "source import",
	"inspect": "source inspect", "drift": "status",
	"serve": "start (ob's own service) or ob mcp (a document as MCP tools)", "server": "start",
	"conform":       "compat, which shows what a contract needs and the commands that add it",
	"correspond":    "compat, which shows what a contract needs and the commands that add it",
	"adopt":         "compat, which shows what a contract needs and the commands that add it",
	"implement":     "compat, which shows what a contract needs and the commands that add it",
	"compatibility": "compat", "compatible": "compat",
	"kinds": "kind", "capabilities": "kind", "handlers": "kind", "binding-specs": "kind",
	"delegates": "delegate", "plugin": "delegate", "plugins": "delegate",
	"contexts": "context", "credentials": "context", "creds": "context", "auth": "context", "login": "context",
}

var nxChildHints = map[string]string{
	"get": "show", "view": "show", "describe": "show", "cat": "show",
	"ls":     "list",
	"create": "add", "new": "add",
	"update": "set", "edit": "set", "modify": "set", "change": "set", "put": "set",
	"delete": "remove", "rm": "remove", "del": "remove",
	"mv": "rename", "move": "rename",
}

func nxUnknown(cmd *cobra.Command, word string) error {
	root := cmd.Root()
	if cmd == root {
		if hint, ok := nxRootHints[word]; ok {
			if root.Annotations["variant"] == "adopt" && strings.HasPrefix(hint, "compat, which") {
				hint = "adopt"
			}
			return fmt.Errorf("unknown command %q; did you mean: ob %s", word, hint)
		}
		return fmt.Errorf("unknown command %q; run \"ob --help\" to see the commands", word)
	}
	if hint, ok := nxChildHints[word]; ok {
		if child, _, err := cmd.Find([]string{hint}); err == nil && child != cmd {
			return fmt.Errorf("unknown command %q for %q; did you mean: %s %s", word, cmd.CommandPath(), cmd.CommandPath(), hint)
		}
	}
	return fmt.Errorf("unknown command %q for %q; run %q", word, cmd.CommandPath(), cmd.CommandPath()+" --help")
}

// NextPreflightArgs catches unknown command words before Cobra answers
// --help for the nearest command or reports a flag error.
func NextPreflightArgs(root *cobra.Command, args []string) error {
	current := root
	for _, word := range args {
		if word == "--" {
			return nil
		}
		if strings.HasPrefix(word, "-") {
			continue
		}
		if !current.HasAvailableSubCommands() || current == root && word == "help" {
			return nil
		}
		var next *cobra.Command
		for _, child := range current.Commands() {
			if child.Name() == word || child.HasAlias(word) {
				next = child
				break
			}
		}
		if next == nil {
			return nxUnknown(current, word)
		}
		current = next
	}
	return nil
}
