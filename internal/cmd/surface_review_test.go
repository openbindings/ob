package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/shlex"
	"github.com/openbindings/openbindings-go"
	"github.com/spf13/cobra"
)

func runSurface(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return runSurfaceRoot(NewSurfaceRoot(), args...)
}

func runSurfaceRoot(root *cobra.Command, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func surfaceLeafIDs(root *cobra.Command) []string {
	var ids []string
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if len(cmd.Commands()) == 0 && cmd.Parent() != nil {
			ids = append(ids, surfaceContractID(cmd))
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	sort.Strings(ids)
	return ids
}

func surfaceHelpExamples(root *cobra.Command) []string {
	var examples []string
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		longExamples := ""
		if i := strings.Index(cmd.Long, "Examples:"); i >= 0 {
			longExamples = cmd.Long[i+len("Examples:"):]
		}
		for _, block := range []string{longExamples, cmd.Example} {
			pending := ""
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if pending == "" && !strings.HasPrefix(line, "ob ") {
					continue
				}
				if strings.HasSuffix(line, "\\") {
					pending += strings.TrimSpace(strings.TrimSuffix(line, "\\")) + " "
					continue
				}
				examples = append(examples, pending+line)
				pending = ""
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	return examples
}

func TestSurfaceEveryHelpExampleParses(t *testing.T) {
	for _, variant := range []struct {
		name  string
		build func() *cobra.Command
	}{
		{"broad", NewSurfaceRoot},
		{"compact", NewCompactSurfaceRoot},
		{"hybrid", NewHybridSurfaceRoot},
	} {
		count := 0
		for _, example := range surfaceHelpExamples(variant.build()) {
			// Strip a trailing shell comment; none of the examples uses a
			// quoted " # " sequence.
			if i := strings.Index(example, "  # "); i >= 0 {
				example = example[:i]
			}
			tokens, err := shlex.Split(example)
			if err != nil {
				t.Errorf("%s: cannot parse example %q: %v", variant.name, example, err)
				continue
			}
			parts := [][]string{{}}
			for _, token := range tokens {
				if token == "|" || token == "&&" {
					parts = append(parts, []string{})
				} else {
					parts[len(parts)-1] = append(parts[len(parts)-1], token)
				}
			}
			for _, part := range parts {
				if len(part) == 0 || part[0] != "ob" {
					continue
				}
				count++
				_, _, err := runSurfaceRoot(variant.build(), part[1:]...)
				if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
					t.Errorf("%s: example %q did not reach an inert leaf: %v", variant.name, example, err)
				}
			}
		}
		if count < 100 {
			t.Errorf("%s: only %d examples checked; extractor may have regressed", variant.name, count)
		}
		t.Logf("%s: %d help example commands parsed", variant.name, count)
	}
}

func TestSurfaceEveryLeafHasOneContractInAllVariants(t *testing.T) {
	broad := NewSurfaceRoot()
	compact := NewCompactSurfaceRoot()
	hybrid := NewHybridSurfaceRoot()
	if len(compact.Commands()) >= len(broad.Commands()) {
		t.Fatalf("compact root did not reduce root choices: %d vs %d", len(compact.Commands()), len(broad.Commands()))
	}
	if len(hybrid.Commands()) >= len(broad.Commands()) || len(hybrid.Commands()) <= len(compact.Commands()) {
		t.Fatalf("hybrid root count not between broad and compact: %d, %d, %d", len(compact.Commands()), len(hybrid.Commands()), len(broad.Commands()))
	}
	gotBroad, gotCompact, gotHybrid := surfaceLeafIDs(broad), surfaceLeafIDs(compact), surfaceLeafIDs(hybrid)
	if len(gotBroad) != len(surfaceContracts) || len(gotCompact) != len(surfaceContracts) || len(gotHybrid) != len(surfaceContracts) {
		t.Fatalf("leaf/contract counts: broad=%d compact=%d hybrid=%d contracts=%d", len(gotBroad), len(gotCompact), len(gotHybrid), len(surfaceContracts))
	}
	for i, id := range gotBroad {
		if _, ok := surfaceContracts[id]; !ok {
			t.Errorf("unclassified leaf: %s", id)
		}
		if i > 0 && id == gotBroad[i-1] {
			t.Errorf("duplicate leaf contract: %s", id)
		}
		if id != gotCompact[i] {
			t.Errorf("variant capability mismatch at %d: %s vs %s", i, id, gotCompact[i])
		}
		if id != gotHybrid[i] {
			t.Errorf("hybrid capability mismatch at %d: %s vs %s", i, id, gotHybrid[i])
		}
	}
}

func TestSurfaceAliasPolicy(t *testing.T) {
	for _, root := range []*cobra.Command{NewSurfaceRoot(), NewCompactSurfaceRoot(), NewHybridSurfaceRoot()} {
		var visit func(*cobra.Command)
		visit = func(cmd *cobra.Command) {
			want := ""
			switch surfaceContractID(cmd) {
			case "ob about":
				want = "describe"
			case "ob strip-ob-metadata":
				want = "purify"
			}
			if got := strings.Join(cmd.Aliases, ","); got != want {
				t.Errorf("%s aliases = %q; want %q", cmd.CommandPath(), got, want)
			}
			for _, child := range cmd.Commands() {
				visit(child)
			}
		}
		visit(root)
	}
}

func TestSurfaceHelpAvoidsRetiredOrInflatedSpecClaims(t *testing.T) {
	for _, root := range []*cobra.Command{NewSurfaceRoot(), NewCompactSurfaceRoot(), NewHybridSurfaceRoot()} {
		var visit func(*cobra.Command)
		visit = func(cmd *cobra.Command) {
			help := cmd.Long + "\n" + cmd.Example
			for _, stale := range []string{
				"OBI-T-07", "OBI-T-08",
				"so the document is conformant (OBI-D-05)",
				"operation corresponds to a published interface",
				"interface operations it corresponds to",
			} {
				if strings.Contains(help, stale) {
					t.Errorf("%s retains stale spec claim %q", cmd.CommandPath(), stale)
				}
			}
			for _, child := range cmd.Commands() {
				visit(child)
			}
		}
		visit(root)
	}
}

func TestSurfaceHelpUsesCanonicalCommandNames(t *testing.T) {
	for _, root := range []*cobra.Command{NewSurfaceRoot(), NewCompactSurfaceRoot(), NewHybridSurfaceRoot()} {
		var visit func(*cobra.Command)
		visit = func(cmd *cobra.Command) {
			help := cmd.Long + "\n" + cmd.Example
			for _, stale := range []string{"ob op ", "ob src ", "ob ctx ", "ob env'", "ob purify ", "ob operation ls ", "ob operation rm ", "ob source ls ", "ob source rm "} {
				if strings.Contains(help, stale) {
					t.Errorf("%s help retains removed alias %q", cmd.CommandPath(), stale)
				}
			}
			for _, child := range cmd.Commands() {
				visit(child)
			}
		}
		visit(root)
	}
}

func TestSurfaceHybridKeepsDailyPathsDirect(t *testing.T) {
	for _, args := range [][]string{
		{"status", "api.obi.json"},
		{"diff", "old.obi.json", "new.obi.json"},
		{"compat", "old.obi.json", "new.obi.json"},
		{"patch", "api.obi.json", "change.json"},
		{"operation", "conform", "api.obi.json", "reference.obi.json"},
		{"source", "inspect", "api.yaml"},
		{"source", "binding-specs", "list"},
		{"serve", "api"},
	} {
		_, _, err := runSurfaceRoot(NewHybridSurfaceRoot(), args...)
		if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
			t.Errorf("%v did not reach inert hybrid leaf: %v", args, err)
		}
	}
}

func TestSurfaceCompactTaskPathsAndSamples(t *testing.T) {
	for _, args := range [][]string{
		{"compare", "compat", "before.obi.json", "after.obi.json"},
		{"edit", "patch", "api.obi.json", "changes.json"},
		{"config", "context", "get", "https://example.test"},
		{"serve", "api"},
		{"source", "status", "api.obi.json"},
	} {
		_, _, err := runSurfaceRoot(NewCompactSurfaceRoot(), args...)
		if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
			t.Errorf("%v did not reach inert compact leaf: %v", args, err)
		}
	}
	out, _, err := runSurfaceRoot(NewCompactSurfaceRoot(), "source", "status", "api.obi.json", "--sample-output", "-F", "json")
	if err != nil || !strings.Contains(out, `"sourceDrift": false`) {
		t.Fatalf("moved sample did not render: out=%q err=%v", out, err)
	}
}

