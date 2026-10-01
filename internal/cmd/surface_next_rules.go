package cmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"path"
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

// nxWalkRefs visits the references in the document resource. A schema that
// declares $id is a resource of its own: its references resolve against its
// base (spec §7.2), so the walk does not enter it.
func nxWalkRefs(v any, visit func(string)) {
	switch t := v.(type) {
	case *nxObj:
		if t.Has("$id") {
			return
		}
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

// An external schema the document references, and where.
type nxExternal struct {
	uri   string
	users []string
}

// nxExternalRefs lists the absolute URIs the document's schemas reference
// that no schema in the document declares as its $id. A reference inside a
// schema that declares $id resolves against that $id.
func nxExternalRefs(doc *nxObj) []nxExternal {
	var out []nxExternal
	index := map[string]int{}
	note := func(uri, user string) {
		if nxDeclaresID(doc, uri) {
			return
		}
		i, ok := index[uri]
		if !ok {
			index[uri] = len(out)
			out = append(out, nxExternal{uri: uri})
			i = len(out) - 1
		}
		if !nxContains(out[i].users, user) {
			out[i].users = append(out[i].users, user)
		}
	}
	for _, key := range nxPartKeys(doc, "schemas") {
		for _, u := range nxRefsIn(doc.Obj("schemas").Get(key), "") {
			note(u, "schema "+key)
		}
	}
	for _, key := range nxPartKeys(doc, "operations") {
		op := doc.Obj("operations").Obj(key)
		for _, side := range []string{"input", "output"} {
			if op != nil && op.Has(side) {
				for _, u := range nxRefsIn(op.Get(side), "") {
					note(u, key+" "+side)
				}
			}
		}
	}
	return out
}

// nxRefsIn lists the absolute URIs (without fragments) a schema references,
// resolving each reference against the nearest enclosing $id.
func nxRefsIn(v any, base string) []string {
	var out []string
	var walk func(v any, base string)
	walk = func(v any, base string) {
		switch t := v.(type) {
		case *nxObj:
			if id, ok := t.Get("$id").(string); ok {
				base = nxResolveURI(base, id)
			}
			for _, k := range t.Keys() {
				if k == "$ref" || k == "$dynamicRef" {
					ref, _ := t.Get(k).(string)
					if strings.HasPrefix(ref, "#") && base == "" {
						continue
					}
					if u := strings.SplitN(nxResolveURI(base, ref), "#", 2)[0]; nxURIScheme.MatchString(u) && !nxContains(out, u) && u != strings.SplitN(base, "#", 2)[0] {
						out = append(out, u)
					}
					continue
				}
				walk(t.Get(k), base)
			}
		case []any:
			for _, item := range t {
				walk(item, base)
			}
		}
	}
	walk(v, base)
	return out
}

func nxResolveURI(base, ref string) string {
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if base == "" {
		return r.String()
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// nxDeclaresID says whether a schema in the document declares this $id.
func nxDeclaresID(doc *nxObj, uri string) bool {
	found := false
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case *nxObj:
			if id, ok := t.Get("$id").(string); ok && strings.TrimSuffix(id, "#") == uri {
				found = true
			}
			for _, k := range t.Keys() {
				walk(t.Get(k))
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(doc.Get("schemas"))
	walk(doc.Get("operations"))
	return found
}

// nxSchemaNameFor names an embedded schema from its URI: the last path
// segment without its extension, made unique.
func nxSchemaNameFor(doc *nxObj, uri string) string {
	name := uri
	if u, err := url.Parse(uri); err == nil {
		name = path.Base(u.Path)
	}
	name = strings.TrimSuffix(name, path.Ext(name))
	if name == "" || !v02NamePattern.MatchString(name) {
		name = "external"
	}
	candidate := name
	for i := 2; doc.Obj("schemas") != nil && doc.Obj("schemas").Has(candidate); i++ {
		candidate = fmt.Sprintf("%s-%d", name, i)
	}
	return candidate
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
		// A schema with its own $id is checked as itself, not also as a
		// definition beside itself.
		if id, ok := obj.Get("$id").(string); ok {
			for _, k := range defs.Keys() {
				if d, isObj := defs.Get(k).(*nxObj); isObj && d.Get("$id") == id {
					defs.Delete(k)
				}
			}
		}
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
	c.UseLoader(nxPublishedLoader{})
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
