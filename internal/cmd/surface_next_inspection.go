package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Self-contained lab adaptations of ob's pinned admission requirements.
// See surface_next_contracts/README.md for the exact source and adaptations.
//
//go:embed surface_next_contracts/*.json
var nxRoleContractsFS embed.FS

var nxRoleRequirements = func() map[string]*nxObj {
	out := map[string]*nxObj{}
	for _, role := range nxRoles {
		raw, err := nxRoleContractsFS.ReadFile("surface_next_contracts/" + role + ".json")
		if err != nil {
			panic(err)
		}
		out[role] = nxMustParse(string(raw)).(*nxObj)
	}
	return out
}()

const nxRolePolicy = "Admission records compatibility, not trust or activation. Runtime requires exact kind support, recipient authorization, and an executable binding. Higher preference orders eligible external delegates; equal preferences retain registration order in ob. Native precedence depends on the entrypoint; no workload failover. Delegates resolve their own context."

var nxRolePurposes = map[string]string{
	"invoke":     "Invoke bindings supplied by value through the Binding Invoker frame protocol.",
	"inspect":    "Inspect a source by value for targets admitted by its governing kind.",
	"synthesize": "Produce an OBI from supplied sources.",
}

func nxProviderInterface(name string, roles []string) *nxObj {
	ops, schemas, bindings := nxNewObj(), nxNewObj(), nxNewObj()
	for _, role := range roles {
		required := nxRoleRequirements[role]
		for _, key := range required.Obj("schemas").Keys() {
			schemas.Set(key, nxClone(required.Obj("schemas").Get(key)))
		}
		for _, key := range required.Obj("operations").Keys() {
			ops.Set(key, nxClone(required.Obj("operations").Get(key)))
			bindings.Set(key+".http", nxNewObj().Set("operation", key).Set("source", "transport").Set("content", nxNewObj().Set("path", "/"+key)))
		}
	}
	return nxNewObj().Set("openbindings", "0.2.0").Set("name", name).Set("version", "1.0.0").
		Set("schemas", schemas).Set("operations", ops).
		Set("sources", nxNewObj().Set("transport", nxNewObj().Set("kind", "example.openapi@1").Set("content", nxNewObj().Set("url", "https://tools.example.com/openapi.json")))).
		Set("bindings", bindings).Set("x-lab", nxNewObj().Set("retained", true).Set("revision", json.Number("0")))
}

func (d nxDelegate) report() *nxObj {
	prefs := nxNewObj()
	for _, role := range d.roles {
		if p, ok := d.preferences[role]; ok {
			prefs.Set(role, p)
		}
	}
	return nxNewObj().Set("id", d.id).Set("name", d.name).Set("interface", nxClone(d.iface)).
		Set("roles", nxToAny(d.roles)).Set("rolePreferences", prefs)
}

func nxUniqueRoles(roles []string, flag string) error {
	seen := map[string]bool{}
	for _, role := range roles {
		if err := nxValidRole(role); err != nil {
			return nxUsageErr("%v", err)
		}
		if seen[role] {
			return nxUsageErr("--%s repeats role %q; give each role once", flag, role)
		}
		seen[role] = true
	}
	return nil
}

// Read only the whole-value inputs needed for inspection/reuse in this pass.
// Never include parser errors or input contents in diagnostics: Context may
// contain secrets. Other preview value flags still use marked placeholders.
func (c *nxCtx) readJSONObject(raw, label string) (*nxObj, error) {
	var data []byte
	var err error
	if raw == "-" {
		data, err = io.ReadAll(c.cmd.InOrStdin())
	} else if strings.HasPrefix(raw, "@") && len(raw) > 1 {
		data, err = os.ReadFile(raw[1:])
	} else {
		return nil, nxUsageErr("%s takes @FILE or - for one JSON object", label)
	}
	if err != nil {
		return nil, nxFail(1, "%s: could not read the JSON input", label)
	}
	if !json.Valid(data) {
		return nil, nxUsageErr("%s: expected exactly one JSON object", label)
	}
	value, err := nxParse(string(data))
	obj, ok := value.(*nxObj)
	if err != nil || !ok {
		return nil, nxUsageErr("%s: expected exactly one JSON object", label)
	}
	return obj, nil
}