func TestSurfaceOutputContractRejectsAmbiguousModes(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"config", "context", "set", "https://example.test", "--value", "{}", "-o", "alternate.json"}, "-o is not an alternate config destination"},
		{[]string{"invoke", "api.obi.json", "listPets", "-o", "events.json"}, "redirect stdout"},
		{[]string{"invoke", "api.obi.json", "listPets", "-F", "yaml"}, "requires --envelope"},
		{[]string{"binding", "invoke"}, "requires --request"},
		{[]string{"binding", "invoke", "--input", "{}"}, "requires --request"},
		{[]string{"binding", "invoke", "--request", "{}", "-F", "yaml"}, "machine lane"},
		{[]string{"binding", "invoke", "api.obi.json", "b", "--request", "{}"}, "takes no positional arguments"},
		{[]string{"edit", "strip-ob-metadata", "api.obi.json", "--check", "-o", "out.json"}, "--check writes no document"},
		{[]string{"new", "-F", "text"}, "use -F json or yaml"},
		{[]string{"new", "-o", "api.obi.json", "-F", "yaml"}, "stdout view"},
		{[]string{"resolve", "example.com", "-o", "api.obi.json", "-F", "yaml"}, "stdout view"},
		{[]string{"synthesize", "-o", "api.obi.json", "-F", "yaml"}, "stdout view"},
		{[]string{"config", "delegate", "requirements", "invoke", "-o", "required.obi.json", "-F", "yaml"}, "stdout view"},
		{[]string{"show", "api.obi.json", "-F", "xml"}, "-F must be"},
		{[]string{"compare", "diff", "before.obi.json", "after.obi.json", "--quiet", "-o", "out.json"}, "--quiet emits no result"},
	} {
		_, _, err := runSurfaceRoot(NewCompactSurfaceRoot(), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestSurfaceBindingMachineSampleIsOneJSONLine(t *testing.T) {
	out, errOut, err := runSurfaceRoot(NewCompactSurfaceRoot(), "binding", "invoke", "--request", "{}", "--sample-output")
	if err != nil || !strings.Contains(errOut, "SAMPLE OUTPUT") || strings.Count(strings.TrimSpace(out), "\n") != 0 || !strings.Contains(out, `"output"`) {
		t.Fatalf("machine sample: stdout=%q stderr=%q err=%v", out, errOut, err)
	}
}

func TestSurfaceCoreTaskPaths(t *testing.T) {
	for _, args := range [][]string{
		{"show", "missing.obi.json"},
		{"source", "add", "missing.obi.json", "api.yaml", "--pull"},
		{"binding", "add", "missing.obi.json", "listPets.http", "--operation", "listPets", "--source", "api"},
		{"dependency", "add", "missing.obi.json", "billing", "--operation", "chargeCard"},
		{"patch", "missing.obi.json", "changes.json"},
		{"invoke", "missing.obi.json", "listPets", "--context", "@context.json"},
		{"invoke", "missing.obi.json", "listPets", "--envelope", "--max-events", "10", "--timeout", "5s"},
		{"invoke", "missing.obi.json", "chat", "--input-stream", "-"},
	} {
		_, _, err := runSurface(t, args...)
		if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
			t.Errorf("%v did not reach an inert leaf: %v", args, err)
		}
	}
}

func TestSurfaceRejectsInvalidShapes(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"source", "add", "api.obi.json"}, "accepts 2 arg(s)"},
		{[]string{"dependency", "add", "api.obi.json", "billing"}, "operation"},
		{[]string{"binding", "add", "api.obi.json", "b", "--operation", "o"}, "source"},
		{[]string{"binding", "add", "api.obi.json", "b", "--operation", "o", "--source", "s", "--preference", "1.5"}, "preference must be an integer"},
		{[]string{"operation", "bind", "api.obi.json", "o", "s", "x", "--preference", "9007199254740992"}, "preference must be an integer"},
		{[]string{"source", "add", "--input", "{}", "--pull"}, "cannot be combined with --pull"},
		{[]string{"new", "first.json"}, "use --name <name>"},
		{[]string{"new", "--force"}, "--force requires -o"},
		{[]string{"start", "-o", "server.json"}, "does not accept --output"},
		{[]string{"mcp", "api.obi.json", "-F", "json"}, "does not accept --format"},
		{[]string{"binding", "add", "api.obi.json", "bad/key", "--operation", "o", "--source", "s"}, "binding key"},
		{[]string{"dependency", "add", "api.obi.json", "billing", "--operation", "o", "--binding-spec", "mcp", "--binding-spec", "mcp"}, "non-empty and unique"},
		{[]string{"invoke", "-", "listPets", "--input", "-"}, "only one"},
		{[]string{"invoke", "api.obi.json", "listPets", "--max-events", "10"}, "only with --envelope"},
		{[]string{"invoke", "api.obi.json", "chat", "--input", "{}", "--input-stream", "@events.ndjson"}, "either --input or --input-stream"},
		{[]string{"invoke", "api.obi.json", "chat", "--input-stream", "events.ndjson"}, "expects @file or -"},
		{[]string{"invoke", "api.obi.json", "chat", "--input-stream", "-", "--context", "-"}, "only one"},
		{[]string{"dependency", "nonsense"}, "unknown command"},
		{[]string{"source", "pull", "api.obi.json", "--strip-ob-metadata"}, "requires -o"},
		{[]string{"codegen", "api.obi.json"}, "--lang is required"},
		{[]string{"merge", "target.obi.json", "source.obi.json", "--out", "other.obi.json"}, "superseded by -o"},
	} {
		_, _, err := runSurface(t, tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestSurfaceDiscoveryRecoveryAcrossVariants(t *testing.T) {
	for _, variant := range []struct {
		name      string
		build     func() *cobra.Command
		diff      []string
		status    string
		context   string
		specCheck string
		strip     string
	}{
		{"broad", NewSurfaceRoot, []string{"diff"}, "ob status", "ob context set", "ob binding-specs check", "ob strip-ob-metadata"},
		{"compact", NewCompactSurfaceRoot, []string{"compare", "diff"}, "ob source status", "ob config context set", "ob config binding-specs check", "ob edit strip-ob-metadata"},
		{"hybrid", NewHybridSurfaceRoot, []string{"diff"}, "ob status", "ob context set", "ob source binding-specs check", "ob strip-ob-metadata"},
	} {
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"obi", "create"}, "OBI tasks are direct"},
			{[]string{"create"}, "ob new"},
			{[]string{"export"}, variant.strip},
			{[]string{"auth"}, variant.context},
			{[]string{"spec"}, variant.specCheck},
			{[]string{"bindings"}, variant.specCheck},
			{[]string{"consumption-point"}, "ob dependency add"},
			{[]string{"consume"}, "ob dependency add"},
			{[]string{"policy"}, "compat"},
			{[]string{"replace", "check", "old.obi.json", "new.obi.json"}, "compat"},
			{[]string{"source", "attach", "api.obi.json", "api.yaml"}, "source add"},
			{[]string{"invoke", "--request", "{}"}, "binding invoke"},
			{[]string{"invoke", "--input", "@request.json"}, "binding invoke"},
			{[]string{"binding", "add", "api.obi.json", "b", "--accept", "example.http@1"}, "dependency add"},
			{[]string{"dependency", "add", "api.obi.json", "billing", "--accept", "example.http@1"}, "repeat --binding-spec"},
			{[]string{"new", "--title", "Acme"}, "use --name"},
			{[]string{"source", "add", "api.obi.json", "api.yaml", "--derive"}, "use --pull"},
			{[]string{"source", "add", "api.obi.json", "--file", "api.yaml"}, "second argument"},
		} {
			_, _, err := runSurfaceRoot(variant.build(), tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %v: got %v, want %q", variant.name, tc.args, err, tc.want)
			}
		}
		contextArgs := append(strings.Fields(variant.context)[1:], "https://example.test", "--bearer", "test")
		_, _, contextErr := runSurfaceRoot(variant.build(), contextArgs...)
		if contextErr == nil || !strings.Contains(contextErr.Error(), "use --bearer-token -") {
			t.Errorf("%s %v: got %v, want bearer-token guidance", variant.name, contextArgs, contextErr)
		}
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"api.obi.json"}, "either provide two OBI arguments or use --from-sources"},
			{[]string{"api.obi.json", "other.obi.json", "--from-sources"}, "cannot use both"},
			{[]string{"api.obi.json", "other.obi.json", "--only", "api"}, "--only requires --from-sources"},
		} {
			args := append(append([]string{}, variant.diff...), tc.args...)
			_, _, err := runSurfaceRoot(variant.build(), args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %v: got %v, want %q", variant.name, args, err, tc.want)
			}
		}
		invalidFlagArgs := append(append([]string{}, variant.diff...), "api.obi.json", "--sources")
		_, _, flagErr := runSurfaceRoot(variant.build(), invalidFlagArgs...)
		if flagErr == nil || !strings.Contains(flagErr.Error(), "use --from-sources") {
			t.Errorf("%s %v: got %v, want --from-sources guidance", variant.name, invalidFlagArgs, flagErr)
		}
		trackedArgs := append(append([]string{}, variant.diff...), "api.obi.json", "--tracked")
		_, _, trackedErr := runSurfaceRoot(variant.build(), trackedArgs...)
		if trackedErr == nil || !strings.Contains(trackedErr.Error(), "use --from-sources") {
			t.Errorf("%s %v: got %v, want --from-sources guidance", variant.name, trackedArgs, trackedErr)
		}
		initPath := strings.Fields(surfacePathForContract(variant.build(), "ob init"))[1:]
		_, _, initErr := runSurfaceRoot(variant.build(), append(initPath, "--name", "Products")...)
		if initErr == nil || !strings.Contains(initErr.Error(), "use \"ob new\"") {
			t.Errorf("%s init --name: got %v, want ob new guidance", variant.name, initErr)
		}
		for _, tail := range [][]string{{"api.obi.json", "other.obi.json"}, {"api.obi.json", "--from-sources"}} {
			args := append(append([]string{}, variant.diff...), tail...)
			_, _, err := runSurfaceRoot(variant.build(), args...)
			if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
				t.Errorf("%s valid diff %v: %v", variant.name, args, err)
			}
		}
		out, _, err := runSurfaceRoot(variant.build(), append(append([]string{}, variant.diff...), "--help")...)
		if err != nil || !strings.Contains(out, variant.status) {
			t.Errorf("%s diff help omits %s: %v %q", variant.name, variant.status, err, out)
		}
	}
}

