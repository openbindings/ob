package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Shared pieces for the five part nouns.

func (c *nxCtx) opKey(doc *nxObj, name string) (string, error) {
	key, ok := nxResolveOperation(doc, name)
	if !ok {
		return "", nxNotFound("no operation named %q in %s%s", name, c.args[0], nxDidYouMean(doc, name))
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
	return nil, nxNotFound("no %s named %q in %s", what, name, c.args[0])
}

// unset removes the members --unset names. Each allowed entry is a name, or
// NAME=MEMBER when the name is a flag's and the member differs, so that
// --unset input-schema undoes --input-schema.
func (c *nxCtx) unset(obj *nxObj, allowed ...string) error {
	names := map[string]string{}
	var list []string
	for _, a := range allowed {
		name, member, found := strings.Cut(a, "=")
		if !found {
			member = name
		}
		names[name] = member
		list = append(list, name)
	}
	for _, u := range c.strs("unset") {
		member, ok := names[u]
		if !ok {
			return nxUsageErr("--unset takes one of: %s", strings.Join(list, ", "))
		}
		if !obj.Has(member) {
			return nxNotFound("there is no %s to remove", u)
		}
		obj.Delete(member)
	}
	return nil
}

// nxContradictions refuses flags that ask for opposite changes to the same
// thing: setting a member and unsetting it, or adding and removing one item.
func nxContradictions(c *nxCtx, setFlags map[string]string, pairs ...[2]string) error {
	for flag, member := range setFlags {
		if c.set(flag) && nxContains(c.strs("unset"), member) {
			return nxUsageErr("--%s and --unset %s ask for opposite changes; choose one", flag, member)
		}
	}
	for _, p := range pairs {
		for _, v := range c.strs(p[0]) {
			if nxContains(c.strs(p[1]), v) {
				return nxUsageErr("--%s and --%s both name %q; choose one", p[0], p[1], v)
			}
		}
	}
	return nil
}

func nxNeedsChange(c *nxCtx) error {
	changed := false
	c.cmd.Flags().Visit(func(f *pflagFlag) {
		if f.Name != "dry-run" && f.Name != "out" {
			changed = true
		}
	})
	if !changed {
		return nxUsageErr("nothing to change; give at least one flag (see %s --help)", c.cmd.CommandPath())
	}
	return nil
}

func nxListFormats(cmd *cobra.Command) *cobra.Command {
	nxFormat(cmd, "text", "json")
	return cmd
}

func (c *nxCtx) render(v any, text func()) {
	switch c.format() {
	case "json":
		c.println(nxPretty(v))
	default:
		text()
	}
}

func nxPartGroup(noun, short, long string, children ...*cobra.Command) *cobra.Command {
	return nxGroupCmd(noun, short, long, children...)
}

func nxRenameCmd(id, short, long, noun, part, example string, apply func(doc *nxObj, from, to string) string) *cobra.Command {
	return nxEditable(nxLeaf(id, "rename <obi> <name> <new-name>", short, long, example, nxArgs(3, 3), func(c *nxCtx) error {
		before := c.doc(c.args[0])
		after := nxClone(before).(*nxObj)
		from, to := c.args[1], c.args[2]
		if err := nxName(noun+" name", to); err != nil {
			return err
		}
		if part == "operations" && (after.Obj("operations") == nil || !after.Obj("operations").Has(from)) {
			if key, ok := nxResolveOperation(after, from); ok {
				return nxRefuse("%s is an alias of operation %s; rename changes an operation's key (ob operation rename %s %s <new-name>), and aliases change with ob operation set --add-alias and --remove-alias", from, key, c.args[0], key)
			}
		}
		if _, err := c.entry(after, part, noun, from); err != nil {
			return err
		}
		if after.Obj(part).Has(to) {
			return nxRefuse("%s %q already exists in %s", noun, to, c.args[0])
		}
		if part == "operations" {
			if other, taken := nxResolveOperation(after, to); taken {
				if other == from {
					return nxRefuse("%q is already an alias of %s; remove that alias first (ob operation set %s %s --remove-alias %s), then rename", to, from, c.args[0], from, to)
				}
				return nxRefuse("%q is already an alias of operation %s", to, other)
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
					return nxRefuse("operation %q already exists in %s; use ob operation set to change it", name, c.args[0])
				}
				return nxRefuse("%q is already an alias of operation %s", name, key)
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
Only the flags you give change anything. --unset removes a member, named as
its flag is (input-schema, output-schema, description, deprecated) or as the
whole list (aliases, tags, examples); unsetting a schema leaves that side
unspecified. Removing an alias or tag the operation does not have is
refused.`,
		`  ob operation set tasks.obi.json createTask --add-alias acme.tasks.addTask
  ob operation set tasks.obi.json listTasks --deprecated
  ob operation set tasks.obi.json completeTask --unset output-schema`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			if err := nxContradictions(c, map[string]string{"description": "description", "deprecated": "deprecated", "input-schema": "input-schema", "output-schema": "output-schema"},
				[2]string{"add-alias", "remove-alias"}, [2]string{"add-tag", "remove-tag"}); err != nil {
				return err
			}
			if (c.set("add-alias") || c.set("remove-alias")) && nxContains(c.strs("unset"), "aliases") || (c.set("add-tag") || c.set("remove-tag")) && nxContains(c.strs("unset"), "tags") {
				return nxUsageErr("--unset removes the whole list, so it does not combine with adding or removing items")
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
			if err := c.unset(op, "input-schema=input", "output-schema=output", "description", "deprecated", "aliases", "tags", "examples"); err != nil {
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
	set.Flags().StringArray("unset", nil, "remove a member: input-schema, output-schema, description, deprecated, aliases, tags, examples")

	rename := nxRenameCmd("operation.rename", "Rename an operation", `Rename an operation's key, and update every binding and dependency that
uses it. Nothing else changes: its aliases stay as they are, and the old
name is gone. To keep answering to the old name, add it as an alias
afterwards with ob operation set --add-alias.`, "operation", "operations",
		`  ob operation rename tasks.obi.json createTask addTask`,
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

	remove := nxEditable(nxLeaf("operation.remove", "remove <obi> <name>", "Remove an operation", `Remove an operation, named by its key. It refuses while bindings or
dependencies use the operation; --cascade removes those too. Given an alias,
it refuses and says whose alias it is: an alias goes with ob operation set
--remove-alias.`,
		`  ob operation remove tasks.obi.json completeTask --cascade`,
		nxArgs(2, 2), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if key, ok := nxResolveOperation(after, c.args[1]); ok && key != c.args[1] {
				return nxRefuse("%s is an alias of operation %s, so nothing was written; to remove the operation, name its key (ob operation remove %s %s); to remove the alias, use ob operation set %s %s --remove-alias %s", c.args[1], key, c.args[0], key, c.args[0], key, c.args[1])
			}
			key, err := c.opKey(after, c.args[1])
			if err != nil {
				return err
			}
			users := append(nxReferrers(after, "bindings", "operation", key), nxReferrers(after, "dependencies", "operation", key)...)
			if len(users) > 0 && !c.on("cascade") {
				return nxRefuse("operation %s is used by %s; remove those first, or use --cascade", key, strings.Join(users, ", "))
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
				return nxRefuse("operation %s already has an example named %q", key, c.args[2])
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
				return nxUsageErr("give the example something: --input, --output, or --description")
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
				return nxNotFound("operation %s has no example named %q", key, c.args[2])
			}
			op.Obj("examples").Delete(c.args[2])
			if op.Obj("examples").Len() == 0 {
				op.Delete("examples")
			}
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Removed example %s from operation %s", c.args[2], key))
		}))
	exampleOf := func(c *nxCtx, doc *nxObj) (string, *nxObj, error) {
		key, err := c.opKey(doc, c.args[1])
		if err != nil {
			return "", nil, err
		}
		examples := doc.Obj("operations").Obj(key).Obj("examples")
		if examples == nil || !examples.Has(c.args[2]) {
			return "", nil, nxNotFound("operation %s has no example named %q", key, c.args[2])
		}
		return key, examples.Obj(c.args[2]), nil
	}
	exampleSet := nxEditable(nxLeaf("operation.example.set", "set <obi> <operation> <name>", "Change an example", `Change an example's input value, output value, or description. Only the
flags you give change anything; --unset removes one. ob warns when a value
does not fit the operation's schema; the schema always wins.`,
		`  ob operation example set tasks.obi.json createTask basic --input '{"title":"Plan the release"}'`,
		nxArgs(3, 3), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			if err := nxContradictions(c, map[string]string{"input": "input", "output": "output", "description": "description"}); err != nil {
				return err
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			key, ex, err := exampleOf(c, after)
			if err != nil {
				return err
			}
			for _, side := range []string{"input", "output"} {
				v, ok, err := c.value(side)
				if err != nil {
					return err
				}
				if ok {
					ex.SetCanon(side, v, nxExampleOrder)
					if problems, checked, err := nxCheck(after, key, side, v); err == nil && checked && len(problems) > 0 {
						c.note(fmt.Sprintf("warning: the example's %s does not fit %s's %s schema: %s", side, key, side, strings.Join(problems, "; ")))
					}
				}
			}
			if c.set("description") {
				ex.SetCanon("description", c.str("description"), nxExampleOrder)
			}
			if err := c.unset(ex, "input", "output", "description"); err != nil {
				return err
			}
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Changed example %s of operation %s", c.args[2], key))
		}))
	exampleSet.Flags().String("input", "", "the example's input value: JSON, @file, or -")
	exampleSet.Flags().String("output", "", "the example's output value: JSON, @file, or -")
	exampleSet.Flags().String("description", "", "what the example shows")
	exampleSet.Flags().StringArray("unset", nil, "remove a member: input, output, description")
	exampleRename := nxEditable(nxLeaf("operation.example.rename", "rename <obi> <operation> <name> <new-name>", "Rename an example", "Rename an example of an operation. Nothing else changes.",
		`  ob operation example rename tasks.obi.json createTask basic minimal`, nxArgs(4, 4), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			key, _, err := exampleOf(c, after)
			if err != nil {
				return err
			}
			if err := nxName("example name", c.args[3]); err != nil {
				return err
			}
			examples := after.Obj("operations").Obj(key).Obj("examples")
			if examples.Has(c.args[3]) {
				return nxRefuse("operation %s already has an example named %q", key, c.args[3])
			}
			examples.Rename(c.args[2], c.args[3])
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Renamed example %s of operation %s to %s", c.args[2], key, c.args[3]))
		}))
	exampleList := nxListFormats(nxLeaf("operation.example.list", "list <obi> <operation>", "List an operation's examples", "List an operation's examples and what each includes.",
		`  ob operation example list tasks.obi.json createTask`, nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			key, err := c.opKey(doc, c.args[1])
			if err != nil {
				return err
			}
			var rows [][]string
			var out []any
			if examples := doc.Obj("operations").Obj(key).Obj("examples"); examples != nil {
				for _, name := range examples.Keys() {
					ex := examples.Obj(name)
					cell := func(m string) string {
						if !ex.Has(m) {
							return "-"
						}
						v := nxCompact(ex.Get(m))
						if len(v) > 40 {
							v = v[:37] + "..."
						}
						return v
					}
					rows = append(rows, []string{name, cell("input"), cell("output")})
					out = append(out, nxNewObj().Set("name", name).Set("example", ex))
				}
			}
			c.render(out, func() { c.table("NAME\tINPUT\tOUTPUT", rows) })
			return nil
		}))
	exampleShow := nxListFormats(nxLeaf("operation.example.show", "show <obi> <operation> <name>", "Show an example", "Show an example. -F json prints the exact stored example.",
		`  ob operation example show tasks.obi.json createTask basic`, nxArgs(3, 3), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			_, ex, err := exampleOf(c, doc)
			if err != nil {
				return err
			}
			c.render(ex, func() { c.println(nxPretty(ex)) })
			return nil
		}))
	example := nxGroupCmd("example", "Add, change, and remove operation examples", `An example is a named input value, output value, or both, that the
author claims fit the operation's schemas.`, exampleAdd, exampleSet, exampleRename, exampleRemove, exampleList, exampleShow)

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
			return nxRefuse("%q is already a name of operation %s; names must be unique across operations and aliases", a, other)
		}
		aliases = nxAppendNew(aliases, a)
	}
	for _, a := range c.strs("remove-alias") {
		if !nxContains(aliases, a) {
			return nxNotFound("operation %s has no alias %q", key, a)
		}
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
		if !nxContains(tags, t) {
			return nxNotFound("operation %s has no tag %q", key, t)
		}
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
				return nxRefuse("source %q already exists in %s; use ob source set to change it", name, c.args[0])
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
			if err := nxContradictions(c, map[string]string{"content": "content", "description": "description"}); err != nil {
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

	rename := nxRenameCmd("source.rename", "Rename a source", `Rename a source, and update every binding that goes through it.`, "source", "sources",
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
				return nxRefuse("source %s is used by %s; remove those first, or use --cascade", c.args[1], strings.Join(users, ", "))
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
				return nxRefuse("this ob has no handler that can import %s artifacts; ob kind list shows what it can handle", kind)
			}
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			if after.Obj("sources").Has(name) {
				return nxRefuse("source %q already exists in %s", name, c.args[0])
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
			var targets [][]string
			for _, t := range nxTargets {
				if t.source != c.args[1] {
					continue
				}
				bound := "-"
				if doc.Obj("bindings").Has(t.binding) {
					bound = t.binding
				}
				targets = append(targets, []string{t.target, t.operation, bound})
			}
			var out []any
			for _, t := range targets {
				entry := nxNewObj().Set("target", t[0]).Set("suggestedOperation", t[1])
				if t[2] != "-" {
					entry.Set("boundBy", t[2])
				}
				for _, nt := range nxTargets {
					if nt.source == c.args[1] && nt.target == t[0] {
						entry.Set("binding", nxNewObj().Set("content", nxMustParse(nt.content)))
					}
				}
				out = append(out, entry)
			}
			c.render(nxNewObj().Set("targets", out).Set("exhaustive", true), func() {
				c.println(fmt.Sprintf("%s (%s): %s, complete", c.args[1], kind, nxCount(len(targets), "target")))
				c.table("TARGET\tSUGGESTED OPERATION\tBOUND BY", targets)
			})
			return nil
		}))

	pull := nxEditable(nxLeaf("source.pull", "pull <obi> [<source>...]", "Update a document from its sources", `Update the document from its sources, using a handler for each source's
kind. With no sources named, pull every source.

Pull refreshes the content of the bindings you have, so they keep up with
their sources. It adds nothing you did not ask for, and never changes an
operation you already have:

  - a target no binding covers is listed, not bound; --target binds one
    (and --all-targets binds every one), with the operation the handler
    suggests, or one you already have when you name it with --operation;
  - where a source describes an operation's schemas differently, pull
    reports it, and --update-operation takes the source's schemas for that
    operation. When two sources being pulled describe it differently, pull
    refuses; pull one source to choose.

A source whose kind this ob cannot read is named and left alone, and pull
exits 4, since it cannot say the document is up to date with that source.
ob status reports the same, as a check, without changing anything.`,
		`  ob source pull tasks.obi.json
  ob source pull tasks.obi.json httpApi --update-operation listTasks
  ob source pull tasks.obi.json httpApi --target "POST /tasks/{id}/archive"
  ob source pull tasks.obi.json mcpServer --target tools/complete_task --operation completeTask
  ob source pull tasks.obi.json httpApi --all-targets`,
		nxArgs(1, -1), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			for _, name := range c.args[1:] {
				if _, err := c.entry(after, "sources", "source", name); err != nil {
					return err
				}
			}
			if c.set("target") {
				if len(c.args) != 2 {
					return nxUsageErr("--target binds a target of one source; name that source: ob source pull %s <source> --target ...", c.args[0])
				}
				if c.set("update-operation") || c.on("all-targets") {
					return nxUsageErr("--target binds one target and changes nothing else, so it does not take --update-operation or --all-targets")
				}
				return nxPullTarget(c, before, after, c.args[1], c.str("target"))
			}
			if c.set("operation") {
				return nxUsageErr("--operation says which operation a --target binding realizes; give it with --target")
			}
			return nxPull(c, before, after)
		}))
	pull.Flags().String("target", "", "bind this one target of the source, as ob source inspect lists it or by its identifier")
	pull.Flags().String("operation", "", "with --target: bind it to this existing operation (key or alias)")
	pull.Flags().StringArray("update-operation", nil, "take the source's schemas for this operation (repeatable)")
	pull.Flags().Bool("all-targets", false, "bind every target no binding covers yet")

	return nxPartGroup("source", "Add, change, and remove sources, and read their artifacts", `A source is a kind plus optional content the kind reads, such as an
artifact's address. Bindings realize operations through sources. import,
inspect, and pull use a handler for the source's kind.`,
		add, set, rename, remove, list, show, importCmd, inspect, pull)
}

