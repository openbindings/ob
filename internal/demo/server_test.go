package demo

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// TestServedDemoOBIRewritesPortAndValidates exercises serveOBI on a non-default
// port: the served document must rewrite the default-port base to the running
// port and remain a valid 0.2.0 OBI.
func TestServedDemoOBIRewritesPortAndValidates(t *testing.T) {
	srv := httptest.NewServer(serveOBI(9999))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	iface, err := openbindings.ParseDocument(body)
	if err != nil {
		t.Fatalf("parse served OBI: %v", err)
	}
	if err := iface.Validate(); err != nil {
		t.Fatalf("served OBI fails 0.2.0 validation: %v", err)
	}

	s := string(body)
	if !strings.Contains(s, "http://localhost:9999/openapi.json") {
		t.Error("served OBI did not rewrite the base to the running port")
	}
	if strings.Contains(s, "localhost:8080") {
		t.Error("served OBI still carries the default-port base")
	}
}
