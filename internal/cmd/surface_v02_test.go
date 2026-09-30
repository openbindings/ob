package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/shlex"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
)

func v02Roots() map[string]func() *cobra.Command {
	return map[string]func() *cobra.Command{
		"object":           func() *cobra.Command { return NewV02SurfaceRoot("object") },
		"action":           func() *cobra.Command { return NewV02SurfaceRoot("action") },
		"capability":       func() *cobra.Command { return NewV02SurfaceRoot("capability") },
		"invoke-root":      func() *cobra.Command { return NewV02SurfaceRoot("invoke-root") },
		"binding-invoke":   func() *cobra.Command { return NewV02SurfaceRoot("binding-invoke") },
		"init":             func() *cobra.Command { return NewV02SurfaceRoot("init") },
		"new":              func() *cobra.Command { return NewV02SurfaceRoot("new") },
		"read-direct":      func() *cobra.Command { return NewV02SurfaceRoot("read-direct") },
		"read-summary":     func() *cobra.Command { return NewV02SurfaceRoot("read-summary") },
		"plain-workflows":  func() *cobra.Command { return NewV02SurfaceRoot("plain-workflows") },
		"idempotent-value": func() *cobra.Command { return NewV02SurfaceRoot("idempotent-value") },
		"task-language":    func() *cobra.Command { return NewV02SurfaceRoot("task-language") },
	}
}

func TestV02SpecPin(t *testing.T) {
	specPath := filepath.Join("..", "..", "..", "spec")
	got, err := exec.Command("git", "-C", specPath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("read 0.2 spec revision: %v", err)
	}
	const want = "ccfe0b6df87cca0a46955e5aeeff550797a23cf8"
	if strings.TrimSpace(string(got)) != want {
		t.Skipf("the previous 0.2 lab tree is pinned to spec %s; the sibling checkout is at %s", want, strings.TrimSpace(string(got)))
	}
}

func TestV02VariantsHaveSameInertActions(t *testing.T) {
	var baseline []string
	for name, build := range v02Roots() {
		var ids []string
		var walk func(*cobra.Command)
		walk = func(command *cobra.Command) {
			if id := command.Annotations["surface-id"]; id != "" {
				ids = append(ids, id)
				if command.RunE == nil {
					t.Errorf("%s: %s is not inert", name, command.CommandPath())
				}
			}
			for _, child := range command.Commands() {
				walk(child)
			}
		}
		walk(build())
		sort.Strings(ids)
		if baseline == nil {
			baseline = ids
		} else if !reflect.DeepEqual(baseline, ids) {
			t.Errorf("%s action IDs = %v, want %v", name, ids, baseline)
		}
	}
}

func TestV02EveryLeafHasContractAndIllustrativeResult(t *testing.T) {
	fixtures := map[string][]string{
		"new": {}, "show": {"a.obi.json"}, "validate": {"a.obi.json"},
		"diff": {"before.obi.json", "after.obi.json"}, "patch": {"a.obi.json", "changes.json"},
		"kind.list": {}, "kind.check": {"example.private@1"},
		"source.add":     {"a.obi.json", "api", "--kind", "example.private@1"},
		"source.import":  {"a.obi.json", "api", "./api.yaml", "--kind", "example.openapi@1"},
		"source.inspect": {"a.obi.json", "api"}, "source.list": {"a.obi.json"},
		"source.pull": {"a.obi.json", "api"}, "source.remove": {"a.obi.json", "api"},
		"source.show": {"a.obi.json", "api"}, "source.synthesize": {"a.obi.json", "api"},
		"binding.add":    {"a.obi.json", "get.http", "--operation", "get", "--source", "api"},
		"binding.invoke": {"a.obi.json", "get.http"}, "binding.list": {"a.obi.json"},
		"binding.remove": {"a.obi.json", "get.http"}, "binding.show": {"a.obi.json", "get.http"},
		"dependency.add":  {"a.obi.json", "billing", "--operation", "get"},
		"dependency.list": {"a.obi.json"}, "dependency.remove": {"a.obi.json", "billing"},
		"dependency.show": {"a.obi.json", "billing"},
		"operation.add":   {"a.obi.json", "get"}, "operation.list": {"a.obi.json"},
		"operation.remove": {"a.obi.json", "get"}, "operation.show": {"a.obi.json", "get"},
		"schema.add":  {"a.obi.json", "Item", "--value", "true"},
		"schema.list": {"a.obi.json"}, "schema.remove": {"a.obi.json", "Item"},
		"schema.show": {"a.obi.json", "Item"},
	}
	for variant, build := range v02Roots() {
		root := build()
		seen := map[string]bool{}
		var walk func(*cobra.Command)
		walk = func(command *cobra.Command) {
			if id := command.Annotations["surface-id"]; id != "" {
				seen[id] = true
				contract, ok := v02Contracts[id]
				if !ok || contract.Result == "" || contract.Destination == "" {
					t.Errorf("%s %s has no complete result contract", variant, id)
				}
				input, ok := fixtures[id]
				if !ok {
					t.Errorf("%s %s has no fixture", variant, id)
				} else {
					args := append(strings.Fields(command.CommandPath())[1:], input...)
					if variant != "new" && id == "new" {
						args = append(args, "-")
					}
					args = append(args, "--sample-output")
					out, stderr, err := runSurfaceRoot(build(), args...)
					if err != nil || strings.TrimSpace(out) == "" || !strings.Contains(stderr, "illustrative fixture") {
						t.Errorf("%s %s sample: err=%v, out=%q, stderr=%q", variant, id, err, out, stderr)
					}
				}
			}
			for _, child := range command.Commands() {
				walk(child)
			}
		}
		walk(root)
		for id := range v02Contracts {
			if !seen[id] {
				t.Errorf("%s missing declared contract %s", variant, id)
			}
		}
	}
}