func (c *nxCtx) delegateInterface(raw string) (*nxObj, error) {
	if raw != "-" {
		c.note(fmt.Sprintf("(preview: %s was not read; using Acme Tools' sample OBI)", raw))
		return nxProviderInterface("Acme Tools", nxRoles), nil
	}
	obi, err := c.readJSONObject(raw, "delegate OBI")
	if err != nil {
		return nil, err
	}
	if violations := nxViolations(obi); len(violations) > 0 {
		// Even a document-rule error can quote an embedded secret value.
		return nil, nxRefuse("the supplied delegate OBI breaks a document rule; nothing was registered or changed")
	}
	c.note("(preview: retained the supplied OBI; role schema compatibility and runtime eligibility are not evaluated)")
	return obi, nil
}

// Mask complete values at the opaque-field boundary. Preserve names of the
// known named maps without interpreting dots or recursively exposing data.
func nxMaskContext(context *nxObj) *nxObj {
	masked := nxNewObj()
	for _, key := range context.Keys() {
		value := any(nxNewObj().Set("masked", true))
		if nxContains([]string{"headers", "credentials", "cookies", "configuration", "apiKeys", "environment", "metadata"}, key) {
			if named := context.Obj(key); named != nil {
				entries := nxNewObj()
				for _, name := range named.Keys() {
					entries.Set(name, nxNewObj().Set("masked", true))
				}
				value = entries
			}
		}
		masked.Set(key, value)
	}
	return masked
}

func nxContextFields(context *nxObj) []string {
	fields := []string{}
	if context == nil {
		return fields
	}
	for _, key := range context.Keys() {
		if nxContains([]string{"headers", "credentials", "cookies", "configuration"}, key) && context.Obj(key) != nil {
			for _, name := range context.Obj(key).Keys() {
				fields = append(fields, key+"."+name)
			}
		} else {
			fields = append(fields, key)
		}
	}
	return fields
}

func (ctx nxContext) hasField(field string) bool {
	if ctx.resolver != nil && ctx.resolver.Has(field) {
		return true
	}
	if ctx.context == nil {
		return false
	}
	// Only a well-known map prefix establishes nesting. The rest is a
	// literal member name, including any dots it contains.
	key, name, nested := strings.Cut(field, ".")
	if nested && nxContains([]string{"headers", "credentials", "cookies", "configuration"}, key) {
		return ctx.context.Obj(key) != nil && ctx.context.Obj(key).Has(name)
	}
	return ctx.context.Has(field)
}

func nxContextRows(value *nxObj, reveal bool) [][]string {
	var rows [][]string
	for _, key := range value.Keys() {
		if nxContains([]string{"headers", "credentials", "cookies", "configuration"}, key) && value.Obj(key) != nil {
			for _, name := range value.Obj(key).Keys() {
				rows = append(rows, nxContextRow(key+"."+name, value.Obj(key).Get(name), reveal))
			}
		} else {
			rows = append(rows, nxContextRow(key, value.Get(key), reveal))
		}
	}
	return rows
}

func nxContextRow(field string, value any, reveal bool) []string {
	text := "••••"
	if reveal {
		if s, ok := value.(string); ok {
			text = s
		} else {
			text = nxCompact(value)
		}
	}
	return []string{"  " + nxContextFlagField(field), text}
}

func nxSourceListEntry(doc *nxObj, key string) *nxObj {
	kind := doc.Obj("sources").Obj(key).Get("kind").(string)
	_, can := nxSupports(kind, "invoke")
	return nxNewObj().Set("name", key).Set("kind", kind).
		Set("bindingCount", len(nxReferrers(doc, "bindings", "source", key))).Set("canInvoke", can)
}

func nxBindingListEntry(doc *nxObj, key string) *nxObj {
	b := doc.Obj("bindings").Obj(key)
	source := b.Get("source").(string)
	out := nxNewObj().Set("name", key).Set("operation", b.Get("operation")).Set("source", source).
		Set("kind", doc.Obj("sources").Obj(source).Get("kind"))
	for _, field := range []string{"preference", "idempotent", "deprecated"} {
		if b.Has(field) {
			out.Set(field, nxClone(b.Get(field)))
		}
	}
	return out
}
