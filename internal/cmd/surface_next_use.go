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
document unless -o is given; -o refuses to replace an existing file unless
--force is given.

Commands that read a document, such as ob show and ob invoke, also accept a
URL directly; fetch is for keeping a copy.`,
		`  ob fetch https://api.example.com -o tasks.obi.json
  ob fetch https://api.example.com | jq '.operations | keys'`,
		nxArgs(1, 1), func(c *nxCtx) error {
			url := c.args[0]
			if !nxIsURL(url) {
				return nxUsageErr("fetch takes an http or https URL; for a local file, use ob show")
			}
			if err := c.creating(c.str("out")); err != nil {
				return err
			}
			trimmed := strings.TrimRight(url, "/")
			if strings.Count(trimmed, "/") == 2 {
				c.note("(looked up " + trimmed + "/.well-known/openbindings)")
			}
			c.note("(preview: nothing was fetched; showing the sample document)")
			if out := c.str("out"); out != "" {
				c.note("Saved " + out)
				c.note("(preview) the document that would be saved:")
			}
			c.println(nxPretty(nxFixture()))
			return nil
		})
	cmd.Flags().StringP("out", "o", "", "save the document to a file")
	cmd.Flags().Bool("force", false, "replace an existing file")
	return cmd
}

// ------------------------------------------------------------------- invoke

// ------------------------------------------------------------------ context

func nxPrefix(p string, list []string) []string {
	var out []string
	for _, s := range list {
		out = append(out, p+s)
	}
	return out
}