func TestV02EveryHelpExampleReachesInertLeaf(t *testing.T) {
	for name, build := range v02Roots() {
		for _, example := range surfaceHelpExamples(build()) {
			words, err := shlex.Split(example)
			if err != nil || len(words) == 0 || words[0] != "ob" {
				t.Errorf("%s: malformed example %q: %v", name, example, err)
				continue
			}
			if err := V02PreflightArgs(build(), words[1:]); err != nil {
				t.Errorf("%s: preflight rejected %q: %v", name, example, err)
				continue
			}
			_, _, err = runSurfaceRoot(build(), words[1:]...)
			if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
				t.Errorf("%s: example %q did not reach placeholder: %v", name, example, err)
			}
		}
	}
}

func TestV02CoreAuthoringSamplesAgainstPinnedSchema(t *testing.T) {
	// The previous 0.2 lab tree was written against spec ccfe0b6; read that
	// revision rather than whatever the sibling checkout holds today.
	data, err := exec.Command("git", "-C", filepath.Join("..", "..", "..", "spec"), "show", "ccfe0b6df87cca0a46955e5aeeff550797a23cf8:openbindings.schema.json").Output()
	if err != nil {
		t.Fatalf("read the pinned spec schema: %v", err)
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("https://openbindings.com/schema/openbindings-0.2.0.json", raw); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("https://openbindings.com/schema/openbindings-0.2.0.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]string{
		{"new", "--name", "Acme API"},
		{"show", "a.obi.json", "--full"},
		{"source", "add", "a.obi.json", "api", "--kind", "private.kind/1"},
		{"source", "add", "a.obi.json", "api", "--kind", "private.kind/1", "--content", "null"},
		{"source", "add", "a.obi.json", "api", "--kind", "private.kind/1", "--content", "true"},
		{"source", "add", "a.obi.json", "api", "--kind", "private.kind/1", "--content", "42"},
		{"source", "add", "a.obi.json", "api", "--kind", "private.kind/1", "--content", `"opaque"`},
		{"source", "add", "a.obi.json", "api", "--kind", "private.kind/1", "--content", "[]"},
		{"source", "add", "a.obi.json", "api", "--kind", "private.kind/1", "--content", "{}"},
		{"binding", "add", "a.obi.json", "get.http", "--operation", "get", "--source", "api", "--content", "null", "--preference", "-42"},
		{"dependency", "add", "a.obi.json", "bill", "--operation", "get", "--kind", "private.kind/1", "--kind", "other.kind/2"},
		{"operation", "add", "a.obi.json", "get", "--input-schema", "false", "--output-schema", "{}", "--alias", "my_team.get"},
		{"operation", "add", "a.obi.json", "repeatable", "--idempotent"},
		{"operation", "add", "a.obi.json", "once", "--idempotent=false"},
		{"schema", "add", "a.obi.json", "Item", "--value", "true"},
		{"source", "import", "a.obi.json", "api", "./api.yaml", "--kind", "example.openapi@1"},
		{"source", "pull", "a.obi.json", "api"},
		{"source", "synthesize", "a.obi.json", "api", "--apply"},
	}
	for variant, build := range v02Roots() {
		for _, input := range cases {
			args := append([]string(nil), input...)
			if variant != "read-summary" && len(args) == 3 && args[0] == "show" && args[2] == "--full" {
				args = args[:2]
			}
			if variant != "new" && len(args) > 0 && args[0] == "new" {
				args[0] = "init"
				args = append(args, "-")
			}
			if variant == "action" && len(args) > 1 && args[1] == "add" {
				args[0], args[1] = args[1], args[0]
			}
			if variant == "plain-workflows" && len(args) > 1 && args[0] == "source" {
				switch args[1] {
				case "import":
					args[1] = "acquire"
				case "pull":
					args[1] = "refresh"
				case "synthesize":
					args[1] = "propose"
				}
			}
			if variant == "idempotent-value" && len(args) > 2 && args[0] == "operation" && args[1] == "add" {
				if args[len(args)-1] == "--idempotent" {
					args = append(args, "true")
				}
			}
			if variant == "task-language" {
				args = v02TaskLanguageTestArgs(args)
			}
			args = append(args, "--sample-output", "-F", "json")
			out, stderr, err := runSurfaceRoot(build(), args...)
			if err != nil {
				t.Errorf("%s %v: %v", variant, args, err)
				continue
			}
			if !strings.Contains(stderr, "illustrative fixture") {
				t.Errorf("%s %v: sample warning absent", variant, args)
			}
			var doc any
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Errorf("%s %v: bad JSON: %v", variant, args, err)
				continue
			}
			if err := schema.Validate(doc); err != nil {
				t.Errorf("%s %v: violates 0.2 schema: %v", variant, args, err)
			}
			if err := v02CheckSampleRelationships(doc); err != nil {
				t.Errorf("%s %v: violates local 0.2 relationship rule: %v", variant, args, err)
			}
		}
	}
}

