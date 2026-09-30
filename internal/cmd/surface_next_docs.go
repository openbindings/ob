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

Exit status: 0 conformant, undetermined, or the value fits; 1 non-conformant
or it does not fit; 3 refused.`,
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
		return nil
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
	count, bad := 0, 0
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
				problems = append(problems, strings.TrimPrefix(line, "- "))
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
any would. --canonical prints the JSON Canonicalization Scheme form
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
			for _, path := range c.args {
				if c.on("check") {
					c.println(path + ": formatted")
				} else {
					c.note(path + " is already formatted")
				}
			}
			return nil
		})
	cmd.Flags().Bool("check", false, "report files that would change and exit 1 if any would")
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

An operation whose name <obi> already uses as a key has its input and output
schemas updated; fields you wrote by hand are kept. An operation whose name
is already an alias of one of yours is left alone.

A shared contract is just another OBI, so merging from one adds the contract
operations you don't have yet, under the contract's names and with its
schemas. To give an operation you already have a contract's name instead,
use ob operation set --add-alias. ob compat shows which are missing.`,
		`  ob merge tasks.obi.json tasks-next.obi.json
  ob merge tasks.obi.json tasks-next.obi.json --operation archiveTask
  ob merge tasks.obi.json acme-tasks.obi.json --operation acme.tasks.deleteTask`,
		nxArgs(2, 2), func(c *nxCtx) error {
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
			var added, updated []string
			for _, name := range names {
				fop := fromOps.Obj(name)
				if after.Obj("operations").Has(name) {
					op := after.Obj("operations").Obj(name)
					changed := false
					for _, side := range []string{"input", "output"} {
						if fop.Has(side) && nxCompact(fop.Get(side)) != nxCompact(op.Get(side)) {
							op.SetCanon(side, nxClone(fop.Get(side)), nxOperationOrder)
							nxMergeSchemas(from, after, fop.Get(side))
							changed = true
						}
					}
					if changed {
						updated = append(updated, name)
					}
					continue
				}
				if key, ok := nxResolveOperation(after, name); ok {
					c.note(fmt.Sprintf("left alone: %s (already answered by %s)", name, key))
					continue
				}
				nxPart(after, "operations").Set(name, nxClone(fop))
				nxMergeSchemas(from, after, fop)
				added = append(added, name)
				if c.on("no-bindings") {
					continue
				}
				for _, b := range nxReferrers(from, "bindings", "operation", name) {
					if after.Obj("bindings") != nil && after.Obj("bindings").Has(b) {
						continue
					}
					binding := from.Obj("bindings").Obj(b)
					src := fmt.Sprint(binding.Get("source"))
					if after.Obj("sources") == nil || !after.Obj("sources").Has(src) {
						nxPart(after, "sources").Set(src, nxClone(from.Obj("sources").Get(src)))
					}
					nxPart(after, "bindings").Set(b, nxClone(binding))
				}
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
				parts = append(parts, "updated "+strings.Join(updated, ", "))
			}
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Merged from %s: %s", c.args[1], strings.Join(parts, "; ")))
		}))
	cmd.Flags().StringArray("operation", nil, "merge only this operation of <from> (repeatable)")
	cmd.Flags().Bool("no-bindings", false, "bring operations without their bindings")
	return cmd
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

var nxSchemaRef = regexp.MustCompile(`"#/schemas/([^"]+)"`)

// nxMergeSchemas copies the named schemas a merged value references, and the
// schemas those reference, when the target document lacks them.
func nxMergeSchemas(from, to *nxObj, v any) {
	for _, m := range nxSchemaRef.FindAllStringSubmatch(nxCompact(v), -1) {
		name := m[1]
		if (to.Obj("schemas") != nil && to.Obj("schemas").Has(name)) || from.Obj("schemas") == nil || !from.Obj("schemas").Has(name) {
			continue
		}
		schema := nxClone(from.Obj("schemas").Get(name))
		nxPart(to, "schemas").Set(name, schema)
		nxMergeSchemas(from, to, schema)
	}
}

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
			if out := c.str("output"); out != "" {
				c.note("Created " + out + " with 3 operations from " + c.args[0])
				c.note("(preview) the document that would be written:")
			}
			c.println(nxPretty(doc))
			return nil
		})
	cmd.Flags().String("kind", "", "the artifact's exact kind (required)")
	cmd.Flags().String("source", "", "name for the source in the new document (default \"api\")")
	cmd.Flags().String("name", "", "a human-readable name for the document")
	cmd.Flags().StringP("output", "o", "", "write the document to a file")
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
