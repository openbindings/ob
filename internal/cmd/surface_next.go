package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
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

Start here:
  ob init tasks.obi.json         create a document (ob synthesize makes one
                                 from an artifact, such as an OpenAPI file)
  ob show tasks.obi.json         see what it describes
  ob validate tasks.obi.json     check it
  ob invoke tasks.obi.json <operation> --input '{...}'
                                 call one of its operations

A document can be a path, - for stdin, or a URL; a bare origin is looked up
at /.well-known/openbindings. Commands that interpret a document refuse one
written for an OpenBindings version this ob does not read (exit 3); ob show
-F json and ob fetch still print it.

Exit status, for every command:
  0    done, or yes (an edit that is already true is "no change")
  1    failed, or no
  2    usage error, including a name that is not in the document
  3    refused: ob understood the request and declined it; nothing was
       written or sent
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
		for flag, hint := range map[string]string{
			"--json": "-F json", "--openbindings": "ob describe -F obi",
			"--usage-spec": "ob describe -F usage", "--agent-primer": "ob help primer",
		} {
			if strings.Contains(err.Error(), "unknown flag: "+flag) {
				return fmt.Errorf("%v; did you mean %s?", err, hint)
			}
		}
		return err
	})

	for _, g := range []*cobra.Group{
		{ID: "docs", Title: "Documents"},
		{ID: "parts", Title: "Parts of a document"},
		{ID: "sources", Title: "Sources"},
		{ID: "use", Title: "Using a service"},
		{ID: "advanced", Title: "Advanced"},
	} {
		root.AddGroup(g)
	}
	nxAdd(root, "docs", nxInitCmd(), nxShowCmd(), nxSetCmd(), nxValidateCmd(), nxDiffCmd(), nxCompatCmd(), nxFmtCmd(), nxPatchCmd(), nxMergeCmd())
	if variant == "adopt" {
		nxAdd(root, "docs", nxAdoptCmd())
	}
	nxAdd(root, "parts", nxOperationCmd(), nxSourceCmd(), nxBindingCmd(), nxDependencyCmd(), nxSchemaCmd())
	nxAdd(root, "sources", nxSynthesizeCmd(), nxStatusCmd())
	nxAdd(root, "use", nxFetchCmd(), nxInvokeCmd(variant), nxContextCmd())
	nxAdd(root, "advanced", nxCodegenCmd(), nxStartCmd(), nxMCPCmd(), nxCACmd(), nxKindCmd(), nxDelegateCmd(), nxDescribeCmd())
	root.AddCommand(nxPrimerTopic())
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
	root.SetHelpCommand(&cobra.Command{
		Use: "help [command]", Short: "Help about any command",
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := root
			for _, word := range args {
				var next *cobra.Command
				for _, child := range topic.Commands() {
					if child.Name() == word || child.HasAlias(word) {
						next = child
						break
					}
				}
				if next == nil {
					return nxUnknown(topic, word)
				}
				topic = next
			}
			return topic.Help()
		},
	})
	nxInstallNameCompletion(root)
	return root
}