// The structural schema cannot decide same-document references or aliases.
// These checks cover the relationships present in our small sample fixtures.
func v02CheckSampleRelationships(value any) error {
	doc, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("sample is not an object")
	}
	operations, _ := doc["operations"].(map[string]any)
	sources, _ := doc["sources"].(map[string]any)
	aliases := map[string]bool{}
	for key, raw := range operations {
		if aliases[key] {
			return fmt.Errorf("operation key %q collides with alias", key)
		}
		aliases[key] = true
		operation, _ := raw.(map[string]any)
		if list, ok := operation["aliases"].([]any); ok {
			for _, item := range list {
				alias, _ := item.(string)
				if aliases[alias] || alias == key {
					return fmt.Errorf("alias %q collides", alias)
				}
				aliases[alias] = true
			}
		}
	}
	bindings, _ := doc["bindings"].(map[string]any)
	for key, raw := range bindings {
		binding, _ := raw.(map[string]any)
		op, _ := binding["operation"].(string)
		source, _ := binding["source"].(string)
		if _, ok := operations[op]; !ok {
			return fmt.Errorf("binding %q references missing operation %q", key, op)
		}
		if _, ok := sources[source]; !ok {
			return fmt.Errorf("binding %q references missing source %q", key, source)
		}
	}
	dependencies, _ := doc["dependencies"].(map[string]any)
	for key, raw := range dependencies {
		dependency, _ := raw.(map[string]any)
		op, _ := dependency["operation"].(string)
		if _, ok := operations[op]; !ok {
			return fmt.Errorf("dependency %q references missing operation %q", key, op)
		}
	}
	return nil
}

