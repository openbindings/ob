package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func migrationTestRegistry(t *testing.T) (*roleRegistry, json.RawMessage) {
	t.Helper()
	catalogue, err := defaultRoleCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	provider, err := RequirementInterface(CapInvoke)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	return &roleRegistry{path: envConfigTestEnv(t), catalogue: catalogue}, raw
}

func seedLegacyMigration(t *testing.T, r *roleRegistry, provider json.RawMessage) []byte {
	t.Helper()
	iface, err := openbindings.ValidateDocument(provider)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := interfaceContentHash(iface)
	if err != nil {
		t.Fatal(err)
	}
	row := `{"location":"https://z.example.invalid/obi","operations":["openbindings.binding-invoker.invokeBinding"],"contentHash":"` + hash + `","preference":0,"operationPreferences":{"openbindings.binding-invoker.invokeBinding":9007199254740993},"bindingSpecPreferences":[{"operation":"openbindings.binding-invoker.invokeBinding","bindingSpec":"example.test@1","preference":-1.25}]}`
	raw := []byte("{\n\"unrelated\":{\"number\":9007199254740993,\"unicode\":\"\\ud800\"},\"authorizedExec\":[\"exec:kept\"],\"delegates\":[" + row + "," + strings.Replace(row, "z.example", "a.example", 1) + "]\n}\n")
	if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func reviewedMigration(t *testing.T, r *roleRegistry, provider json.RawMessage) *DelegateMigrationPlan {
	t.Helper()
	plan, err := r.migrationPreview()
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.Entries {
		plan.Entries[i].Disposition = "convert"
		plan.Entries[i].Review = "Explicitly enroll recovered provider for invocation only."
		plan.Entries[i].Interface = provider
		plan.Entries[i].Roles = []string{"invoke"}
	}
	return plan
}

func TestDelegateMigrationPreview(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	original := seedLegacyMigration(t, r, provider)
	before, err := os.ReadDir(r.path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := r.migrationPreview()
	if err != nil {
		t.Fatal(err)
	}
	if plan.SourceHash != HashContent(original) || len(plan.Entries) != 2 {
		t.Fatal("preview lost source identity/rows")
	}
	for i, entry := range plan.Entries {
		if entry.Index != i || entry.Disposition != "unresolved" || entry.Interface != nil || len(entry.Roles) != 0 {
			t.Fatal("preview activated/inferred recovery")
		}
	}
	after, err := os.ReadDir(r.path)
	if err != nil || len(after) != len(before) {
		t.Fatal("preview wrote files")
	}
	got, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
	if !bytes.Equal(got, original) {
		t.Fatal("preview changed source")
	}
	// Unknown/retired rows remain fully visible for explicit disposition.
	raw := []byte(`{"delegates":[{"location":"exec:never-run","formats":["retired"],"mystery":9007199254740993}]}`)
	if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = r.migrationPreview()
	if err != nil || len(plan.Entries) != 1 || !bytes.Contains(plan.Entries[0].Original, []byte("mystery")) {
		t.Fatal("retired/unknown fields hidden")
	}
	if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = r.migrationPreview()
	if err != nil || plan.Entries == nil || len(plan.Entries) != 0 {
		t.Fatal("empty legacy environment mishandled")
	}
}

func TestDelegateMigrationApply(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	original := seedLegacyMigration(t, r, provider)
	plan := reviewedMigration(t, r, provider)
	if _, err := r.migrationApply(plan, false); err == nil {
		t.Fatal("quiescence not required")
	}
	receipt, err := r.migrationApply(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	rows := requireRecords(t, r, "", 2)
	if rows[0].ID != receipt.Registrations[0] || rows[1].ID != receipt.Registrations[1] || rows[0].ID == rows[1].ID {
		t.Fatal("original order/identity correspondence lost")
	}
	if rows[0].RolePreferences["invoke"] != "9007199254740993" {
		t.Fatal("exact old preference rounded")
	}
	config, err := LoadEnvConfig(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.AuthorizedExec) != 1 || config.AuthorizedExec[0] != "exec:kept" || !bytes.Contains(config.Extra["unrelated"], []byte("9007199254740993")) {
		t.Fatal("unrelated config changed")
	}
	state, err := readRoleState(config)
	if err != nil {
		t.Fatal(err)
	}
	if state.BindingPreferences[rows[0].ID]["invoke"]["example.test@1"] != "-1.25" {
		t.Fatal("native preference not mapped")
	}
	directory := filepath.Join(r.path, ".delegate-migrations", strings.TrimPrefix(receipt.PlanHash, "sha256:"))
	backup, err := readMigrationFile(filepath.Join(directory, "original.json"))
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup is not exact original bytes")
	}
	f, err := os.Open(filepath.Join(directory, "original.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if private, err := envFileIsPrivate(f); err != nil || !private {
		t.Fatal("backup not private")
	}
	repeated, err := r.migrationApply(plan, true)
	if err != nil || repeated.PlanHash != receipt.PlanHash {
		t.Fatal("native apply retry did not consult receipt")
	}
	requireRecords(t, r, "", 2)
	// A later management edit does not turn apply retry into a reapplication.
	n := json.Number("0")
	if err := r.prefer(rows[0].ID, "invoke", &n); err != nil {
		t.Fatal(err)
	}
	if _, err := r.migrationApply(plan, true); err != nil {
		t.Fatal(err)
	}
	if requireRecords(t, r, "", 2)[0].RolePreferences["invoke"] != "0" {
		t.Fatal("apply retry undid subsequent edit")
	}
}

func TestDelegateMigrationPlanValidation(t *testing.T) {
	for _, name := range []string{"unresolved", "missing-row", "reordered", "changed-original", "stale-source", "changed-catalogue", "unknown-role", "missing-provider", "changed-provider", "unknown-field", "unknown-preference", "duplicate-override"} {
		t.Run(name, func(t *testing.T) {
			r, provider := migrationTestRegistry(t)
			original := seedLegacyMigration(t, r, provider)
			plan := reviewedMigration(t, r, provider)
			switch name {
			case "unresolved":
				plan.Entries[0].Disposition = "unresolved"
			case "missing-row":
				plan.Entries = plan.Entries[:1]
			case "reordered":
				plan.Entries[0], plan.Entries[1] = plan.Entries[1], plan.Entries[0]
			case "changed-original":
				plan.Entries[0].Original = json.RawMessage(`{}`)
			case "stale-source":
				original = append(original, ' ')
				if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), original, 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-catalogue":
				plan.Catalogue = "different"
			case "unknown-role":
				plan.Entries[0].Roles = []string{"unknown"}
			case "missing-provider":
				plan.Entries[0].Interface = nil
			case "changed-provider":
				plan.Entries[0].Interface = json.RawMessage(strings.Replace(string(provider), `"name":`, `"x-change":true,"name":`, 1))
			default:
				var fields map[string]json.RawMessage
				jsonvalue.Unmarshal(original, &fields)
				var rows []map[string]json.RawMessage
				jsonvalue.Unmarshal(fields["delegates"], &rows)
				if name == "unknown-field" {
					rows[0]["unknown"] = json.RawMessage(`{"nested":9007199254740993}`)
				}
				if name == "unknown-preference" {
					rows[0]["operationPreferences"] = json.RawMessage(`{"another.operation":0}`)
				}
				if name == "duplicate-override" {
					rows[0]["bindingSpecPreferences"] = json.RawMessage(`[{"operation":"openbindings.binding-invoker.invokeBinding","bindingSpec":"example.test@1","preference":0},{"operation":"openbindings.binding-invoker.invokeBinding","bindingSpec":"example.test@1","preference":2}]`)
				}
				fields["delegates"], _ = jsonvalue.Marshal(rows)
				original, _ = jsonvalue.Marshal(fields)
				if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), original, 0600); err != nil {
					t.Fatal(err)
				}
				plan = reviewedMigration(t, r, provider)
			}
			if _, err := r.migrationApply(plan, true); err == nil {
				t.Fatal("invalid plan accepted")
			}
			after, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
			if !bytes.Equal(after, original) {
				t.Fatal("invalid plan partly committed")
			}
		})
	}
}