func nxContextCmd() *cobra.Command {
	scopeHelp := `A scope is the exact string a binding names when it asks for context,
usually a service's origin such as https://api.example.com. ob prints it,
ready to copy, whenever a binding asks for something that is not stored.`
	set := nxLeaf("context.set", "set <scope>", "Store context for a scope", `Store what bindings need beyond their input, for one scope: credentials,
headers, and configuration values. ob uses a stored context only for that
exact scope, and only when the binding says the value may be reused.

Give a secret as - to read it from stdin and keep it out of your shell
history. --basic - reads USER:PASSWORD from stdin; --basic alone asks for
them. --config answers a configuration point a binding asks for, such as
which server to use. --value replaces the whole context with one JSON
object.

--token-provider pins a token service: when a binding asks this scope for a
bearer token, ob mints one from that provider (and only that provider) with
the credential given by --token-credential, and renews it before it expires.

`+scopeHelp,
		`  ob context set https://api.example.com --bearer-token -
  printf 'ada:s3cret' | ob context set https://legacy.example.com --basic -
  ob context set https://api.example.com/openapi.json --config server='{"url":"https://eu.example.com"}'
  ob context set https://api.example.com --token-provider https://auth.example.com --token-credential -`,
		nxArgs(1, 1), func(c *nxCtx) error {
			if c.args[0] == "" {
				return nxUsageErr("a scope must not be empty")
			}
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			if c.set("value") {
				for _, f := range []string{"bearer-token", "access-token", "api-key", "basic", "header", "config", "unset", "token-provider"} {
					if c.set(f) {
						return nxUsageErr("--value replaces the whole context, so it does not combine with --%s", f)
					}
				}
				if _, _, err := c.object("value"); err != nil {
					return err
				}
				c.println("Replaced the context for " + c.args[0])
				return nil
			}
			if c.set("token-provider") != c.set("token-credential") {
				return nxUsageErr("--token-provider and --token-credential go together")
			}
			var changes []string
			for _, f := range []struct{ flag, field string }{{"bearer-token", "bearerToken"}, {"access-token", "accessToken"}, {"api-key", "apiKey"}} {
				if c.set(f.flag) {
					changes = append(changes, f.field)
				}
			}
			if c.set("basic") {
				if c.str("basic") == "ask" {
					c.note("(preview: ob would ask for a user name and password here)")
				}
				changes = append(changes, "basic")
			}
			for _, h := range c.strs("header") {
				if !strings.Contains(h, "=") {
					return nxUsageErr("--header takes NAME=VALUE")
				}
				changes = append(changes, "headers."+strings.SplitN(h, "=", 2)[0])
			}
			for _, cfg := range c.strs("config") {
				if !strings.Contains(cfg, "=") {
					return nxUsageErr("--config takes POINT=VALUE")
				}
				changes = append(changes, "configuration."+strings.SplitN(cfg, "=", 2)[0])
			}
			if c.set("token-provider") {
				changes = append(changes, "tokenProvider")
			}
			for _, u := range c.strs("unset") {
				for _, ch := range changes {
					if ch == u {
						return nxUsageErr("%s is both set and unset; choose one", u)
					}
				}
			}
			line := "Context for " + c.args[0] + ":"
			if len(changes) > 0 {
				line += " set " + strings.Join(changes, ", ")
			}
			if u := c.strs("unset"); len(u) > 0 {
				line += "; removed " + strings.Join(u, ", ")
			}
			c.println(line)
			return nil
		})
	set.Flags().String("bearer-token", "", "a bearer token (- reads it from stdin)")
	set.Flags().String("access-token", "", "an OAuth 2.0 access token (- reads it from stdin)")
	set.Flags().String("api-key", "", "an API key (- reads it from stdin)")
	set.Flags().String("basic", "", "HTTP Basic credentials: - reads USER:PASSWORD from stdin; alone, ob asks")
	set.Flags().Lookup("basic").NoOptDefVal = "ask"
	set.Flags().StringArray("header", nil, "a header to send, NAME=VALUE (repeatable)")
	set.Flags().StringArray("config", nil, "a configuration value, POINT=VALUE; VALUE is JSON or a bare string (repeatable)")
	set.Flags().String("value", "", "replace the whole context: JSON, @file, or -")
	set.Flags().String("token-provider", "", "mint bearer tokens from this provider (a document path or URL)")
	set.Flags().String("token-credential", "", "the credential to mint with (- reads it from stdin)")
	set.Flags().StringArray("unset", nil, "remove a field, as ob context show names it (e.g. bearerToken, headers.X-Client)")

	list := nxListFormats(nxLeaf("context.list", "list", "List stored contexts", "List the scopes that have stored context, and what each holds. Secrets are masked.",
		`  ob context list`, nxArgs(0, 0), func(c *nxCtx) error {
			var rows [][]string
			var out []any
			for _, ctx := range nxContexts {
				var fields []string
				for _, h := range ctx.holds {
					fields = append(fields, h[0])
				}
				rows = append(rows, []string{ctx.scope, strings.Join(fields, ", ")})
				out = append(out, nxNewObj().Set("scope", ctx.scope).Set("fields", nxToAny(fields)))
			}
			c.render(out, func() { c.table("SCOPE\tFIELDS", rows) })
			return nil
		}))
	show := nxListFormats(nxLeaf("context.show", "show <scope>", "Show a stored context", `Show what is stored for one scope. Every value is masked, since headers
and configuration can hold secrets too; --reveal prints them.`,
		`  ob context show https://api.example.com`, nxArgs(1, 1), func(c *nxCtx) error {
			ctx, ok := nxStoredContext(c.args[0])
			if !ok {
				return nxNotFound("nothing is stored for %q; ob context list shows the scopes that have context", c.args[0])
			}
			fields := nxNewObj()
			var rows [][]string
			for _, h := range ctx.holds {
				value := h[1]
				switch {
				case c.on("reveal") && strings.HasPrefix(value, "••••"):
					value = "preview-secret-" + strings.TrimPrefix(value, "••••")
				case !c.on("reveal") && !strings.HasPrefix(value, "••••"):
					value = "••••"
				}
				fields.Set(h[0], value)
				rows = append(rows, []string{"  " + h[0], value})
			}
			c.render(nxNewObj().Set("scope", ctx.scope).Set("fields", fields), func() {
				c.println(ctx.scope)
				c.table("", rows)
			})
			return nil
		}))
	show.Flags().Bool("reveal", false, "print the values instead of masking them")
	remove := nxLeaf("context.remove", "remove <scope>", "Remove a stored context", "Remove everything stored for one scope.",
		`  ob context remove https://api.example.com/openapi.json`, nxArgs(1, 1), func(c *nxCtx) error {
			if _, ok := nxStoredContext(c.args[0]); !ok {
				return nxNotFound("nothing is stored for %q; ob context list shows the scopes that have context", c.args[0])
			}
			c.println("Removed the context for " + c.args[0])
			return nil
		})
	return nxGroupCmd("context", "Store credentials and settings for calls", `A context holds what a binding needs beyond its input to call a service:
credentials, headers, and configuration values. ob stores it by scope and
uses it for its own invocations: only for the exact scope a binding asks
for, only when the binding says the value may be reused, and only the fields
that one request needs. Delegates resolve their own context; ob never sends
them stored context.

`+scopeHelp, set, list, show, remove)
}

