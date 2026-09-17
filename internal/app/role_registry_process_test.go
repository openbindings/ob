package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func startRoleProcess(t *testing.T, r *roleRegistry, id, operation, stopAt string) (*exec.Cmd, io.WriteCloser, *bufio.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRoleRegistryChild$")
	cmd.Env = append(os.Environ(), "OB_ROLE_TEST_PATH="+r.path, "OB_ROLE_TEST_ID="+id, "OB_ROLE_TEST_OPERATION="+operation, "OB_ROLE_TEST_STOP="+stopAt)
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
	return cmd, in, bufio.NewScanner(out)
}

func TestRoleRegistryProcesses(t *testing.T) {
	for _, scenario := range []struct {
		name, first, second string
		rejected, removed   bool
	}{
		{"different-role-preferences", "prefer-A", "prefer-B", false, false},
		{"replacement-then-preference", "replace", "prefer-A", false, false},
		{"preference-then-replacement", "prefer-A", "replace", false, false},
		{"remove-then-preference", "remove", "prefer-A", true, true},
		{"remove-then-replacement", "remove", "replace", true, true},
		{"preference-then-remove", "prefer-A", "remove", false, true},
		{"replacement-then-remove", "replace", "remove", false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			r := testRoleRegistry(t)
			record, err := r.register(testRegistration("A", "B"))
			if err != nil {
				t.Fatal(err)
			}
			first, in, out := startRoleProcess(t, r, record.ID, scenario.first, "before-rename")
			readProcessLine(t, out, "starting")
			readProcessLine(t, out, "before-rename")
			second, _, other := startRoleProcess(t, r, record.ID, scenario.second, "")
			readProcessLine(t, other, "starting")
			readProcessLine(t, other, "contended")
			if _, err := io.WriteString(in, "continue\n"); err != nil {
				t.Fatal(err)
			}
			readProcessLine(t, out, "success")
			if err := first.Wait(); err != nil {
				t.Fatal(err)
			}
			want := "success"
			if scenario.rejected {
				want = "refused"
			}
			readProcessLine(t, other, want)
			if err := second.Wait(); err != nil {
				t.Fatal(err)
			}
			if scenario.removed {
				requireRecords(t, r, "", 0)
				return
			}
			result := requireRecords(t, r, "", 1)[0]
			if result.RolePreferences["A"] != "9007199254740993" {
				t.Fatal("concurrent update was lost")
			}
			if scenario.name == "different-role-preferences" && result.RolePreferences["B"] != "-1.25" {
				t.Fatal("second role preference lost")
			}
			if strings.Contains(scenario.name, "replacement") && !strings.Contains(string(result.Interface), "replacement") {
				t.Fatal("replacement lost")
			}
		})
	}
}

func TestRoleRegistryRecovery(t *testing.T) {
	for _, stage := range []string{"temporary-write", "before-rename", "after-rename"} {
		t.Run(stage, func(t *testing.T) {
			r := testRoleRegistry(t)
			child, _, out := startRoleProcess(t, r, "", "register", stage)
			readProcessLine(t, out, "starting")
			readProcessLine(t, out, stage)
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("child survived termination")
			}
			count := 0
			if stage == "after-rename" {
				count = 1
			}
			previous := requireRecords(t, r, "", count)
			// Fresh native registration after uncertain completion creates another
			// identity. No attempted correlation or hidden idempotent retry occurs.
			restart, _, after := startRoleProcess(t, r, "", "register", "")
			readProcessLine(t, after, "starting")
			readProcessLine(t, after, "success")
			if err := restart.Wait(); err != nil {
				t.Fatal(err)
			}
			rows := requireRecords(t, r, "", count+1)
			if count == 1 && (rows[0].ID != previous[0].ID || rows[1].ID == previous[0].ID) {
				t.Fatal("committed identity lost/reused")
			}
		})
	}
}

func TestRoleRegistryConcurrentReaders(t *testing.T) {
	r := testRoleRegistry(t)
	record, err := r.register(testRegistration("A"))
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errors := make(chan error, 3)
	for reader := 0; reader < 3; reader++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 50; i++ {
				rows, err := r.list("")
				if err != nil {
					errors <- err
					return
				}
				if len(rows) != 1 {
					errors <- fmt.Errorf("partial record set")
					return
				}
				a := rows[0].Roles[0] == "A"
				marker := strings.Contains(string(rows[0].Interface), "replacement")
				if a == marker {
					errors <- fmt.Errorf("mixed record observed")
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		input := testRegistration("A")
		if i%2 == 0 {
			input.Roles = []string{"B"}
			input.Interface = json.RawMessage(strings.TrimSuffix(registryTestInterface, "}") + `,"description":"replacement"}`)
		}
		input.ID = record.ID
		if _, err := r.register(input); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestRoleRegistryUnrelatedConfigProcess(t *testing.T) {
	r := testRoleRegistry(t)
	first, in, out := startRoleProcess(t, r, "", "register", "before-rename")
	readProcessLine(t, out, "starting")
	readProcessLine(t, out, "before-rename")
	other, _, secondOut := startConfigProcess(t, r.path, "write", "unrelated")
	readProcessLine(t, secondOut, "starting")
	readProcessLine(t, secondOut, "contended")
	if _, err := io.WriteString(in, "continue\n"); err != nil {
		t.Fatal(err)
	}
	readProcessLine(t, out, "success")
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := other.Wait(); err != nil {
		t.Fatal(err)
	}
	requireRecords(t, r, "", 1)
	config, err := LoadEnvConfig(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(config.Extra["unrelated"]) != "9007199254740993" {
		t.Fatal("unrelated update lost or rounded")
	}
}

func TestRoleRegistryChild(t *testing.T) {
	path := os.Getenv("OB_ROLE_TEST_PATH")
	if path == "" {
		return
	}
	r := &roleRegistry{path: path, catalogue: testRoleCatalogue(t)}
	id, operation, stop := os.Getenv("OB_ROLE_TEST_ID"), os.Getenv("OB_ROLE_TEST_OPERATION"), os.Getenv("OB_ROLE_TEST_STOP")
	reported := false
	envLockContended = func() {
		if !reported {
			fmt.Println("contended")
			reported = true
		}
	}
	envCommitBoundary = func(stage string) error {
		if stop != "" && stage == stop {
			fmt.Println(stage)
			bufio.NewScanner(os.Stdin).Scan()
		}
		return nil
	}
	fmt.Println("starting")
	var err error
	switch operation {
	case "prefer-A":
		n := json.Number("9007199254740993")
		err = r.prefer(id, "A", &n)
	case "prefer-B":
		n := json.Number("-1.25")
		err = r.prefer(id, "B", &n)
	case "remove":
		err = r.unregister(id)
	case "replace":
		input := testRegistration("A", "B")
		input.ID = id
		input.Interface = json.RawMessage(strings.TrimSuffix(registryTestInterface, "}") + `,"description":"replacement"}`)
		_, err = r.register(input)
	case "register":
		_, err = r.register(testRegistration("A"))
	default:
		t.Fatal("unknown child scenario")
	}
	if err != nil {
		fmt.Println("refused")
	} else {
		fmt.Println("success")
	}
}
