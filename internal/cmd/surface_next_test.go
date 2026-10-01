package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/shlex"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
)

var nxVariants = []string{"", "filter-edits", "binding-invoke", "adopt"}

func nxExec(variant string, args ...string) (string, string, error) {
	return nxExecIn(variant, "", args...)
}

// nxExecIn runs a command with stdin holding the given text.
func nxExecIn(variant, stdin string, args ...string) (string, string, error) {
	root := NewNextSurfaceRoot(variant)
	var out, errb bytes.Buffer
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&errb)
	if err := NextPreflightArgs(root, args); err != nil {
		return "", "", err
	}
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errb.String(), err
}

func nxExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 2
}

func nxWalk(root *cobra.Command, visit func(*cobra.Command)) {
	visit(root)
	for _, child := range root.Commands() {
		nxWalk(child, visit)
	}
}

// nxExampleCommands turns a help Example into argument lists: continuation
// lines are joined, and only the ob command before a pipe or redirect runs.
// nxExample is one ob command from a help example, with the stdin a printf
// before it in the pipeline would give it.
type nxExample struct {
	args  []string
	stdin string
}

func nxExamples(t *testing.T, example string) []nxExample {
	var out []nxExample
	joined := strings.ReplaceAll(example, "\\\n", " ")
	for _, line := range strings.Split(joined, "\n") {
		line = strings.TrimSpace(line)
		stdin := ""
		for _, segment := range strings.Split(line, " | ") {
			segment = strings.TrimSpace(segment)
			if strings.HasPrefix(segment, "printf '") && strings.HasSuffix(segment, "'") {
				stdin = strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(segment, "printf '"), "'"), `\n`, "\n")
				continue
			}
			if !strings.HasPrefix(segment, "ob ") {
				stdin = ""
				continue
			}
			if i := strings.Index(segment, " > "); i >= 0 {
				segment = segment[:i]
			}
			words, err := shlex.Split(segment)
			if err != nil {
				t.Fatalf("split %q: %v", segment, err)
			}
			out = append(out, nxExample{words[1:], stdin})
			stdin = ""
		}
	}
	return out
}

func nxExampleCommands(t *testing.T, example string) [][]string {
	var out [][]string
	for _, ex := range nxExamples(t, example) {
		out = append(out, ex.args)
	}
	return out
}

func TestNextEveryExampleRuns(t *testing.T) {
	for _, variant := range nxVariants {
		nxWalk(NewNextSurfaceRoot(variant), func(cmd *cobra.Command) {
			for _, ex := range nxExamples(t, cmd.Example) {
				_, _, err := nxExecIn(variant, ex.stdin, ex.args...)
				if code := nxExitCode(err); code == 2 {
					t.Errorf("variant %q: example `ob %s` failed as a usage error: %v", variant, strings.Join(ex.args, " "), err)
				}
			}
		})
	}
}

func TestNextEveryCommandIsDocumented(t *testing.T) {
	nxWalk(NewNextSurfaceRoot(""), func(cmd *cobra.Command) {
		if cmd.Name() == "help" || strings.HasPrefix(cmd.CommandPath(), "ob completion") || cmd.IsAdditionalHelpTopicCommand() {
			return
		}
		if cmd.Short == "" {
			t.Errorf("%s has no short description", cmd.CommandPath())
		}
		if cmd.HasSubCommands() {
			return
		}
		if cmd.Annotations["surface-id"] == "" {
			t.Errorf("%s is a leaf without a surface id", cmd.CommandPath())
		}
		if cmd.Example == "" {
			t.Errorf("%s has no example", cmd.CommandPath())
		}
	})
}

func nxSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "openbindings-0.2.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("https://openbindings.com/schema/openbindings-0.2.json", raw); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("https://openbindings.com/schema/openbindings-0.2.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func nxConforms(t *testing.T, schema *jsonschema.Schema, label, text string) {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(text))
	if err != nil {
		t.Errorf("%s: output is not one JSON document: %v\n%s", label, err, text)
		return
	}
	if err := schema.Validate(doc); err != nil {
		t.Errorf("%s: result fails the 0.2 schema: %v", label, err)
	}
	// OBI-D-04: operation keys and aliases form one namespace.
	parsed, err := nxParse(text)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	ops, _ := parsed.(*nxObj)
	if ops = ops.Obj("operations"); ops == nil {
		return
	}
	for _, key := range ops.Keys() {
		for _, name := range append([]string{key}, nxStrings(ops.Obj(key).Get("aliases"))...) {
			if seen[name] {
				t.Errorf("%s: two operations answer to %q (OBI-D-04)", label, name)
			}
			seen[name] = true
		}
	}
}

