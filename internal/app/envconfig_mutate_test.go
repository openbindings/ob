package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envConfigTestEnv(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	if _, err := Init(false); err != nil {
		t.Fatal(err)
	}
	path, err := FindEnvPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvConfigProcesses(t *testing.T) {
	path := envConfigTestEnv(t)
	first, in, out := startConfigProcess(t, path, "hold", "first")
	readProcessLine(t, out, "locked")
	second, _, secondOut := startConfigProcess(t, path, "write", "second")
	readProcessLine(t, secondOut, "starting")
	readProcessLine(t, secondOut, "contended")
	// First process is inside the transaction; the second must acquire the
	// same OS lock and then reload. Both write different fields.
	if _, err := io.WriteString(in, "continue\n"); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	config, err := LoadEnvConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.AuthorizedExec) != 1 || config.AuthorizedExec[0] != "exec:first" || string(config.Extra["second"]) != "9007199254740993" {
		t.Fatalf("lost unrelated concurrent update: %+v", config)
	}
}

func TestEnvConfigStaleSave(t *testing.T) {
	path := envConfigTestEnv(t)
	a, _ := LoadEnvConfig(path)
	b, _ := LoadEnvConfig(path)
	a.AuthorizedExec = []string{"exec:a"}
	if err := SaveEnvConfig(path, a); err != nil {
		t.Fatal(err)
	}
	b.AuthorizedExec = []string{"exec:b"}
	if err := SaveEnvConfig(path, b); !errors.Is(err, errEnvConfigConflict) {
		t.Fatalf("stale save: %v", err)
	}
	final, _ := LoadEnvConfig(path)
	if len(final.AuthorizedExec) != 1 || final.AuthorizedExec[0] != "exec:a" {
		t.Fatal("stale save overwrote newer state")
	}
}

func TestEnvConfigRecovery(t *testing.T) {
	for _, stage := range []string{"temporary-write", "before-rename", "after-rename"} {
		t.Run(stage, func(t *testing.T) {
			path := envConfigTestEnv(t)
			child, _, out := startConfigProcess(t, path, "crash", stage)
			readProcessLine(t, out, stage)
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("expected terminated child")
			}
			// A cold process proves parseable state and automatic OS-lock release.
			restart, _, stdout := startConfigProcess(t, path, "write", "restart")
			readProcessLine(t, stdout, "starting")
			if err := restart.Wait(); err != nil {
				t.Fatal(err)
			}
			config, err := LoadEnvConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			committed := len(config.AuthorizedExec) == 1
			if committed != (stage == "after-rename") {
				t.Fatalf("commit=%v at %s", committed, stage)
			}
			if string(config.Extra["restart"]) != "9007199254740993" {
				t.Fatal("cold write missing")
			}
		})
	}
}

func TestEnvConfigFailureAndNoop(t *testing.T) {
	path := envConfigTestEnv(t)
	before, _ := os.ReadFile(filepath.Join(path, EnvConfigFile))
	_, err := mutateEnvConfig(path, func(c *EnvConfig) (bool, error) {
		c.AuthorizedExec = []string{"discard"}
		return false, errEnvConfigNoop
	})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(path, EnvConfigFile))
	if !bytes.Equal(before, after) {
		t.Fatal("no-op wrote state")
	}
	old := envCommitBoundary
	t.Cleanup(func() { envCommitBoundary = old })
	for _, stage := range []string{"temporary-write", "before-rename", "after-rename"} {
		envCommitBoundary = func(at string) error {
			if at == stage {
				return errors.New("injected")
			}
			return nil
		}
		_, err := mutateEnvConfig(path, func(c *EnvConfig) (bool, error) { c.AuthorizedExec = []string{"committed"}; return true, nil })
		if err == nil {
			t.Fatal("injected write reported success")
		}
		state, err := LoadEnvConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		if (len(state.AuthorizedExec) == 1) != (stage == "after-rename") {
			t.Fatal("partial or lost commit")
		}
	}
}

