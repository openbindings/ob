package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDelegateOBI is a minimal delegate interface: it carries the
// key-value-store get operation by alias (and a couple of local ops), but
// satisfies none of ob's three format capabilities — an inert-for-ob delegate.
const fakeDelegateOBI = `{"openbindings":"0.2.0","name":"fake-kv","version":"0.1.0","operations":{"get":{"aliases":["openbindings.key-value-store.get"]},"translate":{"aliases":["acme.fake.translate"]}}}`

// fakeDelegateOBIv2 is the same delegate after a change (an extra operation).
const fakeDelegateOBIv2 = `{"openbindings":"0.2.0","name":"fake-kv","version":"0.1.0","operations":{"get":{"aliases":["openbindings.key-value-store.get"]},"translate":{"aliases":["acme.fake.translate"]},"delete":{}}}`

// writeFakeDelegate writes an executable that answers --openbindings with the
// given OBI, returning its exec: location.
func writeFakeDelegate(t *testing.T, dir, name, obiJSON string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nif [ \"$1\" = \"--openbindings\" ]; then\ncat <<'OBI'\n" + obiJSON + "\nOBI\nfi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return "exec:" + path
}

// delegateTestEnv gives the test its own environment (registry) in a temp
// working directory.
func delegateTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := Init(false); err != nil {
		t.Fatalf("init environment: %v", err)
	}
	return dir
}

func TestRegisterDelegate_FailsOnUnresolvable(t *testing.T) {
	delegateTestEnv(t)
	if _, err := RegisterDelegate("exec:/definitely/not/a/real/binary-xyz", nil); err == nil {
		t.Fatal("expected registration to fail for an unresolvable location — a delegate is its OBI")
	}
}

func TestRegisterDelegate_SnapshotsAndPins(t *testing.T) {
	dir := delegateTestEnv(t)
	loc := writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBI)

	summary, err := RegisterDelegate(loc, nil)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if summary.Location != loc {
		t.Errorf("location = %q, want %q", summary.Location, loc)
	}
	if summary.Name != "fake-kv" {
		t.Errorf("name = %q, want the delegate OBI's name", summary.Name)
	}
	// The snapshot's operations are keys AND aliases — the flat identifier set.
	for _, want := range []string{"get", "openbindings.key-value-store.get", "acme.fake.translate"} {
		if !carriesOperation(summary.Operations, want) {
			t.Errorf("snapshot should carry %q; got %v", want, summary.Operations)
		}
	}
	if !strings.HasPrefix(summary.ContentHash, "sha256:") {
		t.Errorf("snapshot should pin the resolved document; contentHash = %q", summary.ContentHash)
	}
	// Inert for ob (none of the three format capabilities) — registered anyway.
	if len(summary.Capabilities) != 0 {
		t.Errorf("expected an inert-for-ob delegate, got capabilities %v", summary.Capabilities)
	}
}

func TestRegisterDelegate_RefreshPreservesPreferences(t *testing.T) {
	dir := delegateTestEnv(t)
	loc := writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBI)
	if _, err := RegisterDelegate(loc, nil); err != nil {
		t.Fatalf("register: %v", err)
	}

	// The registrar builds a preference index...
	if _, err := SetDelegatePreference(SetDelegatePreferenceInput{
		Location: loc, Preference: prefOf(7), Operation: "openbindings.key-value-store.get",
	}); err != nil {
		t.Fatalf("set operation preference: %v", err)
	}

	// ...the delegate changes, and the registrar re-registers to refresh.
	writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBIv2)
	before, _ := RegisterDelegate(loc, nil)

	// The snapshot is the delegate's data: refreshed.
	if !carriesOperation(before.Operations, "delete") {
		t.Errorf("refresh should replace the snapshot; operations = %v", before.Operations)
	}
	// The preferences are the registrar's data: untouched.
	if got := before.OperationPreferences["openbindings.key-value-store.get"]; got != 7 {
		t.Errorf("refresh must preserve the preference index; got %v", before.OperationPreferences)
	}
}

func TestSetDelegatePreference_NullClears(t *testing.T) {
	dir := delegateTestEnv(t)
	loc := writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBI)
	if _, err := RegisterDelegate(loc, prefOf(5)); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Clear the delegate-level value back to the unset baseline.
	s, err := SetDelegatePreference(SetDelegatePreferenceInput{Location: loc, Preference: nil})
	if err != nil {
		t.Fatalf("clear delegate-level: %v", err)
	}
	if s.Preference != nil {
		t.Errorf("delegate-level preference should be cleared to unset, got %v", *s.Preference)
	}

	// Set then remove an operation entry.
	op := "openbindings.key-value-store.get"
	if _, err := SetDelegatePreference(SetDelegatePreferenceInput{Location: loc, Preference: prefOf(9), Operation: op}); err != nil {
		t.Fatalf("set op preference: %v", err)
	}
	s, err = SetDelegatePreference(SetDelegatePreferenceInput{Location: loc, Preference: nil, Operation: op})
	if err != nil {
		t.Fatalf("clear op preference: %v", err)
	}
	if len(s.OperationPreferences) != 0 {
		t.Errorf("operation entry should be removed, got %v", s.OperationPreferences)
	}
}

func TestSetDelegatePreference_RequiresRegistration(t *testing.T) {
	delegateTestEnv(t)
	if _, err := SetDelegatePreference(SetDelegatePreferenceInput{Location: "exec:ghost", Preference: prefOf(1)}); err == nil {
		t.Fatal("expected an error for an unregistered delegate")
	}
}

