package cmd

import (
	"strings"
	"testing"
)

func TestNextPullExplicitNames(t *testing.T) {
	schema := nxSchema(t)
	for _, variant := range nxVariants {
		out, _, err := nxExec(variant, "source", "pull", "-", "httpApi", "--target", "POST /tasks/{id}/archive", "--new-operation", "archive", "--binding-key", "archivalHttp")
		if err != nil {
			t.Fatalf("variant %q: %v", variant, err)
		}
		nxConforms(t, schema, "explicit pulled names", out)
		doc := nxMustParse(out).(*nxObj)
		if doc.Obj("operations").Obj("archive") == nil || doc.Obj("operations").Has("archiveTask") {
			t.Fatalf("new operation name was not honored: %s", out)
		}
		binding := doc.Obj("bindings").Obj("archivalHttp")
		if binding == nil || binding.Get("operation") != "archive" || binding.Get("source") != "httpApi" {
			t.Fatalf("binding names were not honored: %s", out)
		}
		if nxCompact(doc.Obj("operations").Obj("archive")) != nxCompact(nxMustParse(nxTargets[3].newOperation)) {
			t.Fatal("creating a named operation lost the handler's framing")
		}
	}

	out, _, err := nxExec("", "source", "pull", "-", "httpApi", "--target", "POST /tasks/{id}/archive", "--operation", "acme.tasks.createTask", "--binding-key", "secondHttp")
	if err != nil {
		t.Fatal(err)
	}
	nxConforms(t, schema, "additional binding through the same source", out)
	doc := nxMustParse(out).(*nxObj)
	if doc.Obj("bindings").Obj("secondHttp").Get("operation") != "createTask" {
		t.Fatal("alias was not stored as the operation key")
	}
	if nxCompact(doc.Obj("operations")) != nxCompact(nxFixture().Obj("operations")) {
		t.Fatal("attaching a binding changed existing operation fields")
	}

	out, _, err = nxExec("", "source", "pull", "-", "mcpServer", "--target", "tools/complete_task", "--operation", "completeTask")
	if err != nil {
		t.Fatal(err)
	}
	doc = nxMustParse(out).(*nxObj)
	if doc.Obj("bindings").Obj("complete_task.mcp").Get("operation") != "completeTask" {
		t.Fatal("choosing an operation changed the handler's suggested binding key")
	}
}

func TestNextPullNamingErrors(t *testing.T) {
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--target", "POST /tasks/{id}/archive", "--operation", "completeTaks"}, 2, "completeTask"},
		{[]string{"--target", "POST /tasks", "--operation", "craeteTask"}, 2, "createTask"},
		{[]string{"--target", "POST /tasks", "--operation", "listTasks"}, 3, "--binding-key"},
		{[]string{"--target", "POST /tasks/{id}/archive", "--operation", "completeTask", "--new-operation", "archive"}, 2, "choose one"},
		{[]string{"--new-operation", "archive"}, 2, "--target"},
		{[]string{"--binding-key", "archivalHttp", "--all-targets"}, 2, "--target"},
		{[]string{"--target", "POST /tasks/{id}/archive", "--operation", "completeTask", "--binding-key", "completeTask.http"}, 3, "occupied"},
		{[]string{"--target", "POST /tasks/{id}/archive", "--new-operation", "createTask"}, 3, "use --operation"},
		{[]string{"--target", "POST /tasks/{id}/archive", "--new-operation", "acme.tasks.createTask"}, 3, "OBI-D-04"},
		{[]string{"--target", "POST /tasks/{id}/archive", "--new-operation", "bad/name"}, 3, "OBI-D-03"},
		{[]string{"--target", "POST /tasks/{id}/archive", "--binding-key", ""}, 3, "OBI-D-03"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append([]string{"source", "pull", "tasks.obi.json", "httpApi"}, tc.args...)
			out, notes, err := nxExec("", args...)
			if nxExitCode(err) != tc.code || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("exit %d: %v; want exit %d containing %q", nxExitCode(err), err, tc.code, tc.want)
			}
			if out != "" || strings.Contains(notes, "no change") {
				t.Fatalf("failed request printed an edit or no-change success: %s %s", out, notes)
			}
		})
	}
}

