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
		if err := nxMetaValid(p.value); err != "" {
			add("OBI-D-10", "%s is not a valid JSON Schema: %s", p.label, err)
		}
	}
	sort.Strings(out)
	return out
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

// The keywords whose values are schemas, as OBI positions follow them
// (spec §7): maps of schemas, arrays of schemas, and single schemas. Values
// under any other keyword, such as const, enum, and examples, are data.
var (
	nxSchemaMapKeywords    = map[string]bool{"$defs": true, "definitions": true, "properties": true, "patternProperties": true, "dependentSchemas": true, "dependencies": true}
	nxSchemaArrayKeywords  = map[string]bool{"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true}
	nxSchemaSingleKeywords = map[string]bool{"additionalProperties": true, "propertyNames": true, "items": true, "contains": true, "not": true, "if": true, "then": true, "else": true, "unevaluatedItems": true, "unevaluatedProperties": true, "contentSchema": true}
)

func nxIsSchema(v any) bool {
	switch v.(type) {
	case *nxObj, bool:
		return true
	}
	return false
}

// nxSubschemas lists the schemas directly inside a schema, each with the
// pointer tokens that reach it.
func nxSubschemas(s *nxObj) []struct {
	tokens []string
	schema any
} {
	var out []struct {
		tokens []string
		schema any
	}
	add := func(v any, tokens ...string) {
		if nxIsSchema(v) {
			out = append(out, struct {
				tokens []string
				schema any
			}{tokens, v})
		}
	}
	for _, k := range s.Keys() {
		v := s.Get(k)
		switch {
		case nxSchemaMapKeywords[k]:
			if m, ok := v.(*nxObj); ok {
				for _, name := range m.Keys() {
					add(m.Get(name), k, name)
				}
			}
		case nxSchemaArrayKeywords[k] || k == "items":
			if arr, ok := v.([]any); ok {
				for i, item := range arr {
					add(item, k, fmt.Sprint(i))
				}
			} else if nxSchemaSingleKeywords[k] {
				add(v, k)
			}
		case nxSchemaSingleKeywords[k]:
			add(v, k)
		}
	}
	return out
}

// nxWalkSchema visits a schema and every schema inside it. visit returns
// false to stop the walk from entering a schema's subschemas.
func nxWalkSchema(v any, visit func(obj *nxObj) bool) {
	obj, ok := v.(*nxObj)
	if !ok || !visit(obj) {
		return
	}
	for _, sub := range nxSubschemas(obj) {
		nxWalkSchema(sub.schema, visit)
	}
}

// An OBI position: where the document model puts a schema.
type nxPosition struct {
	label string
	value any
}

func nxPositions(doc *nxObj) []nxPosition {
	var out []nxPosition
	for _, key := range nxPartKeys(doc, "schemas") {
		out = append(out, nxPosition{"schema " + key, doc.Obj("schemas").Get(key)})
	}
	for _, key := range nxPartKeys(doc, "operations") {
		op := doc.Obj("operations").Obj(key)
		for _, side := range []string{"input", "output"} {
			if op != nil && op.Has(side) {
				out = append(out, nxPosition{key + " " + side, op.Get(side)})
			}
		}
	}
	return out
}

// nxWalkRefs visits the references in the document resource. A schema that
// declares $id is a resource of its own: its references resolve against its
// base (spec §7.2), so the walk does not enter it.
func nxWalkRefs(v any, visit func(string)) {
	nxWalkSchema(v, func(obj *nxObj) bool {
		if obj.Has("$id") {
			return false
		}
		for _, k := range []string{"$ref", "$dynamicRef"} {
			if ref, ok := obj.Get(k).(string); ok {
				visit(ref)
			}
		}
		return true
	})
}

// nxRefsIn lists the absolute URIs (without fragments) a schema references,
// resolving each reference against the nearest enclosing $id.
func nxRefsIn(v any, base string) []string {
	var out []string
	var walk func(v any, base string)
	walk = func(v any, base string) {
		obj, ok := v.(*nxObj)
		if !ok {
			return
		}
		if id, ok := obj.Get("$id").(string); ok {
			base = nxResolveURI(base, id)
		}
		for _, k := range []string{"$ref", "$dynamicRef"} {
			ref, ok := obj.Get(k).(string)
			if !ok || strings.HasPrefix(ref, "#") && base == "" {
				continue
			}
			u := strings.SplitN(nxResolveURI(base, ref), "#", 2)[0]
			if nxURIScheme.MatchString(u) && !nxContains(out, u) && u != strings.SplitN(base, "#", 2)[0] {
				out = append(out, u)
			}
		}
		for _, sub := range nxSubschemas(obj) {
			walk(sub.schema, base)
		}
	}
	walk(v, base)
	return out
}

// nxDeclaresID says whether a schema in the document declares this $id.
func nxDeclaresID(doc *nxObj, uri string) bool {
	found := false
	for _, p := range nxPositions(doc) {
		nxWalkSchema(p.value, func(obj *nxObj) bool {
			if id, ok := obj.Get("$id").(string); ok && strings.TrimSuffix(id, "#") == uri {
				found = true
			}
			return true
		})
	}
	return found
}

// nxFragment decodes a same-document reference's fragment (spec §7.2).
func nxFragment(ref string) (string, bool) {
	frag, err := url.PathUnescape(strings.TrimPrefix(ref, "#"))
	return frag, err == nil
}

// nxSchemaRefName says which named schema a same-document reference names,
// if it names one directly ("#/schemas/<name>", however it is encoded).
func nxSchemaRefName(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "#") {
		return "", false
	}
	frag, ok := nxFragment(ref)
	if !ok || !strings.HasPrefix(frag, "/schemas/") {
		return "", false
	}
	rest := strings.TrimPrefix(frag, "/schemas/")
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return strings.ReplaceAll(strings.ReplaceAll(rest, "~1", "/"), "~0", "~"), true
}

