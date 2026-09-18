package app

import (
	"encoding/json"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/compare"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Admission is an application concern, but its compatibility evidence must come
// from a qualified general comparator. These deliberately unbound documents use
// only local schema values and cannot touch a delegate, config or credential store.
// Two different exact JSON numbers must never establish a false compatibility
// claim through parse-time float64 rounding. This is an upstream prerequisite,
// not a second comparator implemented inside the Delegate Manager.
func TestDelegateMigrationPrerequisiteExactAdmission(t *testing.T) {
	const expected = `{"openbindings":"0.2.0","operations":{"example.role.read":{"input":{"type":"integer","enum":[9007199254740992]},"output":{"type":"string"}}}}`
	const supplied = `{"openbindings":"0.2.0","operations":{"read":{"aliases":["example.role.read"],"input":{"type":"integer","enum":[9007199254740993]},"output":{"type":"string"}}}}`
	target, err := openbindings.ValidateDocument([]byte(expected))
	if err != nil {
		t.Fatalf("valid expected document: %v", err)
	}
	provider, err := openbindings.ValidateDocument([]byte(supplied))
	if err != nil {
		t.Fatalf("valid provider document: %v", err)
	}
	if issues := compare.CheckInterfaceCompatibility(target, provider); len(issues) == 0 {
		t.Fatal("unsafe admission evidence: disjoint exact integer input sets were classified compatible; qualify the SDK value/comparison cohort before implementing admission")
	}
}

// This is deliberately a direct SDK test: no manager, WebSocket, provider,
// transform, application decoder, or personal configuration participates.
// The corresponding integration sentinel exercises the real dependency route.
func TestDelegateMigrationPrerequisiteExactAsyncAPI(t *testing.T) {
	decode, _ := asyncapi.NewInvoker().BuiltinHooks()
	for _, encoded := range []string{"9007199254740993", "1e400", "1e-400"} {
		t.Run(encoded, func(t *testing.T) {
			value, err := decode(invoke.InvokeSite{}, invoke.RawResult{Body: []byte(encoded), Meta: invoke.Metadata{"content-type": []string{"application/json"}}})
			if err != nil {
				t.Fatalf("SDK JSON decoder refused a supported exact number: %v", err)
			}
			equal, err := jsonvalue.Equal(value, json.Number(encoded))
			if err != nil || !equal {
				t.Fatalf("SDK JSON decoder changed %s into %v (%T): %v", encoded, value, value, err)
			}
		})
	}
}
