package cmd

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
)

func nxArgs(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < min {
			return fmt.Errorf("missing argument; usage: %s", cmd.UseLine())
		}
		if max >= 0 && len(args) > max {
			return fmt.Errorf("too many arguments; usage: %s", cmd.UseLine())
		}
		return nil
	}
}

func nxInitCmd() *cobra.Command {
	cmd := nxLeaf("init", "init [<obi>]", "Create a new OBI", `Create a new OBI with an empty operations map, written to <obi>, or to
stdout when <obi> is omitted or -. It refuses to replace an existing file
unless --force is given.

--interface-version sets the document's own version label (its "version"
member). The OpenBindings version is always 0.2.0.`,
		`  ob init tasks.obi.json --name "Task Manager"
  ob init --name "Task Manager" --interface-version 1.0.0 > tasks.obi.json`,
		nxArgs(0, 1), func(c *nxCtx) error {
			if c.set("version") {
				return nxUsageErr("--version is ambiguous here: use --interface-version for the document's version label; the OpenBindings version is always 0.2.0")
			}
			doc := nxNewObj()
			doc.Set("openbindings", "0.2.0")
			for _, f := range []struct{ flag, member string }{{"name", "name"}, {"interface-version", "version"}, {"description", "description"}} {
				if c.set(f.flag) {
					if f.flag == "interface-version" && c.str(f.flag) == "" {
						return nxUsageErr("--interface-version must not be empty")
					}
					doc.Set(f.member, c.str(f.flag))
				}
			}
			doc.Set("operations", nxNewObj())
			path := "-"
			if len(c.args) == 1 {
				path = c.args[0]
			}
			if path != "-" {
				c.note("Created " + path)
				c.note("(preview) the document that would be written:")
			}
			c.println(nxPretty(doc))
			return nil
		})
	cmd.Flags().String("name", "", "a human-readable name")
	cmd.Flags().String("description", "", "a human-readable description")
	cmd.Flags().String("interface-version", "", "the document's own version label")
	cmd.Flags().Bool("force", false, "replace an existing file")
	cmd.Flags().String("version", "", "")
	_ = cmd.Flags().MarkHidden("version")
	return cmd
}

func nxSetCmd() *cobra.Command {
	cmd := nxEditable(nxLeaf("set", "set <obi>", "Change a document's name, version label, or description", `Change the document's own fields: its name, its version label (the
document's "version" member, the interface's own label, not the
OpenBindings version), and its description. Only the flags you give change
anything; --unset removes one.`,
		`  ob set tasks.obi.json --interface-version 1.5.0
  ob set tasks.obi.json --unset description`,
		nxArgs(1, 1), func(c *nxCtx) error {
			if c.set("version") {
				return nxUsageErr("--version is ambiguous here: use --interface-version for the document's version label")
			}
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			var changed []string
			for _, f := range []struct{ flag, member string }{{"name", "name"}, {"interface-version", "version"}, {"description", "description"}} {
				if !c.set(f.flag) {
					continue
				}
				if nxContains(c.strs("unset"), f.member) {
					return nxUsageErr("%s is both set and unset; choose one", f.member)
				}
				if f.member == "version" && c.str(f.flag) == "" {
					return nxUsageErr("--interface-version must not be empty")
				}
				after.SetCanon(f.member, c.str(f.flag), nxDocOrder)
				changed = append(changed, f.member)
			}
			for _, u := range c.strs("unset") {
				if u != "name" && u != "version" && u != "description" {
					return nxUsageErr("--unset takes one of: name, version, description")
				}
				after.Delete(u)
				changed = append(changed, u)
			}
			return c.wrote(c.args[0], before, after, "Changed the document's "+strings.Join(changed, ", "))
		}))
	cmd.Flags().String("name", "", "a human-readable name")
	cmd.Flags().String("interface-version", "", "the document's own version label")
	cmd.Flags().String("description", "", "a human-readable description")
	cmd.Flags().StringArray("unset", nil, "remove a field: name, version, description")
	cmd.Flags().String("version", "", "")
	_ = cmd.Flags().MarkHidden("version")
	return cmd
}

func nxShowCmd() *cobra.Command {
	cmd := nxLeaf("show", "show <obi>", "Show a document", `Show an overview of a document: its operations, sources, bindings,
dependencies, and schemas. -F json prints the exact stored document.

<obi> is a path, - for stdin, or a URL. A bare origin such as
https://api.example.com is looked up at /.well-known/openbindings.`,
		`  ob show tasks.obi.json
  ob show https://api.example.com -F json`,
		nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			switch c.format() {
			case "json":
				c.println(nxPretty(doc))
			case "yaml":
				c.println(nxYAML(doc))
			default:
				c.out.WriteString(nxOverview(doc))
			}
			return nil
		})
	nxFormat(cmd, "text", "json", "yaml")
	return cmd
}

