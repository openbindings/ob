package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/ob/internal/server"
)

func TestAgentPrimerFlagPrintsEmbeddedPrimer(t *testing.T) {
	want, err := server.SpecResource("agent-primer.md")
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	root := NewRoot()
	root.SetOut(&stdout)
	root.SetArgs([]string{"--agent-primer"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Fatal("ob --agent-primer did not print the embedded primer byte-for-byte")
	}
}

func TestAgentPrimerServedByStartResourceHandler(t *testing.T) {
	want, err := server.SpecResource("agent-primer.md")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/spec/agent-primer.md", nil)
	req.SetPathValue("name", "agent-primer.md")
	res := httptest.NewRecorder()
	handleSpecResource(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !bytes.Equal(res.Body.Bytes(), want) {
		t.Fatal("ob start did not serve the embedded primer byte-for-byte")
	}
}

func TestEmbeddedAgentPrimerMatchesCanonicalSpec(t *testing.T) {
	specRoot := ""
	if corpus := os.Getenv("OB_SPEC_CORPUS"); corpus != "" {
		specRoot = filepath.Dir(corpus)
	} else {
		candidate := filepath.Clean(filepath.Join("..", "..", "..", "spec"))
		if _, err := os.Stat(filepath.Join(candidate, "agent-primer.md")); err == nil {
			specRoot = candidate
		}
	}
	if specRoot == "" {
		t.Skip("canonical spec checkout not available")
	}

	want, err := os.ReadFile(filepath.Join(specRoot, "agent-primer.md"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := server.SpecResource("agent-primer.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("embedded agent primer is stale; copy spec/agent-primer.md to internal/server/resources/agent-primer.md")
	}
}
