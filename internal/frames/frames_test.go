package frames

import (
	"encoding/json"
	"errors"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestInputFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		frame InputFrame
		want  string
	}{
		{
			name: "open",
			frame: Open(&BindingInvocationInput{
				Source: InvokeSource{BindingSpec: "openbindings.openapi@1", Location: "https://x/openapi.yaml"},
				Ref:    "#/paths/~1t/get",
			}),
			want: `{"kind":"open","input":{"source":{"bindingSpec":"openbindings.openapi@1","location":"https://x/openapi.yaml"},"ref":"#/paths/~1t/get"}}`,
		},
		{
			name:  "input",
			frame: Input(map[string]any{"a": float64(1)}),
			want:  `{"kind":"input","value":{"a":1}}`,
		},
		{
			name:  "input null value",
			frame: Input(nil),
			want:  `{"kind":"input","value":null}`,
		},
		{
			name:  "close",
			frame: Close(),
			want:  `{"kind":"close"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.frame)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Errorf("marshal = %s, want %s", b, tc.want)
			}
			var back InputFrame
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.Kind != tc.frame.Kind {
				t.Errorf("kind = %q, want %q", back.Kind, tc.frame.Kind)
			}
		})
	}
}

func TestInputFrameStrictDecode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"unknown frame property", `{"kind":"input","value":1,"extra":true}`},
		{"unknown close property", `{"kind":"close","value":1}`},
		{"missing value", `{"kind":"input"}`},
		{"missing kind", `{"value":1}`},
		{"unknown kind", `{"kind":"flush"}`},
		{"non-string kind", `{"kind":5}`},
		{"not an object", `[1,2]`},
		{"open without input", `{"kind":"open"}`},
		{"open with legacy input sibling", `{"kind":"open","input":{"source":{"format":"f","location":"x"},"ref":"r","input":{}}}`},
		{"open with legacy bearerToken", `{"kind":"open","input":{"source":{"format":"f","location":"x"},"ref":"r","bearerToken":"t"}}`},
		{"open without ref", `{"kind":"open","input":{"source":{"format":"f","location":"x"}}}`},
		{"open without source format", `{"kind":"open","input":{"source":{"location":"x"},"ref":"r"}}`},
		{"open without source carrier", `{"kind":"open","input":{"source":{"bindingSpec":"f"},"ref":"r"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f InputFrame
			err := json.Unmarshal([]byte(tc.raw), &f)
			if err == nil {
				t.Fatal("expected a protocol error, got nil")
			}
			var pe *ProtocolError
			if !errors.As(err, &pe) {
				t.Errorf("expected *ProtocolError, got %T: %v", err, err)
			}
		})
	}
}

func TestBindingOpenAcceptsExtensibleSource(t *testing.T) {
	// Source is deliberately open in the OBI contract. Transport-irrelevant
	// extension fields must not turn an otherwise valid open frame into a
	// protocol violation.
	raw := `{"kind":"open","input":{"source":{"bindingSpec":"f","location":"x","binary":"b","x-driver":{"mode":"fast"}},"ref":"r"}}`
	var frame InputFrame
	if err := json.Unmarshal([]byte(raw), &frame); err != nil {
		t.Fatalf("extensible Source was rejected: %v", err)
	}
	if frame.Input == nil || frame.Input.Source.BindingSpec != "f" {
		t.Fatalf("decoded frame = %#v", frame)
	}
}

func TestOutputFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		frame OutputFrame
		want  string
	}{
		{"output", Output("v"), `{"kind":"output","value":"v"}`},
		{"output null value", Output(nil), `{"kind":"output","value":null}`},
		{"input_closed", InputClosed(), `{"kind":"input_closed"}`},
		{"complete", Complete(), `{"kind":"complete"}`},
		{
			name:  "error",
			frame: Error(openbindings.NewInvocationError("ERR_RUNTIME")),
			want:  `{"kind":"error","error":{"code":"ERR_RUNTIME"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.frame)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Errorf("marshal = %s, want %s", b, tc.want)
			}
			var back OutputFrame
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.Kind != tc.frame.Kind {
				t.Errorf("kind = %q, want %q", back.Kind, tc.frame.Kind)
			}
		})
	}
}

func TestErrorFramePreservesDataPresenceAndValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *openbindings.InvocationError
	}{
		{name: "absent", err: openbindings.NewInvocationError(openbindings.ErrCodeExecutionFailed)},
		{name: "explicit null", err: openbindings.NewInvocationErrorWithData(openbindings.ErrCodeExecutionFailed, nil)},
		{name: "object", err: openbindings.NewInvocationErrorWithData(openbindings.ErrCodeExecutionFailed, map[string]any{"reason": "missing"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := json.Marshal(Error(tc.err))
			if err != nil {
				t.Fatal(err)
			}
			var back OutputFrame
			if err := json.Unmarshal(wire, &back); err != nil {
				t.Fatal(err)
			}
			got := back.Error.InvocationError()
			if got.HasData() != tc.err.HasData() {
				t.Fatalf("data presence = %v, want %v (%s)", got.HasData(), tc.err.HasData(), wire)
			}
			gotJSON, _ := json.Marshal(got.Data)
			wantJSON, _ := json.Marshal(tc.err.Data)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("data = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestErrorFrameUsesMinimalStructuralWireShape(t *testing.T) {
	b, err := json.Marshal(Error(openbindings.NewInvocationError("ERR_RUNTIME")))
	if err != nil {
		t.Fatal(err)
	}
	var frame map[string]any
	if err := json.Unmarshal(b, &frame); err != nil {
		t.Fatal(err)
	}
	errorValue, _ := frame["error"].(map[string]any)
	if errorValue["code"] != "ERR_RUNTIME" || len(errorValue) != 1 {
		t.Fatalf("structural error fields changed: %s", b)
	}
	if _, present := errorValue["category"]; present {
		t.Fatalf("closed category leaked onto wire: %s", b)
	}
	if _, present := errorValue["effects"]; present {
		t.Fatalf("retry effects leaked onto wire: %s", b)
	}
}

func TestOutputFrameStrictDecode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"unknown property", `{"kind":"output","value":1,"meta":{}}`},
		{"legacy event envelope", `{"type":"event","output":1}`},
		{"unknown kind", `{"kind":"event"}`},
		{"error without code", `{"kind":"error","error":{"message":"m"}}`},
		{"retired error message", `{"kind":"error","error":{"code":"C","message":"m"}}`},
		{"complete with extras", `{"kind":"complete","value":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f OutputFrame
			err := json.Unmarshal([]byte(tc.raw), &f)
			if err == nil {
				t.Fatal("expected a protocol error, got nil")
			}
			var pe *ProtocolError
			if !errors.As(err, &pe) {
				t.Errorf("expected *ProtocolError, got %T: %v", err, err)
			}
		})
	}
}

func TestTerminal(t *testing.T) {
	for _, tc := range []struct {
		frame OutputFrame
		want  bool
	}{
		{Output(1), false},
		{InputClosed(), false},
		{Complete(), true},
		{Error(openbindings.NewInvocationError("C")), true},
	} {
		if got := tc.frame.Terminal(); got != tc.want {
			t.Errorf("Terminal(%s) = %v, want %v", tc.frame.Kind, got, tc.want)
		}
	}
}

func TestContextRequiredDetailsCrossTheWire(t *testing.T) {
	// CONTEXT_REQUIRED data serializes on the error frame and decodes back
	// into the typed shape via ContextRequiredFrom.
	details := &openbindings.ContextRequiredDetails{
		Target: "api.example.com",
		Alternatives: []openbindings.ContextAlternative{
			{Requirements: []openbindings.ContextRequirement{{Type: "auth.bearer"}}},
		},
	}
	frame := Error(openbindings.NewContextRequiredError(details))

	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	var back OutputFrame
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	got := openbindings.ContextRequiredFrom(back.Error.InvocationError())
	if got == nil {
		t.Fatal("ContextRequiredFrom returned nil after wire round trip")
	}
	if got.Target != "api.example.com" || len(got.Alternatives) != 1 ||
		got.Alternatives[0].Requirements[0].Type != "auth.bearer" {
		t.Errorf("details after round trip = %#v", got)
	}
}

func TestOperationInputFrameRoundTrip(t *testing.T) {
	iface := &openbindings.Interface{
		OpenBindings: openbindings.MaxTestedVersion,
		Operations:   map[string]openbindings.Operation{"echo": {}},
	}
	frame := OperationOpen(&OperationInvocationInput{Interface: iface, Operation: "echo"})
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	var back OperationInputFrame
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Kind != KindOpen || back.Input == nil || back.Input.Operation != "echo" || back.Input.Interface == nil {
		t.Fatalf("round trip = %#v", back)
	}
}

func TestOperationInputFrameRequiresExclusiveTarget(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"open","input":{"interface":{"openbindings":"0.2.0","operations":{}}}}`,
		`{"kind":"open","input":{"interface":{"openbindings":"0.2.0","operations":{}},"operation":"x","binding":"x.http"}}`,
	} {
		var frame OperationInputFrame
		if err := json.Unmarshal([]byte(raw), &frame); err == nil {
			t.Fatalf("expected protocol error for %s", raw)
		}
	}
}