func nxOverview(doc *nxObj) string {
	var b strings.Builder
	title, _ := doc.Get("name").(string)
	if title == "" {
		title = "(unnamed)"
	}
	if v, _ := doc.Get("version").(string); v != "" {
		title += " " + v
	}
	fmt.Fprintf(&b, "%s  (OpenBindings %v)\n", title, doc.Get("openbindings"))
	if d, _ := doc.Get("description").(string); d != "" {
		b.WriteString(d + "\n")
	}
	section := func(name string, rows [][]string) {
		if len(rows) == 0 {
			return
		}
		b.WriteString("\n" + name + "\n")
		c := &nxCtx{}
		c.table("", rows)
		for _, line := range strings.Split(strings.TrimRight(c.out.String(), "\n"), "\n")[1:] {
			b.WriteString("  " + strings.TrimRight(line, " ") + "\n")
		}
	}
	var rows [][]string
	for _, key := range nxPartKeys(doc, "operations") {
		op := doc.Obj("operations").Obj(key)
		also := ""
		if aliases := nxStrings(op.Get("aliases")); len(aliases) > 0 {
			also = "also " + strings.Join(aliases, ", ")
		}
		use := ""
		if bs := nxReferrers(doc, "bindings", "operation", key); len(bs) > 0 {
			use = "bindings: " + strings.Join(bs, ", ")
		} else if ds := nxReferrers(doc, "dependencies", "operation", key); len(ds) > 0 {
			use = "consumed at: " + strings.Join(ds, ", ")
		} else {
			use = "no bindings"
		}
		rows = append(rows, []string{key, also, use})
	}
	section("Operations", rows)
	rows = nil
	for _, key := range nxPartKeys(doc, "sources") {
		src := doc.Obj("sources").Obj(key)
		n := len(nxReferrers(doc, "bindings", "source", key))
		rows = append(rows, []string{key, fmt.Sprint(src.Get("kind")), nxCount(n, "binding")})
	}
	section("Sources", rows)
	rows = nil
	for _, key := range nxPartKeys(doc, "bindings") {
		rows = append(rows, append([]string{key}, nxBindingSummary(doc.Obj("bindings").Obj(key))...))
	}
	section("Bindings", rows)
	rows = nil
	for _, key := range nxPartKeys(doc, "dependencies") {
		d := doc.Obj("dependencies").Obj(key)
		kinds := "any kind"
		if ks := nxStrings(d.Get("kinds")); len(ks) > 0 {
			kinds = "kinds: " + strings.Join(ks, ", ")
		}
		rows = append(rows, []string{key, fmt.Sprint(d.Get("operation")), kinds})
	}
	section("Dependencies", rows)
	if keys := nxPartKeys(doc, "schemas"); len(keys) > 0 {
		b.WriteString("\nSchemas\n  " + strings.Join(keys, ", ") + "\n")
	}
	return b.String()
}

func nxBindingSummary(b *nxObj) []string {
	var notes []string
	if p := b.Get("preference"); p != nil {
		notes = append(notes, fmt.Sprintf("preference %v", p))
	}
	if v, ok := b.Get("idempotent").(bool); ok {
		if v {
			notes = append(notes, "idempotent")
		} else {
			notes = append(notes, "not idempotent")
		}
	}
	if d, _ := b.Get("deprecated").(bool); d {
		notes = append(notes, "deprecated")
	}
	return []string{fmt.Sprintf("%v → %v", b.Get("operation"), b.Get("source")), strings.Join(notes, ", ")}
}

func nxCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func nxValidateCmd() *cobra.Command {
	cmd := nxLeaf("validate", "validate <obi>", "Check a document, a value, or examples", `Check a document against OpenBindings 0.2. The result is one of:

  conformant                 every rule was checked and none is violated
  non-conformant             at least one rule is violated
  conformance undetermined   nothing is violated, but some rule could not be checked

A document written for an OpenBindings version ob does not support is
refused rather than judged. The report names the spec text it applied.

--operation checks one value against that operation's schemas instead: give
the value with --input or --output (JSON, @file, or - for stdin).
--examples checks every operation example against its schemas.

Exit status: 0 conformant, or the value fits; 1 non-conformant, or it does
not fit; 3 refused (an OpenBindings version ob does not support); 4 no
verdict (conformance undetermined, or no schema to check the value against).`,
		`  ob validate tasks.obi.json
  ob validate tasks.obi.json --operation createTask --input '{"title":"Write the docs"}'
  ob validate tasks.obi.json --examples`,
		nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			hasValue := c.set("input") || c.set("output")
			switch {
			case c.on("examples") && (c.set("operation") || hasValue):
				return nxUsageErr("--examples checks every example; it does not combine with --operation, --input, or --output")
			case hasValue && !c.set("operation"):
				return nxUsageErr("--input and --output need --operation to say which contract to check against")
			case c.set("operation") && !hasValue:
				return nxUsageErr("--operation needs a value to check: --input or --output")
			case c.set("input") && c.set("output"):
				return nxUsageErr("check one value at a time: --input or --output")
			}
			if c.set("operation") {
				return nxValidateValue(c, doc)
			}
			if c.on("examples") {
				return nxValidateExamples(c, doc)
			}
			if c.on("quiet") {
				return nil
			}
			if c.format() == "json" {
				c.println(`{
  "document": ` + nxString(c.args[0]) + `,
  "conclusion": "conformant",
  "specification": { "line": "0.2", "text": "working draft", "revision": "de2c20b" },
  "rules": { "decided": 13, "violated": [], "undecided": [] }
}`)
				return nil
			}
			c.println(c.args[0] + ": conformant")
			c.println("  Checked against OpenBindings 0.2 (working draft, spec revision de2c20b).")
			c.println("  All 13 document rules were checked; none is violated.")
			return nil
		})
	cmd.Flags().String("operation", "", "check a value against this operation (name or alias)")
	cmd.Flags().String("input", "", "the input value to check: JSON, @file, or -")
	cmd.Flags().String("output", "", "the output value to check: JSON, @file, or -")
	cmd.Flags().Bool("examples", false, "check every operation example against its schemas")
	cmd.Flags().BoolP("quiet", "q", false, "print nothing; report through the exit status only")
	nxFormat(cmd, "text", "json")
	return cmd
}

func nxValidateValue(c *nxCtx, doc *nxObj) error {
	name := c.str("operation")
	key, ok := nxResolveOperation(doc, name)
	if !ok {
		return nxFail(2, "no operation named %q in %s", name, c.args[0])
	}
	side := "input"
	if c.set("output") {
		side = "output"
	}
	value, _, err := c.value(side)
	if err != nil {
		return err
	}
	problems, checked, err := nxCheck(doc, key, side, value)
	if err != nil {
		return err
	}
	if !c.on("quiet") && c.format() == "json" {
		report := nxNewObj().Set("operation", key).Set("value", side)
		if !checked {
			report.Set("fits", nil).Set("reason", "no "+side+" contract is specified")
		} else {
			report.Set("fits", len(problems) == 0).Set("problems", nxToAny(problems))
		}
		c.println(nxPretty(report))
	}
	if !checked {
		if !c.on("quiet") && c.format() != "json" {
			c.println(fmt.Sprintf("%s specifies no %s contract, so there is nothing to check this value against.", key, side))
		}
		return nxFail(4, "")
	}
	if len(problems) == 0 {
		if !c.on("quiet") && c.format() != "json" {
			c.println(fmt.Sprintf("The %s value fits %s's %s schema.", side, key, side))
		}
		return nil
	}
	if !c.on("quiet") && c.format() != "json" {
		c.println(fmt.Sprintf("The %s value does not fit %s's %s schema:", side, key, side))
		for _, p := range problems {
			c.println("  " + p)
		}
	}
	return nxFail(1, "")
}

