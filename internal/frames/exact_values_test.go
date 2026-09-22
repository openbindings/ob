package frames

import (
	"encoding/json"
	"github.com/openbindings/openbindings-go/jsonvalue"
	"reflect"
	"testing"
)

func TestFrameExactValues(t *testing.T) {
	for _, token := range []string{"9007199254740993", "0.123456789012345678901", "1e400", "1e-400"} {
		for _, test := range []struct {
			name, wire string
			target     any
			value      func() any
		}{
			func() struct {
				name, wire string
				target     any
				value      func() any
			} {
				f := new(InputFrame)
				return struct {
					name, wire string
					target     any
					value      func() any
				}{"binding-input", `{"kind":"input","value":` + token + `}`, f, func() any { return f.Value }}
			}(),
			func() struct {
				name, wire string
				target     any
				value      func() any
			} {
				f := new(OperationInputFrame)
				return struct {
					name, wire string
					target     any
					value      func() any
				}{"operation-input", `{"kind":"input","value":` + token + `}`, f, func() any { return f.Value }}
			}(),
			func() struct {
				name, wire string
				target     any
				value      func() any
			} {
				f := new(OutputFrame)
				return struct {
					name, wire string
					target     any
					value      func() any
				}{"output", `{"kind":"output","value":` + token + `}`, f, func() any { return f.Value }}
			}(),
			func() struct {
				name, wire string
				target     any
				value      func() any
			} {
				f := new(OutputFrame)
				return struct {
					name, wire string
					target     any
					value      func() any
				}{"error", `{"kind":"error","error":{"code":"ERR_RUNTIME","data":` + token + `}}`, f, func() any { return f.Error.Data }}
			}(),
			func() struct {
				name, wire string
				target     any
				value      func() any
			} {
				f := new(InputFrame)
				return struct {
					name, wire string
					target     any
					value      func() any
				}{"context", `{"kind":"open","input":{"source":{"bindingSpec":"test","content":{}},"selector":"x","context":{"n":` + token + `}}}`, f, func() any { return f.Input.Context["n"] }}
			}(),
		} {
			t.Run(test.name+"/"+token, func(t *testing.T) {
				if err := json.Unmarshal([]byte(test.wire), test.target); err != nil {
					t.Fatal(err)
				}
				value, err := json.Marshal(test.value())
				if err != nil || string(value) != token {
					t.Fatalf("got %s (%v), want %s", value, err, token)
				}
			})
		}
	}
}

func TestLogicalFramesMatchWireGrammar(t *testing.T) {
	for index, raw := range []string{
		`{"kind":"output","value":null}`,
		`{"kind":"output","value":{"n":9007199254740993,"s":"\ud800"}}`,
		`{"kind":"complete"}`, `{"kind":"input_closed"}`,
		`{"kind":"error","error":{"code":"ERR_RUNTIME"}}`,
		`{"kind":"error","error":{"code":"ERR_RUNTIME","data":null}}`,
		`{"kind":"output"}`, `{"kind":"complete","extra":1}`,
		`{"kind":"error","error":{"code":"ERR_RUNTIME","extra":1}}`,
		`{"kind":"error","error":null}`, `null`, `{"kind":null}`,
	} {
		var logical any
		if err := jsonvalue.Unmarshal([]byte(raw), &logical); err != nil {
			t.Fatal(err)
		}
		got, err := outputFrameFromValue(logical)
		if (err == nil) != (index < 6) {
			t.Fatalf("frame grammar verdict for %s: %v", raw, err)
		}
		var wire OutputFrame
		wireErr := jsonvalue.Unmarshal([]byte(raw), &wire)
		if (err == nil) != (wireErr == nil) || err == nil && !reflect.DeepEqual(got, wire) {
			t.Fatalf("%s: %v / %v", raw, err, wireErr)
		}
	}
}
