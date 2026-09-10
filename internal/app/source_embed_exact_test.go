package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestEmbedOpenAPIExactSource(t *testing.T) {
	for _, edition := range []string{"2.0", "3.0", "3.1", "3.2"} {
		for _, token := range []string{"9007199254740993", "0.123456789012345678901", "1e400", "1e-400"} {
			for _, source := range []string{fmt.Sprintf(`{"x-value":%s}`, token), "x-value: " + token + "\n"} {
				t.Run(edition+"/"+source, func(t *testing.T) {
					content, err := ParseContentForEmbed([]byte(source), "openbindings.openapi-"+edition+"@1")
					if err != nil {
						t.Fatal(err)
					}
					var raw map[string]json.RawMessage
					if err := json.Unmarshal(content, &raw); err != nil {
						t.Fatal(err)
					}
					if string(raw["x-value"]) != token {
						t.Fatalf("got %s; want %s", raw["x-value"], token)
					}
				})
			}
		}
	}
}
