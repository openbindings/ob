package app

import (
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
)

// OperationCodegenNameOutput is the result of setting or clearing an
// operation's codegen-name override.
type OperationCodegenNameOutput struct {
	Key         string `json:"key"`
	CodegenName string `json:"codegenName,omitempty"`
	Cleared     bool   `json:"cleared,omitempty"`
}

// Render returns a human-friendly representation.
func (o OperationCodegenNameOutput) Render() string {
	if o.Cleared {
		return Styles.Header.Render("Cleared codegen name") + Styles.Dim.Render(" on ") + Styles.Key.Render(o.Key)
	}
	return Styles.Header.Render("Set codegen name") + " " +
		Styles.Key.Render(o.CodegenName) + Styles.Dim.Render(" on ") + Styles.Key.Render(o.Key)
}

// OperationSetCodegenName sets (or, when name == "", clears) an operation's
// x-ob.codegenName override — the friendly symbol that `ob codegen` emits for
// the operation in place of the verbose, namespace-prefixed key.
//
// Unlike a spec-field edit, this never detaches a source-owned operation: the
// hint is ob metadata that coexists with sync management, and the sync/pull
// merge paths carry it forward across regeneration. The operation key itself is
// never touched — bindings and the wire still reference it verbatim.
func OperationSetCodegenName(obiPath, opRef, name string) (OperationCodegenNameOutput, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationCodegenNameOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	key, op, found := openbindings.ResolveOperation(iface, opRef)
	if !found {
		return OperationCodegenNameOutput{}, fmt.Errorf("operation %q not found", opRef)
	}
	if err := SetCodegenName(&op.LosslessFields, name); err != nil {
		return OperationCodegenNameOutput{}, fmt.Errorf("set codegen name: %w", err)
	}
	iface.Operations[key] = op
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationCodegenNameOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationCodegenNameOutput{Key: key, CodegenName: name, Cleared: name == ""}, nil
}
