package cmd

import (
	"fmt"
	"sort"
	"strings"
)

// The preview answers every command from this one sample document, modeled
// on the spec's own §4 example, and from a fixed pretend installation. The
// kinds are the spec's illustrative ones; nothing here claims how a
// published kind reads its content.
const nxFixtureJSON = `{
  "openbindings": "0.2.0",
  "name": "Task Manager",
  "version": "1.4.0",
  "description": "Create, list, and complete tasks.",
  "schemas": {
    "Task": {
      "type": "object",
      "properties": {
        "id": { "type": "string" },
        "title": { "type": "string" },
        "done": { "type": "boolean" }
      },
      "required": ["id", "title"]
    },
    "Problem": {
      "type": "object",
      "properties": { "problem": { "type": "string" } },
      "required": ["problem"],
      "additionalProperties": false
    }
  },
  "operations": {
    "createTask": {
      "description": "Create a new task.",
      "aliases": ["acme.tasks.createTask"],
      "input": {
        "type": "object",
        "properties": { "title": { "type": "string" } },
        "required": ["title"]
      },
      "output": {
        "anyOf": [{ "$ref": "#/schemas/Task" }, { "$ref": "#/schemas/Problem" }]
      },
      "examples": {
        "basic": {
          "input": { "title": "Write the docs" },
          "output": { "id": "t_1", "title": "Write the docs", "done": false }
        }
      }
    },
    "listTasks": {
      "description": "List all tasks.",
      "input": { "type": "object", "maxProperties": 0 },
      "output": { "type": "array", "items": { "$ref": "#/schemas/Task" } }
    },
    "completeTask": {
      "description": "Mark a task done.",
      "input": {
        "type": "object",
        "properties": { "id": { "type": "string" } },
        "required": ["id"]
      },
      "output": { "$ref": "#/schemas/Task" }
    },
    "events.deliver": {
      "description": "Deliver a task event to a subscriber.",
      "aliases": ["acme.events.deliver"],
      "input": { "type": "object" },
      "output": { "type": "object", "maxProperties": 0 }
    }
  },
  "dependencies": {
    "notifier": {
      "operation": "events.deliver",
      "kinds": ["example.openapi@1", "example.grpc@1"],
      "description": "Where task events are sent."
    }
  },
  "sources": {
    "httpApi": {
      "kind": "example.openapi@1",
      "content": { "location": "https://api.example.com/openapi.json" }
    },
    "mcpServer": {
      "kind": "example.mcp@1",
      "content": { "location": "https://api.example.com/mcp" }
    }
  },
  "bindings": {
    "createTask.http": {
      "operation": "createTask",
      "source": "httpApi",
      "content": { "target": "#/paths/~1tasks/post" },
      "preference": 10
    },
    "createTask.mcp": {
      "operation": "createTask",
      "source": "mcpServer",
      "content": { "target": "tools/create_task" }
    },
    "listTasks.http": {
      "operation": "listTasks",
      "source": "httpApi",
      "content": { "target": "#/paths/~1tasks/get" },
      "idempotent": true
    },
    "completeTask.http": {
      "operation": "completeTask",
      "source": "httpApi",
      "content": { "target": "#/paths/~1tasks~1{id}~1complete/post" },
      "idempotent": true
    }
  }
}`

// The contract the adopt and compat previews compare against.
const nxContractJSON = `{
  "openbindings": "0.2.0",
  "name": "Acme Tasks",
  "operations": {
    "acme.tasks.createTask": {
      "input": {
        "type": "object",
        "properties": { "title": { "type": "string" } },
        "required": ["title"]
      },
      "output": { "type": "object" }
    },
    "acme.tasks.listTasks": {
      "input": { "type": "object", "maxProperties": 0 },
      "output": { "type": "array" }
    },
    "acme.tasks.deleteTask": {
      "input": {
        "type": "object",
        "properties": { "id": { "type": "string" } },
        "required": ["id"]
      },
      "output": { "type": "object", "maxProperties": 0 }
    }
  }
}`

var (
	nxDocOrder       = []string{"openbindings", "name", "version", "description", "schemas", "operations", "dependencies", "sources", "bindings"}
	nxOperationOrder = []string{"description", "deprecated", "tags", "aliases", "input", "output", "examples"}
	nxExampleOrder   = []string{"description", "input", "output"}
	nxSourceOrder    = []string{"kind", "content", "description"}
	nxBindingOrder   = []string{"operation", "source", "content", "idempotent", "preference", "description", "deprecated"}
	nxDependOrder    = []string{"operation", "kinds", "description"}
)

func nxFixture() *nxObj  { return nxMustParse(nxFixtureJSON).(*nxObj) }
func nxContract() *nxObj { return nxMustParse(nxContractJSON).(*nxObj) }

// A part map of the document, created in canonical position when missing.
func nxPart(doc *nxObj, part string) *nxObj {
	if m := doc.Obj(part); m != nil {
		return m
	}
	m := nxNewObj()
	doc.SetCanon(part, m, nxDocOrder)
	return m
}

