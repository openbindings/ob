package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

A service may ask for sign-in to read its OBI. ob then uses the context
stored for the URL's origin (ob context set <origin>), asks at a terminal,
and otherwise stops. fetch keeps a document written for an OpenBindings
version this ob does not read, and says so.

Commands that read a document, such as ob show and ob invoke, also accept a
URL directly and answer the same way; fetch is for keeping a copy.

Exit status: 0 fetched; 1 no OBI is published there; 3 refused: the
service asks for sign-in and nothing stored or given answers it.`,
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

// nxSecretFlag accepts a secret only from stdin (-, which asks at a
// terminal) or a file (@FILE), never from the command line itself.
func nxSecretFlag(c *nxCtx, flag string) error {
	v := c.str(flag)
	if v == "-" || (strings.HasPrefix(v, "@") && len(v) > 1) {
		return nil
	}
	return nxUsageErr("--%s takes - (stdin, or asked for at a terminal) or @FILE; a secret on the command line stays in your shell history", flag)
}

// nxContextField maps an --unset name to the stored field it removes.
func nxContextField(name string) (string, bool) {
	switch name {
	case "bearer-token":
		return "bearerToken", true
	case "access-token":
		return "accessToken", true
	case "refresh-token":
		return "refreshToken", true
	case "token-credential":
		return "tokenCredential", true
	case "api-key":
		return "apiKey", true
	case "basic", "token-provider":
		return map[string]string{"basic": "basic", "token-provider": "tokenProvider"}[name], true
	}
	if rest, ok := strings.CutPrefix(name, "header."); ok && rest != "" {
		return "headers." + rest, true
	}
	if rest, ok := strings.CutPrefix(name, "config."); ok && rest != "" {
		return "configuration." + rest, true
	}
	if rest, ok := strings.CutPrefix(name, "credential."); ok && rest != "" {
		return "credentials." + rest, true
	}
	if rest, ok := strings.CutPrefix(name, "cookie."); ok && rest != "" {
		return "cookies." + rest, true
	}
	return "", false
}

func nxContextFlagField(field string) string {
	switch field {
	case "bearerToken":
		return "bearer-token"
	case "accessToken":
		return "access-token"
	case "refreshToken":
		return "refresh-token"
	case "apiKey":
		return "api-key"
	case "tokenProvider":
		return "token-provider"
	case "tokenCredential":
		return "token-credential"
	}
	if rest, ok := strings.CutPrefix(field, "headers."); ok {
		return "header." + rest
	}
	if rest, ok := strings.CutPrefix(field, "configuration."); ok {
		return "config." + rest
	}
	if rest, ok := strings.CutPrefix(field, "credentials."); ok {
		return "credential." + rest
	}
	if rest, ok := strings.CutPrefix(field, "cookies."); ok {
		return "cookie." + rest
	}
	return field
}

// The operator determines the type, including for file and stdin sources.
func nxConfigAssignment(raw string) (point, kind string, err error) {
	point, value, ok := strings.Cut(raw, "=")
	if !ok || point == "" {
		return "", "", nxUsageErr("--config takes POINT=VALUE for a string or POINT:=JSON for a typed value; either value may be @FILE or -")
	}
	typed := strings.HasSuffix(point, ":")
	if typed {
		point = strings.TrimSuffix(point, ":")
	}
	if point == "" {
		return "", "", nxUsageErr("--config needs a configuration point before = or :=")
	}
	if value == "@" {
		return "", "", nxUsageErr("--config %s=@ must be followed by a file path", point)
	}
	if !typed {
		return point, "string", nil
	}
	if value == "-" || strings.HasPrefix(value, "@") {
		return point, "JSON from file or stdin", nil
	}
	v, parseErr := nxParse(value)
	if parseErr != nil {
		return "", "", nxUsageErr("--config %s:= needs a JSON value: %v", point, parseErr)
	}
	switch v.(type) {
	case nil:
		kind = "JSON null"
	case bool:
		kind = "JSON boolean"
	case json.Number:
		kind = "JSON number"
	case string:
		kind = "JSON string"
	case *nxObj:
		kind = "JSON object"
	case []any:
		kind = "JSON array"
	}
	return point, kind, nil
}

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

A secret never goes on the command line, where it would stay in your shell
history: --bearer-token, --access-token, --refresh-token, --api-key, --basic, and
--token-credential take - (read from stdin, or asked for at a terminal
without echo) or @FILE. --basic reads USER:PASSWORD. --header NAME=VALUE
takes a literal value, or NAME=- or NAME=@FILE for a secret one; the
headers that carry credentials (Authorization, Proxy-Authorization, Cookie)
take only those. --config POINT=VALUE answers a configuration point a binding
asks for, such as which server to use. POINT=VALUE always stores a string,
so code=001 and enabled=false keep their exact text. POINT:=JSON stores a
typed value, such as enabled:=false or server:={"url":"https://api.example.com"}.
Either operator accepts @FILE or -: = reads text and := parses JSON.
--value replaces the whole context with a JSON object, from @FILE or -.
Only one value may read stdin per command; use separate files for the others.

--credential NAME=- or NAME=@FILE stores a named credential in
credentials[NAME]. Read a JSON string for a bearer token or API key, a JSON
object {"username":...,"password":...} for Basic, or an OAuth object
beginning with accessToken. NAME is the scheme name the binding asks for,
an exact string. --cookie NAME=VALUE stores a non-secret cookie, or use
NAME=- or NAME=@FILE for a secret one.

--token-provider pins a token service: the OBI of a service that implements
the token-provider interface. ob keeps a copy of it, and when a binding asks
this scope for a bearer token, ob mints one from that provider (and only that
provider) and renews it before it expires. --token-credential is the
credential to mint with; leave it out for a provider that uses an identity
you are already signed in with. --token-binding chooses among the provider's
bindings, as ob invoke --binding does. A binding key names its operation, so
one list covers them all: give the mint binding and the refresh binding you
want, in any order, and each applies to its own operation.

--unset removes a field, named as its flag is: bearer-token, access-token,
refresh-token, api-key, basic, credential.NAME, cookie.NAME, token-provider,
token-credential, header.NAME, or config.POINT.
Text reports use these flag names; JSON reports keep the interface's field
names. A stored token provider also lets --token-credential rotate on its own.

`+scopeHelp,
		`  ob context set https://api.example.com --bearer-token -
  printf 'ada:s3cret' | ob context set https://legacy.example.com --basic -
  ob context set https://api.example.com/openapi.json --config 'server:={"url":"https://eu.example.com"}'
  ob context set https://api.example.com --credential primary=@primary.json --credential secondary=@secondary.json
  ob context set https://api.example.com --cookie locale=en --refresh-token -
  ob context set https://api.example.com --token-provider https://auth.example.com --token-credential @creds.txt
  ob context set https://api.example.com --unset header.X-Client`,
		nxArgs(1, 1), func(c *nxCtx) error {
			scope := c.args[0]
			if scope == "" {
				return nxUsageErr("a scope must not be empty")
			}
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			for _, f := range []string{"bearer-token", "access-token", "refresh-token", "api-key", "basic", "token-credential"} {
				if c.set(f) {
					if err := nxSecretFlag(c, f); err != nil {
						return err
					}
				}
			}
			if c.set("value") {
				for _, f := range []string{"bearer-token", "access-token", "refresh-token", "api-key", "basic", "credential", "cookie", "header", "config", "unset", "token-provider", "token-credential", "token-binding"} {
					if c.set(f) {
						return nxUsageErr("--value replaces the whole context, so it does not combine with --%s", f)
					}
				}
				if raw := c.str("value"); raw != "-" && !strings.HasPrefix(raw, "@") {
					return nxUsageErr("--value takes @FILE or - (stdin): a context can hold secrets, and a value on the command line stays in your shell history")
				}
				if _, _, err := c.object("value"); err != nil {
					return err
				}
				c.println("Replaced the context for " + scope)
				return nil
			}
			stored, _ := nxStoredContext(scope)
			pinned := false
			for _, h := range stored.holds {
				pinned = pinned || h[0] == "tokenProvider"
			}
			if c.set("token-credential") && !c.set("token-provider") && !pinned {
				return nxUsageErr("--token-credential needs a stored token provider or --token-provider")
			}
			if c.set("token-binding") && !c.set("token-provider") {
				return nxUsageErr("--token-binding goes with --token-provider")
			}
			// Each change, by the name --unset would use and the stored field.
			type change struct{ name, field string }
			var changes []change
			for _, f := range []struct{ flag, field string }{{"bearer-token", "bearerToken"}, {"access-token", "accessToken"}, {"refresh-token", "refreshToken"}, {"api-key", "apiKey"}, {"basic", "basic"}, {"token-credential", "tokenCredential"}} {
				if c.set(f.flag) {
					changes = append(changes, change{f.flag, f.field})
				}
			}
			for _, credential := range c.strs("credential") {
				name, source, ok := strings.Cut(credential, "=")
				if !ok || name == "" || !(source == "-" || strings.HasPrefix(source, "@") && len(source) > 1) {
					return nxUsageErr("--credential takes NAME=- or NAME=@FILE; supply a JSON credential value, never a secret on the command line")
				}
				changes = append(changes, change{"credential." + name, "credentials." + name})
			}
			for _, cookie := range c.strs("cookie") {
				name, value, ok := strings.Cut(cookie, "=")
				if !ok || name == "" || value == "@" {
					return nxUsageErr("--cookie takes NAME=VALUE, NAME=-, or NAME=@FILE; keep secret values off the command line")
				}
				changes = append(changes, change{"cookie." + name, "cookies." + name})
			}
			for _, h := range c.strs("header") {
				name, value, ok := strings.Cut(h, "=")
				if !ok || name == "" {
					return nxUsageErr("--header takes NAME=VALUE, NAME=-, or NAME=@FILE")
				}
				if value == "@" {
					return nxUsageErr("--header %s=@ must be followed by a file path", name)
				}
				switch strings.ToLower(name) {
				case "authorization", "proxy-authorization", "cookie":
					if value != "-" && !strings.HasPrefix(value, "@") {
						return nxUsageErr("the %s header carries credentials, so it takes %s=- (stdin, or asked for) or %s=@FILE; for a bearer token, --bearer-token - is simpler", name, name, name)
					}
				}
				changes = append(changes, change{"header." + name, "headers." + name})
			}
			for _, cfg := range c.strs("config") {
				point, kind, err := nxConfigAssignment(cfg)
				if err != nil {
					return err
				}
				c.note(fmt.Sprintf("(preview: config.%s would be stored as %s)", point, kind))
				changes = append(changes, change{"config." + point, "configuration." + point})
			}
			if c.set("token-provider") {
				changes = append(changes, change{"token-provider", "tokenProvider"})
				c.note(fmt.Sprintf("(preview: ob would keep a copy of %s's OBI and check that it offers the token-provider operations)", c.str("token-provider")))
			}
			var removed []string
			for _, u := range c.strs("unset") {
				field, ok := nxContextField(u)
				if !ok {
					return nxUsageErr("--unset takes bearer-token, access-token, refresh-token, api-key, basic, credential.NAME, cookie.NAME, token-provider, token-credential, header.NAME, or config.POINT")
				}
				for _, ch := range changes {
					if ch.name == u {
						return nxUsageErr("--%s and --unset %s ask for opposite changes; choose one", strings.SplitN(u, ".", 2)[0], u)
					}
				}
				held := false
				for _, h := range stored.holds {
					held = held || h[0] == field
				}
				if !held {
					return nxNotFound("nothing is stored as %s for %q; ob context show %s lists what is", u, scope, scope)
				}
				removed = append(removed, u)
			}
			line := "Context for " + scope + ":"
			if len(changes) > 0 {
				var fields []string
				for _, ch := range changes {
					fields = append(fields, ch.name)
				}
				line += " set " + strings.Join(fields, ", ")
				if len(removed) > 0 {
					line += ";"
				}
			}
			if len(removed) > 0 {
				line += " removed " + strings.Join(removed, ", ")
			}
			c.println(line)
			knownScope := false
			for _, need := range nxBindingNeeds {
				knownScope = knownScope || need.scope == scope
			}
			if !knownScope {
				c.note("hint: no binding in the sample document asks for this exact scope; ob uses only the scope string a binding names")
			}
			return nil
		})
	set.Flags().String("bearer-token", "", "a bearer token: - (stdin, or asked for) or @FILE")
	set.Flags().String("access-token", "", "an OAuth 2.0 access token: - (stdin, or asked for) or @FILE")
	set.Flags().String("refresh-token", "", "an OAuth 2.0 refresh token: - (stdin, or asked for) or @FILE")
	set.Flags().StringArray("credential", nil, "a named JSON credential: NAME=- or NAME=@FILE (repeatable)")
	set.Flags().StringArray("cookie", nil, "a non-secret cookie: NAME=VALUE; for a secret use NAME=- or NAME=@FILE (repeatable)")
	set.Flags().String("api-key", "", "an API key: - (stdin, or asked for) or @FILE")
	set.Flags().String("basic", "", "HTTP Basic credentials as USER:PASSWORD: - (stdin, or asked for) or @FILE")
	set.Flags().StringArray("header", nil, "a header to send: NAME=VALUE, or NAME=- or NAME=@FILE for a secret (repeatable)")
	set.Flags().StringArray("config", nil, "POINT=VALUE stores a string; POINT:=JSON stores a typed value; either accepts @FILE or - (repeatable)")
	set.Flags().String("value", "", "replace the whole context with a JSON object: @FILE or -")
	set.Flags().String("token-provider", "", "mint bearer tokens from this token service (its OBI: a path or URL)")
	set.Flags().String("token-credential", "", "the credential to mint with: - (stdin, or asked for) or @FILE")
	set.Flags().StringArray("token-binding", nil, "use this binding of the token service; repeat for an ordered list")
	set.Flags().StringArray("unset", nil, "remove a field: bearer-token, access-token, refresh-token, api-key, basic, credential.NAME, cookie.NAME, token-provider, token-credential, header.NAME, config.POINT")

	list := nxListFormats(nxLeaf("context.list", "list", "List stored contexts", "List the scopes that have stored context, and what each holds. Secrets are masked.",
		`  ob context list`, nxArgs(0, 0), func(c *nxCtx) error {
			var rows [][]string
			var out []any
			for _, ctx := range nxContexts {
				var fields []string
				for _, h := range ctx.holds {
					fields = append(fields, h[0])
				}
				var labels []string
				for _, f := range fields {
					labels = append(labels, nxContextFlagField(f))
				}
				rows = append(rows, []string{ctx.scope, strings.Join(labels, ", ")})
				out = append(out, nxNewObj().Set("scope", ctx.scope).Set("fields", nxToAny(fields)))
			}
			c.render(out, func() { c.table("SCOPE\tFIELDS", rows) })
			return nil
		}))
	show := nxListFormats(nxLeaf("context.show", "show <scope>", "Show a stored context", `Show what is stored for one scope. Every value is masked, since headers
and configuration can hold secrets too; --reveal prints them. Text names
match --unset; JSON keeps the interface's field names and represents each
hidden value as {"masked":true}.`,
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
				case !c.on("reveal"):
					value = "••••"
				}
				if c.on("reveal") {
					fields.Set(h[0], value)
				} else {
					fields.Set(h[0], nxNewObj().Set("masked", true))
				}
				rows = append(rows, []string{"  " + nxContextFlagField(h[0]), value})
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

Well-known interface fields are bearerToken, apiKey, credentials (named
strings or Basic/OAuth objects), apiKeys (historical named API keys), basic,
accessToken, refreshToken, expiresAt, headers, cookies, environment,
metadata, and configuration. Context is opaque and may also hold custom
fields. context set --value takes any of these as a whole JSON object;
the individual flags cover the common fields. Pinned token-provider settings
are ob's local resolver configuration.

`+scopeHelp, set, list, show, remove)
}

// ------------------------------------------------------------------ codegen

func nxCodegenCmd() *cobra.Command {
	cmd := nxLeaf("codegen", "codegen <obi> --lang <language>", "Generate a typed client", `Generate client code with one typed function per operation that has
bindings. Calls go through ob's invocation libraries and choose a binding
the way ob invoke does: each call takes an optional ordered list of binding
keys, like --binding; without one, a call uses the operation's only usable
binding and refuses when there are several.

-o names the output directory, and is required. It refuses to replace files
already there unless --force is given.`,
		`  ob codegen tasks.obi.json --lang go -o ./taskclient
  ob codegen https://api.example.com --lang typescript --package @acme/tasks -o ./tasks-client`,
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
				return nxUsageErr("-o is required: the directory to write the client to")
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
	cmd.Flags().StringP("out", "o", "", "the directory to write (required)")
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

// nxDelegatePreferences checks --preference ROLE=NUMBER against the roles
// the delegate has.
func nxDelegatePreferences(c *nxCtx, roles []string) error {
	for _, p := range c.strs("preference") {
		role, n, ok := strings.Cut(p, "=")
		if !ok || !nxContains(roles, role) {
			return nxUsageErr("--preference takes ROLE=NUMBER for a role the delegate has")
		}
		if _, err := strconv.Atoi(n); err != nil {
			return nxUsageErr("--preference %s: %q is not a whole number", role, n)
		}
	}
	return nil
}

func nxCompatCmd() *cobra.Command {
	cmd := nxLeaf("compat", "compat <obi> <contract>", "Check a document against a shared contract", `Check whether <obi> satisfies a shared contract: for each contract
operation, some operation must answer to its name, and fit the contract in
the role the document gives it:

  provider   it has bindings: it must accept every input the contract
             allows, and return only outputs the contract allows
  consumer   a dependency calls it (spec §5.5): it must send only inputs
             the contract accepts, and take every output the contract allows

An operation with both is checked both ways; one with neither, such as an
operation in a pure contract, is also checked both ways. Schemas are compared under the Schema Comparison Profile
OB-2020-12, published with the OpenBindings interfaces (schema-comparison).
Both arguments may be paths or URLs.

To check whether a new version of a document breaks callers of the old one,
compare it against the old one as the contract: ob compat new.obi.json
old.obi.json.

For each contract operation that nothing answers to, it prints the two ways
to meet it: give one of your operations the contract's name with ob
operation set --add-alias, or add the contract's operation with ob merge.

The result is compatible, incompatible, or indeterminate, and as the profile
defines, indeterminate outranks incompatible: one comparison outside the
profile makes the whole result indeterminate, while the report still lists
every operation found incompatible.

JSON reports list operations in sorted contract-key order. Each issue has
an operation, kind (missing, output_incompatible, or input_incompatible),
and detail carrying the profile's reason; output issues precede input issues
for a matched pair. Outside-profile comparisons are reported as
indeterminate with their reason. This preview's matched sample schemas fit;
the sample also demonstrates missing operations and their remedies.

Exit status: 0 compatible; 1 incompatible; 4 indeterminate.`,
		`  ob compat tasks.obi.json acme-tasks.obi.json
  ob compat https://api.example.com https://contracts.example.com/acme-tasks.json -q`,
		nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			contract := nxContract()
			c.note(fmt.Sprintf("(preview: a sample contract, Acme Tasks, stands in for %s)", c.args[1]))
			var rows [][]string
			var out []any
			issues := []any{}
			var missing []string
			ok := true
			indeterminate := false
			for _, cname := range nxSorted(contract.Obj("operations").Keys()) {
				if key, found := nxResolveOperation(doc, cname); found {
					roles := nxOperationRoles(doc, key)
					opIssues := []any{}
					var met any = true
					label := "yes"
					// Stable output-before-input ordering is part of the
					// profile's interface-level report.
					for _, kind := range []string{"output_incompatible", "input_incompatible", "outside-profile"} {
						for _, result := range nxCompatComparisons[cname] {
							if result.kind != kind {
								continue
							}
							issue := nxNewObj().Set("operation", cname).Set("kind", kind).Set("detail", result.detail)
							opIssues = append(opIssues, issue)
							issues = append(issues, issue)
							if kind == "outside-profile" {
								indeterminate = true
								met, label = nil, "undetermined"
							} else {
								ok = false
								met, label = false, "no"
							}
						}
					}
					by := key + " (schemas fit)"
					if len(opIssues) > 0 {
						by = key
					}
					rows = append(rows, []string{cname, label, by, strings.Join(roles, ", ")})
					out = append(out, nxNewObj().Set("operation", cname).Set("satisfied", met).Set("by", key).Set("as", nxToAny(roles)).Set("issues", opIssues))
				} else {
					ok = false
					missing = append(missing, cname)
					rows = append(rows, []string{cname, "no", "no operation answers to this name", "-"})
					issue := nxNewObj().Set("operation", cname).Set("kind", "missing").Set("detail", "no operation answers to this contract name")
					issues = append(issues, issue)
					out = append(out, nxNewObj().Set("operation", cname).Set("satisfied", false).Set("issues", []any{issue}).Set("remedies", nxToAny(nxRemedies(c, cname))))
				}
			}
			conclusion := "compatible"
			if !ok {
				conclusion = "incompatible"
			}
			if indeterminate {
				conclusion = "indeterminate"
			}
			if !c.on("quiet") {
				c.render(nxNewObj().Set("conclusion", conclusion).Set("profile", "OB-2020-12").Set("operations", out).Set("issues", issues), func() {
					verdict := "satisfies"
					if !ok {
						verdict = "does not satisfy"
					}
					if indeterminate {
						verdict = "could not be compared with"
					}
					c.println(fmt.Sprintf("%s %s Acme Tasks (%s)", c.args[0], verdict, c.args[1]))
					c.table("CONTRACT OPERATION\tMET\tBY\tAS", rows)
					for _, issue := range issues {
						i := issue.(*nxObj)
						if i.Get("kind") != "missing" {
							c.println(fmt.Sprintf("  %s: %s: %s", i.Get("operation"), i.Get("kind"), i.Get("detail")))
						}
					}
					for _, cname := range missing {
						r := nxRemedies(c, cname)
						c.println("")
						c.println("To meet " + cname + ":")
						c.println("  if one of your operations already does it:  " + r[0])
						c.println("  otherwise, add it from the contract:        " + r[1])
					}
				})
			}
			if indeterminate {
				return nxFail(4, "")
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

// nxOperationRoles says how a document uses an operation: as a provider (it has
// bindings), a consumer (a dependency calls it), or both. With neither, it
// is checked both ways, making pure-contract changes protect both sides.
func nxOperationRoles(doc *nxObj, key string) []string {
	var roles []string
	if len(nxReferrers(doc, "bindings", "operation", key)) > 0 {
		roles = append(roles, "provider")
	}
	if len(nxReferrers(doc, "dependencies", "operation", key)) > 0 {
		roles = append(roles, "consumer")
	}
	if len(roles) == 0 {
		roles = []string{"provider", "consumer"}
	}
	return roles
}

func nxRemedies(c *nxCtx, contractOp string) []string {
	return []string{
		fmt.Sprintf("ob operation set %s <operation> --add-alias %s", c.args[0], contractOp),
		fmt.Sprintf("ob merge %s %s --operation %s --no-bindings", c.args[0], c.args[1], contractOp),
	}
}

// ------------------------------------------------------------------ serving

func nxStartCmd() *cobra.Command {
	cmd := nxLeaf("start", "start", "Run ob as a local service", `Serve ob's own operations over HTTP and WebSocket on this machine, for
editors, browsers, and other tools. ob describes itself with an OBI at
/.well-known/openbindings.

Who may call it: every request must carry this run's access token as a
bearer token. ob saves the addresses and token in a private run record,
readable only by this user (file mode 0600, directory 0700). Its own commands
read the record automatically for an exact matching service address.
Text output names the file; -F json prints the startup record, including
the token, as one line. Other tools can read that record to connect.
The record is removed when this run stops; simultaneous runs use separate
records by HTTP port.

ob answers only requests addressed
to 127.0.0.1 or localhost by name, which stops a web page from reaching it
through DNS rebinding, and a browser page only from an origin given with
--allow-origin.

--tls also serves HTTPS, on the next port, with a certificate from ob's local
certificate authority. ob never changes what this machine trusts on its own:
install the authority first with ob ca install.`,
		`  ob start
  ob start --port 8080 --allow-origin https://editor.example.com
  ob start --tls`, nxArgs(0, 0), func(c *nxCtx) error {
			port, _ := c.cmd.Flags().GetInt("port")
			if c.on("tls") && !nxCAInstalled {
				return nxRefuse("--tls needs ob's local certificate authority, which is not installed, so nothing was started;\n  install it (this asks for an administrator's password):  ob ca install")
			}
			for _, o := range c.strs("allow-origin") {
				if u, err := url.Parse(o); err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" {
					return nxUsageErr("--allow-origin takes an origin, such as https://editor.example.com")
				}
			}
			if port < 1 || port > 65535 || c.on("tls") && port == 65535 {
				return nxUsageErr("--port must be 1 through 65535 (through 65534 with --tls)")
			}
			configDir, err := os.UserConfigDir()
			if err != nil {
				return nxFail(1, "could not locate this user's configuration directory: %v", err)
			}
			stateFile := filepath.Join(configDir, "ob", "runs", fmt.Sprintf("%d.json", port))
			address := fmt.Sprintf("http://127.0.0.1:%d", port)
			record := nxNewObj().Set("address", address).Set("token", "ob_run_4c1e9a7d2b").Set("stateFile", stateFile).Set("obi", address+"/.well-known/openbindings")
			if c.on("tls") {
				record.Set("tlsAddress", fmt.Sprintf("https://127.0.0.1:%d", port+1))
			}
			c.note("(preview: ob would save this run record privately and remove it when the service stops; nothing was written)")
			if c.format() == "json" {
				c.println(nxCompact(record))
				return nil
			}
			c.println("ob is serving on " + address)
			if c.on("tls") {
				c.println(fmt.Sprintf("              and https://127.0.0.1:%d", port+1))
			}
			c.println("Connection record (user only): " + stateFile)
			c.println("ob's own commands use its token automatically for this address.")
			if o := c.strs("allow-origin"); len(o) > 0 {
				c.println("Browser pages allowed from: " + strings.Join(o, ", "))
			}
			c.println(fmt.Sprintf("Its OBI: http://127.0.0.1:%d/.well-known/openbindings", port))
			c.println("Press Ctrl-C to stop.")
			return nil
		})
	cmd.Flags().Int("port", 20290, "the HTTP port; with --tls, HTTPS uses the next one")
	cmd.Flags().Bool("tls", false, "also serve HTTPS, with a certificate from ob's local certificate authority")
	cmd.Flags().StringArray("allow-origin", nil, "let browser pages from this origin call ob (repeatable)")
	nxFormat(cmd, "text", "json")
	return cmd
}

// nxCACmd manages ob's local certificate authority, which ob start --tls
// uses. Changing what the machine trusts is always its own step.
func nxCACmd() *cobra.Command {
	install := nxLeaf("ca.install", "install", "Install ob's local certificate authority", `Create ob's local certificate authority, if there is none yet, and install it
into this machine's trust store, so browsers and tools trust ob start --tls.
It asks for an administrator's password. Nothing else changes what this
machine trusts.`,
		`  ob ca install`, nxArgs(0, 0), func(c *nxCtx) error {
			if nxCAInstalled {
				c.note("ob's local certificate authority is already installed; no change")
				return nil
			}
			c.note("(preview: ob would ask for an administrator's password, then install its authority)")
			c.println("Installed ob's local certificate authority (expires 2036-10-01).")
			return nil
		})
	remove := nxLeaf("ca.remove", "remove", "Remove ob's local certificate authority", `Remove ob's local certificate authority from this machine's trust store and
delete its key. ob start --tls stops working until it is installed again.`,
		`  ob ca remove`, nxArgs(0, 0), func(c *nxCtx) error {
			if !nxCAInstalled {
				return nxNotFound("ob's local certificate authority is not installed; there is nothing to remove")
			}
			c.println("Removed ob's local certificate authority.")
			return nil
		})
	show := nxLeaf("ca.show", "show", "Show ob's local certificate authority", "Say whether ob's local certificate authority is installed, and if so, its fingerprint and expiry.",
		`  ob ca show`, nxArgs(0, 0), func(c *nxCtx) error {
			info := nxNewObj().Set("installed", nxCAInstalled)
			if nxCAInstalled {
				info.Set("fingerprint", "SHA256:4f:9c:1a:e2:77:0b:5d:c3").Set("expires", "2036-10-01")
			}
			c.render(info, func() {
				if !nxCAInstalled {
					c.println("ob's local certificate authority is not installed (ob ca install installs it).")
					return
				}
				c.println("ob's local certificate authority is installed in this machine's trust store.")
				c.println("  Fingerprint:  SHA256:4f:9c:1a:e2:77:0b:5d:c3")
				c.println("  Expires:      2036-10-01")
			})
			return nil
		})
	nxFormat(show, "text", "json")
	return nxGroupCmd("ca", "Manage the certificate authority ob start --tls uses", `ob start --tls serves HTTPS with certificates from a local certificate
authority. These commands install it into this machine's trust store, show
it, and remove it.`, install, remove, show)
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

--operation offers only the named operations (a key or an alias). A name
that is not in the document is a usage error, and one ob cannot offer is
refused.

How operations appear as MCP tools (their names, input and output shapes,
operations whose bindings stream, and errors) will follow the MCP binding
specification, which is not yet written for OpenBindings 0.2. Until then
this command is a placeholder for that mapping.`,
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
			var tools, skipped, warnings []string
			warnContext := func(op, binding string) {
				if need, asks := nxBindingNeeds[binding]; asks {
					if _, stored := nxStoredContext(need.scope); !stored {
						warnings = append(warnings, fmt.Sprintf("%s (via %s) needs context for %s; before calling it, run:\n    ob invoke %s %s --binding %s --preflight", op, binding, need.scope, c.args[0], op, binding))
					}
				}
			}
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
					warnContext(key, chosen)
					continue
				}
				var usable []string
				for _, b := range nxReferrers(doc, "bindings", "operation", key) {
					if _, ok := nxSupports(nxKindOf(doc, b), "invoke"); ok {
						usable = append(usable, b)
					}
				}
				reason := ""
				switch len(usable) {
				case 0:
					reason = "no binding this ob can invoke"
					if len(nxReferrers(doc, "bindings", "operation", key)) == 0 {
						reason = "no bindings"
					}
				case 1:
					tools = append(tools, key)
					warnContext(key, usable[0])
				default:
					reason = fmt.Sprintf("%d bindings ob can invoke (%s); choose with --binding", len(usable), strings.Join(usable, ", "))
				}
				if reason == "" {
					continue
				}
				if only[key] {
					return nxRefuse("ob cannot offer %s, which you asked for: %s; nothing was served", key, reason)
				}
				skipped = append(skipped, key+": "+reason)
			}
			c.note(fmt.Sprintf("Serving %s on stdio: %s", nxCount(len(tools), "tool"), strings.Join(tools, ", ")))
			if len(skipped) > 0 {
				c.note("Skipped:\n  " + strings.Join(skipped, "\n  "))
			}
			if len(warnings) > 0 {
				c.note("Context needed:\n  " + strings.Join(warnings, "\n  "))
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
				handlers := nxNewObj()
				for _, role := range k.roles {
					handler, _ := nxSupports(k.kind, role)
					handlers.Set(role, handler)
				}
				out = append(out, nxNewObj().Set("kind", k.kind).Set("roles", nxToAny(k.roles)).Set("handlers", handlers))
			}
			c.render(out, func() { c.table("KIND\tINVOKE\tINSPECT\tSYNTHESIZE\tHANDLED BY", rows) })
			return nil
		}))
	list.Flags().String("role", "", "only kinds this ob can handle for: invoke, inspect, or synthesize")
	check := nxListFormats(nxLeaf("kind.check", "check <kind>", "Check whether this ob can handle a kind", `Say whether this ob can handle an exact kind, for one kind of work (--role)
or for each. Kinds are compared exactly: example.openapi@1 and
example.openapi@2 are unrelated kinds. To see which handler ob would use,
and why, use ob delegate resolve.

Without --role, this is a report: exit 0 even if no work is supported.
With --role, it is a yes/no check: exit 0 supported, 1 unsupported.`,
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
						switch {
						case k.kind == kind:
							c.println(fmt.Sprintf("Note: this ob handles %s for %s, not for %s.", kind, strings.Join(k.roles, " and "), c.str("role")))
						case strings.EqualFold(k.kind, kind) || strings.SplitN(k.kind, "@", 2)[0] == strings.SplitN(kind, "@", 2)[0]:
							c.println(fmt.Sprintf("Note: %s is handled, but kinds match only exactly; %s is a different kind.", k.kind, kind))
						default:
							continue
						}
						break
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
	add := nxLeaf("delegate.add", "add <delegate-obi> --role <role>", "Register a delegate", `Register another tool as a delegate for one or more roles. <delegate-obi>
is the tool's OBI (a path, - for stdin, or a URL); ob keeps a copy, checks
that it offers the operations each role needs, and gives the registration an
ID. --preference ROLE=N sets how strongly ob prefers it among delegates for
that role; higher wins.`,
		`  ob delegate add ./acme-rpc.obi.json --role invoke --preference invoke=10
  ob fetch https://tools.example.com | ob delegate add - --role inspect --role synthesize`,
		nxArgs(1, 1), func(c *nxCtx) error {
			if len(c.strs("role")) == 0 {
				return nxUsageErr("--role is required: invoke, inspect, or synthesize (repeatable)")
			}
			for _, r := range c.strs("role") {
				if err := nxValidRole(r); err != nil {
					return nxUsageErr("%v", err)
				}
			}
			if err := nxDelegatePreferences(c, c.strs("role")); err != nil {
				return err
			}
			id := "d_4e21"
			c.note(fmt.Sprintf("(preview: %s was not read)", c.args[0]))
			c.render(nxNewObj().Set("id", id).Set("name", "Acme Tools").Set("roles", nxToAny(c.strs("role"))), func() {
				c.println(fmt.Sprintf("Registered %s (Acme Tools) for %s.", id, strings.Join(c.strs("role"), ", ")))
				c.println("It offers every operation those roles need.")
			})
			return nil
		})
	add.Flags().StringArray("role", nil, "a role to register for: invoke, inspect, or synthesize (repeatable)")
	add.Flags().StringArray("preference", nil, "ROLE=NUMBER; higher is preferred (repeatable)")
	nxFormat(add, "text", "json")
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
	show := nxListFormats(nxLeaf("delegate.show", "show <id>", "Show a delegate", "Show a registered delegate: its roles, its preference for each, and the operations it offers for each.",
		`  ob delegate show d_91c2`, nxArgs(1, 1), func(c *nxCtx) error {
			d, err := find(c.args[0])
			if err != nil {
				return err
			}
			prefs := nxNewObj()
			offers := nxNewObj()
			for _, r := range d.roles {
				if p, ok := d.preferences[r]; ok {
					prefs.Set(r, p)
				}
				offers.Set(r, nxToAny(nxRoleInterfaces[r]))
			}
			c.render(nxNewObj().Set("id", d.id).Set("name", d.name).Set("roles", nxToAny(d.roles)).Set("preferences", prefs).Set("offers", offers), func() {
				c.println(fmt.Sprintf("%s (%s)", d.id, d.name))
				for _, r := range d.roles {
					pref := "no preference"
					if p, ok := d.preferences[r]; ok {
						pref = fmt.Sprintf("preference %d", p)
					}
					c.println(fmt.Sprintf("  %s, %s, offering:", r, pref))
					for _, op := range nxRoleInterfaces[r] {
						c.println("    " + op)
					}
				}
			})
			return nil
		}))
	set := nxLeaf("delegate.set", "set <id>", "Change a delegate", `Change a registered delegate: replace its OBI with --obi, change its roles,
or set its preference for a role. Only the flags you give change anything.
--unset preference.ROLE removes a preference.`,
		`  ob delegate set d_91c2 --preference invoke=20
  ob delegate set d_91c2 --add-role synthesize
  ob delegate set d_91c2 --unset preference.invoke`,
		nxArgs(1, 1), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			if err := nxContradictions(c, nil, [2]string{"add-role", "remove-role"}); err != nil {
				return err
			}
			d, err := find(c.args[0])
			if err != nil {
				return err
			}
			roles := append([]string(nil), d.roles...)
			var changes []string
			for _, r := range c.strs("add-role") {
				if err := nxValidRole(r); err != nil {
					return nxUsageErr("%v", err)
				}
				if !nxContains(roles, r) {
					roles = append(roles, r)
					changes = append(changes, "added role "+r)
				}
			}
			for _, r := range c.strs("remove-role") {
				if !nxContains(roles, r) {
					return nxNotFound("%s is not registered for %s", d.id, r)
				}
				roles = nxWithout(roles, r)
				changes = append(changes, "removed role "+r)
			}
			if len(roles) == 0 {
				return nxRefuse("that would leave %s with no role, so nothing was changed; to remove the delegate, use ob delegate remove %s", d.id, d.id)
			}
			if err := nxDelegatePreferences(c, roles); err != nil {
				return err
			}
			for _, p := range c.strs("preference") {
				changes = append(changes, "preference "+p)
			}
			for _, u := range c.strs("unset") {
				role, ok := strings.CutPrefix(u, "preference.")
				if !ok {
					return nxUsageErr("--unset takes preference.ROLE")
				}
				if _, has := d.preferences[role]; !has {
					return nxNotFound("%s has no preference for %s", d.id, role)
				}
				changes = append(changes, "removed the preference for "+role)
			}
			if c.set("obi") {
				c.note(fmt.Sprintf("(preview: %s was not read)", c.str("obi")))
				changes = append(changes, "replaced its OBI")
			}
			if len(changes) == 0 {
				c.note(d.id + ": no change")
				return nil
			}
			c.println(fmt.Sprintf("%s: %s.", d.id, strings.Join(changes, "; ")))
			return nil
		})
	set.Flags().String("obi", "", "replace the delegate's OBI: a path, - for stdin, or a URL")
	set.Flags().StringArray("add-role", nil, "register it for another role (repeatable)")
	set.Flags().StringArray("remove-role", nil, "stop using it for a role (repeatable)")
	set.Flags().StringArray("preference", nil, "ROLE=NUMBER; higher is preferred (repeatable)")
	set.Flags().StringArray("unset", nil, "remove a preference: preference.ROLE")
	remove := nxLeaf("delegate.remove", "remove <id>", "Remove a delegate", "Remove a registered delegate. ob stops handing it work and deletes its copy of the delegate's OBI.",
		`  ob delegate remove d_7f3a`, nxArgs(1, 1), func(c *nxCtx) error {
			d, err := find(c.args[0])
			if err != nil {
				return err
			}
			c.println(fmt.Sprintf("Removed %s (%s).", d.id, d.name))
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
		roles, add, set, remove, list, show, resolve)
}