// The preview always starts from its sample. Calling the pull handler with
// a staged document lets these tests exercise retries on its previous result.
func nxPullDocument(t *testing.T, before *nxObj, args ...string) (*nxObj, string, string, error) {
	t.Helper()
	root := NewNextSurfaceRoot("")
	cmd, _, err := root.Find([]string{"source", "pull"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.ParseFlags(append([]string{"tasks.obi.json"}, args...)); err != nil {
		t.Fatal(err)
	}
	c := &nxCtx{cmd: cmd, args: cmd.Flags().Args()}
	after := nxClone(before).(*nxObj)
	if c.set("target") {
		err = nxPullTarget(c, before, after, c.args[1], c.str("target"))
	} else {
		err = nxPull(c, before, after)
	}
	return after, c.out.String(), strings.Join(c.notes, "\n"), err
}

func TestNextPullRetriesHonorTheRequestedResult(t *testing.T) {
	args := []string{"httpApi", "--target", "POST /tasks/{id}/archive", "--new-operation", "archive", "--binding-key", "archivalHttp"}
	doc, _, _, err := nxPullDocument(t, nxFixture(), args...)
	if err != nil {
		t.Fatal(err)
	}
	after, out, notes, err := nxPullDocument(t, doc, args...)
	if err != nil || nxCompact(after) != nxCompact(doc) || out != "" || !strings.Contains(notes, "no change") {
		t.Fatalf("identical creation retry was not no change: %v %s %s", err, out, notes)
	}
	for _, flags := range [][]string{
		{"--operation", "completeTask", "--binding-key", "archivalHttp"},
		{"--operation", "completeTask"},
		{"--new-operation", "archive", "--binding-key", "anotherArchive"},
	} {
		args := append([]string{"httpApi", "--target", "POST /tasks/{id}/archive"}, flags...)
		original := nxCompact(doc)
		_, out, notes, err := nxPullDocument(t, doc, args...)
		if nxExitCode(err) != 3 || out != "" || strings.Contains(notes, "no change") || nxCompact(doc) != original {
			t.Fatalf("different requested result was swallowed: %v %s %s", err, out, notes)
		}
	}
	additional := []string{"httpApi", "--target", "POST /tasks/{id}/archive", "--operation", "archive", "--binding-key", "anotherArchive"}
	after, _, _, err = nxPullDocument(t, doc, additional...)
	if err != nil || !after.Obj("bindings").Has("anotherArchive") {
		t.Fatalf("explicit second binding was not created: %v", err)
	}
	retry, out, notes, err := nxPullDocument(t, after, additional...)
	if err != nil || nxCompact(retry) != nxCompact(after) || out != "" || !strings.Contains(notes, "no change") {
		t.Fatalf("second-binding retry was not no change: %v %s %s", err, out, notes)
	}
}

func TestNextPullBulkChoicesAndRecovery(t *testing.T) {
	out, notes, err := nxExec("", "source", "pull", "tasks.obi.json", "httpApi", "--all-targets", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"new operation archiveTask", "new operation getHealth", "in place of --all-targets", "--target 'POST /tasks/{id}/archive' --operation <existing-operation>"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("dry run lacks %q: %s", want, notes)
		}
	}
	if !strings.Contains(out, "dry run, nothing written") || strings.Contains(notes, "ob binding set") {
		t.Fatalf("dry run did not report choices before creation: %s %s", out, notes)
	}
	_, notes, err = nxExec("", "source", "pull", "tasks.obi.json", "httpApi", "--all-targets")
	if err != nil || !strings.Contains(notes, "ob binding set 'tasks.obi.json' 'archiveTask.http' --operation <existing-operation>") || !strings.Contains(notes, "ob operation remove 'tasks.obi.json' 'archiveTask'") || strings.Contains(notes, "in place of --all-targets") {
		t.Fatalf("post-edit repair was not actionable: %v %s", err, notes)
	}
}

func TestNextPullBulkNamingConflictsRefuseTheWholeEdit(t *testing.T) {
	doc := nxFixture()
	doc.Obj("bindings").Set("getHealth.http", nxNewObj().Set("operation", "listTasks").Set("source", "httpApi").Set("content", nxNewObj().Set("target", "another-target")))
	original := nxCompact(doc)
	_, out, notes, err := nxPullDocument(t, doc, "httpApi", "--all-targets")
	if nxExitCode(err) != 3 || !strings.Contains(err.Error(), "--binding-key") || out != "" || strings.Contains(notes, "new operation") || nxCompact(doc) != original {
		t.Fatalf("bulk collision did not refuse all staged changes: %v %s %s", err, out, notes)
	}

	originalTargets := nxTargets
	t.Cleanup(func() { nxTargets = originalTargets })
	for _, conflict := range []string{"operation", "binding"} {
		t.Run(conflict, func(t *testing.T) {
			extra := originalTargets[3]
			extra.target = "POST /other"
			extra.content = `{"target":"#/paths/~1other/post"}`
			if conflict == "operation" {
				extra.binding = "other.http"
			} else {
				extra.operation = "other"
			}
			nxTargets = append(append([]nxTarget(nil), originalTargets...), extra)
			doc := nxFixture()
			original := nxCompact(doc)
			_, out, notes, err := nxPullDocument(t, doc, "httpApi", "--all-targets", "--dry-run")
			if nxExitCode(err) != 3 || out != "" || strings.Contains(notes, "new operation") || nxCompact(doc) != original {
				t.Fatalf("prospective %s collision wrote an edit: %v %s %s", conflict, err, out, notes)
			}
		})
	}
}

func TestNextPullOptionalSuggestionsAndAliasDefaults(t *testing.T) {
	originalTargets := nxTargets
	t.Cleanup(func() { nxTargets = originalTargets })
	target := nxTarget{source: "httpApi", target: "minimal", content: `{"target":"minimal"}`}
	nxTargets = append(append([]nxTarget(nil), originalTargets...), target)
	_, _, _, err := nxPullDocument(t, nxFixture(), "httpApi", "--target", "minimal")
	if nxExitCode(err) != 3 || !strings.Contains(err.Error(), "--new-operation") {
		t.Fatalf("missing suggestion was silently invented: %v", err)
	}
	doc, _, _, err := nxPullDocument(t, nxFixture(), "httpApi", "--target", "minimal", "--new-operation", "minimalOp", "--binding-key", "minimalBinding")
	if err != nil || nxCompact(doc.Obj("operations").Obj("minimalOp")) != "{}" {
		t.Fatalf("missing framing was not represented by absent schemas: %v", err)
	}

	target.operation, target.binding = "acme.tasks.createTask", "aliasSuggestedBinding"
	nxTargets = append(append([]nxTarget(nil), originalTargets...), target)
	doc, _, notes, err := nxPullDocument(t, nxFixture(), "httpApi", "--all-targets", "--dry-run")
	if err != nil || doc.Obj("bindings").Obj(target.binding).Get("operation") != "createTask" || !strings.Contains(notes, "existing operation createTask") || nxCompact(doc.Obj("operations").Obj("createTask")) != nxCompact(nxFixture().Obj("operations").Obj("createTask")) {
		t.Fatalf("suggested alias was not exact reuse: %v %s", err, notes)
	}
}