func nxPartKeys(doc *nxObj, part string) []string {
	if m := doc.Obj(part); m != nil {
		return m.Keys()
	}
	return nil
}

// nxResolveOperation finds an operation by key or alias, as OBI-T-07
// resolves names: exact match, key and alias equally.
func nxResolveOperation(doc *nxObj, name string) (string, bool) {
	ops := doc.Obj("operations")
	if ops == nil {
		return "", false
	}
	if ops.Has(name) {
		return name, true
	}
	for _, key := range ops.Keys() {
		for _, alias := range nxStrings(ops.Obj(key).Get("aliases")) {
			if alias == name {
				return key, true
			}
		}
	}
	return "", false
}

func nxStrings(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, t...)
	}
	return out
}

func nxToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Entries in part whose member field equals value, e.g. the bindings whose
// "operation" is createTask.
func nxReferrers(doc *nxObj, part, field, value string) []string {
	var out []string
	for _, key := range nxPartKeys(doc, part) {
		if s, _ := doc.Obj(part).Obj(key).Get(field).(string); s == value {
			out = append(out, key)
		}
	}
	return out
}

func nxSchemaReferrers(doc *nxObj, name string) []string {
	ref := "#/schemas/" + name
	var out []string
	for _, key := range nxPartKeys(doc, "operations") {
		if strings.Contains(nxCompact(doc.Obj("operations").Obj(key)), `"`+ref+`"`) {
			out = append(out, "operation "+key)
		}
	}
	for _, key := range nxPartKeys(doc, "schemas") {
		if key != name && strings.Contains(nxCompact(doc.Obj("schemas").Get(key)), `"`+ref+`"`) {
			out = append(out, "schema "+key)
		}
	}
	return out
}

// nxRewriteRefs rewrites every "$ref" equal to from, anywhere in v.
func nxRewriteRefs(v any, from, to string) {
	switch t := v.(type) {
	case *nxObj:
		for _, k := range t.Keys() {
			if k == "$ref" {
				if s, _ := t.Get(k).(string); s == from {
					t.Set(k, to)
				}
				continue
			}
			nxRewriteRefs(t.Get(k), from, to)
		}
	case []any:
		for _, item := range t {
			nxRewriteRefs(item, from, to)
		}
	}
}

// The pretend installation: which kinds ob can handle, for which roles, and
// through which handler.
type nxKindSupport struct {
	kind    string
	roles   []string
	handler string
}

var nxInstalledKinds = []nxKindSupport{
	{"example.openapi@1", []string{"invoke", "inspect", "synthesize"}, "built in"},
	{"example.mcp@1", []string{"invoke", "inspect", "synthesize"}, "built in"},
	{"example.grpc@1", []string{"invoke", "synthesize"}, "built in"},
	{"my-cli.usage@1", []string{"invoke"}, "delegate d_7f3a (Usage Tools)"},
	{"acme.billing-rpc@1", []string{"invoke"}, "delegate d_91c2 (Acme RPC)"},
}

var nxRoles = []string{"invoke", "inspect", "synthesize"}

func nxSupports(kind, role string) (string, bool) {
	for _, k := range nxInstalledKinds {
		if k.kind != kind {
			continue
		}
		for _, r := range k.roles {
			if r == role {
				return k.handler, true
			}
		}
	}
	return "", false
}

type nxDelegate struct {
	id, name    string
	roles       []string
	preferences map[string]int
}

var nxDelegates = []nxDelegate{
	{"d_7f3a", "Usage Tools", []string{"invoke"}, map[string]int{}},
	{"d_91c2", "Acme RPC", []string{"invoke", "inspect"}, map[string]int{"invoke": 10}},
}

var nxRoleInterfaces = map[string][]string{
	"invoke":     {"openbindings.binding-invoker.invokeBinding", "openbindings.binding-invoker.preflightBinding", "openbindings.binding-invoker.listSupportedKinds", "openbindings.binding-invoker.checkKindSupport"},
	"inspect":    {"openbindings.source-inspector.inspectSource", "openbindings.source-inspector.listSupportedKinds", "openbindings.source-inspector.checkKindSupport"},
	"synthesize": {"openbindings.interface-synthesizer.synthesizeInterface", "openbindings.interface-synthesizer.listSupportedKinds", "openbindings.interface-synthesizer.checkKindSupport"},
}

type nxContext struct {
	name  string
	holds []string
}

var nxContexts = []nxContext{
	{"default", []string{"bearer token ••••3f9a", "header X-Client: ob"}},
	{"staging", []string{"bearer token ••••c071", "config server = https://staging.example.com"}},
}

func nxValidRole(role string) error {
	for _, r := range nxRoles {
		if r == role {
			return nil
		}
	}
	return fmt.Errorf("unknown role %q; ob hands off three kinds of work: invoke, inspect, synthesize", role)
}

func nxSorted(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}
