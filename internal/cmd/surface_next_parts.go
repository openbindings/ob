package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Shared pieces for the five part nouns.

func (c *nxCtx) opKey(doc *nxObj, name string) (string, error) {
	key, ok := nxResolveOperation(doc, name)
	if !ok {
		return "", nxFail(1, "no operation named %q in %s", name, c.args[0])
	}
	if key != name {
		c.note(fmt.Sprintf("(%s is an alias of operation %s)", name, key))
	}
	return key, nil
}

func (c *nxCtx) entry(doc *nxObj, part, what, name string) (*nxObj, error) {
	if m := doc.Obj(part); m != nil && m.Has(name) {
		return m.Obj(name), nil
	}
	return nil, nxFail(1, "no %s named %q in %s", what, name, c.args[0])
}

func (c *nxCtx) unset(obj *nxObj, allowed ...string) error {
	for _, member := range c.strs("unset") {
		ok := false
		for _, a := range allowed {
			ok = ok || a == member
		}
		if !ok {
			return nxUsageErr("--unset takes one of: %s", strings.Join(allowed, ", "))
		}
		obj.Delete(member)
	}
	return nil
}

func nxNeedsChange(c *nxCtx) error {
	changed := false
	c.cmd.Flags().Visit(func(f *pflagFlag) {
		if f.Name != "dry-run" && f.Name != "output" {
			changed = true
		}
	})
	if !changed {
		return nxUsageErr("nothing to change; give at least one flag (see %s --help)", c.cmd.CommandPath())
	}
	return nil
}

func nxListFormats(cmd *cobra.Command) *cobra.Command {
	nxFormat(cmd, "text", "json", "yaml")
	return cmd
}

func (c *nxCtx) render(v any, text func()) {
	switch c.format() {
	case "json":
		c.println(nxPretty(v))
	case "yaml":
		c.println(nxYAML(v))
	default:
		text()
	}
}

func nxPartGroup(noun, short, long string, children ...*cobra.Command) *cobra.Command {
	return nxGroupCmd(noun, short, long, children...)
}

func nxRenameCmd(noun, part, id, long, example string, apply func(doc *nxObj, from, to string) string) *cobra.Command {
	return nxEditable(nxLeaf(id, "rename <obi> <name> <new-name>", "Rename a "+noun+long, "", example, nxArgs(3, 3), func(c *nxCtx) error {
		before := c.doc(c.args[0])
		after := nxClone(before).(*nxObj)
		from, to := c.args[1], c.args[2]
		if err := nxName(noun+" name", to); err != nil {
			return err
		}
		if _, err := c.entry(after, part, noun, from); err != nil {
			return err
		}
		if after.Obj(part).Has(to) {
			return nxFail(1, "%s %q already exists in %s", noun, to, c.args[0])
		}
		if part == "operations" {
			if other, taken := nxResolveOperation(after, to); taken {
				return nxFail(1, "%q is already an alias of operation %s", to, other)
			}
		}
		after.Obj(part).Rename(from, to)
		extra := apply(after, from, to)
		return c.wrote(c.args[0], before, after, fmt.Sprintf("Renamed %s %s to %s%s", noun, from, to, extra))
	}))
}

// ---------------------------------------------------------------- operation

