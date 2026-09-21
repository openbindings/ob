package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

func TestInvocationDisplayUsesLogicalJSONRepresentation(t *testing.T) {
	value := map[string]any{"photo": []byte{0, 1, 255}, "id": json.Number("9007199254740993"), "unit": "\xed\xa0\x80"}
	out, err := FormatOutput(value, OutputFormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var displayed map[string]any
	if err := jsonvalue.Unmarshal(out, &displayed); err != nil {
		t.Fatal(err)
	}
	if displayed["photo"] != "AAH/" || displayed["id"] != json.Number("9007199254740993") || displayed["unit"] != "\xed\xa0\x80" {
		t.Fatalf("changed display: %s", out)
	}
	if !strings.Contains(FormatOpOutput(value), `"photo": "AAH/"`) {
		t.Fatal("operation display lost byte representation")
	}
}
