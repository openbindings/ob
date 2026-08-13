// Package frames implements the wire types of the binding-invoker
// frame protocol (spec interfaces/binding-invoker/0.1): the
// invokeBinding operation serialized as a typed bidirectional frame stream.
// The caller streams BindingInvokerInputFrame messages (one `open`, zero or
// more `input`, one `close`); the service streams BindingInvokerOutputFrame
// messages (zero or more `output`/`input_closed`, exactly one terminal
// `complete` or `error`).
//
// Both directions decode strictly: frame variants declare
// additionalProperties: false, so unknown properties are a protocol violation
// (rule 7) and surface as ERR_FRAME_PROTOCOL. The server and the delegate client
// share these types so the two sides cannot drift.
package frames

import (
	"encoding/json"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
)

// Frame kinds. Input frames: open, input, close. Output frames: output,
// input_closed, complete, error.
const (
	KindOpen        = "open"
	KindInput       = "input"
	KindClose       = "close"
	KindOutput      = "output"
	KindInputClosed = "input_closed"
	KindComplete    = "complete"
	KindError       = "error"
)

// InvokeSource is the binding source carried by the open frame, mirroring the
// contract's InvokeSource schema (format plus location and/or content).
// Content is raw JSON with the core's presence semantics (nil = absent
// member, a `null` literal = present null).
type InvokeSource struct {
	BindingSpec string          `json:"bindingSpec"`
	Location    string          `json:"location,omitempty"`
	Content     json.RawMessage `json:"content,omitempty"`
	Description string          `json:"description,omitempty"`
}

// BindingInvocationInput is the payload of the open frame (and the input of
// prepareBinding): the binding to invoke plus opaque runtime context.
type BindingInvocationInput struct {
	Source  InvokeSource   `json:"source"`
	Ref     string         `json:"ref"`
	Context map[string]any `json:"context,omitempty"`
}

// OperationInvocationInput is the payload of invokeOperation's open frame.
// Exactly one of Operation or Binding is set. Binding-addressed calls derive
// the operation from the named binding before entering the operation layer.
type OperationInvocationInput struct {
	Interface *openbindings.Interface `json:"interface"`
	Operation string                  `json:"operation,omitempty"`
	Binding   string                  `json:"binding,omitempty"`
	Context   map[string]any          `json:"context,omitempty"`
}

// WireError is the minimal InvocationError wire shape carried by a terminal
// error frame. dataPresent preserves the Core distinction between an absent
// data member and an explicitly authored JSON null.
type WireError struct {
	Code        string `json:"code"`
	Data        any    `json:"-"`
	dataPresent bool
}

func (e WireError) MarshalJSON() ([]byte, error) {
	if e.dataPresent {
		return json.Marshal(struct {
			Code string `json:"code"`
			Data any    `json:"data"`
		}{Code: e.Code, Data: e.Data})
	}
	return json.Marshal(struct {
		Code string `json:"code"`
	}{Code: e.Code})
}

func (e *WireError) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("InvocationError must be an object")
	}
	for key := range fields {
		if key != "code" && key != "data" {
			return fmt.Errorf("unknown InvocationError property %q", key)
		}
	}
	codeRaw, ok := fields["code"]
	if !ok || json.Unmarshal(codeRaw, &e.Code) != nil || e.Code == "" {
		return fmt.Errorf("InvocationError.code must be a non-empty string")
	}
	e.Data = nil
	e.dataPresent = false
	if dataRaw, ok := fields["data"]; ok {
		if err := json.Unmarshal(dataRaw, &e.Data); err != nil {
			return fmt.Errorf("InvocationError.data: %w", err)
		}
		e.dataPresent = true
	}
	return nil
}

// WireErrorFrom converts an SDK terminal error to its wire shape.
func WireErrorFrom(err *openbindings.InvocationError) *WireError {
	if err == nil {
		return &WireError{Code: openbindings.ErrCodeRuntime}
	}
	return &WireError{
		Code:        err.Code,
		Data:        err.Data,
		dataPresent: err.HasData(),
	}
}