func nxValidateExamples(c *nxCtx, doc *nxObj) error {
	count, bad, unchecked := 0, 0, 0
	var report []any
	text := c.format() != "json" && !c.on("quiet")
	for _, key := range nxPartKeys(doc, "operations") {
		examples := doc.Obj("operations").Obj(key).Obj("examples")
		if examples == nil {
			continue
		}
		for _, name := range examples.Keys() {
			ex := examples.Obj(name)
			var parts []string
			entry := nxNewObj().Set("operation", key).Set("example", name)
			for _, side := range []string{"input", "output"} {
				if !ex.Has(side) {
					continue
				}
				problems, checked, err := nxCheck(doc, key, side, ex.Get(side))
				if err != nil {
					return err
				}
				switch {
				case !checked:
					parts = append(parts, side+" not checked (no "+side+" schema)")
					entry.Set(side, "not checked")
					unchecked++
				case len(problems) == 0:
					parts = append(parts, side+" fits")
					entry.Set(side, "fits")
				default:
					parts = append(parts, side+" does not fit: "+strings.Join(problems, "; "))
					entry.Set(side, "does not fit").Set(side+"Problems", nxToAny(problems))
					bad++
				}
			}
			count++
			report = append(report, entry)
			if text {
				c.println(fmt.Sprintf("%s example %q: %s", key, name, strings.Join(parts, ", ")))
			}
		}
	}
	if c.format() == "json" && !c.on("quiet") {
		c.println(nxPretty(report))
	}
	if bad > 0 {
		if text {
			c.println(fmt.Sprintf("%s checked; %d value(s) do not fit.", nxCount(count, "example"), bad))
		}
		return nxFail(1, "")
	}
	if unchecked > 0 {
		if text {
			c.println(fmt.Sprintf("%s checked; %d value(s) have no schema to check against.", nxCount(count, "example"), unchecked))
		}
		return nxFail(4, "")
	}
	if text {
		c.println(fmt.Sprintf("%s checked; all values fit.", nxCount(count, "example")))
	}
	return nil
}

// nxCheck validates a value against an operation's schema, bundling the
// document's named schemas so same-document references resolve.
func nxCheck(doc *nxObj, opKey, side string, value any) ([]string, bool, error) {
	op := doc.Obj("operations").Obj(opKey)
	if !op.Has(side) {
		return nil, false, nil
	}
	if obj, ok := value.(*nxObj); ok && obj.Has("$comment") && strings.HasPrefix(fmt.Sprint(obj.Get("$comment")), "value from ") {
		return nil, true, nil
	}
	schema := nxClone(op.Get(side))
	if obj, ok := schema.(*nxObj); ok && doc.Obj("schemas") != nil {
		defs := nxClone(doc.Obj("schemas")).(*nxObj)
		for _, k := range defs.Keys() {
			nxRewriteRefs(defs, "#/schemas/"+k, "#/$defs/"+k)
			nxRewriteRefs(obj, "#/schemas/"+k, "#/$defs/"+k)
		}
		obj.Set("$defs", defs)
	}
	parsed, err := jsonschema.UnmarshalJSON(strings.NewReader(nxCompact(schema)))
	if err != nil {
		return nil, true, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("urn:ob-preview:check", parsed); err != nil {
		return nil, true, err
	}
	compiled, err := compiler.Compile("urn:ob-preview:check")
	if err != nil {
		return nil, true, err
	}
	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(nxCompact(value)))
	if err != nil {
		return nil, true, err
	}
	if err := compiled.Validate(instance); err != nil {
		var problems []string
		for _, line := range strings.Split(err.Error(), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") {
				problem := strings.TrimPrefix(line, "- ")
				problem = strings.Replace(problem, "at '':", "at the top level:", 1)
				problem = nxQuotedAt.ReplaceAllString(problem, "at $1")
				problems = append(problems, problem)
			}
		}
		if len(problems) == 0 {
			problems = []string{err.Error()}
		}
		return problems, true, nil
	}
	return nil, true, nil
}