// The preview completes the names in its sample. A real build reads the
// explicitly named document; there is no file-extension convention.
func nxInstallNameCompletion(root *cobra.Command) {
	doc := nxFixture()
	names := func(part string, aliases bool, prefix string) []string {
		keys := nxPartKeys(doc, part)
		if aliases {
			for _, key := range nxPartKeys(doc, "operations") {
				keys = append(keys, nxStrings(doc.Obj("operations").Obj(key).Get("aliases"))...)
			}
		}
		var out []string
		for _, key := range nxSorted(keys) {
			if strings.HasPrefix(key, prefix) {
				out = append(out, key)
			}
		}
		return out
	}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		id := cmd.Annotations["surface-id"]
		cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 && strings.Contains(cmd.Use, "<obi>") {
				return nil, cobra.ShellCompDirectiveDefault
			}
			part, aliases := "", false
			if id == "invoke" && len(args) == 1 {
				if root.Annotations["variant"] == "binding-invoke" {
					part = "bindings"
				} else {
					part, aliases = "operations", true
				}
			}
			for _, p := range []string{"operation", "binding", "source", "dependency", "schema"} {
				if strings.HasPrefix(id, p+".") && len(args) == 1 {
					action := strings.TrimPrefix(id, p+".")
					if action == "show" || action == "set" || action == "remove" || action == "rename" || action == "inspect" || action == "pull" || strings.HasPrefix(action, "example.") {
						part = p + "s"
						if p == "dependency" {
							part = "dependencies"
						}
						aliases = p == "operation" && action != "rename" && action != "remove"
					}
				}
			}
			if id == "source.pull" && len(args) >= 1 {
				part = "sources"
			}
			if part == "" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return names(part, aliases, prefix), cobra.ShellCompDirectiveNoFileComp
		}
		for flag, part := range map[string]string{"operation": "operations", "update-operation": "operations", "binding": "bindings", "source": "sources"} {
			if cmd.Flags().Lookup(flag) != nil {
				part := part
				_ = cmd.RegisterFlagCompletionFunc(flag, func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
					keys := names(part, part == "operations", prefix)
					if id == "invoke" && part == "bindings" && len(args) > 1 {
						if key, ok := nxResolveOperation(doc, args[1]); ok {
							allowed := nxReferrers(doc, "bindings", "operation", key)
							var filtered []string
							for _, b := range keys {
								if nxContains(allowed, b) {
									filtered = append(filtered, b)
								}
							}
							keys = filtered
						}
					}
					return keys, cobra.ShellCompDirectiveNoFileComp
				})
			}
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func nxAdd(root *cobra.Command, group string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = group
		root.AddCommand(c)
	}
}

func nxRootRun(cmd *cobra.Command, args []string) error {
	return cmd.Help()
}

// nxDescribeCmd describes this ob itself, including as an OBI and as a
// usage spec, for tools that drive ob.
func nxDescribeCmd() *cobra.Command {
	cmd := nxLeaf("describe", "describe", "Describe this ob", `Describe this ob: its version, the OpenBindings versions it reads, and what
it can handle. -F obi prints ob's own OBI, for tools that drive ob through
OpenBindings; -F usage prints its usage spec (usage.kdl).`,
		`  ob describe
  ob describe -F obi > ob.obi.json`, nxArgs(0, 0), func(c *nxCtx) error {
			switch c.format() {
			case "obi":
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
				c.note("(abbreviated in the preview)")
			case "usage":
				c.println(`name "ob"
bin "ob"
about "Work with OpenBindings interface documents"
cmd "init" help="Create a new OBI" { … }
cmd "show" help="Show a document" { … }
…`)
				c.note("(abbreviated in the preview)")
			default:
				info := nxNewObj().Set("version", "0.2-preview").Set("openbindings", nxNewObj().Set("reads", "0.2.x").Set("writes", "0.2.0")).
					Set("kinds", len(nxInstalledKinds)).Set("delegates", len(nxDelegates))
				c.render(info, func() {
					c.println("ob 0.2-preview")
					c.println("  OpenBindings: reads 0.2.x, writes 0.2.0")
					c.println(fmt.Sprintf("  Kinds:        %d (ob kind list)", len(nxInstalledKinds)))
					c.println(fmt.Sprintf("  Delegates:    %d (ob delegate list)", len(nxDelegates)))
				})
			}
			return nil
		})
	nxFormat(cmd, "text", "json", "obi", "usage")
	return cmd
}

// nxPrimerTopic is a help topic: ob help primer.
func nxPrimerTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "primer",
		Short: "OpenBindings, explained for AI agents",
		Long: `OpenBindings for agents (abbreviated in the preview)

An OBI declares each operation once, as a protocol-independent contract,
and binds it to the protocols that carry it out. …`,
	}
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
	// The flag's own slice keeps empty values, which reading it back as a
	// string array would drop.
	if f := c.cmd.Flags().Lookup(flag); f != nil {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			return sv.GetSlice()
		}
	}
	return nil
}
func (c *nxCtx) on(flag string) bool {
	v, _ := c.cmd.Flags().GetBool(flag)
	return v
}
func (c *nxCtx) set(flag string) bool {
	f := c.cmd.Flags().Lookup(flag)
	return f != nil && f.Changed
}