func nxOperationCmd() *cobra.Command {
	add := nxEditable(nxLeaf("operation.add", "add <obi> <name>", "Add an operation", `Add an operation: a protocol-independent contract, with an optional schema
for each input value and each output value. Leaving a schema out leaves that
side unspecified. Aliases are other names the operation answers to, such as a
shared contract's name for it.`,
		`  ob operation add tasks.obi.json archiveTask --description "Archive a task." \
      --input-schema '{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}' \
      --output-schema '{"$ref":"#/schemas/Task"}'`,
		nxArgs(2, 2), func(c *nxCtx) error {
			name := c.args[1]
			if err := nxName("operation name", name); err != nil {
				return err
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if key, ok := nxResolveOperation(after, name); ok {
				if key == name {
					return nxFail(1, "operation %q already exists in %s; use ob operation set to change it", name, c.args[0])
				}
				return nxFail(1, "%q is already an alias of operation %s", name, key)
			}
			op := nxNewObj()
			if err := nxOperationFields(c, after, op, name); err != nil {
				return err
			}
			nxPart(after, "operations").Set(name, op)
			return c.wrote(c.args[0], before, after, "Added operation "+name)
		}))
	add.Flags().String("description", "", "what the operation does")
	add.Flags().String("input-schema", "", "schema for each input value: JSON, @file, or -")
	add.Flags().String("output-schema", "", "schema for each output value: JSON, @file, or -")
	add.Flags().StringArray("alias", nil, "another name the operation answers to (repeatable)")
	add.Flags().StringArray("tag", nil, "a documentation tag (repeatable)")
	add.Flags().Bool("deprecated", false, "mark the operation deprecated")

	set := nxEditable(nxLeaf("operation.set", "set <obi> <name>", "Change an operation", `Change an operation's description, schemas, aliases, tags, or deprecation.
Only the flags you give change anything. --unset removes a member, which
for a schema leaves that side unspecified.`,
		`  ob operation set tasks.obi.json createTask --add-alias acme.tasks.addTask
  ob operation set tasks.obi.json listTasks --deprecated
  ob operation set tasks.obi.json completeTask --unset output`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			key, err := c.opKey(after, c.args[1])
			if err != nil {
				return err
			}
			op := after.Obj("operations").Obj(key)
			if err := nxOperationFields(c, after, op, key); err != nil {
				return err
			}
			if err := c.unset(op, "description", "deprecated", "tags", "aliases", "input", "output", "examples"); err != nil {
				return err
			}
			return c.wrote(c.args[0], before, after, "Changed operation "+key)
		}))
	set.Flags().String("description", "", "what the operation does")
	set.Flags().String("input-schema", "", "schema for each input value: JSON, @file, or -")
	set.Flags().String("output-schema", "", "schema for each output value: JSON, @file, or -")
	set.Flags().StringArray("add-alias", nil, "add an alias (repeatable)")
	set.Flags().StringArray("remove-alias", nil, "remove an alias (repeatable)")
	set.Flags().StringArray("add-tag", nil, "add a tag (repeatable)")
	set.Flags().StringArray("remove-tag", nil, "remove a tag (repeatable)")
	set.Flags().Bool("deprecated", false, "mark deprecated; --deprecated=false marks it current")
	set.Flags().StringArray("unset", nil, "remove a member: description, deprecated, tags, aliases, input, output, examples")

	rename := nxRenameCmd("operation", "operations", "operation.rename", ` and update its bindings and dependencies`,
		`  ob operation rename tasks.obi.json createTask addTask
  ob operation rename tasks.obi.json createTask addTask --keep-alias`,
		func(doc *nxObj, from, to string) string {
			n := 0
			for _, part := range []string{"bindings", "dependencies"} {
				for _, k := range nxReferrers(doc, part, "operation", from) {
					doc.Obj(part).Obj(k).Set("operation", to)
					n++
				}
			}
			return fmt.Sprintf(" (%s updated)", nxCount(n, "reference"))
		})
	rename.Long = `Rename an operation and update every binding and dependency that uses it.
--keep-alias keeps the old name as an alias, so anything that looks the
operation up by its old name still finds it.`
	rename.Flags().Bool("keep-alias", false, "keep the old name as an alias")
	baseRun := rename.RunE
	rename.RunE = func(cmd *cobra.Command, args []string) error {
		keep, _ := cmd.Flags().GetBool("keep-alias")
		if !keep {
			return baseRun(cmd, args)
		}
		return nxRun(cmd, args, func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			from, to := c.args[1], c.args[2]
			if err := nxName("operation name", to); err != nil {
				return err
			}
			if _, err := c.entry(after, "operations", "operation", from); err != nil {
				return err
			}
			if _, taken := nxResolveOperation(after, to); taken {
				return nxFail(1, "%q is already an operation name or alias in %s", to, c.args[0])
			}
			ops := after.Obj("operations")
			ops.Rename(from, to)
			op := ops.Obj(to)
			op.SetCanon("aliases", append(nxToAny(nxStrings(op.Get("aliases"))), from), nxOperationOrder)
			n := 0
			for _, part := range []string{"bindings", "dependencies"} {
				for _, k := range nxReferrers(after, part, "operation", from) {
					after.Obj(part).Obj(k).Set("operation", to)
					n++
				}
			}
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Renamed operation %s to %s, keeping %s as an alias (%s updated)", from, to, from, nxCount(n, "reference")))
		})
	}

	remove := nxEditable(nxLeaf("operation.remove", "remove <obi> <name>", "Remove an operation", `Remove an operation. It refuses while bindings or dependencies use the
operation; --cascade removes those too.`,
		`  ob operation remove tasks.obi.json completeTask --cascade`,
		nxArgs(2, 2), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			key, err := c.opKey(after, c.args[1])
			if err != nil {
				return err
			}
			users := append(nxReferrers(after, "bindings", "operation", key), nxReferrers(after, "dependencies", "operation", key)...)
			if len(users) > 0 && !c.on("cascade") {
				return nxFail(1, "operation %s is used by %s; remove those first, or use --cascade", key, strings.Join(users, ", "))
			}
			for _, part := range []string{"bindings", "dependencies"} {
				for _, k := range nxReferrers(after, part, "operation", key) {
					after.Obj(part).Delete(k)
				}
			}
			after.Obj("operations").Delete(key)
			summary := "Removed operation " + key
			if len(users) > 0 {
				summary += " and " + strings.Join(users, ", ")
			}
			return c.wrote(c.args[0], before, after, summary)
		}))
	remove.Flags().Bool("cascade", false, "also remove the bindings and dependencies that use it")

	list := nxListFormats(nxLeaf("operation.list", "list <obi>", "List operations", "List operations with their aliases and how each is realized or consumed.",
		`  ob operation list tasks.obi.json`, nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			var rows [][]string
			var out []any
			for _, key := range nxPartKeys(doc, "operations") {
				op := doc.Obj("operations").Obj(key)
				aliases := nxStrings(op.Get("aliases"))
				binds := nxReferrers(doc, "bindings", "operation", key)
				deps := nxReferrers(doc, "dependencies", "operation", key)
				rows = append(rows, []string{key, nxDash(strings.Join(aliases, ", ")), nxDash(strings.Join(binds, ", ")), nxDash(strings.Join(deps, ", "))})
				out = append(out, nxNewObj().Set("name", key).Set("aliases", nxToAny(aliases)).Set("bindings", nxToAny(binds)).Set("dependencies", nxToAny(deps)))
			}
			c.render(out, func() { c.table("NAME\tALIASES\tBINDINGS\tDEPENDENCIES", rows) })
			return nil
		}))

	show := nxListFormats(nxLeaf("operation.show", "show <obi> <name>", "Show an operation", `Show an operation, looked up by name or alias, with the bindings that
realize it. -F json prints the exact stored operation.`,
		`  ob operation show tasks.obi.json createTask
  ob operation show tasks.obi.json acme.tasks.createTask -F json`,
		nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			key, err := c.opKey(doc, c.args[1])
			if err != nil {
				return err
			}
			op := doc.Obj("operations").Obj(key)
			c.render(op, func() {
				c.println(key)
				if d, _ := op.Get("description").(string); d != "" {
					c.println("  " + d)
				}
				if a := nxStrings(op.Get("aliases")); len(a) > 0 {
					c.println("  Also answers to: " + strings.Join(a, ", "))
				}
				for _, side := range []string{"input", "output"} {
					if op.Has(side) {
						c.printf("  %s:%s%s\n", strings.ToUpper(side[:1])+side[1:], strings.Repeat(" ", 8-len(side)), nxCompact(op.Get(side)))
					} else {
						c.printf("  %s:%sunspecified\n", strings.ToUpper(side[:1])+side[1:], strings.Repeat(" ", 8-len(side)))
					}
				}
				if ex := op.Obj("examples"); ex != nil {
					c.println("  Examples: " + strings.Join(ex.Keys(), ", "))
				}
				if d, _ := op.Get("deprecated").(bool); d {
					c.println("  Deprecated")
				}
				binds := nxReferrers(doc, "bindings", "operation", key)
				if len(binds) == 0 {
					c.println("  No bindings")
				} else {
					c.println("  Bindings:")
					var rows [][]string
					for _, b := range binds {
						bo := doc.Obj("bindings").Obj(b)
						src := fmt.Sprint(bo.Get("source"))
						kind := fmt.Sprint(doc.Obj("sources").Obj(src).Get("kind"))
						rows = append(rows, []string{"    " + b, src + " (" + kind + ")", nxBindingSummary(bo)[1]})
					}
					c.table("", rows)
				}
				if deps := nxReferrers(doc, "dependencies", "operation", key); len(deps) > 0 {
					c.println("  Consumed at: " + strings.Join(deps, ", "))
				}
			})
			return nil
		}))

	exampleAdd := nxEditable(nxLeaf("operation.example.add", "add <obi> <operation> <name>", "Add an example to an operation", `Add a named example: an input value, an output value, or both. ob checks
each value against the operation's schema and warns when it does not fit;
the schema always wins.`,
		`  ob operation example add tasks.obi.json listTasks empty --input '{}' --output '[]'`,
		nxArgs(3, 3), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			key, err := c.opKey(after, c.args[1])
			if err != nil {
				return err
			}
			if err := nxName("example name", c.args[2]); err != nil {
				return err
			}
			op := after.Obj("operations").Obj(key)
			examples := op.Obj("examples")
			if examples == nil {
				examples = nxNewObj()
				op.SetCanon("examples", examples, nxOperationOrder)
			}
			if examples.Has(c.args[2]) {
				return nxFail(1, "operation %s already has an example named %q", key, c.args[2])
			}
			ex := nxNewObj()
			if c.set("description") {
				ex.Set("description", c.str("description"))
			}
			for _, side := range []string{"input", "output"} {
				v, ok, err := c.value(side)
				if err != nil {
					return err
				}
				if ok {
					ex.Set(side, v)
					if problems, checked, err := nxCheck(after, key, side, v); err == nil && checked && len(problems) > 0 {
						c.note(fmt.Sprintf("warning: the example's %s does not fit %s's %s schema: %s", side, key, side, strings.Join(problems, "; ")))
					}
				}
			}
			if ex.Len() == 0 {
				return nxUsageErr("give the example an --input, an --output, or both")
			}
			examples.Set(c.args[2], ex)
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Added example %s to operation %s", c.args[2], key))
		}))
	exampleAdd.Flags().String("input", "", "the example's input value: JSON, @file, or -")
	exampleAdd.Flags().String("output", "", "the example's output value: JSON, @file, or -")
	exampleAdd.Flags().String("description", "", "what the example shows")
	exampleRemove := nxEditable(nxLeaf("operation.example.remove", "remove <obi> <operation> <name>", "Remove an example", "Remove a named example from an operation.",
		`  ob operation example remove tasks.obi.json createTask basic`, nxArgs(3, 3), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			key, err := c.opKey(after, c.args[1])
			if err != nil {
				return err
			}
			op := after.Obj("operations").Obj(key)
			if op.Obj("examples") == nil || !op.Obj("examples").Has(c.args[2]) {
				return nxFail(1, "operation %s has no example named %q", key, c.args[2])
			}
			op.Obj("examples").Delete(c.args[2])
			if op.Obj("examples").Len() == 0 {
				op.Delete("examples")
			}
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Removed example %s from operation %s", c.args[2], key))
		}))
	example := nxGroupCmd("example", "Add and remove operation examples", "", exampleAdd, exampleRemove)

	return nxPartGroup("operation", "Add, change, and remove operations", `An operation is a protocol-independent contract: a name, and optional
schemas for each input and output value. Bindings say how to carry it out;
dependencies say where it is called.`, add, set, rename, remove, list, show, example)
}

