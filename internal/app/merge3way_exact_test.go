package app

import (
	"encoding/json"
	"testing"
)

func TestMergeJSONEqualityExactValues(t *testing.T) {
	for _, test := range []struct {
		a, b  string
		equal bool
	}{
		{`9007199254740992`, `9007199254740993`, false},
		{`0.123456789012345678901`, `0.123456789012345678902`, false},
		{`0.1`, `0.10`, true}, {`1e400`, `10e399`, true},
		{`{"n":9007199254740993}`, `{"n":9007199254740992}`, false},
	} {
		if got := jsonEqual(json.RawMessage(test.a), json.RawMessage(test.b)); got != test.equal {
			t.Fatalf("%s = %s: %v", test.a, test.b, got)
		}
	}
}