// The sample sources' targets, as ob source inspect lists them, with what a
// binding for each would carry and the operation the handler suggests.
type nxTarget struct {
	source, target, operation, binding string
	content                            string
	newOperation                       string
}

var nxTargets = []nxTarget{
	{"httpApi", "POST /tasks", "createTask", "createTask.http", `{"target":"#/paths/~1tasks/post"}`, ""},
	{"httpApi", "GET /tasks", "listTasks", "listTasks.http", `{"target":"#/paths/~1tasks/get"}`, ""},
	{"httpApi", "POST /tasks/{id}/complete", "completeTask", "completeTask.http", `{"target":"#/paths/~1tasks~1{id}~1complete/post"}`, ""},
	{"httpApi", "POST /tasks/{id}/archive", "archiveTask", "archiveTask.http", `{"target":"#/paths/~1tasks~1{id}~1archive/post"}`,
		`{"description":"Archive a task.","input":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]},"output":{"$ref":"#/schemas/Task"}}`},
	{"httpApi", "GET /health", "getHealth", "getHealth.http", `{"target":"#/paths/~1health/get"}`,
		`{"description":"Report whether the service is up.","input":{"type":"object","maxProperties":0},"output":{"type":"object"}}`},
	{"mcpServer", "tools/create_task", "createTask", "createTask.mcp", `{"target":"tools/create_task"}`, ""},
	{"mcpServer", "tools/list_tasks", "listTasks", "listTasks.mcp", `{"target":"tools/list_tasks"}`, ""},
	{"mcpServer", "tools/complete_task", "complete_task", "complete_task.mcp", `{"target":"tools/complete_task"}`,
		`{"description":"Mark a task done.","input":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]},"output":{"$ref":"#/schemas/Task"}}`},
}