func TestV02NearMissesAndInertness(t *testing.T) {
	for variant, build := range v02Roots() {
		add := []string{"source", "add"}
		schema := []string{"schema", "add"}
		if variant == "action" {
			add, schema = []string{"add", "source"}, []string{"add", "schema"}
		}
		for _, tc := range []struct {
			args []string
			want string
		}{
			{append(append([]string{}, add...), "a.obi.json", "api"), "--kind is required"},
			{append(append([]string{}, add...), "a.obi.json", "api", "--kind", ""), "--kind must be a non-empty"},
			{append(append([]string{}, add...), "a.obi.json", "api", "--kind", "k", "--content", "bad"), "expected one JSON value"},
			{append(append([]string{}, schema...), "a.obi.json", "Item", "--value", "5", "--sample-output"), "JSON Schema object or boolean"},
			{append(append([]string{}, add...), "a.obi.json", "api", "--kind"), "flag needs an argument"},
			{append(append([]string{}, add...), "-", "api", "--kind", "k", "--content", "-"), "stdin (-) can supply only one input"},
			{[]string{"validate", "a.obi.json", "-F", "bogus"}, "-F must be text, json, or yaml"},
			{[]string{"diff", "a.obi.json", "b.obi.json", "-F", "yaml"}, "supports text or json"},
			{[]string{"new", "-F", "text"}, "produces a JSON OBI"},
			{[]string{"new", "-F", "yaml", "-o", "a.obi.json"}, "stdout view"},
			{[]string{"validate", "a.obi.json", "--quiet", "-F", "json"}, "emits no report"},
			{[]string{"dependency", "add", "a.obi.json", "bill", "--operation", "get", "--kind", "x", "--kind", "x"}, "unique exact strings"},
			{[]string{"source"}, "needs a subcommand"},
		} {
			args := tc.args
			want := tc.want
			if variant == "action" && len(args) > 1 && args[0] == "dependency" && args[1] == "add" {
				args = append([]string{"add", "dependency"}, args[2:]...)
			}
			if variant != "new" && len(args) > 0 && args[0] == "new" {
				args = append([]string{"init", "-"}, args[1:]...)
				if want == "stdout view" {
					want = "not both"
				}
			}
			if variant == "task-language" {
				args = v02TaskLanguageTestArgs(args)
			}
			_, _, err := runSurfaceRoot(build(), args...)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s %v: got %v, want %q", variant, args, err, want)
			}
		}
	}
}

func v02TaskLanguageTestArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	switch args[0] {
	case "init", "show", "validate", "diff", "patch":
		return append([]string{"document"}, args...)
	case "binding":
		args[0] = "realization"
	case "dependency":
		args[0] = "consumption"
	case "kind":
		args[0] = "handler"
	case "invoke":
		return append([]string{"realization"}, args...)
	}
	return args
}

func TestV02TaskLanguageHasCanonicalMapping(t *testing.T) {
	for _, tc := range []struct {
		path []string
		id   string
	}{
		{[]string{"document", "init"}, "new"},
		{[]string{"document", "show"}, "show"},
		{[]string{"document", "validate"}, "validate"},
		{[]string{"document", "diff"}, "diff"},
		{[]string{"document", "patch"}, "patch"},
		{[]string{"realization", "add"}, "binding.add"},
		{[]string{"realization", "invoke"}, "binding.invoke"},
		{[]string{"consumption", "add"}, "dependency.add"},
		{[]string{"handler", "check"}, "kind.check"},
	} {
		root := NewV02SurfaceRoot("task-language")
		command, rest, err := root.Find(tc.path)
		if err != nil || len(rest) != 0 || command.Annotations["surface-id"] != tc.id {
			t.Errorf("%v mapped to %s, rest %v, error %v; want %s", tc.path, command.CommandPath(), rest, err, tc.id)
		}
	}
	for _, old := range [][]string{{"init"}, {"show"}, {"binding"}, {"dependency"}, {"kind"}, {"invoke"}} {
		root := NewV02SurfaceRoot("task-language")
		if err := V02PreflightArgs(root, old); err == nil {
			t.Errorf("task-language retained competing path %v", old)
		}
	}
	for _, command := range [][]string{
		{"document", "show", "a.obi.json", "--json"},
		{"realization", "invoke", "a.obi.json", "get.http", "--input", "null"},
		{"consumption", "add", "a.obi.json", "billing", "--operation", "charge"},
		{"handler", "check", "private.kind/1", "--action", "invoke"},
	} {
		_, _, err := runSurfaceRoot(NewV02SurfaceRoot("task-language"), command...)
		if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
			t.Errorf("%v: expected inert parser acceptance, got %v", command, err)
		}
	}
}