// Every edit, run with - as the document, prints its result; each result
// must still be a document the current 0.2 schema accepts.
func TestNextEditsKeepDocumentsConformant(t *testing.T) {
	schema := nxSchema(t)
	nxConforms(t, schema, "the sample document", nxFixtureJSON)
	nxConforms(t, schema, "the sample contract", nxContractJSON)
	cases := [][]string{
		{"operation", "add", "-", "archiveTask", "--description", "Archive a task.", "--input-schema", `{"type":"object"}`, "--output-schema", `{"$ref":"#/schemas/Task"}`, "--alias", "acme.tasks.archiveTask", "--tag", "admin", "--deprecated"},
		{"operation", "set", "-", "createTask", "--add-alias", "x.createTask", "--remove-alias", "acme.tasks.createTask", "--add-tag", "core", "--deprecated=false", "--unset", "examples"},
		{"operation", "rename", "-", "createTask", "addTask"},
		{"set", "-", "--interface-version", "2.0.0", "--unset", "description"},
		{"operation", "example", "set", "-", "createTask", "basic", "--input", `{"title":"Plan"}`, "--unset", "output"},
		{"operation", "example", "rename", "-", "createTask", "basic", "minimal"},
		{"source", "pull", "-", "httpApi", "--target", "GET /health"},
		{"source", "pull", "-", "mcpServer", "--target", "tools/list_tasks"},
		{"operation", "rename", "-", "listTasks", "allTasks"},
		{"operation", "remove", "-", "createTask", "--cascade"},
		{"operation", "example", "add", "-", "listTasks", "empty", "--input", "{}", "--output", "[]"},
		{"operation", "example", "remove", "-", "createTask", "basic"},
		{"source", "add", "-", "billing", "--kind", "acme.billing-rpc@1", "--content", "null", "--description", "Billing RPC."},
		{"source", "set", "-", "httpApi", "--kind", "example.openapi@2", "--unset", "content"},
		{"source", "rename", "-", "httpApi", "restApi"},
		{"source", "remove", "-", "mcpServer", "--cascade"},
		{"source", "import", "-", "billing", "https://billing.example.com/openapi.json", "--kind", "example.openapi@1"},
		{"source", "pull", "-", "httpApi", "--update-operation", "listTasks"},
		{"source", "pull", "-", "mcpServer", "httpApi", "--all-targets"},
		{"source", "pull", "-", "httpApi"},
		{"source", "pull", "-", "mcpServer", "--target", "tools/complete_task", "--operation", "completeTask"},
		{"source", "pull", "-", "httpApi", "--target", "#/paths/~1health/get"},
		{"operation", "set", "-", "completeTask", "--unset", "output-schema"},
		{"operation", "example", "add", "-", "listTasks", "note", "--description", "Lists everything."},
		{"binding", "add", "-", "completeTask.mcp", "--operation", "completeTask", "--source", "mcpServer", "--content", `{"target":"tools/complete_task"}`, "--idempotent=false", "--preference", "-3", "--description", "Via MCP.", "--deprecated"},
		{"binding", "set", "-", "createTask.mcp", "--unset", "content", "--preference", "7"},
		{"binding", "rename", "-", "createTask.http", "createTask.rest"},
		{"binding", "remove", "-", "createTask.mcp"},
		{"dependency", "add", "-", "audit", "--operation", "acme.events.deliver", "--kind", "k.one@1", "--kind", "k.two@1", "--description", "Audit sink."},
		{"dependency", "set", "-", "notifier", "--add-kind", "example.mcp@1", "--remove-kind", "example.grpc@1"},
		{"dependency", "rename", "-", "notifier", "sink"},
		{"dependency", "remove", "-", "notifier"},
		{"schema", "add", "-", "TaskList", "--value", `{"type":"array","items":{"$ref":"#/schemas/Task"}}`},
		{"schema", "set", "-", "Problem", "--value", "true"},
		{"schema", "rename", "-", "Task", "Todo"},
		{"schema", "bundle", "-"},
		{"schema", "bundle", "-", "https://schemas.example.com/people/person.json"},
		{"patch", "-", "changes.json"},
		{"merge", "-", "other.obi.json", "--theirs"},
		{"merge", "-", "acme-tasks.obi.json", "--ours"},
		{"merge", "-", "acme-tasks.obi.json", "--operation", "acme.tasks.deleteTask", "--no-bindings"},
	}
	covered := map[string]bool{}
	for _, args := range cases {
		out, _, err := nxExec("", args...)
		if err != nil {
			t.Errorf("ob %s: %v", strings.Join(args, " "), err)
			continue
		}
		nxConforms(t, schema, "ob "+strings.Join(args, " "), out)
		root := NewNextSurfaceRoot("")
		if leaf, _, err := root.Find(args); err == nil {
			covered[leaf.Annotations["surface-id"]] = true
		}
	}
	nxWalk(NewNextSurfaceRoot(""), func(cmd *cobra.Command) {
		// schema remove succeeds only on an unreferenced schema, and every
		// schema in the sample is referenced; its refusal is tested below.
		if cmd.Annotations["edits"] == "true" && !covered[cmd.Annotations["surface-id"]] && cmd.Annotations["surface-id"] != "schema.remove" {
			t.Errorf("no conformance case exercises %s", cmd.CommandPath())
		}
	})
	adopt := []string{"adopt", "-", "contract.obi.json", "--as", "acme.tasks.listTasks=listTasks"}
	if out, _, err := nxExec("adopt", adopt...); err != nil {
		t.Errorf("ob %s: %v", strings.Join(adopt, " "), err)
	} else {
		nxConforms(t, schema, "ob "+strings.Join(adopt, " "), out)
	}
	for _, args := range [][]string{
		{"init", "--name", "Task Manager", "--interface-version", "1.0.0"},
		{"synthesize", "./openapi.json", "--kind", "example.openapi@1"},
		{"fetch", "https://api.example.com"},
		{"show", "tasks.obi.json", "-F", "json"},
	} {
		out, _, err := nxExec("", args...)
		if err != nil {
			t.Errorf("ob %s: %v", strings.Join(args, " "), err)
			continue
		}
		nxConforms(t, schema, "ob "+strings.Join(args, " "), out)
	}
}