// nxTargetID is a target's identifier in its source, as a binding's content
// carries it.
func nxTargetID(t nxTarget) string {
	id, _ := nxMustParse(t.content).(*nxObj).Get("target").(string)
	return id
}

func nxPullTarget(c *nxCtx, before, after *nxObj, source, target string) error {
	src := after.Obj("sources").Obj(source)
	if kind := fmt.Sprint(src.Get("kind")); !nxCanRead(kind) {
		return nxRefuse("this ob cannot read %s sources, so nothing was pulled; ob kind list shows what it can handle", kind)
	}
	for _, t := range nxTargets {
		if t.source != source || (t.target != target && nxTargetID(t) != target) {
			continue
		}
		for _, key := range nxReferrers(after, "bindings", "source", source) {
			if nxCompact(after.Obj("bindings").Obj(key).Get("content")) == nxCompact(nxMustParse(t.content)) {
				c.note(fmt.Sprintf("%s is already bound, by %s", t.target, key))
				return c.wrote(c.args[0], before, after, "")
			}
		}
		op, binding := t.operation, t.binding
		summary := "Bound " + t.target + " as "
		if c.set("operation") {
			key, err := c.opKey(after, c.str("operation"))
			if err != nil {
				return err
			}
			op, binding = key, key+binding[strings.Index(binding, "."):]
		}
		if after.Obj("bindings").Has(binding) {
			return nxRefuse("binding %q already exists in %s, so nothing was written", binding, c.args[0])
		}
		summary += binding
		if !after.Obj("operations").Has(op) {
			after.Obj("operations").Set(op, nxMustParse(t.newOperation))
			summary += ", with a new operation " + op
		} else {
			summary += ", for operation " + op
		}
		after.Obj("bindings").Set(binding, nxNewObj().Set("operation", op).Set("source", source).Set("content", nxMustParse(t.content)))
		return c.wrote(c.args[0], before, after, summary)
	}
	return nxNotFound("%s offers no target %q; ob source inspect %s %s lists them", source, target, c.args[0], source)
}

