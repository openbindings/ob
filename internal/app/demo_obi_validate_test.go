package app

import (
	"os"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestDemoOBIValidatesAs020(t *testing.T) {
	raw, err := os.ReadFile("../demo/api/openbindings.json")
	if err != nil {
		t.Fatal(err)
	}
	iface, err := openbindings.ParseDocument(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := iface.Validate(); err != nil {
		t.Fatalf("demo OBI fails 0.2.0 validation:\n%v", err)
	}
}