func TestNextFilterEditsPrintTheSameDocument(t *testing.T) {
	args := []string{"binding", "add", "tasks.obi.json", "x.http", "--operation", "listTasks", "--source", "httpApi"}
	filtered, _, err := nxExec("filter-edits", args...)
	if err != nil {
		t.Fatal(err)
	}
	args[2] = "-"
	piped, _, err := nxExec("", args...)
	if err != nil {
		t.Fatal(err)
	}
	if filtered != piped {
		t.Errorf("filter-edits and - disagree:\n%s\n---\n%s", filtered, piped)
	}
}

func TestNextJSONOutputsParse(t *testing.T) {
	nxWalk(NewNextSurfaceRoot(""), func(cmd *cobra.Command) {
		if cmd.Flags().Lookup("format") == nil || !strings.Contains(cmd.Flags().Lookup("format").Usage, "json") {
			return
		}
		path := strings.Fields(strings.TrimPrefix(cmd.CommandPath(), "ob "))
		for _, args := range nxExampleCommands(t, cmd.Example) {
			if len(args) < len(path) || strings.Join(args[:len(path)], " ") != strings.Join(path, " ") {
				continue // another command in the example's pipeline
			}
			out, _, err := nxExec("", append(args, "-F", "json")...)
			if nxExitCode(err) == 2 {
				t.Errorf("ob %s -F json: %v", strings.Join(args, " "), err)
				continue
			}
			if out == "" {
				continue
			}
			var v any
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Errorf("ob %s -F json printed something that is not JSON: %v\n%s", strings.Join(args, " "), err, out)
			}
		}
	})
}

func TestNextNearMissesPointToTheCommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"new"}, "ob init"},
		{[]string{"get", "x"}, "ob show"},
		{[]string{"call", "x", "y"}, "ob invoke"},
		{[]string{"operations", "list"}, "ob operation"},
		{[]string{"conform"}, "ob compat"},
		{[]string{"correspond"}, "ob compat"},
		{[]string{"adopt"}, "ob compat"},
		{[]string{"resolve", "https://x"}, "ob fetch"},
		{[]string{"serve"}, "ob start"},
		{[]string{"binding-specs"}, "ob kind"},
		{[]string{"operation", "get", "x", "y"}, "ob operation show"},
		{[]string{"binding", "update", "x", "y"}, "ob binding set"},
		{[]string{"source", "rm", "x", "y"}, "ob source remove"},
		{[]string{"schema", "mv", "x", "a", "b"}, "ob schema rename"},
	} {
		_, _, err := nxExec("", tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ob %s: got %v, want a hint naming %q", strings.Join(tc.args, " "), err, tc.want)
		}
	}
}

// Merging from a contract never adds a name one of your operations already
// answers to, and compat names both remedies for what is missing.
func TestNextContractsAreMetWithAliasesAndMerge(t *testing.T) {
	out, _, err := nxExec("", "merge", "-", "acme-tasks.obi.json", "--ours")
	if err != nil {
		t.Fatal(err)
	}
	doc := nxMustParse(out).(*nxObj)
	if doc.Obj("operations").Has("acme.tasks.createTask") {
		t.Error("merge added acme.tasks.createTask, which createTask already answers to")
	}
	for _, name := range []string{"acme.tasks.listTasks", "acme.tasks.deleteTask"} {
		if !doc.Obj("operations").Has(name) {
			t.Errorf("merge did not add %s", name)
		}
	}
	report, _, err := nxExec("", "compat", "tasks.obi.json", "acme-tasks.obi.json")
	if nxExitCode(err) != 1 {
		t.Fatalf("compat exit %d, want 1", nxExitCode(err))
	}
	for _, want := range []string{
		"ob operation set tasks.obi.json <operation> --add-alias acme.tasks.listTasks",
		"ob merge tasks.obi.json acme-tasks.obi.json --operation acme.tasks.deleteTask",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("compat does not suggest %q:\n%s", want, report)
		}
	}
	if _, _, err := nxExec("", "adopt", "a", "b"); err == nil {
		t.Error("adopt is still in the default tree")
	}
}