// nxCanRead says whether this ob can read a source of a kind, which is what
// inspect, pull, and status need.
func nxCanRead(kind string) bool {
	_, ok := nxSupports(kind, "inspect")
	return ok
}

// A pull's findings for one source: targets to bind, and operations whose
// schemas the source describes differently.
type nxSourceDrift struct {
	source, kind string
	readable     bool
	unbound      []nxTarget
	differs      []nxSchemaDrift
}

type nxSchemaDrift struct {
	operation, side, upstream string
}

// What the sample sources say upstream that the document does not.
var nxUpstreamSchemas = map[string][]nxSchemaDrift{
	"httpApi":   {{"listTasks", "output", `{"type":"array","items":{"$ref":"#/schemas/Task"},"maxItems":500}`}},
	"mcpServer": {{"createTask", "input", `{"type":"object","properties":{"title":{"type":"string"},"priority":{"type":"integer"}},"required":["title"]}`}},
}

func nxDrift(doc *nxObj, sources []string) []nxSourceDrift {
	if len(sources) == 0 {
		sources = nxPartKeys(doc, "sources")
	}
	var out []nxSourceDrift
	for _, name := range sources {
		kind := fmt.Sprint(doc.Obj("sources").Obj(name).Get("kind"))
		d := nxSourceDrift{source: name, kind: kind, readable: nxCanRead(kind)}
		if d.readable {
			for _, t := range nxTargets {
				if t.source != name {
					continue
				}
				bound := false
				for _, key := range nxReferrers(doc, "bindings", "source", name) {
					bound = bound || nxCompact(doc.Obj("bindings").Obj(key).Get("content")) == nxCompact(nxMustParse(t.content))
				}
				if !bound {
					d.unbound = append(d.unbound, t)
				}
			}
			d.differs = nxUpstreamSchemas[name]
		}
		out = append(out, d)
	}
	return out
}

