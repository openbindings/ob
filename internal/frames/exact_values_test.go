package frames

import (
	"encoding/json"
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