// InvocationError converts the wire shape back to the SDK terminal error.
// CONTEXT_REQUIRED data crosses as a generic map; ContextRequiredFrom decodes
// them back into the typed shape on demand.
func (e *WireError) InvocationError() *openbindings.InvocationError {
	if e == nil {
		return openbindings.NewInvocationError(openbindings.ErrCodeRuntime)
	}
	if e.dataPresent {
		return openbindings.NewInvocationErrorWithData(e.Code, e.Data)
	}
	return openbindings.NewInvocationError(e.Code)
}

// ProtocolError reports a frame that violates the protocol (rule 1, 2, or 7).
// Carriers map it to a terminal ERR_FRAME_PROTOCOL.
type ProtocolError struct{ Reason string }

func (e *ProtocolError) Error() string { return e.Reason }

func protocolErrorf(format string, args ...any) error {
	return &ProtocolError{Reason: fmt.Sprintf(format, args...)}
}

// ---------------------------------------------------------------------------
// Input frames (caller -> service)
// ---------------------------------------------------------------------------

// InputFrame is one BindingInvokerInputFrame. Exactly one variant is
// populated, selected by Kind: open carries Input, input carries Value
// (which may be JSON null), close carries nothing.
type InputFrame struct {
	Kind  string
	Input *BindingInvocationInput // open
	Value any                     // input
}

// OperationInputFrame is one OperationInvokerInputFrame. Its input and close
// variants deliberately share InputFrame's wire shape; only the open payload
// differs.
type OperationInputFrame struct {
	Kind  string
	Input *OperationInvocationInput
	Value any
}

// OperationOpen constructs an invokeOperation open frame.
func OperationOpen(input *OperationInvocationInput) OperationInputFrame {
	return OperationInputFrame{Kind: KindOpen, Input: input}
}

// OperationValue constructs an invokeOperation input frame.
func OperationValue(value any) OperationInputFrame {
	return OperationInputFrame{Kind: KindInput, Value: value}
}

// OperationClose constructs an invokeOperation close frame.
func OperationClose() OperationInputFrame { return OperationInputFrame{Kind: KindClose} }

func (f OperationInputFrame) MarshalJSON() ([]byte, error) {
	switch f.Kind {
	case KindOpen:
		return json.Marshal(struct {
			Kind  string                    `json:"kind"`
			Input *OperationInvocationInput `json:"input"`
		}{f.Kind, f.Input})
	case KindInput:
		return json.Marshal(struct {
			Kind  string `json:"kind"`
			Value any    `json:"value"`
		}{f.Kind, f.Value})
	case KindClose:
		return json.Marshal(struct {
			Kind string `json:"kind"`
		}{f.Kind})
	default:
		return nil, fmt.Errorf("frames: unknown operation input frame kind %q", f.Kind)
	}
}

// UnmarshalJSON strictly decodes an operation-invoker input frame.
func (f *OperationInputFrame) UnmarshalJSON(b []byte) error {
	fields, kind, err := decodeFrameObject(b)
	if err != nil {
		return err
	}
	switch kind {
	case KindOpen:
		if err := requireExactKeys(fields, kind, "kind", "input"); err != nil {
			return err
		}
		input, err := DecodeOperationInvocationInput(fields["input"])
		if err != nil {
			return err
		}
		*f = OperationInputFrame{Kind: kind, Input: input}
	case KindInput:
		if err := requireExactKeys(fields, kind, "kind", "value"); err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(fields["value"], &value); err != nil {
			return protocolErrorf("input frame: invalid value: %v", err)
		}
		*f = OperationInputFrame{Kind: kind, Value: value}
	case KindClose:
		if err := requireExactKeys(fields, kind, "kind"); err != nil {
			return err
		}
		*f = OperationInputFrame{Kind: kind}
	default:
		return protocolErrorf("unknown input frame kind %q", kind)
	}
	return nil
}

// Open constructs an open frame.
func Open(input *BindingInvocationInput) InputFrame {
	return InputFrame{Kind: KindOpen, Input: input}
}

// Input constructs an input frame carrying one value.
func Input(value any) InputFrame { return InputFrame{Kind: KindInput, Value: value} }

// Close constructs a close frame.
func Close() InputFrame { return InputFrame{Kind: KindClose} }