// ------------------------------------------------------------------ codegen

func nxCodegenCmd() *cobra.Command {
	cmd := nxLeaf("codegen", "codegen <obi> --lang <language>", "Generate a typed client", `Generate client code with one typed function per operation that has
bindings. Calls go through ob's invocation libraries and choose a binding
the way ob invoke does: each call takes an optional ordered list of binding
keys, like --binding; without one, a call uses the operation's only usable
binding and refuses when there are several.

-o names the output directory. It refuses to replace files already there
unless --force is given.`,
		`  ob codegen tasks.obi.json --lang go -o ./taskclient
  ob codegen https://api.example.com --lang typescript --package @acme/tasks`,
		nxArgs(1, 1), func(c *nxCtx) error {
			lang := c.str("lang")
			if lang == "" {
				return nxUsageErr("--lang is required: go or typescript")
			}
			if lang != "go" && lang != "typescript" {
				return nxUsageErr("--lang must be go or typescript; ob cannot generate %s yet", lang)
			}
			dir := c.str("out")
			if dir == "" {
				dir = "."
			}
			ext := map[string]string{"go": ".go", "typescript": ".ts"}[lang]
			for _, f := range []string{"client", "types", "operations"} {
				if err := c.creating(dir + "/" + f + ext); err != nil {
					return err
				}
			}
			c.println(fmt.Sprintf("Would write %s:", dir))
			c.println("  client" + ext + "       the client and its options")
			c.println("  types" + ext + "        Task, Problem, CreateTaskInput, CompleteTaskInput")
			c.println("  operations" + ext + "   CreateTask, ListTasks, CompleteTask, ImportTasks, WatchTasks")
			c.note("skipped events.deliver: it has no bindings")
			return nil
		})
	cmd.Flags().String("lang", "", "go or typescript (required)")
	cmd.Flags().String("package", "", "the package or module name")
	cmd.Flags().StringP("out", "o", "", "the directory to write")
	cmd.Flags().Bool("force", false, "replace files already in the directory")
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
compared under the Schema Comparison Profile OB-2020-12, published with the
OpenBindings interfaces (schema-comparison); a comparison ob cannot decide
is reported as undecided, not as a failure. Both arguments may be paths or
URLs.

To check whether a new version of a document breaks callers of the old one,
compare it against the old one as the contract: ob compat new.obi.json
old.obi.json.

For each contract operation that nothing answers to, it prints the two ways
to meet it: give one of your operations the contract's name with ob
operation set --add-alias, or add the contract's operation with ob merge.

Exit status: 0 compatible; 1 not compatible; 4 no verdict (some comparison
could not be decided, and nothing is known to be incompatible).`,
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
			conclusion := "compatible"
			if !ok {
				conclusion = "not compatible"
			}
			if !c.on("quiet") {
				c.render(nxNewObj().Set("conclusion", conclusion).Set("profile", "OB-2020-12").Set("operations", out), func() {
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
/.well-known/openbindings.

--tls also serves HTTPS, on the next port, with a certificate from a local
certificate authority. ob never changes what this machine trusts on its own:
the first time, add --install-ca to install that authority into the system
trust store (it asks for an administrator's password).`,
		`  ob start
  ob start --port 8080
  ob start --tls --install-ca`, nxArgs(0, 0), func(c *nxCtx) error {
			port, _ := c.cmd.Flags().GetInt("port")
			if c.on("install-ca") && !c.on("tls") {
				return nxUsageErr("--install-ca installs the authority --tls uses; give it with --tls")
			}
			if c.on("tls") && !c.on("install-ca") && !nxCAInstalled {
				return nxRefuse("--tls needs ob's local certificate authority, which is not installed, so nothing was started;\nto install it (this asks for an administrator's password): ob start --tls --install-ca")
			}
			if c.on("install-ca") {
				c.note("(preview: ob would install its local certificate authority into the system trust store here)")
			}
			c.println(fmt.Sprintf("ob is serving on http://127.0.0.1:%d", port))
			if c.on("tls") {
				c.println(fmt.Sprintf("              and https://127.0.0.1:%d", port+1))
			}
			c.println(fmt.Sprintf("Its OBI: http://127.0.0.1:%d/.well-known/openbindings", port))
			c.println("Press Ctrl-C to stop.")
			return nil
		})
	cmd.Flags().Int("port", 20290, "the HTTP port; with --tls, HTTPS uses the next one")
	cmd.Flags().Bool("tls", false, "also serve HTTPS, with a certificate from ob's local certificate authority")
	cmd.Flags().Bool("install-ca", false, "with --tls: install that authority into the system trust store first")
	return cmd
}