func TestNextRefusalsExitWithTheirOwnStatus(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"operation", "remove", "tasks.obi.json", "createTask"}, 3},
		{[]string{"operation", "add", "tasks.obi.json", "createTask"}, 3},
		{[]string{"invoke", "tasks.obi.json", "events.deliver", "--input", "{}"}, 3},
		{[]string{"invoke", "tasks.obi.json", "createTask", "--input", "{}"}, 3},
		{[]string{"validate", "tasks.obi.json", "--operation", "createTask", "--input", `{"title":5}`}, 1},
		{[]string{"kind", "check", "example.openapi@2", "--role", "invoke"}, 1},
		{[]string{"schema", "remove", "tasks.obi.json", "Task"}, 3},
		{[]string{"kind", "check", "nope@1"}, 1},
		{[]string{"dependency", "set", "tasks.obi.json", "notifier", "--remove-kind", "example.openapi@1", "--remove-kind", "example.grpc@1"}, 3},
		{[]string{"operation", "rename", "tasks.obi.json", "acme.tasks.createTask", "addTask"}, 3},
		{[]string{"operation", "rename", "tasks.obi.json", "createTask", "acme.tasks.createTask"}, 3},
		{[]string{"operation", "add", "tasks.obi.json", "x", "--output-schema", `{"$ref":"#/schemas/Missing"}`}, 3},
		{[]string{"operation", "add", "tasks.obi.json", "x", "--input-schema", `{"type":42}`}, 3},
		{[]string{"merge", "tasks.obi.json", "tasks-next.obi.json"}, 3},
		{[]string{"merge", "tasks.obi.json", "tasks-next.obi.json", "--ours", "--theirs"}, 2},
		{[]string{"operation", "set", "tasks.obi.json", "createTask", "--input-schema", "false", "--unset", "input"}, 2},
		{[]string{"operation", "set", "tasks.obi.json", "createTask", "--add-alias", "x", "--remove-alias", "x"}, 2},
		{[]string{"operation", "add", "https://api.example.com", "x"}, 2},
		{[]string{"operation", "show", "tasks.obi.json", "nope"}, 2},
		{[]string{"operation", "set", "tasks.obi.json", "nope", "--description", "x"}, 2},
		{[]string{"context", "show", "https://nope.example"}, 2},
		{[]string{"context", "remove", "https://nope.example"}, 2},
		{[]string{"operation", "remove", "tasks.obi.json", "acme.tasks.createTask", "--cascade"}, 3},
		{[]string{"operation", "set", "tasks.obi.json", "createTask", "--remove-alias", "nope"}, 2},
		{[]string{"operation", "set", "tasks.obi.json", "createTask", "--add-alias", "acme.tasks.createTask"}, 0},
		{[]string{"operation", "set", "tasks.obi.json", "listTasks", "--unset", "description"}, 0},
		{[]string{"operation", "set", "tasks.obi.json", "listTasks", "--unset", "tags"}, 2},
		{[]string{"operation", "set", "tasks.obi.json", "createTask", "--add-alias", "listTasks"}, 3},
		{[]string{"operation", "set", "tasks.obi.json", "createTask", "--unset", "input"}, 2},
		{[]string{"dependency", "add", "tasks.obi.json", "d2", "--operation", "events.deliver", "--kind", ""}, 2},
		{[]string{"source", "pull", "tasks.obi.json"}, 4},
		{[]string{"source", "pull", "tasks.obi.json", "grpcApi"}, 3},
		{[]string{"source", "pull", "tasks.obi.json", "--update-operation", "createTask"}, 3},
		{[]string{"source", "pull", "tasks.obi.json", "httpApi", "--target", "GET /health", "--operation", "nope"}, 2},
		{[]string{"status", "tasks.obi.json"}, 4},
		{[]string{"status", "tasks.obi.json", "--exit-code"}, 1},
		{[]string{"start", "--tls"}, 0},
		{[]string{"start", "--allow-origin", "https://editor.example.com/app"}, 2},
		{[]string{"ca", "install"}, 0},
		{[]string{"show", "https://private.example.com"}, 3},
		{[]string{"show", "https://nothing.example.com"}, 1},
		{[]string{"validate", "https://old.example.com"}, 3},
		{[]string{"fetch", "https://old.example.com"}, 0},
		{[]string{"mcp", "tasks.obi.json", "--operation", "nope"}, 2},
		{[]string{"mcp", "tasks.obi.json", "--binding", "nope"}, 2},
		{[]string{"invoke", "tasks.obi.json", "createTask", "--binding", "createTask.mcp", "--preflight", "--input", "{}"}, 2},
		{[]string{"invoke", "tasks.obi.json", "completeTask", "--input", `{"id":"x"}`, "--context", `{"accessToken":"t"}`}, 2},
		{[]string{"context", "set", "https://api.example.com", "--value", "{}"}, 2},
		{[]string{"context", "set", "https://api.example.com", "--bearer-token", "abc"}, 2},
		{[]string{"context", "set", "https://api.example.com", "--bearer-token", "-", "--unset", "bearer-token"}, 2},
		{[]string{"context", "set", "https://api.example.com", "--unset", "header.X-Client"}, 0},
		{[]string{"context", "set", "https://api.example.com", "--unset", "header.Nope"}, 2},
		{[]string{"context", "set", "https://api.example.com", "--token-provider", "https://auth.example.com"}, 0},
		{[]string{"validate", "tasks.obi.json", "--operation", "nope", "--input", "{}"}, 2},
		{[]string{"init", "surface_next.go"}, 3},
		{[]string{"fetch", "https://api.example.com", "-o", "surface_next.go"}, 3},
		{[]string{"mcp", "tasks.obi.json", "--operation", "createTask"}, 3},
		{[]string{"codegen", "tasks.obi.json", "--lang", "go"}, 2},
		{[]string{"validate", "tasks.obi.json", "--examples", "--operation", "createTask"}, 0},
		{[]string{"validate", "tasks.obi.json", "--operation", "createTask", "--input", `{"title":"a"}`, "--input", `{"title":5}`}, 1},
		{[]string{"delegate", "set", "d_91c2", "--remove-role", "synthesize"}, 2},
		{[]string{"delegate", "set", "d_7f3a", "--remove-role", "invoke"}, 3},
		{[]string{"delegate", "set", "d_91c2", "--add-role", "inspect"}, 0},
		{[]string{"schema", "bundle", "tasks.obi.json", "https://nope.example.com/x.json"}, 2},
		{[]string{"ca", "remove"}, 0},
		{[]string{"validate"}, 2},
		{[]string{"validate", "tasks.obi.json", "--input", "{}"}, 2},
	} {
		_, _, err := nxExec("", tc.args...)
		if got := nxExitCode(err); got != tc.code {
			t.Errorf("ob %s: exit %d, want %d (%v)", strings.Join(tc.args, " "), got, tc.code, err)
		}
	}
}

