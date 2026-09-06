package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestColdUserEmbeddedBaseAndNumericInvocation(t *testing.T) {
	ResetDefaultInvoker()
	t.Cleanup(ResetDefaultInvoker)
	var document string
	var received string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/openapi.json" {
			fmt.Fprint(w, document)
			return
		}
		received = r.URL.Path + "?" + r.URL.RawQuery
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	for _, edition := range []string{"2.0", "3.0.4", "3.1.2", "3.2.0"} {
		mu.Lock()
		document = fmt.Sprintf(`{"openapi":%q,"info":{"title":"Cold","version":"1"},"servers":[{"url":"/api"}],"paths":{"/list":{"get":{"operationId":"list","parameters":[{"name":"limit","in":"query","schema":{"type":"integer"}},{"name":"enabled","in":"query","schema":{"type":"boolean"}}],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}}}}}}}}`, edition)
		spec := "openbindings.openapi-" + edition[:3] + "@1"
		if edition == "2.0" {
			address, _ := url.Parse(server.URL)
			document = fmt.Sprintf(`{"swagger":"2.0","info":{"title":"Cold","version":"1"},"host":%q,"schemes":["http"],"basePath":"/api","produces":["application/json"],"paths":{"/list":{"get":{"operationId":"list","parameters":[{"name":"limit","in":"query","type":"integer"},{"name":"enabled","in":"query","type":"boolean"}],"responses":{"200":{"description":"ok","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}}}}}}`, address.Host)
		}
		mu.Unlock()
		for _, output := range []string{"", server.URL + "/openapi.json"} {
			iface, err := SynthesizeInterface(SynthesizeInterfaceInput{Sources: []SynthesizeInterfaceSource{{BindingSpec: spec, Name: "api", Location: server.URL + "/openapi.json", OutputLocation: output, Embed: true}}})
			if err != nil {
				t.Fatal(err)
			}
			src := iface.Sources["api"]
			if src.Location != server.URL+"/openapi.json" || len(src.Content) == 0 {
				t.Fatalf("lost embedded base: %+v", src)
			}
			run, err := invokeOnInterface(context.Background(), iface, "list", "", map[string]any{"limit": float64(2), "enabled": true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for event := range run.Events {
				if event.Error != nil {
					t.Fatalf("strict %s: %+v", edition, event.Error)
				}
			}
			mu.Lock()
			wire := received
			mu.Unlock()
			if wire != "/api/list?enabled=true&limit=2" && wire != "/api/list?limit=2&enabled=true" {
				t.Fatalf("wire: %s", wire)
			}
		}
	}
}

func TestColdUserDetectionPreservesRecognizedInvalidAndRetrievalFailures(t *testing.T) {
	ResetDefaultInvoker()
	t.Cleanup(ResetDefaultInvoker)
	invalid := []byte(`{"swagger":"2.0","info":{"title":"Invalid","version":"1"},"paths":{"/x/{id}":{"get":{"parameters":[{"name":"id","in":"path","type":"string"}],"responses":{"200":{"description":"ok"}}}}}}`)
	_, err := DetectSourceFormatFromBytes(invalid)
	if err == nil || !strings.Contains(err.Error(), "openbindings.openapi-2.0@1") || !strings.Contains(err.Error(), "required") {
		t.Fatalf("invalid document disappeared: %v", err)
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	_, err = DetectSourceFormat(server.URL + "/absent")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("retrieval disappeared: %v", err)
	}
	_, err = DetectSourceFormatFromBytes([]byte(`{"openapi":"9.0.0"}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported edition: %v", err)
	}
}
