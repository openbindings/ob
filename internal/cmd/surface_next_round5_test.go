package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNextRuleRefusalsNameTheRule(t *testing.T) {
	for _, tc := range []struct {
		args []string
		rule string
	}{
		{[]string{"operation", "add", "-", "bad/name"}, "OBI-D-03"},
		{[]string{"operation", "add", "-", "ok", "--alias", "bad name"}, "OBI-D-03"},
		{[]string{"operation", "add", "-", "acme.tasks.createTask"}, "OBI-D-04"},
		{[]string{"init", "-", "--interface-version", ""}, "OBI-D-02"},
		{[]string{"set", "-", "--interface-version", ""}, "OBI-D-02"},
		{[]string{"schema", "add", "-", "Bad", "--value", `{"$schema":"http://json-schema.org/draft-07/schema#"}`}, "OBI-D-06"},
		{[]string{"schema", "add", "-", "Bad", "--value", `{"properties":{"value":{"$schema":"http://json-schema.org/draft-07/schema#"}}}`}, "OBI-D-06"},
	} {
		out, _, err := nxExec("", tc.args...)
		if nxExitCode(err) != 3 || !strings.Contains(err.Error(), tc.rule) || out != "" {
			t.Errorf("%v: output %q, error %v; want refusal 3 naming %s and no written document", tc.args, out, err, tc.rule)
		}
		if tc.rule == "OBI-D-06" && strings.Contains(err.Error(), "OBI-D-02") {
			t.Errorf("dialect refusal includes structural branch noise: %v", err)
		}
	}
}

