package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func nxIsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func nxFetchCmd() *cobra.Command {
	cmd := nxLeaf("fetch", "fetch <url>", "Download a service's OBI", `Download the OBI a service publishes. A bare origin, such as
https://api.example.com, is looked up at /.well-known/openbindings
(OpenBindings HTTP Discovery); any other URL is fetched as given. Prints the
document unless -o is given.

Commands that read a document, such as ob show and ob invoke, also accept a
URL directly.`,
		`  ob fetch https://api.example.com -o tasks.obi.json
  ob fetch https://api.example.com | jq '.operations | keys'`,
		nxArgs(1, 1), func(c *nxCtx) error {
			url := c.args[0]
			if !nxIsURL(url) {
				return nxUsageErr("fetch takes an http or https URL; for a local file, use ob show")
			}
			trimmed := strings.TrimRight(url, "/")
			if strings.Count(trimmed, "/") == 2 {
				c.note("(looked up " + trimmed + "/.well-known/openbindings)")
			}
			c.note("(preview: nothing was fetched; showing the sample document)")
			if out := c.str("output"); out != "" {
				c.note("Saved " + out)
				c.note("(preview) the document that would be saved:")
			}
			c.println(nxPretty(nxFixture()))
			return nil
		})
	cmd.Flags().StringP("output", "o", "", "save the document to a file")
	return cmd
}

// ------------------------------------------------------------------- invoke

func nxInvokeCmd(variant string) *cobra.Command {
	if variant == "binding-invoke" {
		return nxBindingInvokeCmd()
	}
	cmd := nxLeaf("invoke", "invoke <obi> <operation>", "Call an operation", `Call an operation, by name or alias, and print each output value as one
line of JSON.

ob chooses a binding: of the operation's bindings whose kind it can invoke,
it passes over deprecated ones unless nothing else is left, then takes the
highest --preference. If that still leaves a tie, it stops and lists them.
--binding chooses one yourself.

--input is the input value (JSON, @file, or - for stdin); ob checks it
against the operation's input schema before sending anything (--no-check
skips that). Credentials and settings come from --context, or the context
named "default". If the service asks for something the context lacks, ob
asks you when attached to a terminal, and stops otherwise.

--events prints one event per line instead (each output, input closing,
completion, or error), for programs that need the whole exchange.

Exit status: 0 completed; 1 the operation reported failure; 2 usage error;
3 refused before anything was sent.`,
		`  ob invoke tasks.obi.json createTask --input '{"title":"Write the docs"}'
  ob invoke tasks.obi.json listTasks --input '{}' | jq -r '.[].title'
  ob invoke https://api.example.com acme.tasks.createTask --input @task.json --context staging`,
		nxArgs(2, 2), nxInvoke)
	cmd.Flags().String("binding", "", "use this binding instead of letting ob choose")
	cmd.Flags().String("input", "", "the input value: JSON, @file, or -")
	cmd.Flags().String("context", "", "the stored context to use (default \"default\")")
	cmd.Flags().Bool("events", false, "print every event, not just output values")
	cmd.Flags().Bool("no-check", false, "send the input without checking it against the schema")
	return cmd
}