// table prints aligned rows under a header; an empty header prints none.
func (c *nxCtx) table(header string, rows [][]string) {
	w := tabwriter.NewWriter(&c.out, 0, 0, 3, ' ', 0)
	if header != "" {
		fmt.Fprintln(w, header)
	}
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

// nxNotFound is a name that is not in the document (or the store): a usage
// error, so that a typo never reads as a "no" or as a refusal.
func nxNotFound(format string, a ...any) error { return nxFail(2, format, a...) }

func nxLeaf(id, use, short, long, example string, args cobra.PositionalArgs, run func(*nxCtx) error) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Long: long, Example: example, Args: args,
		Annotations: map[string]string{"surface-id": id},
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
	if err := nxDocumentGate(c); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), c.bannerText())
		for _, n := range c.notes {
			fmt.Fprintln(cmd.ErrOrStderr(), n)
		}
		return err
	}
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
		switch v := f.Value.(type) {
		case pflag.SliceValue:
			for _, item := range v.GetSlice() {
				_, assigned, assignment := strings.Cut(item, "=")
				keyedInput := cmd.Annotations["surface-id"] == "context.set" && nxContains([]string{"credential", "cookie", "header", "config"}, f.Name)
				if item == "-" || keyedInput && assignment && assigned == "-" {
					n++
				}
			}
		default:
			if f.Value.Type() == "string" && f.Value.String() == "-" {
				n++
			}
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

// Commands whose arguments are not documents: scopes, artifacts.
var nxNotDocumentArgs = map[string]bool{
	"context.set": true, "context.show": true, "context.remove": true,
	"synthesize": true, "source.import": true,
}

// nxDocumentGate answers for the pretend services a document URL can name:
// one that asks for sign-in to read its OBI, one that publishes none, and
// one whose OBI is written for OpenBindings 0.1.
func nxDocumentGate(c *nxCtx) error {
	if nxNotDocumentArgs[c.cmd.Annotations["surface-id"]] {
		return nil
	}
	for _, arg := range c.args {
		if !nxIsURL(arg) {
			continue
		}
		u, err := url.Parse(arg)
		if err != nil {
			continue
		}
		origin := u.Scheme + "://" + u.Host
		if origin == "http://127.0.0.1:20290" || origin == "https://127.0.0.1:20291" {
			c.note("(preview: using this local service's access token from its private run record)")
		}
		switch u.Hostname() {
		case "private.example.com":
			if _, ok := nxStoredContext(origin); ok {
				c.note("using stored context for " + origin)
				continue
			}
			if f, ok := c.cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
				c.note(fmt.Sprintf("(preview: %s asks for sign-in to read its OBI; ob would ask you for a credential and offer to store it for %s)", origin, origin))
				continue
			}
			return nxRefuse("%s asks for sign-in to read its OBI (401), so nothing was read;\n  store a credential for it:  ob context set '%s' --bearer-token -", origin, origin)
		case "nothing.example.com":
			return nxFail(1, "no OBI is published at %s/.well-known/openbindings (404)", origin)
		case "old.example.com":
			id := c.cmd.Annotations["surface-id"]
			if id == "fetch" || id == "show" && c.format() == "json" {
				c.note(fmt.Sprintf("note: %s publishes an OBI written for OpenBindings 0.1.0, which this ob does not read; it is shown as published", origin))
				continue
			}
			return nxRefuse("%s publishes an OBI written for OpenBindings 0.1.0, and this ob reads 0.2.x, so it was not interpreted; ob show -F json or ob fetch still prints it", origin)
		}
	}
	return nil
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
		// A filter always passes its document on, changed or not.
		if path == "-" || c.variant() == "filter-edits" {
			c.println(nxPretty(after))
		}
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
		return nxRefuse("OBI-D-03: %s %q is not a valid name: use letters, digits, and _ . - (starting with a letter, digit, or _); nothing was written", what, value)
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
	"get": "show", "view": "show", "cat": "show", "print": "show", "read": "show",
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
	"bundle": "schema bundle", "trust": "ca", "cert": "ca", "certs": "ca",
}

var nxChildHints = map[string]string{
	"get": "show", "view": "show", "describe": "show", "cat": "show",
	"ls":     "list",
	"create": "add", "new": "add",
	"update": "set", "edit": "set", "modify": "set", "change": "set", "put": "set",
	"delete": "remove", "rm": "remove", "del": "remove",
	"mv": "rename", "move": "rename",
	"register": "add", "unregister": "remove", "prefer": "set --preference",
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
	root.InitDefaultCompletionCmd()
	current := root
	for _, word := range args {
		if word == "--" || strings.HasPrefix(word, "__complete") {
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
