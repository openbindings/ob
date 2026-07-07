package app

import (
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
)

// OperationOutputSchemaOutput reports an output-schema election result.
type OperationOutputSchemaOutput struct {
	Key     string `json:"key"`
	Cleared bool   `json:"cleared,omitempty"`
}

// Render returns a human-friendly summary.
func (o OperationOutputSchemaOutput) Render() string {
	if o.Cleared {
		return fmt.Sprintf("Cleared the output-schema election on %q (the next pull re-derives it)", o.Key)
	}
	return fmt.Sprintf("Elected the output schema for %q", o.Key)
}

// OperationSetOutputSchema is the non-detaching output-schema election
// (`ob operation output-schema`): it WRITES the elected schema into
// op.Output (readers never overlay) and stamps an op-level election marker
// carrying a copy, so `ob source pull` re-applies it onto a fresh
// derivation and compares content modulo the election — a grown,
// non-floor-stamped source schema wins and displaces the election loudly.
// A nil schema clears the marker (the elected value stays in op.Output
// until the next pull re-derives it). `ob purify` strips the marker.
func OperationSetOutputSchema(obiPath, opRef string, schema openbindings.JSONSchema) (OperationOutputSchemaOutput, error) {
	iface, err := loadInterfaceFile(obiPath)
	if err != nil {
		return OperationOutputSchemaOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	key, op, found := openbindings.ResolveOperation(iface, opRef)
	if !found {
		return OperationOutputSchemaOutput{}, fmt.Errorf("operation %q not found", opRef)
	}

	if schema != nil {
		op.Output = schema
	}
	if err := SetOutputSchemaElection(&op.LosslessFields, schema); err != nil {
		return OperationOutputSchemaOutput{}, fmt.Errorf("set output-schema election: %w", err)
	}

	iface.Operations[key] = op
	if err := WriteInterfaceFile(obiPath, iface); err != nil {
		return OperationOutputSchemaOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return OperationOutputSchemaOutput{Key: key, Cleared: schema == nil}, nil
}
