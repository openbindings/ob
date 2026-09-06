package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
	openapibinding "github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/synthesize"
)

// Stand in for another family; detection must preserve generic multi-provider
// probing rather than making artifact GET or OpenAPI recognition authoritative.
type additionalDetectionClaim struct {
	synthesize.InterfaceSynthesizer
}

func (s additionalDetectionClaim) BindingSpecs() []openbindings.BindingSpecInfo {
	return append(s.InterfaceSynthesizer.BindingSpecs(), openbindings.BindingSpecInfo{BindingSpec: "openbindings.test@1"})
}
func (s additionalDetectionClaim) SynthesizeInterface(ctx context.Context, in *synthesize.SynthesizeInput) (*openbindings.Interface, error) {
	if in.Sources[0].BindingSpec != "openbindings.test@1" {
		return s.InterfaceSynthesizer.SynthesizeInterface(ctx, in)
	}
	return &openbindings.Interface{Sources: map[string]openbindings.Source{"test": {BindingSpec: "openbindings.test@1"}}}, nil
}

func TestDetectionKeepsOtherProviderClaims(t *testing.T) {
	ResetDefaultInvoker()
	t.Cleanup(ResetDefaultInvoker)
	runtime := defaultCLIRuntime()
	runtime.Synthesizer = additionalDetectionClaim{runtime.Synthesizer}
	_, err := DetectSourceFormatFromBytes([]byte(`{"openapi":"3.1.2","info":{"title":"Detection","version":"1"},"paths":{}}`))
	if err == nil || !strings.Contains(err.Error(), "disagree") {
		t.Fatalf("lost ambiguity: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusMethodNotAllowed) }))
	defer server.Close()
	got, err := DetectSourceFormat(server.URL)
	if err != nil || got != "openbindings.test@1" {
		t.Fatalf("artifact GET blocked service provider: %s %v", got, err)
	}
}

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