func (f InputFrame) MarshalJSON() ([]byte, error) {
	switch f.Kind {
	case KindOpen:
		return json.Marshal(struct {
			Kind  string                  `json:"kind"`
			Input *BindingInvocationInput `json:"input"`
		}{f.Kind, f.Input})
	case KindInput:
		// `value` is required by the schema even when null: no omitempty.
		return json.Marshal(struct {
			Kind  string `json:"kind"`
			Value any    `json:"value"`
		}{f.Kind, f.Value})
	case KindClose:
		return json.Marshal(struct {
			Kind string `json:"kind"`
		}{f.Kind})
	default:
		return nil, fmt.Errorf("frames: unknown input frame kind %q", f.Kind)
	}
}

// UnmarshalJSON decodes one input frame strictly: the discriminator must name
// a known variant, the variant's required properties must be present, and any
// unknown property is rejected (rule 7). Violations return *ProtocolError.
func (f *InputFrame) UnmarshalJSON(b []byte) error {
	fields, kind, err := decodeFrameObject(b)
	if err != nil {
		return err
	}
	switch kind {
	case KindOpen:
		if err := requireExactKeys(fields, kind, "kind", "input"); err != nil {
			return err
		}
		input, err := DecodeInvocationInput(fields["input"])
		if err != nil {
			return err
		}
		*f = InputFrame{Kind: kind, Input: input}
	case KindInput:
		if err := requireExactKeys(fields, kind, "kind", "value"); err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(fields["value"], &value); err != nil {
			return protocolErrorf("input frame: invalid value: %v", err)
		}
		*f = InputFrame{Kind: kind, Value: value}
	case KindClose:
		if err := requireExactKeys(fields, kind, "kind"); err != nil {
			return err
		}
		*f = InputFrame{Kind: kind}
	default:
		return protocolErrorf("unknown input frame kind %q", kind)
	}
	return nil
}

// DecodeInvocationInput strictly decodes a BindingInvocationInput (the open
// frame's payload and prepareBinding's input), enforcing the contract's
// additionalProperties: false on the invocation object and required
// properties on it and its extensible Source value.
func DecodeInvocationInput(raw json.RawMessage) (*BindingInvocationInput, error) {
	var fields map[string]json.RawMessage
	if raw == nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, protocolErrorf("open frame: input must be an object")
	}
	for k := range fields {
		switch k {
		case "source", "ref", "context":
		default:
			return nil, protocolErrorf("open frame: unknown input property %q", k)
		}
	}
	if _, ok := fields["source"]; !ok {
		return nil, protocolErrorf("open frame: input.source is required")
	}
	if _, ok := fields["ref"]; !ok {
		return nil, protocolErrorf("open frame: input.ref is required")
	}

	var srcFields map[string]json.RawMessage
	if json.Unmarshal(fields["source"], &srcFields) != nil || srcFields == nil {
		return nil, protocolErrorf("open frame: input.source must be an object")
	}
	if _, hasLocation := srcFields["location"]; !hasLocation {
		if _, hasContent := srcFields["content"]; !hasContent {
			return nil, protocolErrorf("open frame: input.source requires location or content")
		}
	}

	var input BindingInvocationInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, protocolErrorf("open frame: invalid input: %v", err)
	}
	if input.Source.BindingSpec == "" {
		return nil, protocolErrorf("open frame: source.bindingSpec is required")
	}
	return &input, nil
}

// DecodeOperationInvocationInput strictly decodes invokeOperation's open
// payload, including the operation-or-binding exclusive choice.
func DecodeOperationInvocationInput(raw json.RawMessage) (*OperationInvocationInput, error) {
	var fields map[string]json.RawMessage
	if raw == nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, protocolErrorf("open frame: input must be an object")
	}
	for k := range fields {
		switch k {
		case "interface", "operation", "binding", "context":
		default:
			return nil, protocolErrorf("open frame: unknown input property %q", k)
		}
	}
	if _, ok := fields["interface"]; !ok {
		return nil, protocolErrorf("open frame: input.interface is required")
	}

	var input OperationInvocationInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, protocolErrorf("open frame: invalid input: %v", err)
	}
	if input.Interface == nil {
		return nil, protocolErrorf("open frame: input.interface must be an object")
	}
	if (input.Operation == "") == (input.Binding == "") {
		return nil, protocolErrorf("open frame: exactly one of input.operation or input.binding is required")
	}
	return &input, nil
}

// ---------------------------------------------------------------------------
// Output frames (service -> caller)
// ---------------------------------------------------------------------------