// nxOperationFields applies the add/set flags to an operation.
func nxOperationFields(c *nxCtx, doc, op *nxObj, key string) error {
	if c.set("description") {
		op.SetCanon("description", c.str("description"), nxOperationOrder)
	}
	if c.set("deprecated") {
		op.SetCanon("deprecated", c.on("deprecated"), nxOperationOrder)
	}
	for _, side := range []string{"input", "output"} {
		v, ok, err := c.schema(side + "-schema")
		if err != nil {
			return err
		}
		if ok {
			op.SetCanon(side, v, nxOperationOrder)
		}
	}
	aliases := nxStrings(op.Get("aliases"))
	for _, a := range append(c.strs("alias"), c.strs("add-alias")...) {
		if err := nxName("alias", a); err != nil {
			return err
		}
		if a == key {
			return nxUsageErr("%q is the operation's own name; an alias must be a different name", a)
		}
		if other, taken := nxResolveOperation(doc, a); taken && other != key {
			return nxFail(1, "%q is already a name of operation %s; names must be unique across operations and aliases", a, other)
		}
		aliases = nxAppendNew(aliases, a)
	}
	for _, a := range c.strs("remove-alias") {
		aliases = nxWithout(aliases, a)
	}
	if c.set("alias") || c.set("add-alias") || c.set("remove-alias") {
		if len(aliases) == 0 {
			op.Delete("aliases")
		} else {
			op.SetCanon("aliases", nxToAny(aliases), nxOperationOrder)
		}
	}
	tags := nxStrings(op.Get("tags"))
	for _, t := range append(c.strs("tag"), c.strs("add-tag")...) {
		tags = nxAppendNew(tags, t)
	}
	for _, t := range c.strs("remove-tag") {
		tags = nxWithout(tags, t)
	}
	if c.set("tag") || c.set("add-tag") || c.set("remove-tag") {
		if len(tags) == 0 {
			op.Delete("tags")
		} else {
			op.SetCanon("tags", nxToAny(tags), nxOperationOrder)
		}
	}
	return nil
}