func nxPull(c *nxCtx, before, after *nxObj) error {
	drift := nxDrift(after, c.args[1:])
	update := map[string]string{}
	for _, name := range c.strs("update-operation") {
		key, err := c.opKey(after, name)
		if err != nil {
			return err
		}
		update[key] = name
	}
	var skipped, notes, parts []string
	taken := map[string]string{}
	for _, d := range drift {
		if !d.readable {
			if len(c.args) > 1 {
				return nxRefuse("this ob cannot read %s sources, so nothing was pulled from %s; ob kind list shows what it can handle", d.kind, d.source)
			}
			skipped = append(skipped, fmt.Sprintf("%s (this ob cannot read %s sources)", d.source, d.kind))
			continue
		}
		var available []string
		for _, t := range d.unbound {
			if !c.on("all-targets") {
				available = append(available, t.target)
				continue
			}
			if !after.Obj("operations").Has(t.operation) {
				after.Obj("operations").Set(t.operation, nxMustParse(t.newOperation))
				parts = append(parts, "added "+t.binding+" with a new operation "+t.operation)
			} else {
				parts = append(parts, "added "+t.binding)
			}
			after.Obj("bindings").Set(t.binding, nxNewObj().Set("operation", t.operation).Set("source", d.source).Set("content", nxMustParse(t.content)))
		}
		if len(available) > 0 {
			notes = append(notes, fmt.Sprintf("%s offers targets no binding covers: %s (--target binds one; --all-targets binds every one)", d.source, strings.Join(available, ", ")))
		}
		for _, s := range d.differs {
			if _, ok := update[s.operation]; !ok {
				notes = append(notes, fmt.Sprintf("%s describes %s's %s schema differently; not applied (--update-operation %s takes it)", d.source, s.operation, s.side, s.operation))
				continue
			}
			taken[s.operation] = d.source
			after.Obj("operations").Obj(s.operation).SetCanon(s.side, nxMustParse(s.upstream), nxOperationOrder)
			parts = append(parts, fmt.Sprintf("took %s's %s schema for %s", d.source, s.side, s.operation))
		}
	}
	// An operation updated from one source must not be described differently
	// by another source pulled in the same run.
	for op, from := range taken {
		for _, d := range drift {
			if d.source == from || !d.readable || len(nxReferrersOf(after, op, d.source)) == 0 {
				continue
			}
			return nxRefuse("refused: %s is bound to %s and %s, and they describe its schemas differently, so nothing was written; pull one source to take its schemas:\n  ob source pull %s %s --update-operation %s", op, from, d.source, c.args[0], from, update[op])
		}
	}
	for key, name := range update {
		if _, ok := taken[key]; !ok {
			notes = append(notes, fmt.Sprintf("%s already matches the sources pulled", name))
		}
	}
	for _, n := range notes {
		c.note(n)
	}
	summary := "Pulled: " + strings.Join(parts, "; ")
	if len(parts) == 0 {
		c.note("The bindings are up to date.")
		summary = ""
	}
	err := c.wrote(c.args[0], before, after, summary)
	if err == nil && len(skipped) > 0 {
		return nxFail(4, "not pulled: %s", strings.Join(skipped, ", "))
	}
	return err
}

