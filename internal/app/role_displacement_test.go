package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/usage"
	"github.com/openbindings/openbindings-go/invoke"
)

// TestOpInvoke_ExternalDelegateDisplacesElections preserves the warning and
// successful-dispatch assertions using a complete, explicitly enrolled provider.
// The old fixture's incomplete unary Usage interface is not a frame realization;
// its management/CLI migration is a separate N5 journey, not assumed here.
func TestOpInvoke_ExternalDelegateDisplacesElections(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	record, err := r.register(RoleRegistrationInput{
		Interface: roleFrameProvider(t), Roles: []string{"invoke"},
		RolePreferences: json.RawMessage(`{"invoke":100}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(_ string, value any) any {
		return roleOperationVerdicts(t, value, true)
	}}
	work := &roleFrameTestInvoker{}
	installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, work, usage.NewInvoker()))
	iface := &openbindings.Interface{
		OpenBindings: "0.2.0", Name: "Standing election displacement",
		Operations: map[string]openbindings.Operation{
			"openbindings.ob.validateInterface": {Input: map[string]any{}, Output: map[string]any{}},
		},
		Sources: map[string]openbindings.Source{
			"usage": {BindingSpec: usage.BindingSpec, Content: openbindings.TextContent("min_usage_version \"2.0.0\"\nbin \"must-not-execute\"\ncmd \"validate\" {}\n")},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"validate.usage": {Operation: "openbindings.ob.validateInterface", Source: "usage", Selector: "validate"},
		},
	}
	file := filepath.Join(t.TempDir(), "invoked.obi.json")
	if err := WriteInterfaceFile(file, iface); err != nil {
		t.Fatal(err)
	}
	run, err := InvokeOBIOperationConfigured(t.Context(), file, "openbindings.ob.validateInterface", "", map[string]any{"delegated": true}, nil)
	if err != nil {
		t.Fatalf("configured invoke: %v", err)
	}
	if run.DisplacedWarning == "" || !strings.Contains(run.DisplacedWarning, record.ID) {
		t.Errorf("warning must name the selected registration: %q", run.DisplacedWarning)
	}
	got := reduceUnaryInvocation(run.Events)
	if got.Error != nil {
		t.Fatalf("delegate invocation errored: %s", got.Error.Message)
	}
	if m, _ := got.Output.(map[string]any); m["delegated"] != true {
		t.Errorf("delegate did not answer the hop: %#v", got.Output)
	}
	work.mu.Lock()
	defer work.mu.Unlock()
	if len(work.selectors) != 1 || work.selectors[0] != "qualified-work" {
		t.Fatalf("selected workload did not run exactly once: %v", work.selectors)
	}
}