func nxAppendNew(list []string, v string) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	return append(list, v)
}

func nxWithout(list []string, v string) []string {
	var out []string
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

func nxDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ------------------------------------------------------------------- source

func nxSourceCmd() *cobra.Command {
	add := nxEditable(nxLeaf("source.add", "add <obi> <name> --kind <kind>", "Add a source", `Add a source: a kind, and optional content that the kind reads, such as an
artifact's address or the artifact itself. ob stores both exactly as given,
and does not need to support the kind to do so. --content null stores an
explicit null, which is not the same as leaving content out.`,
		`  ob source add tasks.obi.json httpApi --kind example.openapi@1 \
      --content '{"location":"https://api.example.com/openapi.json"}'`,
		nxArgs(2, 2), func(c *nxCtx) error {
			name := c.args[1]
			if err := nxName("source name", name); err != nil {
				return err
			}
			kind := c.str("kind")
			if kind == "" {
				return nxUsageErr("--kind is required: the source's exact kind, such as example.openapi@1")
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if after.Obj("sources") != nil && after.Obj("sources").Has(name) {
				return nxFail(1, "source %q already exists in %s; use ob source set to change it", name, c.args[0])
			}
			src := nxNewObj().Set("kind", kind)
			if err := nxSourceFields(c, src); err != nil {
				return err
			}
			nxPart(after, "sources").Set(name, src)
			nxKindNote(c, kind)
			return c.wrote(c.args[0], before, after, "Added source "+name)
		}))
	add.Flags().String("kind", "", "the source's exact kind (required)")
	add.Flags().String("content", "", "content the kind reads: JSON, @file, or -")
	add.Flags().String("description", "", "a human-readable description")

	set := nxEditable(nxLeaf("source.set", "set <obi> <name>", "Change a source", `Change a source's kind, content, or description. Only the flags you give
change anything; --unset removes content or the description. Changing the
kind changes how the source's bindings are read.`,
		`  ob source set tasks.obi.json httpApi --content '{"location":"https://api.example.com/v2/openapi.json"}'`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			src, err := c.entry(after, "sources", "source", c.args[1])
			if err != nil {
				return err
			}
			if c.set("kind") {
				if c.str("kind") == "" {
					return nxUsageErr("--kind must not be empty")
				}
				src.Set("kind", c.str("kind"))
				nxKindNote(c, c.str("kind"))
			}
			if err := nxSourceFields(c, src); err != nil {
				return err
			}
			if err := c.unset(src, "content", "description"); err != nil {
				return err
			}
			return c.wrote(c.args[0], before, after, "Changed source "+c.args[1])
		}))
	set.Flags().String("kind", "", "the source's exact kind")
	set.Flags().String("content", "", "content the kind reads: JSON, @file, or -")
	set.Flags().String("description", "", "a human-readable description")
	set.Flags().StringArray("unset", nil, "remove a member: content, description")

	rename := nxRenameCmd("source", "sources", "source.rename", " and update its bindings",
		`  ob source rename tasks.obi.json httpApi restApi`,
		func(doc *nxObj, from, to string) string {
			refs := nxReferrers(doc, "bindings", "source", from)
			for _, k := range refs {
				doc.Obj("bindings").Obj(k).Set("source", to)
			}
			return fmt.Sprintf(" (%s updated)", nxCount(len(refs), "binding"))
		})

	remove := nxEditable(nxLeaf("source.remove", "remove <obi> <name>", "Remove a source", `Remove a source. It refuses while bindings use the source; --cascade
removes those bindings too.`,
		`  ob source remove tasks.obi.json mcpServer --cascade`, nxArgs(2, 2), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if _, err := c.entry(after, "sources", "source", c.args[1]); err != nil {
				return err
			}
			users := nxReferrers(after, "bindings", "source", c.args[1])
			if len(users) > 0 && !c.on("cascade") {
				return nxFail(1, "source %s is used by %s; remove those first, or use --cascade", c.args[1], strings.Join(users, ", "))
			}
			for _, k := range users {
				after.Obj("bindings").Delete(k)
			}
			after.Obj("sources").Delete(c.args[1])
			summary := "Removed source " + c.args[1]
			if len(users) > 0 {
				summary += " and " + strings.Join(users, ", ")
			}
			return c.wrote(c.args[0], before, after, summary)
		}))
	remove.Flags().Bool("cascade", false, "also remove the bindings that use it")

	list := nxListFormats(nxLeaf("source.list", "list <obi>", "List sources", "List sources with their kinds, and whether this ob can invoke through them.",
		`  ob source list tasks.obi.json`, nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			var rows [][]string
			var out []any
			for _, key := range nxPartKeys(doc, "sources") {
				kind := fmt.Sprint(doc.Obj("sources").Obj(key).Get("kind"))
				_, can := nxSupports(kind, "invoke")
				rows = append(rows, []string{key, kind, nxCount(len(nxReferrers(doc, "bindings", "source", key)), "binding"), nxYesNo(can)})
				out = append(out, nxNewObj().Set("name", key).Set("kind", kind))
			}
			c.render(out, func() { c.table("NAME\tKIND\tBINDINGS\tCAN INVOKE", rows) })
			return nil
		}))

	show := nxListFormats(nxLeaf("source.show", "show <obi> <name>", "Show a source", `Show a source's kind, content, and the bindings that use it. -F json prints
the exact stored source; content is shown as stored, never interpreted.`,
		`  ob source show tasks.obi.json httpApi`, nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			src, err := c.entry(doc, "sources", "source", c.args[1])
			if err != nil {
				return err
			}
			c.render(src, func() {
				c.println(c.args[1])
				c.println("  Kind:     " + fmt.Sprint(src.Get("kind")))
				if src.Has("content") {
					c.println("  Content:  " + nxCompact(src.Get("content")))
				} else {
					c.println("  Content:  none")
				}
				if d, _ := src.Get("description").(string); d != "" {
					c.println("  " + d)
				}
				c.println("  Bindings: " + nxDash(strings.Join(nxReferrers(doc, "bindings", "source", c.args[1]), ", ")))
			})
			return nil
		}))

	importCmd := nxEditable(nxLeaf("source.import", "import <obi> <name> <artifact> --kind <kind>", "Add a source built from an artifact", `Add a source for an artifact (a path or URL), using a handler for its kind.
The handler decides what content to store, such as the artifact's address or
the artifact itself. To add operations and bindings from it, run ob source
pull afterwards.`,
		`  ob source import tasks.obi.json billing https://billing.example.com/openapi.json --kind example.openapi@1`,
		nxArgs(3, 3), func(c *nxCtx) error {
			name, artifact, kind := c.args[1], c.args[2], c.str("kind")
			if err := nxName("source name", name); err != nil {
				return err
			}
			if kind == "" {
				return nxUsageErr("--kind is required: the artifact's exact kind, such as example.openapi@1")
			}
			if _, ok := nxSupports(kind, "synthesize"); !ok {
				return nxFail(1, "this ob has no handler that can import %s artifacts; ob kind list shows what it can handle", kind)
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if after.Obj("sources").Has(name) {
				return nxFail(1, "source %q already exists in %s", name, c.args[0])
			}
			nxPart(after, "sources").Set(name, nxNewObj().Set("kind", kind).Set("content", nxNewObj().Set("location", artifact)))
			c.note(fmt.Sprintf("(preview: %s was not read; the %s handler would decide the content)", artifact, kind))
			return c.wrote(c.args[0], before, after, "Added source "+name+" from "+artifact)
		}))
	importCmd.Flags().String("kind", "", "the artifact's exact kind (required)")

	inspect := nxListFormats(nxLeaf("source.inspect", "inspect <obi> <name>", "List what a source offers", `List the targets a source offers, the things a binding could be written
for, using a handler for its kind. Each shows the operation name the
handler suggests and any binding that already uses it.`,
		`  ob source inspect tasks.obi.json httpApi`, nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			src, err := c.entry(doc, "sources", "source", c.args[1])
			if err != nil {
				return err
			}
			kind := fmt.Sprint(src.Get("kind"))
			if _, ok := nxSupports(kind, "inspect"); !ok {
				return nxFail(1, "this ob cannot inspect %s sources; ob kind list shows what it can handle", kind)
			}
			targets := [][]string{
				{"POST /tasks", "createTask", "createTask.http"},
				{"GET /tasks", "listTasks", "listTasks.http"},
				{"POST /tasks/{id}/complete", "completeTask", "completeTask.http"},
				{"POST /tasks/{id}/archive", "archiveTask", "-"},
				{"GET /health", "getHealth", "-"},
			}
			if c.args[1] == "mcpServer" {
				targets = [][]string{{"tools/create_task", "createTask", "createTask.mcp"}, {"tools/list_tasks", "listTasks", "-"}}
			}
			var out []any
			for _, t := range targets {
				out = append(out, nxNewObj().Set("sourceRef", t[0]).Set("operationKey", t[1]))
			}
			c.render(nxNewObj().Set("targets", out).Set("exhaustive", true), func() {
				c.println(fmt.Sprintf("%s (%s): %s, complete", c.args[1], kind, nxCount(len(targets), "target")))
				c.table("TARGET\tSUGGESTED OPERATION\tBOUND BY", targets)
			})
			return nil
		}))

	pull := nxEditable(nxLeaf("source.pull", "pull <obi> [<name>...]", "Update a document from its sources", `Update the document from its sources: add operations and bindings for new
targets, update changed ones, and report bindings whose target is gone. With
no names, pull every source. Uses a handler for each source's kind. ob status
shows the same changes without making them.`,
		`  ob source pull tasks.obi.json
  ob source pull tasks.obi.json httpApi --dry-run`,
		nxArgs(1, -1), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			for _, name := range c.args[1:] {
				if _, err := c.entry(after, "sources", "source", name); err != nil {
					return err
				}
			}
			_, next := nxDiffSample()
			if len(c.args) == 1 || nxContains(c.args[1:], "httpApi") {
				after.Obj("operations").Set("archiveTask", nxClone(next.Obj("operations").Get("archiveTask")))
				after.Obj("operations").Obj("listTasks").Set("output", nxClone(next.Obj("operations").Obj("listTasks").Get("output")))
				after.Obj("bindings").Set("archiveTask.http", nxClone(next.Obj("bindings").Get("archiveTask.http")))
				return c.wrote(c.args[0], before, after, "Pulled httpApi: 1 operation added, 1 updated")
			}
			c.note("mcpServer is up to date")
			return nil
		}))

	return nxPartGroup("source", "Add, change, and remove sources, and read their artifacts", `A source is a kind plus optional content the kind reads, such as an
artifact's address. Bindings realize operations through sources. import,
inspect, and pull use a handler for the source's kind.`,
		add, set, rename, remove, list, show, importCmd, inspect, pull)
}