// TestSetDelegatePreference_FormatScopeSurfacesInSummary pins Fix B4-4:
// DelegateRecord.FormatPreferences used to be write-only — set by
// SetDelegatePreference's Format scope but never copied by
// summaryFromRecord, so it never appeared in the summary SetDelegatePreference
// itself returns, in listDelegates' output, or in the human-readable Render().
func TestSetDelegatePreference_FormatScopeSurfacesInSummary(t *testing.T) {
	dir := delegateTestEnv(t)
	loc := writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBI)
	if _, err := RegisterDelegate(loc, nil); err != nil {
		t.Fatalf("register: %v", err)
	}

	op := "openbindings.key-value-store.get"
	s, err := SetDelegatePreference(SetDelegatePreferenceInput{
		Location: loc, Preference: prefOf(3), Operation: op, Format: "grpc",
	})
	if err != nil {
		t.Fatalf("set format preference: %v", err)
	}

	// The summary SetDelegatePreference itself returns.
	if len(s.FormatPreferences) != 1 {
		t.Fatalf("summary.FormatPreferences = %+v, want exactly one entry", s.FormatPreferences)
	}
	fp := s.FormatPreferences[0]
	if fp.Operation != op || fp.Format != "grpc" || fp.Preference != 3 {
		t.Errorf("format preference = %+v, want {%s grpc 3}", fp, op)
	}

	// A fresh read through listDelegates (persisted registry -> summaryFromRecord).
	var found bool
	for _, d := range ListDelegates().Delegates {
		if d.Location != loc {
			continue
		}
		found = true
		if len(d.FormatPreferences) != 1 || d.FormatPreferences[0].Format != "grpc" {
			t.Errorf("listDelegates summary.FormatPreferences = %+v, want the grpc override", d.FormatPreferences)
		}
	}
	if !found {
		t.Fatalf("registered delegate missing from listDelegates")
	}

	// The human-readable rendering.
	rendered := s.Render()
	if !strings.Contains(rendered, op) || !strings.Contains(rendered, "grpc") || !strings.Contains(rendered, "3") {
		t.Errorf("Render() missing the format-scoped preference; got:\n%s", rendered)
	}
}

func TestUnregisterDelegate_Idempotent(t *testing.T) {
	dir := delegateTestEnv(t)
	loc := writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBI)
	if _, err := RegisterDelegate(loc, nil); err != nil {
		t.Fatalf("register: %v", err)
	}

	removed, err := UnregisterDelegate(loc)
	if err != nil || !removed {
		t.Fatalf("first unregister: removed=%v err=%v", removed, err)
	}
	removed, err = UnregisterDelegate(loc)
	if err != nil || removed {
		t.Fatalf("second unregister should be a no-op success: removed=%v err=%v", removed, err)
	}
}

func TestResolveDelegate_OrdersByEffectivePreference(t *testing.T) {
	dir := delegateTestEnv(t)
	// An operation ob itself does not carry, so the candidates are exactly the
	// two externals. (Resolving openbindings.key-value-store.get would return
	// three: ob's own context store carries the kv-store keys by alias.)
	op := "acme.fake.translate"
	locA := writeFakeDelegate(t, dir, "kv-a", fakeDelegateOBI)
	locB := writeFakeDelegate(t, dir, "kv-b", fakeDelegateOBI)
	if _, err := RegisterDelegate(locA, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterDelegate(locB, prefOf(5)); err != nil {
		t.Fatal(err)
	}

	out, err := ResolveDelegate(op)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("expected both carriers, got %d", len(out.Candidates))
	}
	if out.Candidates[0].Location != locB {
		t.Errorf("higher preference should come first; got %q", out.Candidates[0].Location)
	}

	// A per-operation entry on A overrides B's delegate-level value.
	if _, err := SetDelegatePreference(SetDelegatePreferenceInput{Location: locA, Preference: prefOf(10), Operation: op}); err != nil {
		t.Fatal(err)
	}
	out, _ = ResolveDelegate(op)
	if out.Candidates[0].Location != locA {
		t.Errorf("operation entry should outrank delegate-level; got %q", out.Candidates[0].Location)
	}

	// An operation nothing carries resolves to an empty candidate list — an
	// answer, not an error.
	out, err = ResolveDelegate("acme.nothing.carries.this")
	if err != nil || len(out.Candidates) != 0 {
		t.Errorf("expected empty candidates, got %v (err %v)", out.Candidates, err)
	}
}

func TestResolveDelegate_SelfCarriesItsOwnOperations(t *testing.T) {
	delegateTestEnv(t)
	// ob's own interface answers to the software-descriptor describe operation.
	out, err := ResolveDelegate("openbindings.software-descriptor.describe")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(out.Candidates) == 0 || !out.Candidates[0].Builtin {
		t.Fatalf("the self-delegate should carry ob's own operations; got %+v", out.Candidates)
	}
	if out.Candidates[0].Location != SelfDelegateLocation {
		t.Errorf("self location = %q, want %q", out.Candidates[0].Location, SelfDelegateLocation)
	}
}

func TestResolvePinnedDelegateInterface_DetectsDrift(t *testing.T) {
	dir := delegateTestEnv(t)
	loc := writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBI)
	if _, err := RegisterDelegate(loc, nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	rec := GetDelegateContext().Delegates[0]

	// Unchanged: resolves and verifies.
	if _, err := resolvePinnedDelegateInterface(rec); err != nil {
		t.Fatalf("pin verification should pass for an unchanged delegate: %v", err)
	}

	// The document behind the location changes wholesale — the location is an
	// address, not a trust anchor, so use must detect it.
	writeFakeDelegate(t, dir, "fake-kv", fakeDelegateOBIv2)
	if _, err := resolvePinnedDelegateInterface(rec); err == nil || !strings.Contains(err.Error(), "re-register") {
		t.Fatalf("expected a digest-mismatch error directing re-registration, got %v", err)
	}
}
