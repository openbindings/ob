package app

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

func startMigrationProcess(t *testing.T, r *roleRegistry, plan *DelegateMigrationPlan, mode, stop string) (*exec.Cmd, *bufio.Scanner) {
	t.Helper()
	data, err := jsonvalue.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "reviewed-plan.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDelegateMigrationChild$")
	cmd.Env = append(os.Environ(), "OB_MIGRATION_TEST_PATH="+r.path, "OB_MIGRATION_TEST_PLAN="+file, "OB_MIGRATION_TEST_MODE="+mode, "OB_MIGRATION_TEST_STOP="+stop)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	return cmd, bufio.NewScanner(out)
}

func TestDelegateMigrationRecovery(t *testing.T) {
	stages := []string{"original.json:write", "original.json:published", "plan.json:write", "plan.json:published", "before-registry", "env:temporary-write", "env:before-rename", "env:after-rename", "after-registry", "before-response"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			r, provider := migrationTestRegistry(t)
			original := seedLegacyMigration(t, r, provider)
			plan := reviewedMigration(t, r, provider)
			child, out := startMigrationProcess(t, r, plan, "apply", stage)
			readProcessLine(t, out, stage)
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("child survived termination")
			}
			current, err := readMigrationFile(filepath.Join(r.path, EnvConfigFile))
			if err != nil {
				t.Fatal(err)
			}
			committed := stage == "env:after-rename" || stage == "after-registry" || stage == "before-response"
			var previous []DelegateRegistration
			if committed {
				previous = requireRecords(t, r, "", 2)
			} else if !bytes.Equal(current, original) {
				t.Fatal("precommit crash changed legacy source")
			}
			// Cold ordinary apply recovers incomplete backup publication or an
			// already committed receipt. It never converts the same rows twice.
			restart, result := startMigrationProcess(t, r, plan, "apply", "")
			readProcessLine(t, result, "success")
			if err := restart.Wait(); err != nil {
				t.Fatal(err)
			}
			rows := requireRecords(t, r, "", 2)
			if committed && (rows[0].ID != previous[0].ID || rows[1].ID != previous[1].ID) {
				t.Fatal("cold recovery reused/replaced committed identities")
			}
			again, reply := startMigrationProcess(t, r, plan, "apply", "")
			readProcessLine(t, reply, "success")
			if err := again.Wait(); err != nil {
				t.Fatal(err)
			}
			if requireRecords(t, r, "", 2)[0].ID != rows[0].ID {
				t.Fatal("apply retry was not receipt-idempotent")
			}
			// Exact old bytes remain recoverable after every boundary.
			planBytes, _ := jsonvalue.Marshal(plan)
			directory, err := migrationBackupPath(r.path, HashContent(planBytes))
			if err != nil {
				t.Fatal(err)
			}
			backup, err := readMigrationFile(filepath.Join(directory, "original.json"))
			if err != nil || !bytes.Equal(backup, original) {
				t.Fatal("backup lost across recovery")
			}
		})
	}
}

func TestDelegateMigrationRollbackRecovery(t *testing.T) {
	for _, stage := range []string{"before-rollback", "env:temporary-write", "env:before-rename", "env:after-rename", "after-rollback", "before-response"} {
		t.Run(stage, func(t *testing.T) {
			r, provider := migrationTestRegistry(t)
			original := seedLegacyMigration(t, r, provider)
			plan := reviewedMigration(t, r, provider)
			if _, err := r.migrationApply(plan, true); err != nil {
				t.Fatal(err)
			}
			child, out := startMigrationProcess(t, r, plan, "rollback", stage)
			readProcessLine(t, out, stage)
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("child survived termination")
			}
			restart, result := startMigrationProcess(t, r, plan, "rollback", "")
			readProcessLine(t, result, "success")
			if err := restart.Wait(); err != nil {
				t.Fatal(err)
			}
			current, err := readMigrationFile(filepath.Join(r.path, EnvConfigFile))
			if err != nil || !bytes.Equal(current, original) {
				t.Fatal("cold rollback did not recover original exactly")
			}
		})
	}
}

func TestDelegateMigrationChild(t *testing.T) {
	path := os.Getenv("OB_MIGRATION_TEST_PATH")
	if path == "" {
		return
	}
	data, err := readMigrationFile(os.Getenv("OB_MIGRATION_TEST_PLAN"))
	if err != nil {
		t.Fatal(err)
	}
	var plan DelegateMigrationPlan
	if err := jsonvalue.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	catalogue, err := defaultRoleCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	r := &roleRegistry{path: path, catalogue: catalogue}
	stop := os.Getenv("OB_MIGRATION_TEST_STOP")
	boundary := func(stage string) error {
		if stop != "" && stage == stop {
			fmt.Println(stage)
			bufio.NewScanner(os.Stdin).Scan()
		}
		return nil
	}
	migrationBoundary = boundary
	envCommitBoundary = func(stage string) error { return boundary("env:" + stage) }
	if os.Getenv("OB_MIGRATION_TEST_MODE") == "rollback" {
		err = r.migrationRollback(HashContent(data), true)
	} else {
		_, err = r.migrationApply(&plan, true)
	}
	if err != nil {
		t.Fatal(err)
	}
	boundary("before-response")
	io.WriteString(os.Stdout, "success\n")
}
