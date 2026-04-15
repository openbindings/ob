package app

import (
	"strings"
	"testing"
	"time"
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