func nxSourceFields(c *nxCtx, src *nxObj) error {
	v, ok, err := c.value("content")
	if err != nil {
		return err
	}
	if ok {
		src.SetCanon("content", v, nxSourceOrder)
	}
	if c.set("description") {
		src.SetCanon("description", c.str("description"), nxSourceOrder)
	}
	return nil
}

func nxKindNote(c *nxCtx, kind string) {
	for _, k := range nxInstalledKinds {
		if k.kind == kind {
			return
		}
	}
	c.note(fmt.Sprintf("note: this ob has no handler for %s; you can still write sources and bindings for it", kind))
}

func nxYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func nxContains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ binding

func nxBindingCmd() *cobra.Command {
	add := nxEditable(nxLeaf("binding.add", "add <obi> <name> --operation <operation> --source <source>", "Add a binding", `Add a binding: a way to carry out an operation through a source. --content
is whatever the source's kind needs, such as which endpoint or tool to call.
An operation can have several bindings, even through the same source.

--idempotent claims that repeating a call through this binding adds no
further effects; --idempotent=false claims it can; leaving it out claims
nothing. --preference ranks bindings of the same operation: ob invoke prefers
the highest.`,
		`  ob binding add tasks.obi.json completeTask.mcp --operation completeTask --source mcpServer \
      --content '{"target":"tools/complete_task"}' --idempotent`,
		nxArgs(2, 2), func(c *nxCtx) error {
			name := c.args[1]
			if err := nxName("binding name", name); err != nil {
				return err
			}
			if !c.set("operation") || !c.set("source") {
				return nxUsageErr("--operation and --source are required")
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if after.Obj("bindings") != nil && after.Obj("bindings").Has(name) {
				return nxFail(1, "binding %q already exists in %s; use ob binding set to change it", name, c.args[0])
			}
			b := nxNewObj()
			if err := nxBindingFields(c, after, b); err != nil {
				return err
			}
			nxPart(after, "bindings").Set(name, b)
			return c.wrote(c.args[0], before, after, "Added binding "+name)
		}))
	nxBindingFlags(add)

	set := nxEditable(nxLeaf("binding.set", "set <obi> <name>", "Change a binding", `Change a binding. Only the flags you give change anything; --unset removes
content, idempotent, preference, description, or deprecated.`,
		`  ob binding set tasks.obi.json createTask.mcp --preference 5
  ob binding set tasks.obi.json listTasks.http --unset idempotent`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			b, err := c.entry(after, "bindings", "binding", c.args[1])
			if err != nil {
				return err
			}
			if err := nxBindingFields(c, after, b); err != nil {
				return err
			}
			if err := c.unset(b, "content", "idempotent", "preference", "description", "deprecated"); err != nil {
				return err
			}
			return c.wrote(c.args[0], before, after, "Changed binding "+c.args[1])
		}))
	nxBindingFlags(set)
	set.Flags().StringArray("unset", nil, "remove a member: content, idempotent, preference, description, deprecated")

	rename := nxRenameCmd("binding", "bindings", "binding.rename", "", `  ob binding rename tasks.obi.json createTask.http createTask.rest`,
		func(*nxObj, string, string) string { return "" })

	remove := nxEditable(nxLeaf("binding.remove", "remove <obi> <name>", "Remove a binding", "Remove a binding. The operation and source stay.",
		`  ob binding remove tasks.obi.json createTask.mcp`, nxArgs(2, 2), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if _, err := c.entry(after, "bindings", "binding", c.args[1]); err != nil {
				return err
			}
			after.Obj("bindings").Delete(c.args[1])
			return c.wrote(c.args[0], before, after, "Removed binding "+c.args[1])
		}))

	list := nxListFormats(nxLeaf("binding.list", "list <obi>", "List bindings", "List bindings with their operation, source, and signals. --operation shows one operation's bindings.",
		`  ob binding list tasks.obi.json
  ob binding list tasks.obi.json --operation createTask`, nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			only := ""
			if c.set("operation") {
				key, err := c.opKey(doc, c.str("operation"))
				if err != nil {
					return err
				}
				only = key
			}
			var rows [][]string
			var out []any
			for _, key := range nxPartKeys(doc, "bindings") {
				b := doc.Obj("bindings").Obj(key)
				if only != "" && b.Get("operation") != only {
					continue
				}
				src := fmt.Sprint(b.Get("source"))
				kind := fmt.Sprint(doc.Obj("sources").Obj(src).Get("kind"))
				rows = append(rows, []string{key, fmt.Sprint(b.Get("operation")), src, kind, nxDash(nxBindingSummary(b)[1])})
				out = append(out, nxNewObj().Set("name", key).Set("operation", b.Get("operation")).Set("source", src).Set("kind", kind))
			}
			c.render(out, func() { c.table("NAME\tOPERATION\tSOURCE\tKIND\tSIGNALS", rows) })
			return nil
		}))
	list.Flags().String("operation", "", "only this operation's bindings (name or alias)")

	show := nxListFormats(nxLeaf("binding.show", "show <obi> <name>", "Show a binding", "Show a binding. -F json prints the exact stored binding; content is shown as stored.",
		`  ob binding show tasks.obi.json createTask.http`, nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			b, err := c.entry(doc, "bindings", "binding", c.args[1])
			if err != nil {
				return err
			}
			c.render(b, func() {
				src := fmt.Sprint(b.Get("source"))
				c.println(c.args[1])
				c.println("  Operation:  " + fmt.Sprint(b.Get("operation")))
				c.println("  Source:     " + src + " (" + fmt.Sprint(doc.Obj("sources").Obj(src).Get("kind")) + ")")
				if b.Has("content") {
					c.println("  Content:    " + nxCompact(b.Get("content")))
				} else {
					c.println("  Content:    none")
				}
				if s := nxBindingSummary(b)[1]; s != "" {
					c.println("  Signals:    " + s)
				}
				if d, _ := b.Get("description").(string); d != "" {
					c.println("  " + d)
				}
			})
			return nil
		}))

	return nxPartGroup("binding", "Add, change, and remove bindings", `A binding is a way to carry out an operation through a source. Its content
is read under the source's kind.`, add, set, rename, remove, list, show)
}