// nxReferrersOf lists the bindings of one operation through one source.
func nxReferrersOf(doc *nxObj, op, source string) []string {
	var out []string
	for _, key := range nxReferrers(doc, "bindings", "operation", op) {
		if fmt.Sprint(doc.Obj("bindings").Obj(key).Get("source")) == source {
			out = append(out, key)
		}
	}
	return out
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
further intended effects (it says nothing about retry safety on its own);
--idempotent=false claims it can; leaving it out claims nothing.
--preference records the author's preference among the operation's
bindings; ob invoke shows it but does not choose by it.`,
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
				return nxRefuse("binding %q already exists in %s; use ob binding set to change it", name, c.args[0])
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
			if err := nxContradictions(c, map[string]string{"content": "content", "idempotent": "idempotent", "preference": "preference", "description": "description", "deprecated": "deprecated"}); err != nil {
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

	rename := nxRenameCmd("binding.rename", "Rename a binding", `Rename a binding. Nothing else in a document refers to a binding by name,
so nothing else changes. Callers that name the old key, such as ob invoke
--binding, ob mcp --binding, or a generated client's binding list, stop
finding it.`, "binding", "bindings", `  ob binding rename tasks.obi.json createTask.http createTask.rest`,
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
	cmd.Flags().String("content", "", "whatever the source's kind reads, such as which target to call: JSON, @file, or -")
	cmd.Flags().Bool("idempotent", false, "claim that repeating a call adds no further intended effects; --idempotent=false claims it can")
	cmd.Flags().Int64("preference", 0, "the author's preference among this operation's bindings (an integer; higher is stronger)")
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
		n, _ := c.cmd.Flags().GetInt64("preference")
		if n < -9007199254740991 || n > 9007199254740991 {
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
binding can serve it (any of those listed); leave it out to declare no kind
constraint (spec §5.5).`,
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
				return nxRefuse("dependency %q already exists in %s; use ob dependency set to change it", name, c.args[0])
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
removes kinds (declaring no kind constraint, spec §5.5) or the description.`,
		`  ob dependency set tasks.obi.json notifier --add-kind example.mcp@1`,
		nxArgs(2, 2), func(c *nxCtx) error {
			if err := nxNeedsChange(c); err != nil {
				return err
			}
			if err := nxContradictions(c, map[string]string{"description": "description"}, [2]string{"add-kind", "remove-kind"}); err != nil {
				return err
			}
			if (c.set("add-kind") || c.set("remove-kind")) && nxContains(c.strs("unset"), "kinds") {
				return nxUsageErr("--unset kinds removes the whole list, so it does not combine with adding or removing kinds")
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

	rename := nxRenameCmd("dependency.rename", "Rename a dependency", `Rename a dependency. Nothing else in a document refers to a dependency by
name, so nothing else changes.`, "dependency", "dependencies", `  ob dependency rename tasks.obi.json notifier eventSink`,
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
		if !nxContains(kinds, k) {
			return nxNotFound("the dependency does not list kind %q", k)
		}
		kinds = nxWithout(kinds, k)
	}
	if c.set("kind") || c.set("add-kind") || c.set("remove-kind") {
		if len(kinds) == 0 {
			return nxRefuse("that would remove the last kind, and a dependency without kinds declares no kind constraint (spec §5.5); to mean that, use --unset kinds")
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
				return nxRefuse("schema %q already exists in %s; use ob schema set to replace it", c.args[1], c.args[0])
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
	rename := nxRenameCmd("schema.rename", "Rename a reusable schema", `Rename a reusable schema, and update every reference to it, such as
{"$ref": "#/schemas/<name>"}.`, "schema", "schemas", `  ob schema rename tasks.obi.json Problem Error`,
		func(doc *nxObj, from, to string) string {
			n := 0
			for _, p := range nxPositions(doc) {
				n += nxRewriteRefs(p.value, from, "#/schemas/"+to)
			}
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
				return nxRefuse("schema %s is referenced by %s", c.args[1], strings.Join(refs, ", "))
			}
			after.Obj("schemas").Delete(c.args[1])
			return c.wrote(c.args[0], before, after, "Removed schema "+c.args[1])
		}))
	list := nxListFormats(nxLeaf("schema.list", "list <obi>", "List reusable schemas", `List named schemas and what references each, then the external schemas the
document references that no schema in it declares (ob schema bundle embeds
them).`,
		`  ob schema list tasks.obi.json`, nxArgs(1, 1), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			var rows [][]string
			var out []any
			for _, key := range nxPartKeys(doc, "schemas") {
				refs := nxSchemaReferrers(doc, key)
				rows = append(rows, []string{key, nxDash(strings.Join(refs, ", "))})
				entry := nxNewObj().Set("name", key).Set("referencedBy", nxToAny(refs))
				if id, ok := doc.Obj("schemas").Obj(key).Get("$id").(string); ok {
					entry.Set("id", id)
				}
				out = append(out, entry)
			}
			external := nxExternalRefs(doc)
			for _, x := range external {
				out = append(out, nxNewObj().Set("uri", x.uri).Set("external", true).Set("referencedBy", nxToAny(x.users)))
			}
			c.render(out, func() {
				c.table("NAME\tREFERENCED BY", rows)
				if len(external) > 0 {
					c.println("")
					c.println("External (ob schema bundle embeds them):")
					var ext [][]string
					for _, x := range external {
						ext = append(ext, []string{"  " + x.uri, strings.Join(x.users, ", ")})
					}
					c.table("", ext)
				}
			})
			return nil
		}))
	bundle := nxEditable(nxLeaf("schema.bundle", "bundle <obi> [<uri>...]", "Embed external schemas", `Embed the external schemas the document references, so it no longer needs
the network to resolve them. ob fetches each one and adds it to schemas
with its $id (the URI it came from, unless it declares its own), along with
the external schemas it references in turn. References do not change: a
reference to that URI now resolves to the copy in the document, as JSON
Schema's bundling defines (spec §7.4). Named URIs limit it to those, and
what they reference. Each copy is named from its URI; ob schema rename
renames one. ob schema list shows what is still external.`,
		`  ob schema bundle tasks.obi.json
  ob schema bundle tasks.obi.json https://schemas.example.com/time/date-time.json`,
		nxArgs(1, -1), func(c *nxCtx) error {
			before := c.doc(c.args[0])
			after := nxClone(before).(*nxObj)
			external := nxExternalRefs(after)
			queue := c.args[1:]
			if len(queue) == 0 {
				for _, x := range external {
					queue = append(queue, x.uri)
				}
			}
			for _, u := range c.args[1:] {
				found := false
				for _, x := range external {
					found = found || x.uri == u
				}
				if !found {
					return nxNotFound("%s does not reference %s as an external schema; ob schema list shows the ones it does", c.args[0], u)
				}
			}
			var embedded []string
			for len(queue) > 0 {
				u := queue[0]
				queue = queue[1:]
				if nxDeclaresID(after, u) {
					continue
				}
				raw, ok := nxPublishedSchemas[u]
				if !ok {
					return nxFail(1, "could not fetch %s, so nothing was written", u)
				}
				schema := nxMustParse(raw).(*nxObj)
				if !schema.Has("$id") {
					schema.Set("$id", u)
				}
				name := nxSchemaNameFor(after, u)
				nxPart(after, "schemas").Set(name, schema)
				embedded = append(embedded, name+" ("+u+")")
				for _, x := range nxRefsIn(schema, u) {
					queue = append(queue, x)
				}
			}
			if len(embedded) == 0 {
				return c.wrote(c.args[0], before, after, "")
			}
			return c.wrote(c.args[0], before, after, fmt.Sprintf("Embedded %s: %s", nxCount(len(embedded), "schema"), strings.Join(embedded, ", ")))
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
{"$ref": "#/schemas/Task"}. bundle embeds the external schemas a document
references.`, add, set, rename, remove, list, show, bundle)
}