func TestSurfacePreflightRejectsInvalidPathsBeforeFlagsOrHelp(t *testing.T) {
	for _, root := range []*cobra.Command{NewSurfaceRoot(), NewCompactSurfaceRoot(), NewHybridSurfaceRoot()} {
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"auth", "set", "https://example.test", "--bearer-token", "test"}, "context set"},
			{[]string{"auth", "--help"}, "context set"},
			{[]string{"interface", "create", "--title", "Inventory"}, "OBI tasks are direct"},
			{[]string{"binding", "supports", "--help"}, "binding-specs check"},
			{[]string{"bindings", "supports", "vendor.widgets@2"}, "binding-specs check"},
			{[]string{"source", "attach", "--help"}, "source add"},
			{[]string{"policy", "check", "a.obi.json", "b.obi.json"}, "compat"},
			{[]string{"replace", "check", "a.obi.json", "b.obi.json"}, "compat"},
		} {
			if err := SurfacePreflightArgs(root, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %v: got %v, want %q", root.Short, tc.args, err, tc.want)
			}
		}
		if surfacePathForContract(root, "ob status") != "ob source status" {
			if err := SurfacePreflightArgs(root, []string{"source", "status", "api.obi.json"}); err == nil || !strings.Contains(err.Error(), "per-source drift report") {
				t.Errorf("source status: got %v, want direct status guidance", err)
			}
		}
		for _, args := range [][]string{
			{"new", "--help"}, {"-F", "json", "show", "api.obi.json"},
			{"source", "add", "api.obi.json", "api.yaml", "--pull"},
		} {
			if err := SurfacePreflightArgs(root, args); err != nil {
				t.Errorf("%s valid %v: %v", root.Short, args, err)
			}
		}
	}
}