func TestDelegateMigrationRollback(t *testing.T) {
	for _, changed := range []string{"none", "registry", "unrelated"} {
		t.Run(changed, func(t *testing.T) {
			r, provider := migrationTestRegistry(t)
			original := seedLegacyMigration(t, r, provider)
			plan := reviewedMigration(t, r, provider)
			receipt, err := r.migrationApply(plan, true)
			if err != nil {
				t.Fatal(err)
			}
			if changed == "registry" {
				n := json.Number("-4")
				if err := r.prefer(receipt.Registrations[0], "invoke", &n); err != nil {
					t.Fatal(err)
				}
			}
			if changed == "unrelated" {
				_, err := mutateEnvConfig(r.path, func(c *EnvConfig) (bool, error) {
					c.AuthorizedExec = append(c.AuthorizedExec, "exec:new-authorization")
					return true, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
			if err := r.migrationRollback(receipt.PlanHash, false); err == nil {
				t.Fatal("rollback did not require quiescence")
			}
			err = r.migrationRollback(receipt.PlanHash, true)
			after, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
			if changed != "none" {
				if err == nil || !bytes.Equal(before, after) {
					t.Fatal("rollback overwrote later edits")
				}
				return
			}
			if err != nil || !bytes.Equal(original, after) {
				t.Fatalf("rollback lost original: %v", err)
			}
			reapplied, err := r.migrationApply(plan, true)
			if err != nil {
				t.Fatal(err)
			}
			if reapplied.Registrations[0] == receipt.Registrations[0] {
				t.Fatal("rollback/reapply reused exposed ID")
			}
		})
	}
}

func TestDelegateMigrationBackupFailure(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	original := seedLegacyMigration(t, r, provider)
	plan := reviewedMigration(t, r, provider)
	old := migrationBoundary
	t.Cleanup(func() { migrationBoundary = old })
	migrationBoundary = func(stage string) error {
		if stage == "original.json:published" {
			return errors.New("injected backup failure")
		}
		return nil
	}
	if _, err := r.migrationApply(plan, true); err == nil {
		t.Fatal("backup failure ignored")
	}
	after, _ := os.ReadFile(filepath.Join(r.path, EnvConfigFile))
	if !bytes.Equal(original, after) {
		t.Fatal("backup failure committed conversion")
	}
	migrationBoundary = old
	if _, err := r.migrationApply(plan, true); err != nil {
		t.Fatal("compatible incomplete backup did not recover:", err)
	}
}

func TestDelegateMigrationReviewedDispositions(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	original := seedLegacyMigration(t, r, provider)
	var root map[string]json.RawMessage
	jsonvalue.Unmarshal(original, &root)
	var rows []map[string]json.RawMessage
	jsonvalue.Unmarshal(root["delegates"], &rows)
	delete(rows[0], "contentHash")
	rows[0]["preference"] = json.RawMessage(`0`)
	rows[0]["operationPreferences"] = json.RawMessage(`{"not.an.invocation.operation":4}`)
	rows[0]["unknown"] = json.RawMessage(`{"sensitive":"synthetic"}`)
	root["delegates"], _ = jsonvalue.Marshal(rows)
	source, _ := jsonvalue.Marshal(root)
	if err := os.WriteFile(filepath.Join(r.path, EnvConfigFile), source, 0600); err != nil {
		t.Fatal(err)
	}
	plan := reviewedMigration(t, r, provider)
	plan.Entries[0].Archive = []string{"/unknown", "/operationPreferences/not.an.invocation.operation"}
	plan.Entries[1].Disposition = "exclude"
	plan.Entries[1].Review = "Explicitly archive the second provider; preserve its original row."
	if _, err := r.migrationApply(plan, true); err == nil {
		t.Fatal("missing pin silently trusted")
	}
	plan.Entries[0].ProviderReview = "Inspected recovered value; old registration had no pin."
	receipt, err := r.migrationApply(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	retained := requireRecords(t, r, "", 1)
	if receipt.Registrations[0] != retained[0].ID || receipt.Registrations[1] != "" || retained[0].RolePreferences["invoke"] != "0" {
		t.Fatal("exclusion/order/explicit zero changed")
	}
	directory, err := migrationDirectory(r.path, receipt.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := readMigrationFile(filepath.Join(directory, "original.json"))
	if err != nil || !bytes.Equal(backup, source) {
		t.Fatal("archived rows/fields not recoverable")
	}
}

func TestDelegateMigrationUncertainCompletion(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	seedLegacyMigration(t, r, provider)
	plan := reviewedMigration(t, r, provider)
	old := migrationBoundary
	t.Cleanup(func() { migrationBoundary = old })
	migrationBoundary = func(stage string) error {
		if stage == "after-registry" {
			return errors.New("response lost")
		}
		return nil
	}
	if _, err := r.migrationApply(plan, true); err == nil || !strings.Contains(err.Error(), "may have completed") {
		t.Fatalf("uncertain completion not reported: %v", err)
	}
	before := requireRecords(t, r, "", 2)
	migrationBoundary = old
	receipt, err := r.migrationApply(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	after := requireRecords(t, r, "", 2)
	if before[0].ID != after[0].ID || receipt.Registrations[0] != before[0].ID {
		t.Fatal("native retry duplicated conversion")
	}
}

func TestDelegateMigrationMixedRetry(t *testing.T) {
	r, provider := migrationTestRegistry(t)
	seedLegacyMigration(t, r, provider)
	plan := reviewedMigration(t, r, provider)
	if _, err := r.migrationApply(plan, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.path, EnvConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["delegates"] = json.RawMessage(`[{"location":"exec:old-writer","operations":[]}]`)
	mixed, _ := jsonvalue.Marshal(fields)
	if err := os.WriteFile(path, mixed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.migrationApply(plan, true); err == nil {
		t.Fatal("receipt concealed mixed old/new registry")
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(current, mixed) {
		t.Fatal("refusal changed mixed state")
	}
}
