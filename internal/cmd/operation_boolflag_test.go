package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// opField reads one field off one operation of an OBI on disk. Absence is
// distinguishable from false, which is the whole point of the tri-state flags
// exercised below.
func opField(t *testing.T, obiPath, op, field string) (any, bool) {
	t.Helper()
	data, err := os.ReadFile(obiPath)
	if err != nil {
		t.Fatalf("read OBI: %v", err)
	}
	var doc struct {
		Operations map[string]map[string]any `json:"operations"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse OBI: %v", err)
	}
	v, ok := doc.Operations[op][field]
	return v, ok
}

// --idempotent and --deprecated are declared as strings so that "unset" stays
// distinguishable from "false", but the value was compared against the literal
// "true". Every other spelling (yes, 1, TRUE) therefore stored FALSE silently,
// at exit 0, under a "Updated operation" success message. idempotent governs
// retry safety, so a silently wrong value misreports whether an operation is
// safe to repeat — the worst shape a CLI defect can take.
//
// Unparseable values must now refuse with a usage error (code 2) and leave the
// document untouched.
func TestOperationBoolFlags_RefuseUnparseableValue(t *testing.T) {
	dir := t.TempDir()
	obiPath := filepath.Join(dir, "iface.obi.json")
	if err := runOB(t, "new", obiPath); err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := runOB(t, "operation", "add", obiPath, "greet"); err != nil {
		t.Fatalf("op add: %v", err)
	}

	for _, flag := range []string{"--idempotent", "--deprecated"} {
		for _, bad := range []string{"yes", "no", "on", "1.0", "bogus"} {
			err := runOB(t, "operation", "set", obiPath, "greet", flag, bad)
			if er, ok := err.(app.ExitResult); !ok || er.Code != 2 {
				t.Fatalf("%s %s: expected usage refusal (code 2), got %v", flag, bad, err)
			}
			// The refusal must be total: nothing written.
			field := flag[2:]
			if v, present := opField(t, obiPath, "greet", field); present {
				t.Fatalf("%s %s was refused but still wrote %s=%v", flag, bad, field, v)
			}
		}
	}

	// The same refusal on the add lane, which had the identical defect.
	err := runOB(t, "operation", "add", obiPath, "wave", "--idempotent", "yes")
	if er, ok := err.(app.ExitResult); !ok || er.Code != 2 {
		t.Fatalf("op add --idempotent yes: expected usage refusal (code 2), got %v", err)
	}
}

// The spellings strconv.ParseBool accepts must all round-trip to the value the
// user meant, and an unset flag must leave the field absent rather than write
// false over it.
func TestOperationBoolFlags_AcceptParseableValues(t *testing.T) {
	dir := t.TempDir()
	obiPath := filepath.Join(dir, "iface.obi.json")
	if err := runOB(t, "new", obiPath); err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := runOB(t, "operation", "add", obiPath, "greet"); err != nil {
		t.Fatalf("op add: %v", err)
	}

	// Unset: the field never appears.
	if v, present := opField(t, obiPath, "greet", "idempotent"); present {
		t.Fatalf("unset --idempotent wrote idempotent=%v", v)
	}

	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"true", true}, {"false", false},
		{"TRUE", true}, {"FALSE", false},
		{"1", true}, {"0", false},
		{"t", true}, {"f", false},
	} {
		if err := runOB(t, "operation", "set", obiPath, "greet", "--idempotent", tc.val); err != nil {
			t.Fatalf("--idempotent %s: %v", tc.val, err)
		}
		v, present := opField(t, obiPath, "greet", "idempotent")
		if !present {
			// false is legitimately omitted from the serialized document;
			// absence then means false.
			if tc.want {
				t.Fatalf("--idempotent %s: want true, field absent", tc.val)
			}
			continue
		}
		if got, _ := v.(bool); got != tc.want {
			t.Fatalf("--idempotent %s: got %v, want %v", tc.val, got, tc.want)
		}
	}
}