func nxBindingFlags(cmd *cobra.Command) {
	cmd.Flags().String("operation", "", "the operation it carries out (name or alias)")
	cmd.Flags().String("source", "", "the source it goes through")
	cmd.Flags().String("content", "", "what the source's kind needs to find the target: JSON, @file, or -")
	cmd.Flags().Bool("idempotent", false, "claim repeats add no further effects; --idempotent=false claims they can")
	cmd.Flags().String("preference", "", "an integer; ob invoke prefers the highest")
	cmd.Flags().String("description", "", "a human-readable description")
	cmd.Flags().Bool("deprecated", false, "mark deprecated; --deprecated=false marks it current")
}

func nxBindingFields(c *nxCtx, doc, b *nxObj) error {
	if c.set("operation") {
		key, err := c.opKey(doc, c.str("operation"))
		if err != nil {
			return err
		}
		b.SetCanon("operation", key, nxBindingOrder)
	}
	if c.set("source") {
		if _, err := c.entry(doc, "sources", "source", c.str("source")); err != nil {
			return err
		}
		b.SetCanon("source", c.str("source"), nxBindingOrder)
	}
	v, ok, err := c.value("content")
	if err != nil {
		return err
	}
	if ok {
		b.SetCanon("content", v, nxBindingOrder)
	}
	if c.set("idempotent") {
		b.SetCanon("idempotent", c.on("idempotent"), nxBindingOrder)
	}
	if c.set("preference") {
		n, err := strconv.ParseInt(c.str("preference"), 10, 64)
		if err != nil || n < -9007199254740991 || n > 9007199254740991 {
			return nxUsageErr("--preference must be a whole number from -9007199254740991 to 9007199254740991")
		}
		b.SetCanon("preference", int(n), nxBindingOrder)
	}
	if c.set("description") {
		b.SetCanon("description", c.str("description"), nxBindingOrder)
	}
	if c.set("deprecated") {
		b.SetCanon("deprecated", c.on("deprecated"), nxBindingOrder)
	}
	return nil
}