func nxInvoke(c *nxCtx) error {
	doc := c.doc(c.args[0])
	key, ok := nxResolveOperation(doc, c.args[1])
	if !ok {
		return nxFail(2, "no operation named %q in %s; ob operation list shows them", c.args[1], c.args[0])
	}
	if err := nxCheckContext(c); err != nil {
		return err
	}
	bindings := nxReferrers(doc, "bindings", "operation", key)
	if len(bindings) == 0 {
		msg := fmt.Sprintf("operation %s has no bindings in %s, so there is no way to call it", key, c.args[0])
		if deps := nxReferrers(doc, "dependencies", "operation", key); len(deps) > 0 {
			msg += fmt.Sprintf(" (the document only calls it, at %s)", strings.Join(deps, ", "))
		}
		return nxFail(3, "%s", msg)
	}
	chosen := c.str("binding")
	kindOf := func(b string) string {
		src := fmt.Sprint(doc.Obj("bindings").Obj(b).Get("source"))
		return fmt.Sprint(doc.Obj("sources").Obj(src).Get("kind"))
	}
	if chosen != "" {
		if !nxContains(bindings, chosen) {
			return nxFail(2, "binding %q does not carry out %s; its bindings are %s", chosen, key, strings.Join(bindings, ", "))
		}
		if _, ok := nxSupports(kindOf(chosen), "invoke"); !ok {
			return nxFail(3, "this ob cannot invoke %s bindings; ob kind list shows what it can handle", kindOf(chosen))
		}
	} else {
		var usable, current []string
		for _, b := range bindings {
			if _, ok := nxSupports(kindOf(b), "invoke"); ok {
				usable = append(usable, b)
				if d, _ := doc.Obj("bindings").Obj(b).Get("deprecated").(bool); !d {
					current = append(current, b)
				}
			}
		}
		if len(usable) == 0 {
			return nxFail(3, "this ob cannot invoke any of %s's bindings (%s)", key, strings.Join(bindings, ", "))
		}
		if len(current) > 0 {
			usable = current
		}
		best, tied := "", []string{}
		bestPref := int64(-1 << 62)
		for _, b := range usable {
			p := int64(-1 << 61)
			if v := doc.Obj("bindings").Obj(b).Get("preference"); v != nil {
				fmt.Sscan(fmt.Sprint(v), &p)
			}
			switch {
			case p > bestPref:
				best, bestPref, tied = b, p, []string{b}
			case p == bestPref:
				tied = append(tied, b)
			}
		}
		if len(tied) > 1 {
			return nxFail(3, "%s has %d equally preferred bindings (%s); choose one with --binding", key, len(tied), strings.Join(tied, ", "))
		}
		chosen = best
		why := kindOf(chosen)
		if bestPref > -1<<61 {
			why += fmt.Sprintf(", preference %d", bestPref)
		}
		c.note(fmt.Sprintf("using binding %s (%s)", chosen, why))
	}
	input, hasInput, err := c.value("input")
	if err != nil {
		return err
	}
	if hasInput && !c.on("no-check") {
		problems, checked, err := nxCheck(doc, key, "input", input)
		if err != nil {
			return err
		}
		if checked && len(problems) > 0 {
			return nxFail(3, "the input does not fit %s's input schema, so nothing was sent:\n  %s", key, strings.Join(problems, "\n  "))
		}
	}
	c.note("(preview: nothing was called; showing an illustrative result)")
	var result any
	switch key {
	case "createTask":
		title := "Write the docs"
		if obj, ok := input.(*nxObj); ok {
			if t, ok := obj.Get("title").(string); ok {
				title = t
			}
		}
		result = nxNewObj().Set("id", "t_2").Set("title", title).Set("done", false)
	case "listTasks":
		result = nxMustParse(`[{"id":"t_1","title":"Write the docs","done":false},{"id":"t_2","title":"Review the spec","done":true}]`)
	case "completeTask":
		id := "t_1"
		if obj, ok := input.(*nxObj); ok {
			if s, ok := obj.Get("id").(string); ok {
				id = s
			}
		}
		result = nxNewObj().Set("id", id).Set("title", "Write the docs").Set("done", true)
	default:
		result = nxNewObj()
	}
	if c.on("events") {
		c.println(nxCompact(nxNewObj().Set("event", "output").Set("value", result)))
		c.println(`{"event":"complete"}`)
		return nil
	}
	c.println(nxCompact(result))
	return nil
}

func nxCheckContext(c *nxCtx) error {
	if !c.set("context") {
		return nil
	}
	for _, ctx := range nxContexts {
		if ctx.name == c.str("context") {
			return nil
		}
	}
	return nxFail(2, "no context named %q; ob context list shows them", c.str("context"))
}

func nxBindingInvokeCmd() *cobra.Command {
	cmd := nxLeaf("invoke", "invoke <obi> <binding>", "Call one exact binding", `Call one binding exactly; ob does not choose among bindings. --input is the
input value (JSON, @file, or - for stdin). Output is one JSON event per line:
each output value, then completion or an error.`,
		`  ob invoke tasks.obi.json createTask.http --input '{"title":"Write the docs"}'`,
		nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			b, err := c.entry(doc, "bindings", "binding", c.args[1])
			if err != nil {
				return err
			}
			if err := nxCheckContext(c); err != nil {
				return err
			}
			if _, _, err := c.value("input"); err != nil {
				return err
			}
			c.note("(preview: nothing was called; showing an illustrative result)")
			c.println(nxCompact(nxNewObj().Set("event", "output").Set("binding", c.args[1]).Set("value", nxNewObj().Set("id", "t_2").Set("operation", b.Get("operation")))))
			c.println(nxCompact(nxNewObj().Set("event", "complete").Set("binding", c.args[1])))
			return nil
		})
	cmd.Flags().String("input", "", "the input value: JSON, @file, or -")
	cmd.Flags().String("context", "", "the stored context to use (default \"default\")")
	return cmd
}