func TestNextIdentifierRulesRespectSchemaPositions(t *testing.T) {
	for _, tc := range []struct {
		name, schemas string
		duplicate     bool
	}{
		{"anchor and dynamic anchor", `{"A":{"$anchor":"same"},"B":{"$dynamicAnchor":"same"}}`, true},
		{"two declarations at one position", `{"A":{"$anchor":"same","$dynamicAnchor":"same"}}`, true},
		{"resource anchors are separate", `{"A":{"$anchor":"same"},"B":{"$id":"https://example.com/b","$anchor":"same"}}`, false},
		{"instance data is not schema", `{"A":{"$anchor":"same","const":{"$anchor":"same","$id":"https://example.com/a"}},"B":{"$id":"https://example.com/a"}}`, false},
		{"dot segments and empty fragment", `{"A":{"$id":"https://example.com/a/../b#"},"B":{"$id":"https://example.com/b"}}`, true},
		{"relative nested identifier", `{"A":{"$id":"https://example.com/a/root","$defs":{"B":{"$id":"../b"}}},"C":{"$id":"https://example.com/b"}}`, true},
		{"host case stays distinct", `{"A":{"$id":"https://EXAMPLE.com/b"},"B":{"$id":"https://example.com/b"}}`, false},
		{"scheme case stays distinct", `{"A":{"$id":"HTTPS://example.com/b"},"B":{"$id":"https://example.com/b"}}`, false},
		{"unknown relative base is excluded", `{"A":{"$id":"a","$defs":{"B":{"$id":"b"}}},"B":{"$id":"b"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := nxNewObj().Set("operations", nxNewObj()).Set("schemas", nxMustParse(tc.schemas))
			violations := strings.Join(nxAdditionalRules(doc), "\n")
			if got := strings.Contains(violations, "OBI-D-13"); got != tc.duplicate {
				t.Fatalf("duplicate = %t, want %t: %s", got, tc.duplicate, violations)
			}
		})
	}
}

func TestNextContextMaskingAndVocabulary(t *testing.T) {
	out, _, err := nxExec("", "context", "show", "https://api.example.com")
	if err != nil || !strings.Contains(out, "bearer-token") || !strings.Contains(out, "header.X-Client") || strings.Contains(out, "3f9a") {
		t.Fatalf("text masking/vocabulary: %q, %v", out, err)
	}
	out, _, err = nxExec("", "context", "show", "https://api.example.com", "-F", "json")
	var report struct {
		Context struct {
			BearerToken struct{ Masked bool }
			Headers     map[string]struct{ Masked bool }
		}
	}
	if err != nil || json.Unmarshal([]byte(out), &report) != nil || !report.Context.BearerToken.Masked || !report.Context.Headers["X-Client"].Masked || strings.Contains(out, "3f9a") {
		t.Fatalf("JSON structural masking: %q, %v", out, err)
	}
	out, _, err = nxExec("", "context", "show", "https://api.example.com", "--reveal")
	if err != nil || !strings.Contains(out, "preview-secret-3f9a") {
		t.Fatalf("explicit reveal: %q, %v", out, err)
	}
	out, note, err := nxExec("", "context", "set", "https://tokens.example.com", "--token-credential", "-")
	if err != nil || !strings.Contains(out, "set token-credential") || !strings.Contains(note, "exact scope") {
		t.Fatalf("rotate a pinned provider credential with a scope hint: %q, %q, %v", out, note, err)
	}
	_, _, err = nxExec("", "context", "set", "https://unconfigured.example.com", "--token-credential", "-")
	if nxExitCode(err) != 2 {
		t.Fatalf("missing provider: %v", err)
	}
}

func TestNextBundleRefusesInvalidFetchedSchemasAtomically(t *testing.T) {
	uri := "https://schemas.example.com/people/person.json"
	before := nxPublishedSchemas[uri]
	t.Cleanup(func() { nxPublishedSchemas[uri] = before })
	for _, raw := range []string{
		`{"$id":"https://different.example.com/person.json","type":"object"}`,
		`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`,
		`{"properties":{"x":{"$schema":"http://json-schema.org/draft-07/schema#"}}}`,
	} {
		nxPublishedSchemas[uri] = raw
		out, _, err := nxExec("", "schema", "bundle", "-")
		if nxExitCode(err) != 3 || out != "" || !strings.Contains(err.Error(), "stays external") {
			t.Fatalf("invalid fetch %s: output %q, error %v", raw, out, err)
		}
		listed, _, err := nxExec("", "schema", "list", "sample", "-F", "json")
		if err != nil || !strings.Contains(listed, uri) {
			t.Fatalf("refusal removed external reference: %q, %v", listed, err)
		}
	}
}

func TestNextRound5ReportsAreStable(t *testing.T) {
	out, _, err := nxExec("", "schema", "list", "sample", "-F", "json")
	var schemas []map[string]any
	if err != nil || json.Unmarshal([]byte(out), &schemas) != nil {
		t.Fatalf("schemas: %q, %v", out, err)
	}
	for _, s := range schemas {
		for _, field := range []string{"name", "uri", "external", "referencedBy"} {
			if _, ok := s[field]; !ok {
				t.Errorf("schema lacks %s: %v", field, s)
			}
		}
	}
	out, _, err = nxExec("", "validate", "sample", "--examples", "--operation", "createTask", "-F", "json")
	if err != nil || strings.Contains(out, `"input": "`) || !strings.Contains(out, `"input": true`) {
		t.Fatalf("example verdict is not a boolean: %q, %v", out, err)
	}
	out, _, err = nxExec("", "compat", "sample", "contract", "-F", "json")
	var compat struct {
		Operations []struct{ Operation string }
		Issues     []struct{ Operation, Kind, Detail string }
	}
	if nxExitCode(err) != 1 || json.Unmarshal([]byte(out), &compat) != nil {
		t.Fatalf("compat: %q, %v", out, err)
	}
	previous := ""
	for _, op := range compat.Operations {
		if op.Operation <= previous {
			t.Errorf("unsorted operations: %s before %s", previous, op.Operation)
		}
		previous = op.Operation
	}
	if len(compat.Issues) == 0 || compat.Issues[0].Kind != "missing" || compat.Issues[0].Detail == "" {
		t.Fatalf("missing profile reason: %q", out)
	}
	out, _, err = nxExec("", "kind", "list", "-F", "json")
	if err != nil || !strings.Contains(out, `"handlers"`) || strings.Contains(out, `"handler":`) {
		t.Fatalf("role handlers: %q, %v", out, err)
	}
}

func TestNextHelpCompletionAndMCPRecovery(t *testing.T) {
	_, _, err := nxExec("", "help", "nope")
	if nxExitCode(err) != 2 || !strings.Contains(err.Error(), "ob --help") {
		t.Fatalf("unknown help: %v", err)
	}
	_, note, err := nxExec("", "mcp", "sample", "--operation", "createTask", "--binding", "createTask.mcp")
	if err != nil || !strings.Contains(note, "Context needed:") || !strings.Contains(note, "ob invoke sample createTask --binding createTask.mcp --preflight") {
		t.Fatalf("MCP context recovery: %q, %v", note, err)
	}
	root := NewNextSurfaceRoot("")
	for _, tc := range []struct {
		path         []string
		args         []string
		prefix, want string
	}{
		{[]string{"invoke"}, []string{"sample"}, "acme.tasks.cr", "acme.tasks.createTask"},
		{[]string{"source", "pull"}, []string{"sample"}, "http", "httpApi"},
		{[]string{"binding", "show"}, []string{"sample"}, "createTask.h", "createTask.http"},
		{[]string{"schema", "show"}, []string{"sample"}, "Ta", "Task"},
	} {
		cmd, _, _ := root.Find(tc.path)
		values, directive := cmd.ValidArgsFunction(cmd, tc.args, tc.prefix)
		if !nxContains(values, tc.want) || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("completion %v: %v, %v", tc.path, values, directive)
		}
	}
}

func TestNextCompatIssuesOrderAndVerdictDominance(t *testing.T) {
	before := nxCompatComparisons
	t.Cleanup(func() { nxCompatComparisons = before })
	nxCompatComparisons = map[string][]nxProfileIssue{
		"acme.tasks.createTask": {
			{"input_incompatible", `required: candidate requires "extra" but target does not`},
			{"output_incompatible", `type: candidate output is not an object`},
			{"outside-profile", `pattern: non-identical schemas are outside this profile`},
		},
	}
	out, _, err := nxExec("", "compat", "sample", "contract", "-F", "json")
	var report struct {
		Conclusion string
		Issues     []struct{ Operation, Kind, Detail string }
	}
	if nxExitCode(err) != 4 || json.Unmarshal([]byte(out), &report) != nil || report.Conclusion != "indeterminate" {
		t.Fatalf("indeterminate must outrank both missing and incompatible: %q, %v", out, err)
	}
	var kinds []string
	for _, issue := range report.Issues {
		if issue.Operation == "acme.tasks.createTask" {
			kinds = append(kinds, issue.Kind)
		}
	}
	if strings.Join(kinds, ",") != "output_incompatible,input_incompatible,outside-profile" {
		t.Fatalf("wrong issue order: %v", kinds)
	}
}
