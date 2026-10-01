package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
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
        "done": { "type": "boolean" },
        "due": { "$ref": "https://schemas.example.com/time/date-time.json" },
        "owner": { "$ref": "https://schemas.example.com/people/person.json" }
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
    "importTasks": {
      "description": "Import tasks, one per input value, and report how many arrived.",
      "input": {
        "type": "object",
        "properties": { "title": { "type": "string" } },
        "required": ["title"]
      },
      "output": {
        "type": "object",
        "properties": { "imported": { "type": "integer" } },
        "required": ["imported"]
      }
    },
    "watchTasks": {
      "description": "Report each task change as it happens.",
      "input": { "type": "object", "maxProperties": 0 },
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
    },
    "grpcApi": {
      "kind": "example.grpc@1",
      "content": { "location": "grpc://tasks.example.com:443" }
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
    },
    "importTasks.grpc": {
      "operation": "importTasks",
      "source": "grpcApi",
      "content": { "target": "tasks.TaskService/ImportTasks" }
    },
    "watchTasks.grpc": {
      "operation": "watchTasks",
      "source": "grpcApi",
      "content": { "target": "tasks.TaskService/WatchTasks" }
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
    },
    "acme.events.deliver": {
      "input": { "type": "object" },
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

// Schemas published elsewhere that the sample references, standing in for
// the network. person.json references contact.json relative to its $id.
var nxPublishedSchemas = map[string]string{
	"https://schemas.example.com/time/date-time.json": `{"$id":"https://schemas.example.com/time/date-time.json","type":"string","format":"date-time"}`,
	"https://schemas.example.com/people/person.json":  `{"$id":"https://schemas.example.com/people/person.json","type":"object","properties":{"name":{"type":"string"},"contact":{"$ref":"contact.json"}},"required":["name"]}`,
	"https://schemas.example.com/people/contact.json": `{"$id":"https://schemas.example.com/people/contact.json","type":"object","properties":{"email":{"type":"string"}}}`,
}

// nxPublishedLoader lets the schema compiler reach the published schemas.
type nxPublishedLoader struct{}

func (nxPublishedLoader) Load(u string) (any, error) {
	raw, ok := nxPublishedSchemas[strings.TrimSuffix(u, "#")]
	if !ok {
		return nil, fmt.Errorf("%s is not published", u)
	}
	return jsonschema.UnmarshalJSON(strings.NewReader(raw))
}

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

// nxDidYouMean suggests the operation name closest to a name that does not
// resolve: one differing only in case, or by at most two edits.
func nxDidYouMean(doc *nxObj, name string) string {
	best, bestDist := "", 3
	for _, key := range nxPartKeys(doc, "operations") {
		for _, cand := range append([]string{key}, nxStrings(doc.Obj("operations").Obj(key).Get("aliases"))...) {
			d := nxEditDistance(strings.ToLower(name), strings.ToLower(cand))
			if d < bestDist {
				best, bestDist = cand, d
			}
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("; did you mean %s?", best)
}

func nxEditDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
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

// The pretend installation has installed ob's local certificate authority
// (with ob ca install), so ob start --tls works.
var nxCAInstalled = true

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

// The pretend context store, keyed by the exact scope an engine asserts in
// its challenge.
type nxContext struct {
	scope string
	holds [][2]string // field, masked value
}

var nxContexts = []nxContext{
	{"https://api.example.com", [][2]string{{"bearerToken", "••••3f9a"}, {"headers.X-Client", "ob"}}},
	{"https://api.example.com/openapi.json", [][2]string{{"configuration.server", `{"url":"https://api.example.com"}`}}},
}

func nxStoredContext(scope string) (nxContext, bool) {
	for _, c := range nxContexts {
		if c.scope == scope {
			return c, true
		}
	}
	return nxContext{}, false
}

// What each sample binding's engine asks for, and how its interaction runs.
type nxNeed struct {
	scope, requirement, describe string
	durable                      bool
}

var nxBindingNeeds = map[string]nxNeed{
	"createTask.http":   {"https://api.example.com", "auth.bearer", "a bearer token", true},
	"listTasks.http":    {"https://api.example.com", "auth.bearer", "a bearer token", true},
	"completeTask.http": {"https://api.example.com", "auth.bearer", "a bearer token", true},
	"createTask.mcp":    {"https://api.example.com/mcp", "auth.oauth2", "an OAuth 2.0 access token", true},
}

var nxBindingShape = map[string]string{
	"importTasks.grpc": "client-stream",
	"watchTasks.grpc":  "server-stream",
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