// ------------------------------------------------------------------ context

func nxContextCmd() *cobra.Command {
	set := nxLeaf("context.set", "set <name>", "Create or change a context", `Create or change a named context: the credentials and settings ob uses
when it calls services. Give a secret as - to read it from stdin and keep it
out of your shell history. --config sets a value a binding needs that its
artifact does not say, such as which server to use.`,
		`  ob context set default --bearer-token -
  ob context set staging --config server=https://staging.example.com --header X-Client=ob`,
		nxArgs(1, 1), func(c *nxCtx) error {
			if err := nxName("context name", c.args[0]); err != nil {
				return err
			}
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			var changes []string
			for _, f := range []string{"bearer-token", "api-key", "basic-user", "basic-password"} {
				if c.set(f) {
					changes = append(changes, strings.ReplaceAll(f, "-", " ")+" set")
				}
			}
			for _, h := range c.strs("header") {
				if !strings.Contains(h, "=") {
					return nxUsageErr("--header takes NAME=VALUE")
				}
				changes = append(changes, "header "+strings.SplitN(h, "=", 2)[0]+" set")
			}
			for _, cfg := range c.strs("config") {
				if !strings.Contains(cfg, "=") {
					return nxUsageErr("--config takes POINT=VALUE")
				}
				changes = append(changes, "config "+strings.SplitN(cfg, "=", 2)[0]+" set")
			}
			changes = append(changes, nxPrefix("removed ", c.strs("unset"))...)
			c.println(fmt.Sprintf("Context %s: %s", c.args[0], strings.Join(changes, ", ")))
			return nil
		})
	set.Flags().String("bearer-token", "", "a bearer token (or - to read it from stdin)")
	set.Flags().String("api-key", "", "an API key (or - to read it from stdin)")
	set.Flags().String("basic-user", "", "a user name for HTTP Basic")
	set.Flags().String("basic-password", "", "a password for HTTP Basic (or - to read it from stdin)")
	set.Flags().StringArray("header", nil, "a header to send, NAME=VALUE (repeatable)")
	set.Flags().StringArray("config", nil, "a configuration value, POINT=VALUE (repeatable)")
	set.Flags().StringArray("unset", nil, "remove something from the context (repeatable)")

	list := nxListFormats(nxLeaf("context.list", "list", "List contexts", "List stored contexts and what each holds. Secrets are masked.",
		`  ob context list`, nxArgs(0, 0), func(c *nxCtx) error {
			var rows [][]string
			var out []any
			for _, ctx := range nxContexts {
				rows = append(rows, []string{ctx.name, strings.Join(ctx.holds, "; ")})
				out = append(out, nxNewObj().Set("name", ctx.name).Set("holds", nxToAny(ctx.holds)))
			}
			c.render(out, func() { c.table("NAME\tHOLDS", rows) })
			return nil
		}))
	show := nxListFormats(nxLeaf("context.show", "show <name>", "Show a context", "Show what a context holds. Secrets are masked.",
		`  ob context show staging`, nxArgs(1, 1), func(c *nxCtx) error {
			for _, ctx := range nxContexts {
				if ctx.name == c.args[0] {
					c.render(nxNewObj().Set("name", ctx.name).Set("holds", nxToAny(ctx.holds)), func() {
						c.println(ctx.name)
						for _, h := range ctx.holds {
							c.println("  " + h)
						}
					})
					return nil
				}
			}
			return nxFail(1, "no context named %q; ob context list shows them", c.args[0])
		}))
	remove := nxLeaf("context.remove", "remove <name>", "Remove a context", "Remove a stored context and everything in it.",
		`  ob context remove staging`, nxArgs(1, 1), func(c *nxCtx) error {
			for _, ctx := range nxContexts {
				if ctx.name == c.args[0] {
					c.println("Removed context " + ctx.name)
					return nil
				}
			}
			return nxFail(1, "no context named %q", c.args[0])
		})
	return nxGroupCmd("context", "Store credentials and settings for calls", `A context holds the credentials and settings ob uses when it calls a
service: tokens, keys, headers, and configuration values. ob invoke and
ob mcp take --context; the context named "default" is used otherwise.`, set, list, show, remove)
}