func TestSurfaceServerHelpOmitsInapplicableOutputFlags(t *testing.T) {
	for _, command := range []string{"start", "mcp", "demo"} {
		out, _, err := runSurface(t, command, "--help")
		if err != nil || strings.Contains(out, "Global Flags:") {
			t.Errorf("%s help has inapplicable global flags: err=%v output=%q", command, err, out)
		}
	}
}

func TestSurfaceLaneHelpOmitsRejectedOutputFlag(t *testing.T) {
	for _, args := range [][]string{
		{"context", "set", "--help"},
		{"invoke", "--help"},
		{"operation", "invoke", "--help"},
	} {
		out, _, err := runSurface(t, args...)
		if err != nil || strings.Contains(out, "-o, --output") || !strings.Contains(out, "-F, --format") {
			t.Errorf("%v: bad lane help: err=%v output=%q", args, err, out)
		}
	}
}

func TestSurfaceSamplesAreExplicitAndInert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.obi.json")
	out, errOut, err := runSurface(t, "show", path, "--sample-output", "-F", "json")
	if err != nil || !strings.Contains(out, `"dependencies": 1`) || !strings.Contains(errOut, "SAMPLE OUTPUT") {
		t.Fatalf("show sample: out=%q stderr=%q err=%v", out, errOut, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("sample accessed or created its locator: %v", err)
	}
	out, _, err = runSurface(t, "invoke", path, "listPets", "--sample-output")
	if err != nil || strings.Count(strings.TrimSpace(out), "\n") != 0 || !strings.Contains(out, `"pets"`) {
		t.Fatalf("invoke sample is not one JSON event: out=%q err=%v", out, err)
	}
	_, _, err = runSurface(t, "show", path, "--sample-output", "-o", filepath.Join(t.TempDir(), "would-write.json"))
	if err == nil || !strings.Contains(err.Error(), "never writes files") {
		t.Fatalf("sample output must reject -o, got %v", err)
	}
}

