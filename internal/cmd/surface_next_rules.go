package cmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The 0.2 derived schema, from spec release/0.2 at de2c20b.
//
//go:embed surface_next_schema.json
var nxSchemaJSON []byte

var nxStructural = func() *jsonschema.Schema {
	raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(nxSchemaJSON))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	const id = "https://openbindings.com/schema/openbindings-0.2.json"
	if err := c.AddResource(id, raw); err != nil {
		panic(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		panic(err)
	}
	return s
}()

var nxURIScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// nxViolations reports the document rules a document breaks, as far as the
// preview can decide them: the structural schema (OBI-D-02), unique names
// (OBI-D-04), reference forms (OBI-D-05), references to operations and
// sources (OBI-D-07, D-08, D-11), meta-schema validity (OBI-D-10), and
// same-document references (OBI-D-12). Each entry names its rule.
func nxViolations(doc *nxObj) []string {
	var out []string
	add := func(rule, format string, a ...any) {
		out = append(out, rule+": "+fmt.Sprintf(format, a...))
	}
	if inst, err := jsonschema.UnmarshalJSON(strings.NewReader(nxCompact(doc))); err == nil {
		if err := nxStructural.Validate(inst); err != nil {
			for _, line := range strings.Split(err.Error(), "\n") {
				if line = strings.TrimSpace(line); strings.HasPrefix(line, "- ") {
					add("OBI-D-02", "%s", strings.TrimPrefix(line, "- "))
				}
			}
		}
	}
	seen := map[string]string{}
	for _, key := range nxPartKeys(doc, "operations") {
		op := doc.Obj("operations").Obj(key)
		if op == nil {
			continue
		}
		for _, name := range append([]string{key}, nxStrings(op.Get("aliases"))...) {
			if other, dup := seen[name]; dup {
				add("OBI-D-04", "%q names both %s and %s", name, other, key)
			}
			seen[name] = key
		}
	}
	for _, key := range nxPartKeys(doc, "bindings") {
		b := doc.Obj("bindings").Obj(key)
		if b == nil {
			continue
		}
		if op, _ := b.Get("operation").(string); op != "" && (doc.Obj("operations") == nil || !doc.Obj("operations").Has(op)) {
			add("OBI-D-07", "binding %s names operation %q, which does not exist", key, op)
		}
		if src, _ := b.Get("source").(string); src != "" && (doc.Obj("sources") == nil || !doc.Obj("sources").Has(src)) {
			add("OBI-D-08", "binding %s names source %q, which does not exist", key, src)
		}
	}
	for _, key := range nxPartKeys(doc, "dependencies") {
		d := doc.Obj("dependencies").Obj(key)
		if d == nil {
			continue
		}
		if op, _ := d.Get("operation").(string); op != "" && (doc.Obj("operations") == nil || !doc.Obj("operations").Has(op)) {
			add("OBI-D-11", "dependency %s names operation %q, which does not exist", key, op)
		}
	}
	type position struct {
		label string
		value any
	}
	var positions []position
	for _, key := range nxPartKeys(doc, "schemas") {
		positions = append(positions, position{"schema " + key, doc.Obj("schemas").Get(key)})
	}
	for _, key := range nxPartKeys(doc, "operations") {
		op := doc.Obj("operations").Obj(key)
		for _, side := range []string{"input", "output"} {
			if op != nil && op.Has(side) {
				positions = append(positions, position{key + " " + side, op.Get(side)})
			}
		}
	}
	for _, p := range positions {
		broken := false
		nxWalkRefs(p.value, func(ref string) {
			switch {
			case strings.HasPrefix(ref, "#"):
				if !nxResolvesToSchema(doc, ref) {
					add("OBI-D-12", "%s: %q does not name a schema in this document", p.label, ref)
					broken = true
				}
			case !nxURIScheme.MatchString(ref):
				add("OBI-D-05", "%s: %q must be an absolute URI or start with #", p.label, ref)
				broken = true
			}
		})
		if broken {
			continue
		}
		if err := nxMetaValid(doc, p.value); err != "" {
			add("OBI-D-10", "%s is not a valid JSON Schema: %s", p.label, err)
		}
	}
	sort.Strings(out)
	return out
}

func nxWalkRefs(v any, visit func(string)) {
	switch t := v.(type) {
	case *nxObj:
		for _, k := range t.Keys() {
			if k == "$ref" || k == "$dynamicRef" {
				if s, ok := t.Get(k).(string); ok {
					visit(s)
				}
				continue
			}
			nxWalkRefs(t.Get(k), visit)
		}
	case []any:
		for _, item := range t {
			nxWalkRefs(item, visit)
		}
	}
}

// nxResolvesToSchema looks a same-document reference up in the document, as
// OBI-D-12 does: a JSON Pointer must land on a schema position.
func nxResolvesToSchema(doc *nxObj, ref string) bool {
	frag := strings.TrimPrefix(ref, "#")
	if !strings.HasPrefix(frag, "/") {
		return false
	}
	parts := strings.Split(frag[1:], "/")
	if len(parts) < 2 {
		return false
	}
	var cur any = doc
	for i, p := range parts {
		p = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		obj, ok := cur.(*nxObj)
		if !ok || !obj.Has(p) {
			return false
		}
		cur = obj.Get(p)
		if i == 0 && p != "schemas" && p != "operations" {
			return false
		}
	}
	switch cur.(type) {
	case *nxObj, bool:
		return parts[0] == "schemas" || len(parts) >= 3 && (parts[2] == "input" || parts[2] == "output")
	}
	return false
}

// nxMetaValid compiles a schema against the 2020-12 meta-schema, with the
// document's named schemas bundled so its references resolve.
func nxMetaValid(doc *nxObj, value any) string {
	schema := nxClone(value)
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
		return err.Error()
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("urn:ob-preview:meta", parsed); err != nil {
		return err.Error()
	}
	if _, err := c.Compile("urn:ob-preview:meta"); err != nil {
		// The most specific problem is the first one at the deepest level.
		lines := strings.Split(err.Error(), "\n")
		best, depth := lines[0], -1
		for _, l := range lines {
			trimmed := strings.TrimLeft(l, " ")
			if indent := len(l) - len(trimmed); strings.HasPrefix(trimmed, "- ") && indent > depth {
				best, depth = strings.TrimPrefix(trimmed, "- "), indent
			}
		}
		best = strings.Replace(best, "at '':", "at the top level:", 1)
		return nxQuotedAt.ReplaceAllString(best, "at $1")
	}
	return ""
}

// nxNewViolations lists the violations after has that before does not.
func nxNewViolations(before, after *nxObj) []string {
	had := map[string]bool{}
	for _, v := range nxViolations(before) {
		had[v] = true
	}
	var added []string
	for _, v := range nxViolations(after) {
		if !had[v] {
			added = append(added, v)
		}
	}
	return added
}