func nxMCPCmd() *cobra.Command {
	cmd := nxLeaf("mcp", "mcp <obi>", "Serve a document's operations as MCP tools", `Run a Model Context Protocol server on stdin and stdout that offers each
operation it can call as a tool, and calls it the way ob invoke does, with
the same context rules. <obi> may be a URL, including a running ob start.

Each tool call uses a binding chosen the way ob invoke chooses one.
--binding names bindings to use, in order, like ob invoke --binding; a
binding key belongs to one operation, so each key applies to its own
operation. An operation with several bindings ob can invoke, and none
named, is skipped and listed with the reason.

--operation offers only the named operations (a key or an alias); a name
that is not in the document is refused.`,
		`  ob mcp tasks.obi.json --binding createTask.http
  ob mcp https://api.example.com --operation createTask --operation listTasks --binding createTask.mcp`,
		nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			only := map[string]bool{}
			for _, name := range c.strs("operation") {
				key, ok := nxResolveOperation(doc, name)
				if !ok {
					return nxNotFound("no operation named %q in %s%s", name, c.args[0], nxDidYouMean(doc, name))
				}
				only[key] = true
			}
			named := map[string][]string{}
			for _, b := range c.strs("binding") {
				if doc.Obj("bindings") == nil || !doc.Obj("bindings").Has(b) {
					return nxNotFound("no binding named %q in %s", b, c.args[0])
				}
				op := fmt.Sprint(doc.Obj("bindings").Obj(b).Get("operation"))
				named[op] = append(named[op], b)
			}
			var tools, skipped []string
			for _, key := range nxPartKeys(doc, "operations") {
				if len(only) > 0 && !only[key] {
					continue
				}
				if list := named[key]; len(list) > 0 {
					chosen := ""
					for _, b := range list {
						if _, ok := nxSupports(nxKindOf(doc, b), "invoke"); ok {
							chosen = b
							break
						}
					}
					if chosen == "" {
						return nxRefuse("this ob cannot invoke any binding you named for %s (%s), so nothing was served", key, strings.Join(list, ", "))
					}
					tools = append(tools, key+" (via "+chosen+")")
					continue
				}
				var usable []string
				for _, b := range nxReferrers(doc, "bindings", "operation", key) {
					if _, ok := nxSupports(nxKindOf(doc, b), "invoke"); ok {
						usable = append(usable, b)
					}
				}
				switch len(usable) {
				case 0:
					if len(nxReferrers(doc, "bindings", "operation", key)) == 0 {
						skipped = append(skipped, key+": no bindings")
					} else {
						skipped = append(skipped, key+": no binding this ob can invoke")
					}
				case 1:
					tools = append(tools, key)
				default:
					skipped = append(skipped, fmt.Sprintf("%s: %d bindings ob can invoke (%s); choose with --binding", key, len(usable), strings.Join(usable, ", ")))
				}
			}
			c.note(fmt.Sprintf("Serving %s on stdio: %s", nxCount(len(tools), "tool"), strings.Join(tools, ", ")))
			if len(skipped) > 0 {
				c.note("Skipped:\n  " + strings.Join(skipped, "\n  "))
			}
			c.note("Waiting for an MCP client on stdin.")
			return nil
		})
	cmd.Flags().StringArray("operation", nil, "offer only this operation, by key or alias (repeatable)")
	cmd.Flags().StringArray("binding", nil, "use this binding for its operation; repeat for an ordered list")
	return cmd
}

