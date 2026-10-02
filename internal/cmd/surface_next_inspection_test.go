package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nxInspectionObject(t *testing.T, out string) *nxObj {
	t.Helper()
	value, err := nxParse(out)
	obj, ok := value.(*nxObj)
	if err != nil || !ok {
		t.Fatalf("expected JSON object: %q, %v", out, err)
	}
	return obj
}

func TestNextContextInspectionPreservesNativeValues(t *testing.T) {
	before := nxContexts[0]
	t.Cleanup(func() { nxContexts[0] = before })
	context := nxMustParse(`{"credentials":{"provider.v1":{"accessToken":"secret-oauth-value","expiresAt":0}},"headers":{"X.Client":"secret-header-value"},"configuration":{"server.v1":{"enabled":false,"attempts":0,"ratio":0.1234567890123456789}},"metadata":{},"custom.with.dot":{"nested":[null,false,0,"secret-custom-value"]}}`).(*nxObj)
	nxContexts[0].context = context
	for _, format := range []string{"text", "json"} {
		out, _, err := nxExec("", "context", "show", before.scope, "-F", format)
		if err != nil || strings.Contains(out, "secret-") || strings.Contains(out, "0.123456789") {
			t.Fatalf("%s leaked a masked value: %q, %v", format, out, err)
		}
		if format == "json" {
			masked := nxInspectionObject(t, out).Obj("context")
			if masked.Obj("credentials").Obj("provider.v1").Get("masked") != true || masked.Obj("configuration").Obj("server.v1").Get("masked") != true || masked.Obj("custom.with.dot").Get("masked") != true || masked.Obj("metadata").Len() != 0 {
				t.Fatalf("masking split literal names or exposed nested data: %s", out)
			}
		}
	}
	out, _, err := nxExec("", "context", "show", before.scope, "--reveal", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	revealed := nxInspectionObject(t, out)
	if nxCompact(revealed.Obj("context")) != nxCompact(context) || revealed.Obj("resolver").Len() != 0 {
		t.Fatalf("revealed Context changed value types or literal names: %s", out)
	}
	root := NewNextSurfaceRoot("")
	root.SetIn(strings.NewReader(nxCompact(revealed.Obj("context"))))
	reader := nxCtx{cmd: root}
	parsed, err := reader.readJSONObject("-", "--value")
	if err != nil || nxCompact(parsed) != nxCompact(context) {
		t.Fatalf("whole-value input reader changed native values: %v", err)
	}
	out, _, err = nxExecIn("", nxCompact(revealed.Obj("context")), "context", "set", before.scope, "--value", "-")
	if err != nil || !strings.Contains(out, "Replaced the context") || strings.Contains(out, "secret-") {
		t.Fatalf("revealed object could not be reused safely: %q, %v", out, err)
	}
	// The preview starts fresh on each command; it must not mutate fixtures.
	if nxCompact(nxContexts[0].context) != nxCompact(context) {
		t.Fatal("context set mutated the shared fixture")
	}
	_, _, err = nxExec("", "context", "set", before.scope, "--unset", "credential.provider.v1", "--unset", "header.X.Client", "--unset", "config.server.v1")
	if err != nil {
		t.Fatalf("literal dotted names cannot be addressed: %v", err)
	}
}

func TestNextContextResolverIsSeparateAndWholeValueInputIsRead(t *testing.T) {
	out, _, err := nxExec("", "context", "show", "https://tokens.example.com", "--reveal", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	report := nxInspectionObject(t, out)
	if report.Obj("context").Len() != 0 || report.Obj("resolver").Get("tokenProvider") != "https://auth.example.com" {
		t.Fatalf("resolver settings were mixed into Context: %s", out)
	}
	file := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(file, []byte(`{"configuration":{"enabled":false},"credentials":{"primary":"a-secret"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, _, err = nxExec("", "context", "set", "https://api.example.com", "--value", "@"+file)
	if err != nil || strings.Contains(out, "a-secret") {
		t.Fatalf("whole file input: %q, %v", out, err)
	}
	for _, input := range []string{"", `[]`, `null`, `{} {}`, `{"credentials":"secret-in-invalid-input"`, `{}]`} {
		out, note, err := nxExecIn("", input, "context", "set", "https://api.example.com", "--value", "-")
		if nxExitCode(err) != 2 || out != "" || strings.Contains(note+err.Error(), "secret-in-invalid-input") {
			t.Fatalf("invalid whole input %q: %q, %q, %v", input, out, note, err)
		}
	}
	_, _, err = nxExec("", "context", "set", "https://api.example.com", "--value", "@"+file+".missing")
	if nxExitCode(err) != 1 {
		t.Fatalf("unreadable whole value: %v", err)
	}
}

func TestNextDelegateInspectionCarriesCompleteInterfaces(t *testing.T) {
	out, _, err := nxExec("", "delegate", "roles", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	roles, ok := nxMustParse(out).([]any)
	if !ok || len(roles) != 3 {
		t.Fatalf("role catalogue: %s", out)
	}
	for _, entry := range roles {
		role := entry.(*nxObj)
		if role.Get("description") == "" {
			t.Fatal("missing application admission/use policy")
		}
		expected := role.Get("acceptedInterfaces").([]any)[0].(*nxObj)
		if expected.Obj("operations").Len() == 0 || expected.Obj("schemas").Len() == 0 || expected.Has("bindings") || expected.Has("dependencies") {
			t.Fatalf("incomplete expected interface: %s", nxPretty(expected))
		}
		if violations := nxViolations(expected); len(violations) != 0 {
			t.Fatalf("invalid expected %s: %v", role.Get("id"), violations)
		}
		if strings.Contains(nxCompact(expected), "#/schemas/BindingSpec") {
			t.Fatal("kinds migration left an unresolved old schema name")
		}
		if role.Get("id") == "invoke" && expected.Obj("operations").Has("openbindings.binding-invoker.preflightBinding") {
			t.Fatal("optional preflight became an admission requirement")
		}
		if role.Get("id") == "inspect" && (!expected.Obj("operations").Has("openbindings.interface-synthesizer.checkKindSupport") || expected.Obj("operations").Has("openbindings.source-inspector.checkKindSupport")) {
			t.Fatal("inspection advertised an invented Source Inspector query")
		}
	}
	out, _, err = nxExec("", "delegate", "show", "d_91c2", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	registration := nxInspectionObject(t, out)
	obi := registration.Obj("interface")
	if obi.Obj("schemas").Len() == 0 || obi.Obj("bindings").Len() == 0 || obi.Obj("sources").Len() == 0 || !obi.Has("x-lab") || registration.Obj("rolePreferences").Get("invoke") != json.Number("10") || registration.Obj("rolePreferences").Has("inspect") {
		t.Fatalf("registration lost retained values or explicit/absent preferences: %s", out)
	}
	obi.Set("x-extra", nxMustParse(`{"nested":[false,0,null,{"literal.key":"value"}]}`))
	obi.Set("name", "Edited delegate")
	out, _, err = nxExecIn("", nxCompact(obi), "delegate", "set", "d_91c2", "--obi", "-", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	changed := nxInspectionObject(t, out)
	if nxCompact(changed.Obj("interface")) != nxCompact(obi) || changed.Obj("rolePreferences").Get("invoke") != json.Number("10") {
		t.Fatalf("export/edit/reuse lost interface or registration preferences: %s", out)
	}
	out, _, err = nxExec("", "delegate", "show", "d_91c2", "-F", "json")
	if err != nil || nxInspectionObject(t, out).Obj("interface").Has("x-extra") {
		t.Fatal("a preview edit mutated the stored fixture")
	}
}

func TestNextDelegatePreferencesRetainJSONNumbers(t *testing.T) {
	for _, number := range []string{"0", "-0", "-1.25", "0.12345678901234567890123456789", "9007199254740993", "1e100000"} {
		for _, args := range [][]string{
			{"delegate", "add", "sample", "--role", "invoke"},
			{"delegate", "set", "d_91c2"},
		} {
			out, _, err := nxExec("", append(args, "--preference", "invoke="+number, "-F", "json")...)
			if err != nil {
				t.Fatal(err)
			}
			prefs := nxInspectionObject(t, out).Obj("rolePreferences")
			if prefs.Get("invoke") != json.Number(number) || prefs.Has("inspect") {
				t.Fatalf("number was coerced or absent preference invented: %s", out)
			}
		}
	}
	for _, number := range []string{"NaN", "Infinity", "01", "+1", "0x10", "null", "[]", "true", `"1"`, "1 2", "1.", ""} {
		out, _, err := nxExec("", "delegate", "set", "d_91c2", "--preference", "invoke="+number, "-F", "json")
		if nxExitCode(err) != 2 || out != "" {
			t.Fatalf("invalid number %q: %q, %v", number, out, err)
		}
	}
}

func TestNextDelegateEditsRejectAmbiguityAndPreserveOtherValues(t *testing.T) {
	for _, args := range [][]string{
		{"add", "sample", "--role", "invoke", "--role", "invoke"},
		{"set", "d_91c2", "--add-role", "synthesize", "--add-role", "synthesize"},
		{"set", "d_91c2", "--remove-role", "inspect", "--remove-role", "inspect"},
		{"set", "d_91c2", "--add-role", "inspect", "--remove-role", "inspect"},
		{"set", "d_91c2", "--preference", "invoke=0", "--preference", "invoke=1"},
		{"set", "d_91c2", "--preference", "invoke=0", "--unset", "preference.invoke"},
		{"set", "d_91c2", "--remove-role", "invoke", "--preference", "invoke=0"},
		{"set", "d_91c2", "--preference", "synthesize=0"},
		{"set", "d_91c2", "-F", "json"},
	} {
		out, _, err := nxExec("", append([]string{"delegate"}, args...)...)
		if nxExitCode(err) != 2 || out != "" {
			t.Fatalf("ambiguous edit %v: %q, %v", args, out, err)
		}
	}
	out, _, err := nxExec("", "delegate", "set", "d_91c2", "--remove-role", "invoke", "-F", "json")
	if err != nil || nxInspectionObject(t, out).Obj("rolePreferences").Has("invoke") {
		t.Fatalf("removed membership retained its preference: %q, %v", out, err)
	}
	out, _, err = nxExec("", "delegate", "set", "d_91c2", "--unset", "preference.invoke", "-F", "json")
	if err != nil || nxInspectionObject(t, out).Obj("rolePreferences").Len() != 0 {
		t.Fatalf("unset did not clear explicit preference: %q, %v", out, err)
	}
	out, note, err := nxExec("", "delegate", "set", "d_91c2", "--add-role", "inspect", "-F", "json")
	if err != nil || !strings.Contains(note, "no change") || nxInspectionObject(t, out).Obj("rolePreferences").Get("invoke") != json.Number("10") {
		t.Fatalf("already-true membership: %q, %q, %v", out, note, err)
	}
	out, _, err = nxExec("", "delegate", "set", "d_7f3a", "--remove-role", "invoke", "-F", "json")
	if nxExitCode(err) != 3 || out != "" {
		t.Fatalf("empty role set was accepted: %q, %v", out, err)
	}
}

func TestNextInspectionListsPreserveSignalsAndAbsence(t *testing.T) {
	before := nxDelegates[1].preferences
	t.Cleanup(func() { nxDelegates[1].preferences = before })
	nxDelegates[1].preferences = map[string]json.Number{"invoke": "0"}
	doc := nxFixture()
	binding := doc.Obj("bindings").Obj("createTask.http")
	for _, field := range []string{"preference", "idempotent", "deprecated"} {
		binding.Delete(field)
	}
	absent := nxBindingListEntry(doc, "createTask.http")
	for _, field := range []string{"preference", "idempotent", "deprecated"} {
		if absent.Has(field) {
			t.Fatalf("invented absent %s: %s", field, nxCompact(absent))
		}
	}
	binding.Set("preference", json.Number("0")).Set("idempotent", false).Set("deprecated", false)
	explicit := nxBindingListEntry(doc, "createTask.http")
	if explicit.Get("preference") != json.Number("0") || explicit.Get("idempotent") != false || explicit.Get("deprecated") != false {
		t.Fatalf("zero or false disappeared: %s", nxCompact(explicit))
	}
	for _, command := range []string{"binding", "source", "delegate"} {
		args := []string{command, "list"}
		if command != "delegate" {
			args = append(args, "sample")
		}
		out, _, err := nxExec("", append(args, "-F", "json")...)
		if err != nil {
			t.Fatal(err)
		}
		entries := nxMustParse(out).([]any)
		for _, item := range entries {
			entry := item.(*nxObj)
			switch command {
			case "binding":
				original := nxFixture().Obj("bindings").Obj(entry.Get("name").(string))
				for _, field := range []string{"preference", "idempotent", "deprecated"} {
					if entry.Has(field) != original.Has(field) || entry.Get(field) != original.Get(field) {
						t.Fatalf("binding list lost %s: %s", field, out)
					}
				}
			case "source":
				if !entry.Has("bindingCount") || !entry.Has("canInvoke") {
					t.Fatalf("source list omitted count/support: %s", out)
				}
			case "delegate":
				if entry.Obj("interface") == nil || entry.Obj("rolePreferences") == nil {
					t.Fatalf("delegate list lost retained values: %s", out)
				}
				if entry.Get("id") == "d_91c2" && (entry.Obj("rolePreferences").Get("invoke") != json.Number("0") || entry.Obj("rolePreferences").Has("inspect")) {
					t.Fatalf("delegate list collapsed zero and absence: %s", out)
				}
			}
		}
	}
	doc.Obj("sources").Set("unsupported", nxNewObj().Set("kind", "unknown@1"))
	unsupported := nxSourceListEntry(doc, "unsupported")
	if unsupported.Get("bindingCount") != 0 || unsupported.Get("canInvoke") != false {
		t.Fatal("source report dropped zero/false")
	}
}