func TestEnvConfigLockTimeoutAndReentrancy(t *testing.T) {
	path := envConfigTestEnv(t)
	started := time.Now()
	_, err := withEnvConfigLock(path, func() (bool, error) {
		_, err := withEnvConfigLock(path, func() (bool, error) { t.Error("reentrant lock acquired"); return true, nil })
		if err == nil {
			t.Error("expected bounded refusal")
		}
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < envLockTimeout || elapsed > envLockTimeout+5*time.Second {
		t.Fatalf("unbounded/unexpected timeout %v", elapsed)
	}
	// Timeout must not unlink/steal the stable lock. The next transaction works.
	if _, err := os.Stat(filepath.Join(path, ".config.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := mutateEnvConfig(path, func(c *EnvConfig) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestEnvConfigFilesystemRefusals(t *testing.T) {
	t.Run("lock-symlink", func(t *testing.T) {
		path := t.TempDir()
		target := filepath.Join(t.TempDir(), "external")
		if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(path, ".config.lock")); err != nil {
			t.Fatal(err)
		}
		if err := SaveEnvConfig(path, &EnvConfig{}); err == nil {
			t.Fatal("symlink lock accepted")
		}
		data, _ := os.ReadFile(target)
		if string(data) != "unchanged" {
			t.Fatal("external file touched")
		}
	})
	t.Run("non-regular-config", func(t *testing.T) {
		path := t.TempDir()
		if err := os.Mkdir(filepath.Join(path, EnvConfigFile), 0700); err != nil {
			t.Fatal(err)
		}
		if err := SaveEnvConfig(path, &EnvConfig{}); err == nil {
			t.Fatal("directory accepted as config")
		}
	})
	t.Run("restrictive-commit", func(t *testing.T) {
		path := envConfigTestEnv(t)
		f, err := os.Open(filepath.Join(path, EnvConfigFile))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if private, err := envFileIsPrivate(f); err != nil || !private {
			t.Fatalf("configuration permissions are not owner-only: %v", err)
		}
	})
}

func TestEnvConfigSyncAndRenameFailure(t *testing.T) {
	for _, stage := range []string{"sync", "rename"} {
		t.Run(stage, func(t *testing.T) {
			path := envConfigTestEnv(t)
			before, err := os.ReadFile(filepath.Join(path, EnvConfigFile))
			if err != nil {
				t.Fatal(err)
			}
			oldSync, oldReplace := envCommitSync, envCommitReplace
			t.Cleanup(func() { envCommitSync, envCommitReplace = oldSync, oldReplace })
			if stage == "sync" {
				envCommitSync = func(*os.File) error { return errors.New("injected sync failure") }
			}
			if stage == "rename" {
				envCommitReplace = func(string, string) error { return errors.New("injected replacement failure") }
			}
			_, err = mutateEnvConfig(path, func(c *EnvConfig) (bool, error) { c.AuthorizedExec = []string{"must-not-land"}; return true, nil })
			if err == nil {
				t.Fatal("filesystem failure ignored")
			}
			after, err := os.ReadFile(filepath.Join(path, EnvConfigFile))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed commit changed state")
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".config-commit-") {
					t.Fatal("recoverable failure leaked temporary file")
				}
			}
		})
	}
}

func startConfigProcess(t *testing.T, path, mode, value string) (*exec.Cmd, io.WriteCloser, *bufio.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEnvConfigChild$")
	cmd.Env = append(os.Environ(), "OB_CONFIG_TEST_PATH="+path, "OB_CONFIG_TEST_MODE="+mode, "OB_CONFIG_TEST_VALUE="+value)
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

func readProcessLine(t *testing.T, scanner *bufio.Scanner, expected string) {
	t.Helper()
	if scanner.Scan() && scanner.Text() == expected {
		return
	}
	// The child's first line is a status token; when it is not the expected
	// one the child is reporting its own failure on the following lines. Drain
	// them (bounded) into the message, otherwise a remote failure on a
	// platform this machine cannot run shows only "got FAIL" and is
	// undiagnosable from CI alone.
	first, err := scanner.Text(), scanner.Err()
	var rest []string
	for len(rest) < 40 && scanner.Scan() {
		rest = append(rest, scanner.Text())
	}
	detail := ""
	if len(rest) > 0 {
		detail = "\nchild output:\n\t" + strings.Join(rest, "\n\t")
	}
	t.Fatalf("child expected %q, got %q (%v)%s", expected, first, err, detail)
}

func TestEnvConfigChild(t *testing.T) {
	path := os.Getenv("OB_CONFIG_TEST_PATH")
	if path == "" {
		return
	}
	mode, value := os.Getenv("OB_CONFIG_TEST_MODE"), os.Getenv("OB_CONFIG_TEST_VALUE")
	if mode == "crash" {
		envCommitBoundary = func(stage string) error {
			if stage == value {
				fmt.Println(stage)
				bufio.NewScanner(os.Stdin).Scan()
			}
			return nil
		}
	}
	if mode == "write" {
		fmt.Println("starting")
		reported := false
		envLockContended = func() {
			if !reported {
				fmt.Println("contended")
				reported = true
			}
		}
	}
	_, err := mutateEnvConfig(path, func(c *EnvConfig) (bool, error) {
		if mode == "hold" {
			fmt.Println("locked")
			bufio.NewScanner(os.Stdin).Scan()
		}
		if mode == "write" {
			if c.Extra == nil {
				c.Extra = map[string]json.RawMessage{}
			}
			c.Extra[value] = json.RawMessage("9007199254740993")
		} else {
			c.AuthorizedExec = append(c.AuthorizedExec, "exec:"+value)
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