func TestV02NounRecoveryAndKindAction(t *testing.T) {
	for _, tc := range []struct{ variant, miss, hint string }{
		{"object", "bindings", "ob binding"},
		{"object", "dependencies", "ob dependency"},
		{"object", "handlers", "ob kind"},
		{"object", "sources", "ob source"},
		{"task-language", "binding", "ob realization"},
		{"task-language", "dependency", "ob consumption"},
		{"task-language", "kinds", "ob handler"},
	} {
		err := V02PreflightArgs(NewV02SurfaceRoot(tc.variant), []string{tc.miss})
		if err == nil || !strings.Contains(err.Error(), tc.hint) {
			t.Errorf("%s %q: got %v, want hint %q", tc.variant, tc.miss, err, tc.hint)
		}
	}
	for _, variant := range []string{"object", "task-language"} {
		path := []string{"kind", "check", "example.private@1"}
		if variant == "task-language" {
			path[0] = "handler"
		}
		args := append(append([]string{}, path...), "--action", "inspect", "--sample-output")
		out, _, err := runSurfaceRoot(NewV02SurfaceRoot(variant), args...)
		if err != nil {
			t.Fatal(err)
		}
		var report map[string]any
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		if report["action"] != "inspect" || report["supported"] != true {
			t.Errorf("%s action report: %v", variant, report)
		}
		args = append(append([]string{}, path...), "--sample-output")
		out, _, err = runSurfaceRoot(NewV02SurfaceRoot(variant), args...)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		if _, ok := report["actions"]; !ok {
			t.Errorf("%s omitted action should list abilities: %v", variant, report)
		}
		args = append(append([]string{}, path...), "--action", "update")
		_, _, err = runSurfaceRoot(NewV02SurfaceRoot(variant), args...)
		if err == nil || !strings.Contains(err.Error(), "pull refreshes stored content") {
			t.Errorf("%s invalid action: %v", variant, err)
		}
	}
}

func TestV02InvokeAndDiffContracts(t *testing.T) {
	for variant, build := range map[string]func() *cobra.Command{
		"object":         func() *cobra.Command { return NewV02SurfaceRoot("object") },
		"binding-invoke": func() *cobra.Command { return NewV02SurfaceRoot("binding-invoke") },
	} {
		path := []string{"invoke"}
		if variant == "binding-invoke" {
			path = []string{"binding", "invoke"}
		}
		cases := []struct {
			tail []string
			want string
		}{
			{[]string{"--input", "null"}, "command-surface placeholder"},
			{[]string{"--input", "bad"}, "expected one JSON value"},
			{[]string{"--input", "null", "--input-stream", "-"}, "mutually exclusive"},
			{[]string{"--input-stream", "@values.ndjson"}, "currently accepts only -"},
			{[]string{"-F", "yaml"}, "-o and -F do not apply"},
			{[]string{"-o", "result.json"}, "-o and -F do not apply"},
		}
		for _, tc := range cases {
			args := append(append(append([]string{}, path...), "a.obi.json", "get.http"), tc.tail...)
			_, _, err := runSurfaceRoot(build(), args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %v: got %v; want %q", variant, args, err, tc.want)
			}
		}
		args := append(append([]string{}, path...), "a.obi.json", "get.http", "--input-stream", "-", "--sample-output")
		out, stderr, err := runSurfaceRoot(build(), args...)
		if err != nil || !strings.Contains(stderr, "illustrative fixture") {
			t.Errorf("%s sample: %v, stderr %q", variant, err, stderr)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 2 {
			t.Errorf("%s NDJSON sample has %d lines, want 2", variant, len(lines))
		}
		for _, line := range lines {
			if !json.Valid([]byte(line)) {
				t.Errorf("%s invalid NDJSON line %q", variant, line)
			}
		}
		helpArgs := append(append([]string{}, path...), "--help")
		help, _, err := runSurfaceRoot(build(), helpArgs...)
		if err != nil || strings.Contains(help, "Global Flags:") || strings.Contains(help, "--output") || strings.Contains(help, "--format") {
			t.Errorf("%s invoke help shows rejected global flags: %v, %q", variant, err, help)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"diff", "before.obi.json", "after.obi.json", "--sample-output"}, "changed /operations"},
		{[]string{"diff", "before.obi.json", "after.obi.json", "--sample-output", "-F", "json"}, `"changes"`},
	} {
		out, _, err := runSurfaceRoot(NewV02SurfaceRoot("object"), tc.args...)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Errorf("diff %v: %v, out %q", tc.args, err, out)
		}
	}
}

