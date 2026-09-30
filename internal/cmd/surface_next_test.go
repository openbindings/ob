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

var nxVariants = []string{"", "filter-edits", "binding-invoke", "correspond"}

func nxExec(variant string, args ...string) (string, string, error) {
	root := NewNextSurfaceRoot(variant)
	var out, errb bytes.Buffer
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
func nxExampleCommands(t *testing.T, example string) [][]string {
	var out [][]string
	joined := strings.ReplaceAll(example, "\\\n", " ")
	for _, line := range strings.Split(joined, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ob ") {
			continue
		}
		for _, cut := range []string{" | ", " > "} {
			if i := strings.Index(line, cut); i >= 0 {
				line = line[:i]
			}
		}
		words, err := shlex.Split(line)
		if err != nil {
			t.Fatalf("split %q: %v", line, err)
		}
		out = append(out, words[1:])
	}
	return out
}

func TestNextEveryExampleRuns(t *testing.T) {
	for _, variant := range nxVariants {
		nxWalk(NewNextSurfaceRoot(variant), func(cmd *cobra.Command) {
			for _, args := range nxExampleCommands(t, cmd.Example) {
				_, _, err := nxExec(variant, args...)
				if code := nxExitCode(err); code == 2 {
					t.Errorf("variant %q: example `ob %s` failed as a usage error: %v", variant, strings.Join(args, " "), err)
				}
			}
		})
	}
}

func TestNextEveryCommandIsDocumented(t *testing.T) {
	nxWalk(NewNextSurfaceRoot(""), func(cmd *cobra.Command) {
		if cmd.Name() == "help" {
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
		{"operation", "rename", "-", "createTask", "addTask", "--keep-alias"},
		{"operation", "rename", "-", "listTasks", "allTasks"},
		{"operation", "remove", "-", "createTask", "--cascade"},
		{"operation", "example", "add", "-", "listTasks", "empty", "--input", "{}", "--output", "[]"},
		{"operation", "example", "remove", "-", "createTask", "basic"},
		{"source", "add", "-", "billing", "--kind", "acme.billing-rpc@1", "--content", "null", "--description", "Billing RPC."},
		{"source", "set", "-", "httpApi", "--kind", "example.openapi@2", "--unset", "content"},
		{"source", "rename", "-", "httpApi", "restApi"},
		{"source", "remove", "-", "mcpServer", "--cascade"},
		{"source", "import", "-", "billing", "https://billing.example.com/openapi.json", "--kind", "example.openapi@1"},
		{"source", "pull", "-"},
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
		{"patch", "-", "changes.json"},
		{"merge", "-", "other.obi.json"},
		{"adopt", "-", "contract.obi.json", "--as", "acme.tasks.listTasks=listTasks"},
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
		for _, args := range nxExampleCommands(t, cmd.Example) {
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
		{[]string{"conform"}, "ob adopt"},
		{[]string{"correspond"}, "ob adopt"},
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

func TestNextRefusalsExitWithTheirOwnStatus(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"operation", "remove", "tasks.obi.json", "createTask"}, 1},
		{[]string{"operation", "add", "tasks.obi.json", "createTask"}, 1},
		{[]string{"invoke", "tasks.obi.json", "events.deliver", "--input", "{}"}, 3},
		{[]string{"invoke", "tasks.obi.json", "createTask", "--input", "{}"}, 3},
		{[]string{"validate", "tasks.obi.json", "--operation", "createTask", "--input", `{"title":5}`}, 1},
		{[]string{"kind", "check", "example.openapi@2", "--role", "invoke"}, 1},
		{[]string{"schema", "remove", "tasks.obi.json", "Task"}, 1},
		{[]string{"validate"}, 2},
		{[]string{"validate", "tasks.obi.json", "--input", "{}"}, 2},
	} {
		_, _, err := nxExec("", tc.args...)
		if got := nxExitCode(err); got != tc.code {
			t.Errorf("ob %s: exit %d, want %d (%v)", strings.Join(tc.args, " "), got, tc.code, err)
		}
	}
}
