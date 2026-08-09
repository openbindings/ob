package app

import (
	"strings"
	"testing"
	"time"

	openapibinding "github.com/openbindings/openbindings-go/formats/openapi"
)

func TestProbeOBI_SynthesizeFromOpenAPI(t *testing.T) {
	result := ProbeOBI("../../testdata/petstore-mini.json", 5*time.Second)
	if result.Status != ProbeStatusOK {
		t.Fatalf("expected status %q, got %q (detail: %s)", ProbeStatusOK, result.Status, result.Detail)
	}
	if !strings.HasPrefix(result.Detail, "synthesized:") {
		t.Fatalf("expected synthesized detail, got %q", result.Detail)
	}
	if result.OBI == "" {
		t.Fatal("expected non-empty OBI")
	}
	if !strings.Contains(result.OBI, "listPets") {
		t.Fatalf("expected OBI to contain listPets operation, got:\n%s", result.OBI)
	}
}

func TestResolveInterfaceDetailed_LocalRawArtifactRetainsCoverage(t *testing.T) {
	resolved, err := ResolveInterfaceDetailed("../../testdata/petstore-mini.json")
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Synthesized || resolved.SourceBindingSpec != openapibinding.BindingSpecV2 {
		t.Fatalf("raw artifact was not identified as synthesized OpenAPI: %#v", resolved)
	}
	if resolved.Coverage == nil {
		t.Fatal("synthesis coverage was discarded during resolution")
	}
	if !resolved.Coverage.Exhaustive || len(resolved.Coverage.Entries) == 0 {
		t.Fatalf("unexpected coverage: %#v", resolved.Coverage)
	}
	if _, ok := resolved.Interface.Operations["listPets"]; !ok {
		t.Fatalf("resolved interface lacks listPets: %#v", resolved.Interface.Operations)
	}
}