// --------------------------------------------------------------- dependency

func nxDependencyCmd() *cobra.Command {
	add := nxEditable(nxLeaf("dependency.add", "add <obi> <name> --operation <operation>", "Add a dependency", `Declare a dependency: a point where the described software calls an
operation that something else provides. --kind limits which kinds of
binding can serve it (any of those listed); leave it out to accept any kind.`,
		`  ob dependency add tasks.obi.json auditLog --operation events.deliver --kind example.grpc@1`,
		nxArgs(2, 2), func(c *nxCtx) error {
			name := c.args[1]
			if err := nxName("dependency name", name); err != nil {
				return err
			}
			if !c.set("operation") {
				return nxUsageErr("--operation is required")
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if after.Obj("dependencies") != nil && after.Obj("dependencies").Has(name) {
				return nxFail(1, "dependency %q already exists in %s; use ob dependency set to change it", name, c.args[0])
			}
			d := nxNewObj()
			if err := nxDependencyFields(c, after, d); err != nil {
				return err
			}
			nxPart(after, "dependencies").Set(name, d)
			return c.wrote(c.args[0], before, after, "Added dependency "+name)
		}))
	add.Flags().String("operation", "", "the operation called at this point (name or alias)")
	add.Flags().StringArray("kind", nil, "a kind of binding that can serve it (repeatable)")
	add.Flags().String("description", "", "a human-readable description")

	set := nxEditable(nxLeaf("dependency.set", "set <obi> <name>", "Change a dependency", `Change a dependency. Only the flags you give change anything; --unset
removes kinds (accept any kind) or the description.`,
		`  ob dependency set tasks.obi.json notifier --add-kind example.mcp@1`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			d, err := c.entry(after, "dependencies", "dependency", c.args[1])
			if err != nil {
				return err
			}
			if err := nxDependencyFields(c, after, d); err != nil {
				return err
			}
			if err := c.unset(d, "kinds", "description"); err != nil {
				return err
			}
			return c.wrote(c.args[0], before, after, "Changed dependency "+c.args[1])
		}))
	set.Flags().String("operation", "", "the operation called at this point (name or alias)")
	set.Flags().StringArray("add-kind", nil, "accept another kind (repeatable)")
	set.Flags().StringArray("remove-kind", nil, "stop accepting a kind (repeatable)")
	set.Flags().String("description", "", "a human-readable description")
	set.Flags().StringArray("unset", nil, "remove a member: kinds, description")

	rename := nxRenameCmd("dependency", "dependencies", "dependency.rename", "", `  ob dependency rename tasks.obi.json notifier eventSink`,
		func(*nxObj, string, string) string { return "" })
	remove := nxEditable(nxLeaf("dependency.remove", "remove <obi> <name>", "Remove a dependency", "Remove a dependency. The operation stays.",
		`  ob dependency remove tasks.obi.json notifier`, nxArgs(2, 2), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if _, err := c.entry(after, "dependencies", "dependency", c.args[1]); err != nil {
				return err
			}
			after.Obj("dependencies").Delete(c.args[1])
			if after.Obj("dependencies").Len() == 0 {
				after.Delete("dependencies")
			}
			return c.wrote(c.args[0], before, after, "Removed dependency "+c.args[1])
		}))
	list := nxListFormats(nxLeaf("dependency.list", "list <obi>", "List dependencies", "List dependencies with the operation each calls and the kinds it accepts.",
		`  ob dependency list tasks.obi.json`, nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			var rows [][]string
			var out []any
			for _, key := range nxPartKeys(doc, "dependencies") {
				d := doc.Obj("dependencies").Obj(key)
				kinds := nxStrings(d.Get("kinds"))
				k := strings.Join(kinds, ", ")
				if k == "" {
					k = "any"
				}
				rows = append(rows, []string{key, fmt.Sprint(d.Get("operation")), k})
				out = append(out, nxNewObj().Set("name", key).Set("operation", d.Get("operation")).Set("kinds", nxToAny(kinds)))
			}
			c.render(out, func() { c.table("NAME\tOPERATION\tKINDS", rows) })
			return nil
		}))
	show := nxListFormats(nxLeaf("dependency.show", "show <obi> <name>", "Show a dependency", "Show a dependency. -F json prints the exact stored dependency.",
		`  ob dependency show tasks.obi.json notifier`, nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			d, err := c.entry(doc, "dependencies", "dependency", c.args[1])
			if err != nil {
				return err
			}
			c.render(d, func() {
				c.println(c.args[1])
				c.println("  Operation: " + fmt.Sprint(d.Get("operation")))
				kinds := strings.Join(nxStrings(d.Get("kinds")), ", ")
				if kinds == "" {
					kinds = "any"
				}
				c.println("  Kinds:     " + kinds)
				if s, _ := d.Get("description").(string); s != "" {
					c.println("  " + s)
				}
			})
			return nil
		}))
	return nxPartGroup("dependency", "Add, change, and remove dependencies", `A dependency is a point where the described software calls an operation
provided by something else. It names the operation, never a provider.`, add, set, rename, remove, list, show)
}