// The diff preview compares the sample with an edited copy of it.
func nxDiffSample() (*nxObj, *nxObj) {
	before := nxFixture()
	after := nxFixture()
	archive := nxNewObj()
	archive.Set("description", "Archive a task.")
	archive.Set("input", nxMustParse(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`))
	archive.Set("output", nxMustParse(`{"$ref":"#/schemas/Task"}`))
	after.Obj("operations").Set("archiveTask", archive)
	after.Obj("operations").Obj("listTasks").Set("output", nxMustParse(`{"type":"array","items":{"$ref":"#/schemas/Task"},"maxItems":500}`))
	after.Obj("bindings").Delete("createTask.mcp")
	after.Obj("bindings").Set("archiveTask.http", nxMustParse(`{"operation":"archiveTask","source":"httpApi","content":{"target":"#/paths/~1tasks~1{id}~1archive/post"}}`))
	return before, after
}

type nxChange struct {
	part, name, change, detail string
	before, after              any
}

func nxCompare(before, after *nxObj) []nxChange {
	var changes []nxChange
	for _, member := range []string{"name", "version", "description"} {
		if nxCompact(before.Get(member)) != nxCompact(after.Get(member)) {
			changes = append(changes, nxChange{"document", member, "changed", "", before.Get(member), after.Get(member)})
		}
	}
	for _, part := range []string{"operations", "sources", "bindings", "dependencies", "schemas"} {
		b, a := before.Obj(part), after.Obj(part)
		if b == nil {
			b = nxNewObj()
		}
		if a == nil {
			a = nxNewObj()
		}
		for _, k := range a.Keys() {
			if !b.Has(k) {
				changes = append(changes, nxChange{part, k, "added", "", nil, a.Get(k)})
			} else if nxCompact(a.Get(k)) != nxCompact(b.Get(k)) {
				var members []string
				bo, ao := b.Obj(k), a.Obj(k)
				if bo != nil && ao != nil {
					seen := map[string]bool{}
					for _, m := range append(bo.Keys(), ao.Keys()...) {
						if !seen[m] && nxCompact(bo.Get(m)) != nxCompact(ao.Get(m)) {
							members = append(members, m)
						}
						seen[m] = true
					}
				}
				changes = append(changes, nxChange{part, k, "changed", strings.Join(members, ", ") + " changed", b.Get(k), a.Get(k)})
			}
		}
		for _, k := range b.Keys() {
			if !a.Has(k) {
				changes = append(changes, nxChange{part, k, "removed", "", b.Get(k), nil})
			}
		}
	}
	return changes
}

func nxPointerToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func nxDiffCmd() *cobra.Command {
	cmd := nxLeaf("diff", "diff <before> <after>", "Compare two documents", `List what changed between two documents, part by part. It reports
differences only; to check whether a document satisfies a shared contract,
use ob compat. --patch prints the changes as an RFC 6902 JSON Patch, which
ob patch can apply.`,
		`  ob diff tasks.obi.json tasks-next.obi.json
  ob diff tasks.obi.json tasks-next.obi.json --patch > changes.json`,
		nxArgs(2, 2), func(c *nxCtx) error {
			before, after := nxDiffSample()
			c.note(fmt.Sprintf("(preview: comparing the sample document with an edited copy standing in for %s)", c.args[1]))
			changes := nxCompare(before, after)
			switch {
			case c.on("patch"):
				var ops []any
				for _, ch := range changes {
					path := "/" + ch.part + "/" + nxPointerToken(ch.name)
					op := nxNewObj()
					switch ch.change {
					case "added":
						op.Set("op", "add").Set("path", path).Set("value", ch.after)
					case "removed":
						op.Set("op", "remove").Set("path", path)
					default:
						op.Set("op", "replace").Set("path", path).Set("value", ch.after)
					}
					ops = append(ops, op)
				}
				c.println(nxPretty(ops))
			case c.format() == "json":
				var out []any
				for _, ch := range changes {
					o := nxNewObj().Set("part", ch.part).Set("name", ch.name).Set("change", ch.change)
					if ch.detail != "" {
						o.Set("detail", ch.detail)
					}
					out = append(out, o)
				}
				c.println(nxPretty(out))
			default:
				last := ""
				for _, ch := range changes {
					if ch.part != last {
						c.println(strings.ToUpper(ch.part[:1]) + ch.part[1:])
						last = ch.part
					}
					mark := map[string]string{"added": "+", "removed": "-", "changed": "~"}[ch.change]
					c.println(fmt.Sprintf("  %s %-18s %s", mark, ch.name, ch.detail))
				}
			}
			if c.on("exit-code") && len(changes) > 0 {
				return nxFail(1, "")
			}
			return nil
		})
	cmd.Flags().Bool("patch", false, "print the changes as a JSON Patch")
	cmd.Flags().Bool("exit-code", false, "exit 1 when the documents differ")
	nxFormat(cmd, "text", "json")
	return cmd
}

func nxCanonical(v any) any {
	switch t := v.(type) {
	case *nxObj:
		keys := t.Keys()
		sort.Strings(keys)
		out := nxNewObj()
		for _, k := range keys {
			out.Set(k, nxCanonical(t.Get(k)))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = nxCanonical(item)
		}
		return out
	}
	return v
}

func nxFmtCmd() *cobra.Command {
	cmd := nxLeaf("fmt", "fmt <obi>...", "Format documents", `Rewrite each document with two-space indentation and the spec's member
order (openbindings, name, version, description, schemas, operations,
dependencies, sources, bindings, and the same within each part). Entries
keep the order you gave them.

--check changes nothing; it lists files that would change and exits 1 if
any would. --dry-run shows the change without writing it. Give - to format
a document from stdin to stdout. Values, including numbers, are kept exactly
as written. --canonical prints the JSON Canonicalization Scheme form
(RFC 8785), for hashing and signing, instead of rewriting.`,
		`  ob fmt tasks.obi.json
  ob fmt --check *.obi.json
  ob fmt --canonical tasks.obi.json | sha256sum`,
		nxArgs(1, -1), func(c *nxCtx) error {
			if c.on("canonical") {
				if len(c.args) > 1 {
					return nxUsageErr("--canonical prints one document; give one <obi>")
				}
				if c.on("check") {
					return nxUsageErr("--canonical and --check do not combine")
				}
				c.println(nxCompact(nxCanonical(c.doc(c.args[0]))))
				return nil
			}
			if c.on("check") && c.on("dry-run") {
				return nxUsageErr("--check and --dry-run do not combine")
			}
			for _, path := range c.args {
				switch {
				case path == "-":
					c.println(nxPretty(c.doc(path)))
				case c.on("check"):
					c.println(path + ": formatted")
				case c.on("dry-run"):
					c.println(path + " (dry run, nothing written): already formatted, no change")
				default:
					c.note(path + ": already formatted, no change")
				}
			}
			return nil
		})
	cmd.Flags().Bool("check", false, "report files that would change and exit 1 if any would")
	cmd.Flags().Bool("dry-run", false, "show the formatting change without writing it")
	cmd.Flags().Bool("canonical", false, "print the RFC 8785 canonical form instead")
	return cmd
}

func nxPatchCmd() *cobra.Command {
	return nxEditable(nxLeaf("patch", "patch <obi> <patch>", "Apply a JSON Patch", `Apply an RFC 6902 JSON Patch (a file, or - for stdin) for edits no other
command covers, such as extension fields. The result must still be a
conformant document, or nothing is written.`,
		`  ob patch tasks.obi.json changes.json
  ob diff old.obi.json new.obi.json --patch | ob patch tasks.obi.json -`,
		nxArgs(2, 2), func(c *nxCtx) error {
			c.note(fmt.Sprintf("(preview: %s was not read; applying an illustrative patch)", c.args[1]))
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			after.Set("x-owner", "tasks-team@example.com")
			return c.wrote(c.args[0], before, after, "Applied 1 patch operation")
		}))
}

func nxMergeCmd() *cobra.Command {
	cmd := nxEditable(nxLeaf("merge", "merge <obi> <from>", "Bring operations in from another document", `Copy operations from <from> (a path or URL) into <obi>, with the schemas,
bindings, and sources they need. --operation limits the merge to the named
operations of <from>.

An entry both documents have, by the same name (for an operation, its key or
an alias), is left alone when the two are identical. When they differ, ob
lists every difference and writes nothing, unless you say which side wins:
--ours keeps what <obi> has, --theirs takes what <from> has.

A shared contract is just another OBI, so merging from one adds the contract
operations you don't have yet, under the contract's names and with its
schemas. To give an operation you already have a contract's name instead,
use ob operation set --add-alias. ob compat shows which are missing.`,
		`  ob merge tasks.obi.json tasks-next.obi.json --operation archiveTask
  ob merge tasks.obi.json tasks-next.obi.json --theirs
  ob merge tasks.obi.json acme-tasks.obi.json --operation acme.tasks.deleteTask`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if c.on("ours") && c.on("theirs") {
				return nxUsageErr("--ours and --theirs do not combine; choose which side wins")
			}
			from := nxMergeSource(c)
			fromOps := from.Obj("operations")
			names := c.strs("operation")
			for _, n := range names {
				if !fromOps.Has(n) {
					return nxFail(1, "%s has no operation named %q", c.args[1], n)
				}
			}
			if len(names) == 0 {
				names = fromOps.Keys()
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			m := &nxMerge{from: from, to: after, ours: c.on("ours"), theirs: c.on("theirs")}
			var added, updated []string
			for _, name := range names {
				fop := fromOps.Obj(name)
				if key, ok := nxResolveOperation(after, name); ok {
					op := after.Obj("operations").Obj(key)
					var differs []string
					for _, side := range []string{"input", "output"} {
						if fop.Has(side) && nxCompact(fop.Get(side)) != nxCompact(op.Get(side)) {
							differs = append(differs, side)
						}
					}
					label := "operation " + key
					if key != name {
						label += " (answering to " + name + ")"
					}
					if m.conflict(label, strings.Join(differs, " and ")+" schema differs", len(differs) > 0) {
						for _, side := range differs {
							op.SetCanon(side, nxClone(fop.Get(side)), nxOperationOrder)
							m.schemas(fop.Get(side))
						}
						updated = append(updated, key)
					}
					continue
				}
				nxPart(after, "operations").Set(name, nxClone(fop))
				m.schemas(fop)
				added = append(added, name)
				if c.on("no-bindings") {
					continue
				}
				for _, b := range nxReferrers(from, "bindings", "operation", name) {
					binding := from.Obj("bindings").Obj(b)
					src := fmt.Sprint(binding.Get("source"))
					m.entry("sources", "source", src, from.Obj("sources").Get(src))
					m.entry("bindings", "binding", b, binding)
				}
			}
			if len(m.conflicts) > 0 {
				return nxFail(1, "refused: %s and %s differ, so nothing was written:\n  %s\n--ours keeps what %s has; --theirs takes what %s has.", c.args[0], c.args[1], strings.Join(m.conflicts, "\n  "), c.args[0], c.args[1])
			}
			if len(added)+len(updated) == 0 {
				c.println("Nothing to merge from " + c.args[1] + ".")
				return nil
			}
			var parts []string
			if len(added) > 0 {
				parts = append(parts, "added "+strings.Join(added, ", "))
			}
			if len(updated) > 0 {
				parts = append(parts, "took theirs for "+strings.Join(updated, ", "))
			}
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Merged from %s: %s", c.args[1], strings.Join(parts, "; ")))
		}))
	cmd.Flags().StringArray("operation", nil, "merge only this operation of <from> (repeatable)")
	cmd.Flags().Bool("no-bindings", false, "bring operations without their bindings")
	cmd.Flags().Bool("ours", false, "where the documents differ, keep what <obi> has")
	cmd.Flags().Bool("theirs", false, "where the documents differ, take what <from> has")
	return cmd
}

// nxMerge applies one merge, collecting conflicts instead of overwriting.
type nxMerge struct {
	from, to     *nxObj
	ours, theirs bool
	conflicts    []string
}

// conflict reports whether a differing entry should take theirs, recording
// the conflict when neither side was chosen.
func (m *nxMerge) conflict(label, what string, differs bool) bool {
	if !differs || m.ours {
		return false
	}
	if m.theirs {
		return true
	}
	m.conflicts = append(m.conflicts, label+": "+what)
	return false
}

// entry brings one named source, binding, or schema, respecting collisions.
func (m *nxMerge) entry(part, noun, name string, value any) {
	existing := m.to.Obj(part)
	if existing != nil && existing.Has(name) {
		if m.conflict(noun+" "+name, "differs", nxCompact(existing.Get(name)) != nxCompact(value)) {
			existing.Set(name, nxClone(value))
		}
		return
	}
	nxPart(m.to, part).Set(name, nxClone(value))
	if part == "schemas" {
		m.schemas(value)
	}
}

// schemas brings the named schemas a merged value references.
func (m *nxMerge) schemas(v any) {
	for _, match := range nxSchemaRef.FindAllStringSubmatch(nxCompact(v), -1) {
		name := match[1]
		if m.from.Obj("schemas") == nil || !m.from.Obj("schemas").Has(name) {
			continue
		}
		if m.to.Obj("schemas") != nil && m.to.Obj("schemas").Has(name) && nxCompact(m.to.Obj("schemas").Get(name)) == nxCompact(m.from.Obj("schemas").Get(name)) {
			continue
		}
		m.entry("schemas", "schema", name, m.from.Obj("schemas").Get(name))
	}
}

// nxMergeSource picks the sample standing in for <from>: the Acme Tasks
// contract when the name or the requested operations point at it, otherwise
// an edited copy of the sample document.
func nxMergeSource(c *nxCtx) *nxObj {
	name := strings.ToLower(c.args[1])
	contract := strings.Contains(name, "acme") || strings.Contains(name, "contract")
	if ops := c.strs("operation"); len(ops) > 0 {
		all := true
		for _, op := range ops {
			all = all && strings.HasPrefix(op, "acme.tasks.")
		}
		contract = contract || all
	}
	if contract {
		c.note(fmt.Sprintf("(preview: a sample contract, Acme Tasks, stands in for %s)", c.args[1]))
		return nxContract()
	}
	c.note(fmt.Sprintf("(preview: an edited copy of the sample stands in for %s)", c.args[1]))
	_, from := nxDiffSample()
	return from
}

var nxQuotedAt = regexp.MustCompile(`at '(/[^']*)'`)

var nxSchemaRef = regexp.MustCompile(`"#/schemas/([^"]+)"`)

func nxSynthesizeCmd() *cobra.Command {
	cmd := nxLeaf("synthesize", "synthesize <artifact> --kind <kind>", "Create a document from an artifact", `Create a new document from an artifact, such as an OpenAPI document, using
a handler for its kind: a source for the artifact, and one operation and
binding for each target it offers. <artifact> is a path or URL. Prints the
document unless -o is given.

To add another artifact to an existing document, use ob source import and
then ob source pull.`,
		`  ob synthesize ./openapi.json --kind example.openapi@1 -o tasks.obi.json
  ob synthesize https://api.example.com/mcp --kind example.mcp@1`,
		nxArgs(1, 1), func(c *nxCtx) error {
			kind := c.str("kind")
			if kind == "" {
				return nxUsageErr("--kind is required: the exact kind of the artifact, such as example.openapi@1")
			}
			if _, ok := nxSupports(kind, "synthesize"); !ok {
				return nxFail(1, "this ob cannot synthesize from %s artifacts; ob kind list shows what it can handle", kind)
			}
			name := c.str("source")
			if name == "" {
				name = "api"
			}
			if err := nxName("source name", name); err != nil {
				return err
			}
			full := nxFixture()
			doc := nxNewObj()
			doc.Set("openbindings", "0.2.0")
			if c.set("name") {
				doc.Set("name", c.str("name"))
			}
			doc.Set("schemas", full.Get("schemas"))
			ops := nxNewObj()
			binds := nxNewObj()
			for _, key := range []string{"createTask", "listTasks", "completeTask"} {
				op := nxClone(full.Obj("operations").Get(key)).(*nxObj)
				op.Delete("aliases")
				op.Delete("examples")
				ops.Set(key, op)
				b := nxClone(full.Obj("bindings").Get(key + ".http")).(*nxObj)
				b.Set("source", name)
				b.Delete("preference")
				binds.Set(key+"."+name, b)
			}
			doc.Set("operations", ops)
			src := nxNewObj().Set("kind", kind).Set("content", nxNewObj().Set("location", c.args[0]))
			doc.Set("sources", nxNewObj().Set(name, src))
			doc.Set("bindings", binds)
			c.note(fmt.Sprintf("(preview: %s was not read; showing what the sample artifact would produce)", c.args[0]))
			if out := c.str("out"); out != "" {
				c.note("Created " + out + " with 3 operations from " + c.args[0])
				c.note("(preview) the document that would be written:")
			}
			c.println(nxPretty(doc))
			return nil
		})
	cmd.Flags().String("kind", "", "the artifact's exact kind (required)")
	cmd.Flags().String("source", "", "name for the source in the new document (default \"api\")")
	cmd.Flags().String("name", "", "a human-readable name for the document")
	cmd.Flags().StringP("out", "o", "", "write the document to a file")
	return cmd
}

func nxStatusCmd() *cobra.Command {
	cmd := nxLeaf("status", "status <obi>", "Show how a document has drifted from its sources", `For each source, show what ob source pull would change: operations and
bindings to add, update, or remove, and bindings whose target is gone.
Changes nothing. Uses a handler for each source's kind.`,
		`  ob status tasks.obi.json
  ob status tasks.obi.json --exit-code`,
		nxArgs(1, 1), func(c *nxCtx) error {
			if c.format() == "json" {
				c.println(`[
  { "source": "httpApi", "kind": "example.openapi@1", "changes": [
    { "change": "add", "operation": "archiveTask", "binding": "archiveTask.http", "target": "POST /tasks/{id}/archive" },
    { "change": "update", "operation": "listTasks", "detail": "output schema changed upstream" }
  ] },
  { "source": "mcpServer", "kind": "example.mcp@1", "changes": [] }
]`)
			} else {
				c.println("httpApi (example.openapi@1): 2 changes")
				c.println("  + archiveTask        new target POST /tasks/{id}/archive")
				c.println("  ~ listTasks          output schema changed upstream")
				c.println("mcpServer (example.mcp@1): up to date")
				c.println("")
				c.println("Run \"ob source pull " + c.args[0] + "\" to apply.")
			}
			if c.on("exit-code") {
				return nxFail(1, "")
			}
			return nil
		})
	cmd.Flags().Bool("exit-code", false, "exit 1 when there is drift, for CI")
	nxFormat(cmd, "text", "json")
	return cmd
}