// nxResolvesToSchema looks a same-document reference up in the document, as
// OBI-D-12 does: a JSON Pointer must land on a schema position, and not
// inside a schema that declares $id; a plain name must be declared by a
// schema in the document resource.
func nxResolvesToSchema(doc *nxObj, ref string) bool {
	frag, ok := nxFragment(ref)
	if !ok || frag == "" {
		return false
	}
	if !strings.HasPrefix(frag, "/") {
		found := false
		for _, p := range nxPositions(doc) {
			nxWalkSchema(p.value, func(obj *nxObj) bool {
				if obj.Has("$id") {
					return false
				}
				found = found || obj.Get("$anchor") == frag || obj.Get("$dynamicAnchor") == frag
				return true
			})
		}
		return found
	}
	tokens := strings.Split(frag[1:], "/")
	for i := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(tokens[i], "~1", "/"), "~0", "~")
	}
	var cur any
	switch {
	case len(tokens) >= 2 && tokens[0] == "schemas" && doc.Obj("schemas") != nil && doc.Obj("schemas").Has(tokens[1]):
		cur, tokens = doc.Obj("schemas").Get(tokens[1]), tokens[2:]
	case len(tokens) >= 3 && tokens[0] == "operations" && (tokens[2] == "input" || tokens[2] == "output") &&
		doc.Obj("operations") != nil && doc.Obj("operations").Has(tokens[1]) && doc.Obj("operations").Obj(tokens[1]).Has(tokens[2]):
		cur, tokens = doc.Obj("operations").Obj(tokens[1]).Get(tokens[2]), tokens[3:]
	default:
		return false
	}
	for len(tokens) > 0 {
		obj, ok := cur.(*nxObj)
		if !ok || obj.Has("$id") {
			return false
		}
		next := false
		for _, sub := range nxSubschemas(obj) {
			if len(sub.tokens) <= len(tokens) && strings.Join(sub.tokens, "/") == strings.Join(tokens[:len(sub.tokens)], "/") {
				cur, tokens, next = sub.schema, tokens[len(sub.tokens):], true
				break
			}
		}
		if !next {
			return false
		}
	}
	return nxIsSchema(cur)
}

// The JSON Schema 2020-12 meta-schema, which the schema library carries, so
// the check runs offline.
var nxMetaSchema = func() *jsonschema.Schema {
	s, err := jsonschema.NewCompiler().Compile("https://json-schema.org/draft/2020-12/schema")
	if err != nil {
		panic(err)
	}
	return s
}()

// nxMetaValid checks a schema against the 2020-12 meta-schemas, as OBI-D-10
// does: with format as an annotation, and resolving none of the document's
// references.
func nxMetaValid(value any) string {
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(nxCompact(value)))
	if err != nil {
		return err.Error()
	}
	if err := nxMetaSchema.Validate(inst); err != nil {
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