// OutputFrame is one BindingInvokerOutputFrame. Exactly one variant is
// populated, selected by Kind: output carries Value (which may be JSON null),
// error carries Error; input_closed and complete carry nothing.
type OutputFrame struct {
	Kind  string
	Value any        // output
	Error *WireError // error
}

// Output constructs an output frame carrying one value.
func Output(value any) OutputFrame { return OutputFrame{Kind: KindOutput, Value: value} }

// InputClosed constructs an input_closed frame.
func InputClosed() OutputFrame { return OutputFrame{Kind: KindInputClosed} }

// Complete constructs the terminal complete frame.
func Complete() OutputFrame { return OutputFrame{Kind: KindComplete} }

// Error constructs the terminal error frame from an SDK terminal error.
func Error(err *openbindings.InvocationError) OutputFrame {
	return OutputFrame{Kind: KindError, Error: WireErrorFrom(err)}
}

// Terminal reports whether the frame ends the output stream (rule 4).
func (f OutputFrame) Terminal() bool { return f.Kind == KindComplete || f.Kind == KindError }

func (f OutputFrame) MarshalJSON() ([]byte, error) {
	switch f.Kind {
	case KindOutput:
		// `value` is required by the schema even when null: no omitempty.
		return json.Marshal(struct {
			Kind  string `json:"kind"`
			Value any    `json:"value"`
		}{f.Kind, f.Value})
	case KindInputClosed, KindComplete:
		return json.Marshal(struct {
			Kind string `json:"kind"`
		}{f.Kind})
	case KindError:
		return json.Marshal(struct {
			Kind  string     `json:"kind"`
			Error *WireError `json:"error"`
		}{f.Kind, f.Error})
	default:
		return nil, fmt.Errorf("frames: unknown output frame kind %q", f.Kind)
	}
}

// UnmarshalJSON decodes one output frame strictly, mirroring InputFrame's
// rules on the consumer side (rule 7's SHOULD). Violations return
// *ProtocolError.
func (f *OutputFrame) UnmarshalJSON(b []byte) error {
	fields, kind, err := decodeFrameObject(b)
	if err != nil {
		return err
	}
	switch kind {
	case KindOutput:
		if err := requireExactKeys(fields, kind, "kind", "value"); err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(fields["value"], &value); err != nil {
			return protocolErrorf("output frame: invalid value: %v", err)
		}
		*f = OutputFrame{Kind: kind, Value: value}
	case KindInputClosed, KindComplete:
		if err := requireExactKeys(fields, kind, "kind"); err != nil {
			return err
		}
		*f = OutputFrame{Kind: kind}
	case KindError:
		if err := requireExactKeys(fields, kind, "kind", "error"); err != nil {
			return err
		}
		var wireErr WireError
		if err := json.Unmarshal(fields["error"], &wireErr); err != nil {
			return protocolErrorf("error frame: invalid error: %v", err)
		}
		if wireErr.Code == "" {
			return protocolErrorf("error frame: error.code is required")
		}
		*f = OutputFrame{Kind: kind, Error: &wireErr}
	default:
		return protocolErrorf("unknown output frame kind %q", kind)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Shared decode helpers
// ---------------------------------------------------------------------------

// decodeFrameObject parses a frame into its raw fields and extracts the
// `kind` discriminator.
func decodeFrameObject(b []byte) (map[string]json.RawMessage, string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil || fields == nil {
		return nil, "", protocolErrorf("frame must be a JSON object")
	}
	rawKind, ok := fields["kind"]
	if !ok {
		return nil, "", protocolErrorf("frame is missing the kind discriminator")
	}
	var kind string
	if err := json.Unmarshal(rawKind, &kind); err != nil {
		return nil, "", protocolErrorf("frame kind must be a string")
	}
	return fields, kind, nil
}

// requireExactKeys enforces a variant's additionalProperties: false plus its
// required-property set: fields must contain exactly `allowed` (rule 7; the
// variants happen to require every property they allow).
func requireExactKeys(fields map[string]json.RawMessage, kind string, allowed ...string) error {
	for k := range fields {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			return protocolErrorf("%s frame: unknown property %q", kind, k)
		}
	}
	for _, a := range allowed {
		if _, ok := fields[a]; !ok {
			return protocolErrorf("%s frame: missing required property %q", kind, a)
		}
	}
	return nil
}