func nxPrefix(p string, list []string) []string {
	var out []string
	for _, s := range list {
		out = append(out, p+s)
	}
	return out
}

// ------------------------------------------------------------------ codegen

func nxCodegenCmd() *cobra.Command {
	cmd := nxLeaf("codegen", "codegen <obi> --lang <language>", "Generate a typed client", `Generate client code with one typed function per operation that has
bindings. Calls go through ob's invocation libraries, which choose bindings
the way ob invoke does. -o names the output directory.`,
		`  ob codegen tasks.obi.json --lang go -o ./taskclient
  ob codegen https://api.example.com --lang typescript --package @acme/tasks`,
		nxArgs(1, 1), func(c *nxCtx) error {
			lang := c.str("lang")
			if lang != "go" && lang != "typescript" {
				return nxUsageErr("--lang is required: go or typescript")
			}
			dir := c.str("output")
			if dir == "" {
				dir = "."
			}
			ext := map[string]string{"go": ".go", "typescript": ".ts"}[lang]
			c.println(fmt.Sprintf("Would write %s:", dir))
			c.println("  client" + ext + "       the client and its options")
			c.println("  types" + ext + "        Task, Problem, CreateTaskInput, CompleteTaskInput")
			c.println("  operations" + ext + "   CreateTask, ListTasks, CompleteTask")
			c.note("skipped events.deliver: it has no bindings")
			return nil
		})
	cmd.Flags().String("lang", "", "go or typescript (required)")
	cmd.Flags().String("package", "", "the package or module name")
	cmd.Flags().StringP("output", "o", "", "the directory to write")
	return cmd
}

// -------------------------------------------------------- shared contracts

// nxAdoptCmd exists only in the adopt variant; the default tree meets a
// contract with ob compat, ob operation set --add-alias, and ob merge.
func nxAdoptCmd() *cobra.Command {
	name := "adopt"
	cmd := nxEditable(nxLeaf("adopt", name+" <obi> <contract>", "Adopt a shared contract's operations", `Make <obi> offer the operations of a shared contract (another OBI, a path
or URL), so its operations answer to the contract's names:

  - an operation that already carries the contract's name is left alone;
  - --as CONTRACT_NAME=OPERATION gives one of your operations that name,
    as an alias;
  - any other contract operation is added under the contract's name, with
    the contract's schemas and no bindings yet.

Where one of your schemas does not fit the contract's, ob reports it and
changes nothing unless --replace-schemas is given.`,
		`  ob `+name+` tasks.obi.json acme-tasks.obi.json --as acme.tasks.listTasks=listTasks
  ob `+name+` tasks.obi.json https://contracts.example.com/acme-tasks.json --dry-run`,
		nxArgs(2, 2), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			contract := nxContract()
			c.note(fmt.Sprintf("(preview: a sample contract, Acme Tasks, stands in for %s)", c.args[1]))
			mapping := map[string]string{}
			for _, m := range c.strs("as") {
				parts := strings.SplitN(m, "=", 2)
				if len(parts) != 2 {
					return nxUsageErr("--as takes CONTRACT_NAME=OPERATION")
				}
				if !contract.Obj("operations").Has(parts[0]) {
					return nxFail(1, "the contract has no operation named %q", parts[0])
				}
				if _, err := c.opKey(after, parts[1]); err != nil {
					return err
				}
				mapping[parts[0]] = parts[1]
			}
			var report []string
			added := 0
			for _, cname := range contract.Obj("operations").Keys() {
				if key, ok := nxResolveOperation(after, cname); ok {
					report = append(report, fmt.Sprintf("%-24s already answered by %s", cname, key))
					continue
				}
				if mine, ok := mapping[cname]; ok {
					key, _ := nxResolveOperation(after, mine)
					op := after.Obj("operations").Obj(key)
					op.SetCanon("aliases", append(nxToAny(nxStrings(op.Get("aliases"))), cname), nxOperationOrder)
					report = append(report, fmt.Sprintf("%-24s %s now answers to it", cname, key))
					continue
				}
				after.Obj("operations").Set(cname, nxClone(contract.Obj("operations").Get(cname)))
				added++
				report = append(report, fmt.Sprintf("%-24s added, with no bindings yet", cname))
			}
			for _, line := range report {
				c.note(line)
			}
			if added > 0 && len(mapping) == 0 {
				c.note("hint: if one of your operations already does what a contract operation does, use --as CONTRACT_NAME=OPERATION instead of adding a new one")
			}
			return c.wrote(c.args[0], before, after, "Adopted Acme Tasks")
		}))
	cmd.Flags().StringArray("as", nil, "CONTRACT_NAME=OPERATION: give your operation the contract's name (repeatable)")
	cmd.Flags().Bool("replace-schemas", false, "replace your schemas where they do not fit the contract's")
	return cmd
}