func TestSurfaceFormatDoesNotSelectSecretsOrEnvelope(t *testing.T) {
	out, _, err := runSurface(t, "context", "get", "https://example.test", "--sample-output", "-F", "json")
	if err != nil || !strings.Contains(out, "********") || strings.Contains(out, "sample-secret") {
		t.Fatalf("JSON format revealed secrets without explicit flag: out=%q err=%v", out, err)
	}
	out, _, err = runSurface(t, "context", "get", "https://example.test", "--reveal-secrets", "--sample-output", "-F", "json")
	if err != nil || !strings.Contains(out, "sample-secret") {
		t.Fatalf("explicit reveal did not change sample content: out=%q err=%v", out, err)
	}
	out, _, err = runSurface(t, "codegen", "api.obi.json", "--lang", "go", "--sample-output", "-F", "json")
	if err != nil || strings.Contains(out, `"language"`) || !strings.Contains(out, "illustrative generated go") {
		t.Fatalf("format changed codegen result shape: out=%q err=%v", out, err)
	}
	out, _, err = runSurface(t, "codegen", "api.obi.json", "--lang", "go", "--envelope", "--sample-output", "-F", "json")
	if err != nil || !strings.Contains(out, `"language": "go"`) {
		t.Fatalf("explicit envelope did not change codegen result shape: out=%q err=%v", out, err)
	}
	_, _, err = runSurface(t, "codegen", "api.obi.json", "--lang", "go", "--sample-output", "-o", filepath.Join(t.TempDir(), "invoker.go"))
	if err == nil || !strings.Contains(err.Error(), "never writes files") {
		t.Fatalf("codegen sample must reject local -o, got %v", err)
	}
}

func TestSurfaceFullSampleIsConformantOBI(t *testing.T) {
	data, err := prettyCanonicalJSON(sampleOBI())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openbindings.ValidateDocument(data); err != nil {
		t.Fatalf("sample OBI is not conformant: %v", err)
	}
}
