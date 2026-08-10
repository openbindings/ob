package app

import (
	"testing"

	openapibinding "github.com/openbindings/openbindings-go/formats/openapi"
)

func TestDetectSourceFormatPrefersCurrentBindingRevision(t *testing.T) {
	artifact := []byte(`{
		"openapi":"3.1.0",
		"info":{"title":"Detection","version":"1"},
		"paths":{"/ping":{"get":{"responses":{"204":{"description":"ok"}}}}}
	}`)
	got, err := DetectSourceFormatFromBytes(artifact)
	if err != nil {
		t.Fatalf("detect OpenAPI source: %v", err)
	}
	if got != openapibinding.BindingSpec {
		t.Fatalf("detected binding specification = %q, want current revision %q", got, openapibinding.BindingSpec)
	}
}