// ------------------------------------------------------- kinds and delegates

func nxKindCmd() *cobra.Command {
	list := nxListFormats(nxLeaf("kind.list", "list", "List the kinds this ob can handle", `List the kinds this ob can handle, for each kind of work, and whether the
handler is built in or a delegate. A document can use any kind; this only
says what this installation can do with one. For delegates the list is what
they advertise and may be incomplete; ob kind check gives the authoritative
answer for one kind.`,
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
example.openapi@2 are unrelated kinds. To see which handler ob would use,
and why, use ob delegate resolve.

Exit status: 0 this ob can handle it (for --role, or for at least one kind of
work); 1 it cannot.`,
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
				entry := nxNewObj().Set("supported", ok)
				if ok {
					entry.Set("handler", handler)
				}
				out.Set(r, entry)
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
			if !any {
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
			c.render(nxNewObj().Set("id", id).Set("name", "Acme Tools").Set("roles", nxToAny(c.strs("role"))), func() {
				c.println(fmt.Sprintf("Registered %s (Acme Tools) for %s.", id, strings.Join(c.strs("role"), ", ")))
				c.println("It offers every operation those roles need.")
			})
			return nil
		})
	nxFormat(register, "text", "json")
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
		return nxDelegate{}, nxNotFound("no delegate %q; ob delegate list shows them", id)
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
				return nxRefuse("%s is not registered for %s", d.id, role)
			}
			if c.on("clear") && len(c.args) == 2 {
				return nxUsageErr("--clear removes the preference, so it does not take a number; choose one")
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
built-in handler or a delegate, and why. ob kind check answers only whether
this ob can handle the kind.`,
		`  ob delegate resolve --role invoke --kind acme.billing-rpc@1`, nxArgs(0, 0), func(c *nxCtx) error {
			role, kind := c.str("role"), c.str("kind")
			if err := nxValidRole(role); err != nil {
				return nxUsageErr("--role is required: %v", err)
			}
			if kind == "" {
				return nxUsageErr("--kind is required")
			}
			handler, ok := nxSupports(kind, role)
			report := nxNewObj().Set("role", role).Set("kind", kind)
			if ok {
				report.Set("handler", handler)
			} else {
				report.Set("handler", nil)
			}
			c.render(report, func() {
				switch {
				case !ok:
					c.println(fmt.Sprintf("Nothing in this ob can %s %s: no built-in handler, and no delegate registered for %s supports it.", role, kind, role))
				case strings.HasPrefix(handler, "delegate"):
					c.println(fmt.Sprintf("To %s %s, ob would use %s.", role, kind, handler))
					c.println("No built-in handler supports this kind; it is the only delegate for " + role + " that does.")
				default:
					c.println(fmt.Sprintf("To %s %s, ob would use its %s handler.", role, kind, handler))
					c.println("Built-in handlers come before delegates for this kind.")
				}
			})
			if !ok {
				return nxFail(1, "")
			}
			return nil
		})
	nxFormat(resolve, "text", "json")
	resolve.Flags().String("role", "", "the kind of work: invoke, inspect, or synthesize (required)")
	resolve.Flags().String("kind", "", "the exact kind (required)")
	return nxGroupCmd("delegate", "Let other tools handle work for ob", `A delegate is another tool, described by its own OBI, that ob hands work
to: invoking bindings, inspecting sources, or synthesizing documents, for
kinds ob does not handle itself. Each kind of work is a role.`,
		roles, register, list, prefer, unregister, resolve)
}
