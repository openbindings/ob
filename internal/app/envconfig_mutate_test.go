package app

import "testing"

// envConfigTestEnv gives the test its own environment (registry) in a temp
// working directory — the same seam delegateTestEnv (delegate_manager_test.go)
// uses for the higher-level delegate tests.
func envConfigTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := Init(false); err != nil {
		t.Fatalf("init environment: %v", err)
	}
	envPath, err := FindEnvPath()
	if err != nil {
		t.Fatalf("find env path: %v", err)
	}
	return envPath
}

// TestMutateEnvConfig_RetriesOnConcurrentWrite simulates the exact
// interleaving the fix closes: a second process runs its own complete
// load-mutate-save cycle strictly between this cycle's load and its save.
// mutateEnvConfig must detect the change (the save's fingerprint no longer
// matches), retry by reloading the fresh on-disk state, and reapply the
// mutation on top of it — so BOTH writers' changes land, rather than the
// second writer's update being silently clobbered by a stale save.
func TestMutateEnvConfig_RetriesOnConcurrentWrite(t *testing.T) {
	envPath := envConfigTestEnv(t)

	calls := 0
	result, err := mutateEnvConfig(envPath, func(config *EnvConfig) (string, error) {
		calls++
		if calls == 1 {
			// A concurrent `ob` process's own full cycle, landing strictly
			// between our load (already done, above) and our save (about to
			// happen, below): it writes directly to disk.
			interfering := &EnvConfig{Delegates: []DelegateRecord{
				{Location: "exec:concurrent", Operations: []string{}},
			}}
			if err := SaveEnvConfig(envPath, interfering); err != nil {
				t.Fatalf("simulate concurrent write: %v", err)
			}
		}
		config.Delegates = append(config.Delegates, DelegateRecord{Location: "exec:ours", Operations: []string{}})
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("mutateEnvConfig: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if calls != 2 {
		t.Fatalf("mutate called %d times, want exactly 2 (one conflict, one successful retry)", calls)
	}

	final, err := LoadEnvConfig(envPath)
	if err != nil {
		t.Fatalf("reload final config: %v", err)
	}
	if len(final.Delegates) != 2 {
		t.Fatalf("final registry has %d delegates, want 2 (both the concurrent writer's and ours) — got %+v",
			len(final.Delegates), final.Delegates)
	}
	var haveConcurrent, haveOurs bool
	for _, d := range final.Delegates {
		switch d.Location {
		case "exec:concurrent":
			haveConcurrent = true
		case "exec:ours":
			haveOurs = true
		}
	}
	if !haveConcurrent || !haveOurs {
		t.Errorf("lost an update: concurrent=%v ours=%v, delegates=%+v", haveConcurrent, haveOurs, final.Delegates)
	}
}

// TestMutateEnvConfig_FailsLoudlyAfterExhaustingRetries interferes on every
// attempt, so every save is refused. mutateEnvConfig must give up loudly
// (a non-nil error) after its bounded retry count rather than looping
// forever or, worse, silently proceeding with a stale save that would
// clobber the last interfering write.
func TestMutateEnvConfig_FailsLoudlyAfterExhaustingRetries(t *testing.T) {
	envPath := envConfigTestEnv(t)

	calls := 0
	_, err := mutateEnvConfig(envPath, func(config *EnvConfig) (string, error) {
		calls++
		// Distinct content on every call: an interfering write that happened
		// to leave byte-identical content behind would look, correctly, like
		// no conflict at all (nothing actually changed). To simulate genuine
		// permanent contention the interference must differ every attempt.
		interfering := &EnvConfig{Delegates: []DelegateRecord{
			{Location: "exec:interference-" + string(rune('0'+calls)), Operations: []string{}},
		}}
		if err := SaveEnvConfig(envPath, interfering); err != nil {
			t.Fatalf("simulate concurrent write: %v", err)
		}
		config.Delegates = append(config.Delegates, DelegateRecord{Location: "exec:ours", Operations: []string{}})
		return "ok", nil
	})
	if err == nil {
		t.Fatal("expected a loud error after exhausting retries under permanent contention, got nil")
	}
	if calls != maxEnvConfigMutateAttempts {
		t.Errorf("mutate called %d times, want exactly %d (the bounded retry count)", calls, maxEnvConfigMutateAttempts)
	}

	// The refused save must never have landed: the file holds only the last
	// interfering write, never "exec:ours" — a silent clobber would show up
	// here as "exec:ours" appearing alongside or instead of the interference.
	final, err := LoadEnvConfig(envPath)
	if err != nil {
		t.Fatalf("reload final config: %v", err)
	}
	if len(final.Delegates) != 1 || final.Delegates[0].Location != "exec:interference-"+string(rune('0'+maxEnvConfigMutateAttempts)) {
		t.Errorf("refused save must not clobber the concurrent writer's content; got %+v", final.Delegates)
	}
}

// TestMutateEnvConfig_NoopSkipsSave proves errEnvConfigNoop takes the "no
// change" exit without ever writing: a delegate absent from the registry (an
// unregister-style no-op) reports its zero result without disturbing the
// file mutateEnvConfig loaded.
func TestMutateEnvConfig_NoopSkipsSave(t *testing.T) {
	envPath := envConfigTestEnv(t)

	before, err := LoadEnvConfig(envPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	result, err := mutateEnvConfig(envPath, func(config *EnvConfig) (bool, error) {
		return false, errEnvConfigNoop
	})
	if err != nil {
		t.Fatalf("mutateEnvConfig: %v", err)
	}
	if result != false {
		t.Errorf("noop result = %v, want the zero value", result)
	}

	after, err := LoadEnvConfig(envPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(before.Delegates) != len(after.Delegates) {
		t.Errorf("a noop mutation must not change the persisted registry: before=%+v after=%+v", before.Delegates, after.Delegates)
	}
}