func nxCompatCmd() *cobra.Command {
	cmd := nxLeaf("compat", "compat <obi> <contract>", "Check a document against a shared contract", `Check whether <obi> satisfies a shared contract: for each contract
operation, some operation must answer to its name, accept every input the
contract allows, and return only outputs the contract allows. Schemas are
compared with ob's comparison rules (profile OB-2020-12); a comparison ob
cannot decide is reported as undecided, not as a failure. Both arguments
may be paths or URLs.

For each contract operation that nothing answers to, it prints the two ways
to meet it: give one of your operations the contract's name with ob
operation set --add-alias, or add the contract's operation with ob merge.

Exit status: 0 compatible; 1 not compatible.`,
		`  ob compat tasks.obi.json acme-tasks.obi.json
  ob compat https://api.example.com https://contracts.example.com/acme-tasks.json -q`,
		nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			contract := nxContract()
			c.note(fmt.Sprintf("(preview: a sample contract, Acme Tasks, stands in for %s)", c.args[1]))
			var rows [][]string
			var out []any
			var missing []string
			ok := true
			for _, cname := range contract.Obj("operations").Keys() {
				if key, found := nxResolveOperation(doc, cname); found {
					rows = append(rows, []string{cname, "yes", key + " (schemas fit)"})
					out = append(out, nxNewObj().Set("operation", cname).Set("satisfied", true).Set("by", key))
				} else {
					ok = false
					missing = append(missing, cname)
					rows = append(rows, []string{cname, "no", "no operation answers to this name"})
					out = append(out, nxNewObj().Set("operation", cname).Set("satisfied", false).Set("remedies", nxToAny(nxRemedies(c, cname))))
				}
			}
			if !c.on("quiet") {
				c.render(out, func() {
					verdict := "satisfies"
					if !ok {
						verdict = "does not satisfy"
					}
					c.println(fmt.Sprintf("%s %s Acme Tasks (%s)", c.args[0], verdict, c.args[1]))
					c.table("CONTRACT OPERATION\tMET\tBY", rows)
					for _, cname := range missing {
						r := nxRemedies(c, cname)
						c.println("")
						c.println("To meet " + cname + ":")
						c.println("  if one of your operations already does it:  " + r[0])
						c.println("  otherwise, add it from the contract:        " + r[1])
					}
				})
			}
			if !ok {
				return nxFail(1, "")
			}
			return nil
		})
	cmd.Flags().BoolP("quiet", "q", false, "print nothing; report through the exit status only")
	nxFormat(cmd, "text", "json")
	return cmd
}

func nxRemedies(c *nxCtx, contractOp string) []string {
	return []string{
		fmt.Sprintf("ob operation set %s <operation> --add-alias %s", c.args[0], contractOp),
		fmt.Sprintf("ob merge %s %s --operation %s", c.args[0], c.args[1], contractOp),
	}
}

// ------------------------------------------------------------------ serving

func nxStartCmd() *cobra.Command {
	cmd := nxLeaf("start", "start", "Run ob as a local service", `Serve ob's own operations over HTTP and WebSocket on this machine, for
editors, browsers, and other tools. ob describes itself with an OBI at
/.well-known/openbindings. HTTPS uses a local certificate authority that ob
installs the first time; --no-tls serves HTTP only.`,
		`  ob start
  ob start --port 8080 --no-tls`, nxArgs(0, 0), func(c *nxCtx) error {
			port := c.str("port")
			c.println("ob is serving on http://127.0.0.1:" + port)
			if !c.on("no-tls") {
				c.println("              and https://127.0.0.1:" + nxNextPort(port))
			}
			c.println("Its OBI: http://127.0.0.1:" + port + "/.well-known/openbindings")
			c.println("Press Ctrl-C to stop.")
			return nil
		})
	cmd.Flags().String("port", "20290", "the HTTP port; HTTPS uses the next one")
	cmd.Flags().Bool("no-tls", false, "serve HTTP only")
	return cmd
}

