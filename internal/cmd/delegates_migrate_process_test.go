package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openbindings/ob/internal/app"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// buildCandidateBinary compiles the candidate ob once per test into a
// task-private directory; nothing is installed.
func buildCandidateBinary(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ob")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", bin, "./cmd/ob")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// invokeRoleProvider is a complete invoke-role provider whose support query
// and workload are served by a loopback OpenAPI fixture with independent
// counters. Qualified aliases carry the expected keys; no bare-name shortcut.
func invokeRoleProvider(t *testing.T, serverURL string) json.RawMessage {
	t.Helper()
	provider, err := app.RequirementInterface(app.CapInvoke)
	if err != nil {
		t.Fatal(err)
	}
	provider.Name = "journey-provider"
	paths := map[string]any{}
	provider.Bindings = map[string]openbindings.BindingEntry{}
	expected := provider.Operations
	provider.Operations = map[string]openbindings.Operation{}
	for key, op := range expected {
		path := "/work"
		switch {
		case strings.HasSuffix(key, ".checkBindingSpecs"):
			path = "/check"
		case strings.HasSuffix(key, ".listBindingSpecs"):
			path = "/list"
		}
		paths[path] = map[string]any{"post": map[string]any{"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{}}}}, "responses": map[string]any{"200": map[string]any{"description": "ok", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{}}}}}}}
		op.Aliases = append(op.Aliases, key)
		provider.Operations["fixture."+key] = op
		provider.Bindings[key] = openbindings.BindingEntry{Operation: "fixture." + key, Source: "api", Selector: "#/paths/~1" + strings.TrimPrefix(path, "/") + "/post", InputTransform: &openbindings.TransformOrRef{Inline: `{"body": $$}`}}
	}
	artifact := map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "Journey fixture", "version": "1"}, "servers": []any{map[string]any{"url": serverURL}}, "paths": paths}
	artifactJSON, err := jsonvalue.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	provider.Sources = map[string]openbindings.Source{"api": {BindingSpec: openapi.BindingSpecOpenAPI31, Content: artifactJSON}}
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func runCandidate(t *testing.T, ctx context.Context, bin, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	command := exec.CommandContext(ctx, bin, args...)
	command.Dir = dir
	command.Env = append(append([]string{}, os.Environ()...), env...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// TestDelegateMigrationCLIJourney is the real separate-process conversion
// journey: preview → review → apply → restart → route → guarded rollback,
// against a synthetic legacy environment. Every step is its own ob process.
func TestDelegateMigrationCLIJourney(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the real binary")
	}
	bin := buildCandidateBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var queries, work atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/check":
			queries.Add(1)
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			tokens, _ := input["bindingSpecs"].([]any)
			verdicts := []any{}
			for _, token := range tokens {
				verdicts = append(verdicts, map[string]any{"bindingSpec": token, "supported": token == "example.journey@1"})
			}
			_ = json.NewEncoder(w).Encode(verdicts)
		default:
			work.Add(1)
			http.Error(w, "unexpected workload", 500)
		}
	}))
	defer server.Close()
	provider := invokeRoleProvider(t, server.URL)

	workDir := t.TempDir()
	envDir := filepath.Join(workDir, app.EnvDir)
	if err := os.MkdirAll(envDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(envDir, app.EnvConfigFile)
	// Two legacy rows in opposed lexical order with equal preferences: the
	// old registry broke ties by registration order, so "z" won.
	row := func(host string) string {
		return `{"location":"https://` + host + `.example.invalid/obi","operations":["openbindings.binding-invoker.invokeBinding"],"preference":0}`
	}
	legacy := []byte("{\n\"unrelated\":{\"number\":9007199254740993},\"authorizedExec\":[\"exec:kept\"],\"delegates\":[" + row("z") + "," + row("a") + "]\n}\n")
	if err := os.WriteFile(configPath, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{"OB_CONFIG_DIR=" + t.TempDir(), "OB_NO_UPDATE_CHECK=1", "OB_CREDENTIALS_FILE=" + filepath.Join(t.TempDir(), "credentials.json")}
	run := func(args ...string) (string, error) { return runCandidate(t, ctx, bin, workDir, env, args...) }
	listing := func() []string {
		entries, _ := os.ReadDir(envDir)
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	before := listing()

	// Preview is read-only and reports every row unresolved.
	out, err := run("delegate", "migrate", "preview", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	var plan app.DelegateMigrationPlan
	if err := jsonvalue.Unmarshal([]byte(out), &plan); err != nil || len(plan.Entries) != 2 || plan.Entries[0].Disposition != "unresolved" {
		t.Fatalf("preview: %v %s", err, out)
	}
	if got, _ := os.ReadFile(configPath); !bytes.Equal(got, legacy) || strings.Join(listing(), ",") != strings.Join(before, ",") {
		t.Fatal("preview wrote to the environment")
	}
	if _, err := run("delegate", "list"); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("legacy state must refuse listing with guidance: %v", err)
	}
	// Explicit export is owner-only and never overwrites.
	planFile := filepath.Join(t.TempDir(), "plan.json")
	if _, err := run("delegate", "migrate", "preview", "-o", planFile); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(planFile); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("exported plan permissions: %v %v", info, err)
	}
	if _, err := run("delegate", "migrate", "preview", "-o", planFile); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing plan file must be refused: %v", err)
	}
	// Review: convert both rows with the recovered provider and explicit roles.
	for i := range plan.Entries {
		plan.Entries[i].Disposition = "convert"
		plan.Entries[i].Review = "Recovered the provider document and enrolled it for invocation only."
		plan.Entries[i].ProviderReview = "Legacy pin absent; document reviewed by hand."
		plan.Entries[i].Interface = provider
		plan.Entries[i].Roles = []string{"invoke"}
	}
	reviewed, _ := jsonvalue.Marshal(plan)
	reviewedFile := filepath.Join(t.TempDir(), "reviewed.json")
	if err := os.WriteFile(reviewedFile, reviewed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run("delegate", "migrate", "apply", reviewedFile); err == nil || !strings.Contains(err.Error(), "--confirm-quiesced") {
		t.Fatalf("apply without quiescence acknowledgment: %v", err)
	}
	out, err = run("delegate", "migrate", "apply", reviewedFile, "--confirm-quiesced", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	var receipt app.DelegateMigrationReceipt
	if err := json.Unmarshal([]byte(out), &receipt); err != nil || len(receipt.Registrations) != 2 || receipt.Registrations[0] == "" {
		t.Fatalf("receipt: %v %s", err, out)
	}
	// Restart: a fresh process sees both registrations in legacy order.
	out, err = run("delegate", "list", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	var listed app.DelegateList
	if err := jsonvalue.Unmarshal([]byte(out), &listed); err != nil || len(listed.Delegates) != 2 || listed.Delegates[0].ID != receipt.Registrations[0] || listed.Delegates[1].ID != receipt.Registrations[1] {
		t.Fatalf("list after apply: %v %s", err, out)
	}
	config, err := app.LoadEnvConfig(envDir)
	if err != nil || len(config.AuthorizedExec) != 1 || string(config.Extra["unrelated"]) != `{"number":9007199254740993}` {
		t.Fatalf("unrelated configuration not preserved: %+v %v", config, err)
	}
	// Routing: equal preferences, so the former tie winner (row 0, "z") is
	// selected; both providers are queried once and no workload runs.
	out, err = run("delegate", "resolve", "--role", "invoke", "--binding-spec", "example.journey@1", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	var resolved app.RoleResolutionOutput
	if err := json.Unmarshal([]byte(out), &resolved); err != nil || !resolved.Available || resolved.Builtin || resolved.RegistrationID != receipt.Registrations[0] {
		t.Fatalf("resolve: %v %s", err, out)
	}
	if queries.Load() != 2 || work.Load() != 0 {
		t.Fatalf("queries=%d work=%d", queries.Load(), work.Load())
	}
	// Repeated apply consults the receipt: no duplicate conversion.
	if _, err := run("delegate", "migrate", "apply", reviewedFile, "--confirm-quiesced"); err != nil {
		t.Fatalf("receipt-idempotent apply: %v", err)
	}
	out, _ = run("delegate", "list", "-F", "json")
	if err := jsonvalue.Unmarshal([]byte(out), &listed); err != nil || len(listed.Delegates) != 2 {
		t.Fatalf("duplicate conversion: %s", out)
	}
	// A changed plan is stale against the applied receipt.
	stale := plan
	stale.Entries = append([]app.DelegateMigrationEntry{}, plan.Entries...)
	stale.Entries[0].Review = "edited after apply"
	staleBytes, _ := jsonvalue.Marshal(stale)
	staleFile := filepath.Join(t.TempDir(), "stale.json")
	_ = os.WriteFile(staleFile, staleBytes, 0600)
	if _, err := run("delegate", "migrate", "apply", staleFile, "--confirm-quiesced"); err == nil {
		t.Fatal("stale plan applied over a converted environment")
	}
	// Guarded rollback restores the exact original bytes; the registry is
	// legacy again and refuses management with guidance.
	if _, err := run("delegate", "migrate", "rollback", receipt.PlanHash); err == nil {
		t.Fatal("rollback without quiescence acknowledgment")
	}
	if _, err := run("delegate", "migrate", "rollback", receipt.PlanHash, "--confirm-quiesced"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(configPath); !bytes.Equal(got, legacy) {
		t.Fatalf("rollback did not restore the original bytes: %s", got)
	}
	if _, err := run("delegate", "list"); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("post-rollback state: %v", err)
	}
	// Re-apply after rollback issues a fresh namespace; edits after conversion
	// make rollback refuse and keep a recovery copy.
	out, err = run("delegate", "migrate", "apply", reviewedFile, "--confirm-quiesced", "-F", "json")
	if err != nil {
		t.Fatal(err)
	}
	var again app.DelegateMigrationReceipt
	_ = json.Unmarshal([]byte(out), &again)
	if again.Registrations[0] == receipt.Registrations[0] {
		t.Fatal("re-application reused an abandoned issuance namespace")
	}
	if _, err := run("delegate", "prefer", again.Registrations[1], "7", "--role", "invoke"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("delegate", "migrate", "rollback", again.PlanHash, "--confirm-quiesced"); err == nil || !strings.Contains(err.Error(), "newer registry edits") {
		t.Fatalf("rollback over newer edits must refuse: %v", err)
	}
	// Every rollback attempt first preserves the current state as a private
	// recovery copy; the refused attempt's copy carries the newer edit.
	recovery, _ := filepath.Glob(filepath.Join(envDir, ".delegate-migrations", strings.TrimPrefix(again.PlanHash, "sha256:"), "current-*.json"))
	preserved := false
	for _, copyPath := range recovery {
		data, _ := os.ReadFile(copyPath)
		if info, err := os.Stat(copyPath); err == nil && (runtime.GOOS == "windows" || info.Mode().Perm() == 0600) && strings.Contains(string(data), `"invoke":7`) {
			preserved = true
		}
	}
	if !preserved {
		t.Fatalf("recovery copy with the newer edit missing: %v", recovery)
	}
	out, _ = run("delegate", "list", "-F", "json")
	if err := jsonvalue.Unmarshal([]byte(out), &listed); err != nil || listed.Delegates[1].RolePreferences["invoke"] != "7" {
		t.Fatalf("newer edit lost: %s", out)
	}
}