func nxDependencyFields(c *nxCtx, doc, d *nxObj) error {
	if c.set("operation") {
		key, err := c.opKey(doc, c.str("operation"))
		if err != nil {
			return err
		}
		d.SetCanon("operation", key, nxDependOrder)
	}
	kinds := nxStrings(d.Get("kinds"))
	for _, k := range append(c.strs("kind"), c.strs("add-kind")...) {
		if k == "" {
			return nxUsageErr("a kind must not be empty")
		}
		kinds = nxAppendNew(kinds, k)
	}
	for _, k := range c.strs("remove-kind") {
		kinds = nxWithout(kinds, k)
	}
	if c.set("kind") || c.set("add-kind") || c.set("remove-kind") {
		if len(kinds) == 0 {
			d.Delete("kinds")
		} else {
			d.SetCanon("kinds", nxToAny(kinds), nxDependOrder)
		}
	}
	if c.set("description") {
		d.SetCanon("description", c.str("description"), nxDependOrder)
	}
	return nil
}

// ------------------------------------------------------------------- schema

func nxSchemaCmd() *cobra.Command {
	add := nxEditable(nxLeaf("schema.add", "add <obi> <name> --value <schema>", "Add a reusable schema", `Add a named JSON Schema that operations can reference as
{"$ref": "#/schemas/<name>"}.`,
		`  ob schema add tasks.obi.json TaskList --value '{"type":"array","items":{"$ref":"#/schemas/Task"}}'`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if err := nxName("schema name", c.args[1]); err != nil {
				return err
			}
			v, ok, err := c.schema("value")
			if err != nil {
				return err
			}
			if !ok {
				return nxUsageErr("--value is required: the schema, as JSON, @file, or -")
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if m := after.Obj("schemas"); m != nil && m.Has(c.args[1]) {
				return nxFail(1, "schema %q already exists in %s; use ob schema set to replace it", c.args[1], c.args[0])
			}
			nxPart(after, "schemas").Set(c.args[1], v)
			return c.wrote(c.args[0], before, after, "Added schema "+c.args[1])
		}))
	add.Flags().String("value", "", "the schema: JSON, @file, or - (required)")
	set := nxEditable(nxLeaf("schema.set", "set <obi> <name> --value <schema>", "Replace a reusable schema", "Replace a named schema with a new one.",
		`  ob schema set tasks.obi.json Problem --value @problem.schema.json`, nxArgs(2, 2), func(c *nxCtx) error {
			v, ok, err := c.schema("value")
			if err != nil {
				return err
			}
			if !ok {
				return nxUsageErr("--value is required: the new schema, as JSON, @file, or -")
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if _, err := c.entry(after, "schemas", "schema", c.args[1]); err != nil {
				return err
			}
			after.Obj("schemas").Set(c.args[1], v)
			return c.wrote(c.args[0], before, after, "Replaced schema "+c.args[1])
		}))
	set.Flags().String("value", "", "the new schema: JSON, @file, or - (required)")
	rename := nxRenameCmd("schema", "schemas", "schema.rename", " and update references to it", `  ob schema rename tasks.obi.json Problem Error`,
		func(doc *nxObj, from, to string) string {
			n := strings.Count(nxCompact(doc), `"#/schemas/`+from+`"`)
			nxRewriteRefs(doc, "#/schemas/"+from, "#/schemas/"+to)
			return fmt.Sprintf(" (%s updated)", nxCount(n, "reference"))
		})
	remove := nxEditable(nxLeaf("schema.remove", "remove <obi> <name>", "Remove a reusable schema", "Remove a named schema. It refuses while anything references it.",
		`  ob schema remove tasks.obi.json Problem`, nxArgs(2, 2), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if _, err := c.entry(after, "schemas", "schema", c.args[1]); err != nil {
				return err
			}
			if refs := nxSchemaReferrers(after, c.args[1]); len(refs) > 0 {
				return nxFail(1, "schema %s is referenced by %s", c.args[1], strings.Join(refs, ", "))
			}
			after.Obj("schemas").Delete(c.args[1])
			return c.wrote(c.args[0], before, after, "Removed schema "+c.args[1])
		}))
	list := nxListFormats(nxLeaf("schema.list", "list <obi>", "List reusable schemas", "List named schemas and what references each.",
		`  ob schema list tasks.obi.json`, nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			var rows [][]string
			var out []any
			for _, key := range nxPartKeys(doc, "schemas") {
				refs := nxSchemaReferrers(doc, key)
				rows = append(rows, []string{key, nxDash(strings.Join(refs, ", "))})
				out = append(out, nxNewObj().Set("name", key).Set("referencedBy", nxToAny(refs)))
			}
			c.render(out, func() { c.table("NAME\tREFERENCED BY", rows) })
			return nil
		}))
	show := nxListFormats(nxLeaf("schema.show", "show <obi> <name>", "Show a reusable schema", "Print a named schema.",
		`  ob schema show tasks.obi.json Task`, nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			s, err := c.entry(doc, "schemas", "schema", c.args[1])
			if err != nil {
				return err
			}
			c.render(s, func() { c.println(nxPretty(s)) })
			return nil
		}))
	return nxPartGroup("schema", "Add, change, and remove reusable schemas", `Named JSON Schemas that operations share by reference, such as
{"$ref": "#/schemas/Task"}.`, add, set, rename, remove, list, show)
}
