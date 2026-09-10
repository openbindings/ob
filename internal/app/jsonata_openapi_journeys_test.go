package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
	"github.com/openbindings/openbindings-go/synthesize"
)

// The peer starts from fixed wire bytes, never our encoder or a native float.
const exactPeerJSON = `{"id":9223372036854775807,"amount":0.12345678901234567890123456789,"huge":1e400,"tiny":1e-400,"label":"😀 e\u0301","isLosslessNumber":true}`
const exactPeerSchema = `{"type":"object","properties":{"id":{"type":"integer"},"amount":{"type":"number"},"huge":{"type":"number"},"tiny":{"type":"number"},"label":{"type":"string"},"isLosslessNumber":{"type":"boolean"}},"required":["id","amount","huge","tiny","label","isLosslessNumber"]}`

func TestJSONataGeneratedOpenAPIFamilyLocalPeer(t *testing.T) {
	for _, version := range []string{"2.0", "3.0.4", "3.1.1", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			ResetDefaultInvoker()
			t.Cleanup(ResetDefaultInvoker)
			var requests atomic.Int32
			var invalidResponse atomic.Bool
			wire := make(chan []byte, 8)
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				body, _ := io.ReadAll(r.Body)
				wire <- body
				w.Header().Set("Content-Type", "application/json")
				response := exactPeerJSON
				if invalidResponse.Load() {
					response = strings.Replace(response, `😀 e\u0301`, `\ud800`, 1)
				}
				_, _ = io.WriteString(w, response)
			}))
			defer peer.Close()
			var artifact, spec string
			if version == "2.0" {
				location, _ := url.Parse(peer.URL)
				spec = "openbindings.openapi-2.0@1"
				artifact = fmt.Sprintf(`{"swagger":"2.0","info":{"title":"Exact peer","version":"1"},"host":%q,"schemes":["http"],"consumes":["application/json"],"produces":["application/json"],"paths":{"/echo":{"post":{"operationId":"echo","parameters":[{"in":"body","name":"body","required":true,"schema":%s}],"responses":{"200":{"description":"ok","schema":%s}}}}}}`, location.Host, exactPeerSchema, exactPeerSchema)
			} else {
				spec = "openbindings.openapi-" + version[:3] + "@1"
				artifact = fmt.Sprintf(`{"openapi":%q,"info":{"title":"Exact peer","version":"1"},"servers":[{"url":%q}],"paths":{"/echo":{"post":{"operationId":"echo","requestBody":{"required":true,"content":{"application/json":{"schema":%s}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":%s}}}}}}}}`, version, peer.URL, exactPeerSchema, exactPeerSchema)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := DefaultRuntime().SynthesizeInterfaceWithCoverage(ctx, &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: spec, Content: openbindings.TextContent(artifact)}}})
			if err != nil {
				t.Fatal(err)
			}
			iface := result.Interface
			generated, _ := jsonvalue.Marshal(iface)
			t.Logf("generated OBI: %s", generated)
			var input any
			if err := jsonvalue.Unmarshal([]byte(exactPeerJSON), &input); err != nil {
				t.Fatal(err)
			}
			operationInput := input
			if version == "2.0" {
				operationInput = map[string]any{"body": input}
			} // generated operation schema retains Swagger's named body parameter
			call := DefaultRuntime().Invoke(ctx, iface, "echo")
			if err := call.Write(ctx, operationInput); err != nil {
				t.Fatalf("input: %#v", err)
			}
			_ = call.Close()
			output, err := invoke.Single(ctx, call.Outputs())
			if err != nil {
				t.Fatalf("output: %#v", err)
			}
			request := <-wire
			encoded, err := jsonvalue.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			for _, material := range [][]byte{request, encoded} {
				// Standard decoder UseNumber is independent of the SDK comparator.
				decoder := json.NewDecoder(strings.NewReader(string(material)))
				decoder.UseNumber()
				var got map[string]any
				if err := decoder.Decode(&got); err != nil {
					t.Fatal(err)
				}
				for key, token := range map[string]string{"id": "9223372036854775807", "amount": "0.12345678901234567890123456789"} {
					if fmt.Sprint(got[key]) != token {
						t.Fatalf("%s changed in %s", key, material)
					}
				}
				if got["label"] != "😀 e\u0301" {
					t.Fatalf("admitted string changed: %s", material)
				}
				if got["isLosslessNumber"] != true {
					t.Fatalf("marker data changed: %s", material)
				}
				for _, key := range []string{"huge", "tiny"} {
					if _, ok := got[key].(json.Number); !ok {
						t.Fatalf("%s stopped being a number: %s", key, material)
					}
				}
			}
			if same, err := jsonvalue.Equal(input, output); err != nil || !same {
				t.Fatalf("round trip changed: %s / %v", encoded, err)
			}
			if requests.Load() != 1 {
				t.Fatalf("unexpected retries: %d", requests.Load())
			}
			// A failed input transform cannot dispatch to the HTTP peer. Output
			// failure may dispatch once, but cannot emit a non-JSON success.
			for _, direction := range []string{"input", "output"} {
				var changed openbindings.Interface
				bytes, _ := jsonvalue.Marshal(iface)
				if err := jsonvalue.Unmarshal(bytes, &changed); err != nil {
					t.Fatal(err)
				}
				for key, binding := range changed.Bindings {
					if binding.Operation != "echo" {
						continue
					}
					bad := &openbindings.TransformOrRef{Inline: `{"bad":[function(){1}]}`}
					if direction == "input" {
						binding.InputTransform = bad
					} else {
						binding.OutputTransform = bad
					}
					changed.Bindings[key] = binding
				}
				before := requests.Load()
				failed := DefaultRuntime().Invoke(ctx, &changed, "echo")
				_ = failed.Write(ctx, operationInput)
				_ = failed.Close()
				if value, err := invoke.Single(ctx, failed.Outputs()); err == nil || value != nil {
					t.Fatalf("%s failure escaped: %#v %v", direction, value, err)
				}
				want := before
				if direction == "output" {
					want++
				}
				if requests.Load() != want {
					t.Fatalf("%s dispatch count=%d want=%d", direction, requests.Load(), want)
				}
			}
			// The OAS family's settled §9.2 profile refuses unpaired surrogates,
			// unlike the broader Core/JSONata carriage domain. This is an expected
			// boundary refusal, never permission to replace or pass the unit.
			var lone any
			if err := jsonvalue.Unmarshal([]byte(strings.Replace(exactPeerJSON, `😀 e\u0301`, `\ud800`, 1)), &lone); err != nil {
				t.Fatal(err)
			}
			if version == "2.0" {
				lone = map[string]any{"body": lone}
			}
			before := requests.Load()
			refused := DefaultRuntime().Invoke(ctx, iface, "echo")
			_ = refused.Write(ctx, lone)
			_ = refused.Close()
			if value, err := invoke.Single(ctx, refused.Outputs()); err == nil || value != nil {
				t.Fatalf("forbidden request escaped: %#v %v", value, err)
			}
			if requests.Load() != before {
				t.Fatal("unpaired surrogate reached HTTP")
			}
			invalidResponse.Store(true)
			badResponse := DefaultRuntime().Invoke(ctx, iface, "echo")
			_ = badResponse.Write(ctx, operationInput)
			_ = badResponse.Close()
			if value, err := invoke.Single(ctx, badResponse.Outputs()); err == nil || value != nil {
				t.Fatalf("forbidden response escaped: %#v %v", value, err)
			}
			if requests.Load() != before+1 {
				t.Fatal("response refusal changed dispatch behavior")
			}
		})
	}
}