func TestV02InitCandidatePreservesVersionMeanings(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"init", "api.obi.json", "--version", "0.2"}, "--version is ambiguous"},
		{[]string{"init", "api.obi.json", "-o", "other.obi.json"}, "not both"},
		{[]string{"init", "api.obi.json", "--interface-version", ""}, "--interface-version must be non-empty"},
		{[]string{"init", "-", "--force"}, "not stdout"},
		{[]string{"init", "api.obi.json", "-F", "yaml"}, "stdout view"},
	} {
		_, _, err := runSurfaceRoot(NewV02SurfaceRoot("init"), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.args, err, tc.want)
		}
	}
	_, _, err := runSurfaceRoot(NewV02SurfaceRoot("init"), "init", "--name", "Acme API", "-o", "api.obi.json")
	if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
		t.Errorf("init with -o did not reach inert leaf: %v", err)
	}
	out, _, err := runSurfaceRoot(NewV02SurfaceRoot("init"), "init", "-", "--interface-version", "2026-09-alpha", "--sample-output")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["openbindings"] != "0.2.0" || doc["version"] != "2026-09-alpha" {
		t.Errorf("init sample conflated spec and interface versions: %v", doc)
	}
}

func TestV02DirectReadsPreserveExactValuesAndRejectFormatCollision(t *testing.T) {
	build := func() *cobra.Command { return NewV02SurfaceRoot("object") }
	out, _, err := runSurfaceRoot(build(), "show", "a.obi.json", "--sample-output", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["sources"] == nil || doc["bindings"] == nil {
		t.Errorf("full read omitted stored maps: %v", doc)
	}
	out, _, err = runSurfaceRoot(build(), "show", "a.obi.json", "--summary", "--sample-output", "--json")
	if err != nil || !strings.Contains(out, `"illustrative": true`) {
		t.Errorf("summary read: %v, %q", err, out)
	}
	out, _, err = runSurfaceRoot(build(), "source", "list", "a.obi.json", "--full", "--json", "--sample-output")
	if err != nil {
		t.Fatal(err)
	}
	var sources map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &sources); err != nil {
		t.Fatal(err)
	}
	if value, ok := sources["api"]["content"]; !ok || value != nil {
		t.Errorf("explicit null source content lost: %v", sources["api"])
	}
	if _, ok := sources["private"]["content"]; ok {
		t.Errorf("absent source content became present: %v", sources["private"])
	}
	for _, args := range [][]string{
		{"show", "a.obi.json", "--json", "-F", "yaml"},
		{"validate", "a.obi.json", "--json", "-F", "text"},
		{"diff", "a.obi.json", "b.obi.json", "--json", "-F", "json"},
		{"show", "a.obi.json", "-F", "json", "-F", "yaml"},
	} {
		_, _, err := runSurfaceRoot(build(), args...)
		if err == nil || !strings.Contains(err.Error(), "cannot be combined") && !strings.Contains(err.Error(), "may be given only once") {
			t.Errorf("%v: got %v, want format collision", args, err)
		}
	}
}

func TestV02PlainWorkflowHelpOrdering(t *testing.T) {
	out, _, err := runSurfaceRoot(NewV02SurfaceRoot("plain-workflows"), "source", "--help")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"acquire", "add", "interpret", "list", "propose", "refresh", "remove", "show"}
	last := -1
	for _, name := range names {
		needle := "  " + name + " "
		index := strings.Index(out, needle)
		if index < 0 || index <= last {
			t.Fatalf("source help has missing or misordered %q: %q", name, out)
		}
		last = index
	}
}

func TestV02IdempotentTriState(t *testing.T) {
	for _, tc := range []struct {
		name string
		tail []string
		want any
		set  bool
	}{
		{"true", []string{"--idempotent"}, true, true},
		{"false", []string{"--idempotent=false"}, false, true},
		{"absent", nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"operation", "add", "a.obi.json", "test"}, tc.tail...)
			args = append(args, "--sample-output")
			out, _, err := runSurfaceRoot(NewV02SurfaceRoot("object"), args...)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatal(err)
			}
			operations := doc["operations"].(map[string]any)
			op := operations["test"].(map[string]any)
			value, present := op["idempotent"]
			if present != tc.set || present && value != tc.want {
				t.Errorf("idempotent = %v (present %v), want %v (present %v)", value, present, tc.want, tc.set)
			}
		})
	}
	_, _, err := runSurfaceRoot(NewV02SurfaceRoot("object"), "operation", "add", "a.obi.json", "bad", "--idempotent=maybe")
	if err == nil || !strings.Contains(err.Error(), "invalid syntax") {
		t.Errorf("invalid Boolean value: %v", err)
	}
}
