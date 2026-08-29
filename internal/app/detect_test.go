package app

import (
	"testing"

	openapibinding "github.com/openbindings/openbindings-go/formats/openapi"
)

func TestDetectSourceFormatSelectsExactOpenAPISibling(t *testing.T) {
	tests := []struct {
		name     string
		artifact string
		want     string
	}{
		{"2.0", `{"swagger":"2.0","info":{"title":"Detection","version":"1"},"paths":{}}`, openapibinding.BindingSpecOpenAPI20},
		{"3.0", `{"openapi":"3.0.4","info":{"title":"Detection","version":"1"},"paths":{}}`, openapibinding.BindingSpecOpenAPI30},
		{"3.1", `{"openapi":"3.1.2","info":{"title":"Detection","version":"1"},"paths":{}}`, openapibinding.BindingSpecOpenAPI31},
		{"3.2", `{"openapi":"3.2.0","info":{"title":"Detection","version":"1"},"paths":{}}`, openapibinding.BindingSpecOpenAPI32},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DetectSourceFormatFromBytes([]byte(test.artifact))
			if err != nil {
				t.Fatalf("detect OpenAPI source: %v", err)
			}
			if got != test.want {
				t.Fatalf("detected binding specification = %q, want %q", got, test.want)
			}
		})
	}
}