func nxNextPort(p string) string {
	var n int
	fmt.Sscan(p, &n)
	return fmt.Sprint(n + 1)
}

func nxMCPCmd() *cobra.Command {
	cmd := nxLeaf("mcp", "mcp <obi>", "Serve a document's operations as MCP tools", `Run a Model Context Protocol server on stdin and stdout that offers each
operation with bindings as a tool, and calls it the way ob invoke does.
<obi> may be a URL, including a running ob start.`,
		`  ob mcp tasks.obi.json
  ob mcp https://api.example.com --context staging --operation createTask --operation listTasks`,
		nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			if err := nxCheckContext(c); err != nil {
				return err
			}
			var tools, skipped []string
			for _, key := range nxPartKeys(doc, "operations") {
				if len(c.strs("operation")) > 0 && !nxContains(c.strs("operation"), key) {
					continue
				}
				if len(nxReferrers(doc, "bindings", "operation", key)) == 0 {
					skipped = append(skipped, key)
					continue
				}
				tools = append(tools, key)
			}
			c.note(fmt.Sprintf("Serving %s on stdio: %s", nxCount(len(tools), "tool"), strings.Join(tools, ", ")))
			if len(skipped) > 0 {
				c.note("Skipped (no bindings): " + strings.Join(skipped, ", "))
			}
			c.note("Waiting for an MCP client on stdin.")
			return nil
		})
	cmd.Flags().String("context", "", "the stored context to use (default \"default\")")
	cmd.Flags().StringArray("operation", nil, "offer only this operation (repeatable)")
	return cmd
}

// ------------------------------------------------------- kinds and delegates

func nxKindCmd() *cobra.Command {
	list := nxListFormats(nxLeaf("kind.list", "list", "List the kinds this ob can handle", `List the kinds this ob can handle, for each kind of work, and whether the
handler is built in or a delegate. A document can use any kind; this only
says what this installation can do with one.`,
		`  ob kind list
  ob kind list --role invoke`, nxArgs(0, 0), func(c *nxCtx) error {
			if c.set("role") {
				if err := nxValidRole(c.str("role")); err != nil {
					return nxUsageErr("%v", err)
				}
			}
			var rows [][]string
			var out []any
			for _, k := range nxInstalledKinds {
				if c.set("role") && !nxContains(k.roles, c.str("role")) {
					continue
				}
				row := []string{k.kind}
				for _, r := range nxRoles {
					row = append(row, nxYesNo(nxContains(k.roles, r)))
				}
				rows = append(rows, append(row, k.handler))
				out = append(out, nxNewObj().Set("kind", k.kind).Set("roles", nxToAny(k.roles)).Set("handler", k.handler))
			}
			c.render(out, func() { c.table("KIND\tINVOKE\tINSPECT\tSYNTHESIZE\tHANDLED BY", rows) })
			return nil
		}))
	list.Flags().String("role", "", "only kinds this ob can handle for: invoke, inspect, or synthesize")
	check := nxListFormats(nxLeaf("kind.check", "check <kind>", "Check whether this ob can handle a kind", `Say whether this ob can handle an exact kind, for one kind of work (--role)
or for each. Kinds are compared exactly: example.openapi@1 and
example.openapi@2 are unrelated kinds.

Exit status with --role: 0 yes; 1 no.`,
		`  ob kind check example.openapi@1
  ob kind check acme.billing-rpc@1 --role invoke`, nxArgs(1, 1), func(c *nxCtx) error {
			kind := c.args[0]
			if kind == "" {
				return nxUsageErr("a kind must not be empty")
			}
			roles := nxRoles
			if c.set("role") {
				if err := nxValidRole(c.str("role")); err != nil {
					return nxUsageErr("%v", err)
				}
				roles = []string{c.str("role")}
			}
			out := nxNewObj().Set("kind", kind)
			any := false
			var lines []string
			for _, r := range roles {
				handler, ok := nxSupports(kind, r)
				any = any || ok
				out.Set(r, ok)
				if ok {
					lines = append(lines, fmt.Sprintf("  %-11s yes (%s)", r, handler))
				} else {
					lines = append(lines, fmt.Sprintf("  %-11s no", r))
				}
			}
			c.render(out, func() {
				c.println(kind)
				for _, l := range lines {
					c.println(l)
				}
				if !any {
					for _, k := range nxInstalledKinds {
						if strings.EqualFold(k.kind, kind) || strings.SplitN(k.kind, "@", 2)[0] == strings.SplitN(kind, "@", 2)[0] {
							c.println(fmt.Sprintf("Note: %s is handled, but kinds match only exactly; %s is a different kind.", k.kind, kind))
							break
						}
					}
				}
			})
			if c.set("role") && !any {
				return nxFail(1, "")
			}
			return nil
		}))
	check.Flags().String("role", "", "the kind of work: invoke, inspect, or synthesize")
	return nxGroupCmd("kind", "See which kinds this ob can handle", `A source's kind says how its content and bindings are read. ob handles a
kind with a built-in handler or through a delegate. These commands report
what this installation can do; they never judge a document.`, list, check)
}