// The invocation pattern: one run is one handle. stdin carries input values,
// end of stdin closes the input, and the exit status is the terminal state.
func TestNextInvokeFollowsTheInvocationPattern(t *testing.T) {
	for _, tc := range []struct {
		name, stdin string
		args        []string
		code        int
		out         []string
	}{
		{"unary, one value", "", []string{"invoke", "t.obi.json", "completeTask", "--input", `{"id":"t_7"}`}, 0,
			[]string{`{"id":"t_7","title":"Write the docs","done":true}`}},
		{"a stream in, one value out", "{\"title\":\"a\"}\n{\"title\":\"b\"}\n{\"title\":\"c\"}\n", []string{"invoke", "t.obi.json", "importTasks", "--input", "-"}, 0,
			[]string{`{"imported":3}`}},
		{"each --input is the next write", "{\"title\":\"c\"}\n", []string{"invoke", "t.obi.json", "importTasks", "--input", `{"title":"a"}`, "--input", `{"title":"b"}`, "--input", "-"}, 0,
			[]string{`{"imported":3}`}},
		{"one value in, a stream out, as frames", "", []string{"invoke", "t.obi.json", "watchTasks", "--frames"}, 0,
			[]string{`{"kind":"input_closed"}`, `{"kind":"output","value":{"id":"t_1","title":"Write the docs","done":false}}`, `{"kind":"output","value":{"id":"t_2","title":"Review the spec","done":true}}`, `{"kind":"output","value":{"id":"t_3","title":"Ship it","done":false}}`, `{"kind":"complete"}`}},
		{"a unary binding closes its input after one value", "{\"title\":\"a\"}\n{\"title\":\"b\"}\n", []string{"invoke", "t.obi.json", "createTask", "--binding", "createTask.http", "--input", "-", "--frames"}, 0,
			[]string{`{"kind":"input_closed"}`, `{"kind":"output","value":{"id":"t_4","title":"a","done":false}}`, `{"kind":"complete"}`}},
		{"a bad first value is refused before anything is sent", "{\"title\":5}\n", []string{"invoke", "t.obi.json", "importTasks", "--input", "-"}, 3, nil},
		{"a bad later value stops after some were sent", "{\"title\":\"a\"}\n{\"title\":5}\n", []string{"invoke", "t.obi.json", "importTasks", "--input", "-", "--frames"}, 1,
			[]string{`{"kind":"error","error":{"code":"ERR_OPERATION_VALIDATION_FAILED"}}`}},
		{"several usable bindings: ob asks you to choose", "", []string{"invoke", "t.obi.json", "createTask", "--input", `{"title":"x"}`}, 3, nil},
		{"missing context: refused before anything is sent", "", []string{"invoke", "t.obi.json", "createTask", "--binding", "createTask.mcp", "--input", `{"title":"x"}`, "--frames"}, 3,
			[]string{`{"kind":"error","error":{"code":"CONTEXT_REQUIRED","data":{"target":"https://api.example.com/mcp","alternatives":[{"requirements":[{"type":"auth.oauth2","durable":true}]}]}}}`}},
		{"context for this call satisfies the binding", `{"accessToken":"t"}`, []string{"invoke", "t.obi.json", "createTask", "--binding", "createTask.mcp", "--input", `{"title":"x"}`, "--context", "-"}, 0,
			[]string{`{"id":"t_4","title":"x","done":false}`}},
		{"preflight sends nothing and prints what it knows", "", []string{"invoke", "t.obi.json", "createTask", "--binding", "createTask.mcp", "--preflight"}, 0,
			[]string{`{"target":"https://api.example.com/mcp","alternatives":[{"requirements":[{"type":"auth.oauth2","durable":true}]}]}`}},
		{"an operation with no bindings cannot be called", "", []string{"invoke", "t.obi.json", "events.deliver"}, 3, nil},
		{"refusals end with the interface's error frame", "", []string{"invoke", "t.obi.json", "events.deliver", "--frames"}, 3,
			[]string{`{"kind":"error","error":{"code":"ERR_BINDING_NOT_FOUND"}}`}},
		{"an unknown operation, as a frame", "", []string{"invoke", "t.obi.json", "deleteTask", "--frames"}, 2,
			[]string{`{"kind":"error","error":{"code":"ERR_OPERATION_NOT_FOUND"}}`}},
		{"a choice to make, as a frame", "", []string{"invoke", "t.obi.json", "createTask", "--input", `{"title":"x"}`, "--frames"}, 3,
			[]string{`{"kind":"error","error":{"code":"ERR_BINDING_SELECTION_REQUIRED"}}`}},
		{"a value in hand is checked before context is sought", "", []string{"invoke", "t.obi.json", "createTask", "--binding", "createTask.mcp", "--input", `{"title":5}`, "--frames"}, 3,
			[]string{`{"kind":"error","error":{"code":"ERR_OPERATION_VALIDATION_FAILED"}}`}},
		{"a stream that is not JSON from the start: ob cancels before sending", "nope\n", []string{"invoke", "t.obi.json", "importTasks", "--input", "-", "--frames"}, 3,
			[]string{`{"kind":"error","error":{"code":"ERR_CANCELLED"}}`}},
		{"a stream that stops being JSON: ob cancels after sending some", "{\"title\":\"a\"}\nnope\n", []string{"invoke", "t.obi.json", "importTasks", "--input", "-", "--frames"}, 1,
			[]string{`{"kind":"error","error":{"code":"ERR_CANCELLED"}}`}},
	} {
		out, errOut, err := nxExecIn("", tc.stdin, tc.args...)
		if got := nxExitCode(err); got != tc.code {
			t.Errorf("%s: exit %d, want %d (%v)\n%s", tc.name, got, tc.code, err, errOut)
			continue
		}
		if tc.out != nil {
			got := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if strings.Join(got, "\n") != strings.Join(tc.out, "\n") {
				t.Errorf("%s: stdout\n%s\nwant\n%s", tc.name, out, strings.Join(tc.out, "\n"))
			}
		}
	}
	_, _, err := nxExecIn("", "", "invoke", "t.obi.json", "createTask", "--binding", "createTask.mcp", "--input", `{"title":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "ob invoke t.obi.json createTask --binding createTask.mcp --preflight") {
		t.Errorf("a sign-in refusal away from a terminal should print the same invoke with --preflight: %v", err)
	}
	_, _, err = nxExecIn("", "{\"title\":\"a\"}\n{\"title\":5}\n", "invoke", "t.obi.json", "importTasks", "--input", "-")
	if err == nil || !strings.Contains(err.Error(), "after sending 1 value,") {
		t.Errorf("a mid-stream failure should count what was sent in words: %v", err)
	}
	_, _, err = nxExecIn("", "", "invoke", "t.obi.json", "createTask", "--input", `{"title":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "createTask.http") || !strings.Contains(err.Error(), "createTask.mcp") || !strings.Contains(err.Error(), "preference 10") {
		t.Errorf("the choice refusal should list both bindings with their signals: %v", err)
	}
}

// Bundling embeds every external schema, so nothing is left to fetch, and
// leaves every reference as it was.
func TestNextSchemaBundleLeavesNothingExternal(t *testing.T) {
	out, _, err := nxExec("", "schema", "bundle", "-")
	if err != nil {
		t.Fatal(err)
	}
	doc := nxMustParse(out).(*nxObj)
	if left := nxExternalRefs(doc); len(left) != 0 {
		t.Errorf("still external after bundling: %v", left)
	}
	if v := nxViolations(doc); len(v) != 0 {
		t.Errorf("the bundled document breaks rules: %v", v)
	}
	if got := nxCompact(doc.Obj("schemas").Obj("Task")); got != nxCompact(nxFixture().Obj("schemas").Obj("Task")) {
		t.Errorf("bundling changed a reference:\n%s", got)
	}
	out, _, err = nxExec("", "schema", "bundle", "-", "https://schemas.example.com/time/date-time.json")
	if err != nil {
		t.Fatal(err)
	}
	if left := nxExternalRefs(nxMustParse(out).(*nxObj)); len(left) != 1 || left[0].uri != "https://schemas.example.com/people/person.json" {
		t.Errorf("a named bundle should leave the rest external: %v", left)
	}
}