func nxDelegateCmd() *cobra.Command {
	roles := nxListFormats(nxLeaf("delegate.roles", "roles", "List the work ob can hand to delegates", `List the roles ob accepts delegates for, with the operations a delegate
must offer for each.`, `  ob delegate roles`, nxArgs(0, 0), func(c *nxCtx) error {
		var out []any
		for _, r := range nxRoles {
			out = append(out, nxNewObj().Set("role", r).Set("requires", nxToAny(nxRoleInterfaces[r])))
		}
		c.render(out, func() {
			for _, r := range nxRoles {
				c.println(r)
				for _, op := range nxRoleInterfaces[r] {
					c.println("  " + op)
				}
			}
		})
		return nil
	}))
	register := nxLeaf("delegate.register", "register <delegate-obi> --role <role>", "Register a delegate", `Register another tool as a delegate for one or more roles. <delegate-obi>
is the tool's OBI (a path, - for stdin, or a URL); ob keeps a copy, and
checks that it offers the operations each role needs. --preference ROLE=N
sets how strongly ob prefers it among delegates for that role. --replace ID
re-registers an existing delegate with a new OBI or roles.`,
		`  ob delegate register ./acme-rpc.obi.json --role invoke --preference invoke=10
  ob fetch https://tools.example.com | ob delegate register - --role inspect --role synthesize`,
		nxArgs(1, 1), func(c *nxCtx) error {
			if len(c.strs("role")) == 0 {
				return nxUsageErr("--role is required: invoke, inspect, or synthesize (repeatable)")
			}
			for _, r := range c.strs("role") {
				if err := nxValidRole(r); err != nil {
					return nxUsageErr("%v", err)
				}
			}
			for _, p := range c.strs("preference") {
				parts := strings.SplitN(p, "=", 2)
				if len(parts) != 2 || !nxContains(c.strs("role"), parts[0]) {
					return nxUsageErr("--preference takes ROLE=NUMBER for a role given with --role")
				}
			}
			id := "d_4e21"
			if c.set("replace") {
				id = c.str("replace")
			}
			c.note(fmt.Sprintf("(preview: %s was not read)", c.args[0]))
			c.println(fmt.Sprintf("Registered %s (Acme Tools) for %s.", id, strings.Join(c.strs("role"), ", ")))
			c.println("It offers every operation those roles need.")
			return nil
		})
	register.Flags().StringArray("role", nil, "a role to register for: invoke, inspect, or synthesize (repeatable)")
	register.Flags().StringArray("preference", nil, "ROLE=NUMBER; higher is preferred (repeatable)")
	register.Flags().String("replace", "", "re-register this delegate ID")
	list := nxListFormats(nxLeaf("delegate.list", "list", "List delegates", "List registered delegates, their roles, and their preferences.",
		`  ob delegate list
  ob delegate list --role invoke`, nxArgs(0, 0), func(c *nxCtx) error {
			var rows [][]string
			var out []any
			for _, d := range nxDelegates {
				if c.set("role") && !nxContains(d.roles, c.str("role")) {
					continue
				}
				var prefs []string
				for _, r := range d.roles {
					if p, ok := d.preferences[r]; ok {
						prefs = append(prefs, fmt.Sprintf("%s=%d", r, p))
					}
				}
				rows = append(rows, []string{d.id, d.name, strings.Join(d.roles, ", "), nxDash(strings.Join(prefs, ", "))})
				out = append(out, nxNewObj().Set("id", d.id).Set("name", d.name).Set("roles", nxToAny(d.roles)))
			}
			c.render(out, func() { c.table("ID\tNAME\tROLES\tPREFERENCES", rows) })
			return nil
		}))
	list.Flags().String("role", "", "only delegates for this role")
	find := func(id string) (nxDelegate, error) {
		for _, d := range nxDelegates {
			if d.id == id {
				return d, nil
			}
		}
		return nxDelegate{}, nxFail(1, "no delegate %q; ob delegate list shows them", id)
	}
	prefer := nxLeaf("delegate.prefer", "prefer <id> <number> --role <role>", "Set a delegate's preference for a role", `Set how strongly ob prefers a delegate among delegates for one role; higher
wins. --clear removes the preference instead.`,
		`  ob delegate prefer d_91c2 20 --role invoke
  ob delegate prefer d_91c2 --clear --role invoke`, nxArgs(1, 2), func(c *nxCtx) error {
			d, err := find(c.args[0])
			if err != nil {
				return err
			}
			role := c.str("role")
			if err := nxValidRole(role); err != nil {
				return nxUsageErr("--role is required: %v", err)
			}
			if !nxContains(d.roles, role) {
				return nxFail(1, "%s is not registered for %s", d.id, role)
			}
			if c.on("clear") {
				c.println(fmt.Sprintf("%s has no preference for %s now.", d.id, role))
				return nil
			}
			if len(c.args) < 2 {
				return nxUsageErr("give a number, or --clear")
			}
			c.println(fmt.Sprintf("%s now has preference %s for %s.", d.id, c.args[1], role))
			return nil
		})
	prefer.Flags().String("role", "", "the role the preference applies to (required)")
	prefer.Flags().Bool("clear", false, "remove the preference")
	unregister := nxLeaf("delegate.unregister", "unregister <id>", "Remove a delegate", "Remove a registered delegate.",
		`  ob delegate unregister d_7f3a`, nxArgs(1, 1), func(c *nxCtx) error {
			d, err := find(c.args[0])
			if err != nil {
				return err
			}
			c.println(fmt.Sprintf("Unregistered %s (%s).", d.id, d.name))
			return nil
		})
	resolve := nxLeaf("delegate.resolve", "resolve --role <role> --kind <kind>", "Explain which handler ob would use", `Explain which handler ob would use for a kind of work on an exact kind: a
built-in handler or a delegate, and why.`,
		`  ob delegate resolve --role invoke --kind acme.billing-rpc@1`, nxArgs(0, 0), func(c *nxCtx) error {
			role, kind := c.str("role"), c.str("kind")
			if err := nxValidRole(role); err != nil {
				return nxUsageErr("--role is required: %v", err)
			}
			if kind == "" {
				return nxUsageErr("--kind is required")
			}
			handler, ok := nxSupports(kind, role)
			if !ok {
				c.println(fmt.Sprintf("Nothing in this ob can %s %s: no built-in handler, and no delegate registered for %s supports it.", role, kind, role))
				return nxFail(1, "")
			}
			c.println(fmt.Sprintf("To %s %s, ob would use %s.", role, kind, handler))
			if strings.HasPrefix(handler, "delegate") {
				c.println("No built-in handler supports this kind; it is the only delegate for " + role + " that does.")
			} else {
				c.println("Built-in handlers come before delegates for this kind.")
			}
			return nil
		})
	resolve.Flags().String("role", "", "the kind of work: invoke, inspect, or synthesize (required)")
	resolve.Flags().String("kind", "", "the exact kind (required)")
	return nxGroupCmd("delegate", "Let other tools handle work for ob", `A delegate is another tool, described by its own OBI, that ob hands work
to: invoking bindings, inspecting sources, or synthesizing documents, for
kinds ob does not handle itself. Each kind of work is a role.`,
		roles, register, list, prefer, unregister, resolve)
}
