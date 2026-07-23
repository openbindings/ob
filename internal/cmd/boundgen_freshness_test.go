package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/openbindings/ob/internal/app"
)

// TestBoundArtifactsAreFresh is the generator-freshness oracle: the committed
// bound artifacts must be byte-identical to what genbound produces from the
// current inputs (the contract, usage.kdl, openapi.yaml). The conformance
// guards in internal/app catch semantic drift; this catches EVERY drift
// class — an edited usage.kdl help string, a changed embedded artifact — so
// a stale committed artifact can never ride a green suite. On failure:
// `go generate ./internal/app`.
func TestBoundArtifactsAreFresh(t *testing.T) {
	tmp := t.TempDir()

	// Bound CLI OBI (paths mirror internal/genbound/main.go, relative to
	// this package).
	cli, err := app.GenerateBoundCLI("../../ob.obi.json", "usage.kdl")
	if err != nil {
		t.Fatalf("generate bound CLI: %v", err)
	}
	genPath := filepath.Join(tmp, "cli.obi.json")
	if err := app.WriteInterfaceFile(genPath, cli); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../app/ob.bound.obi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("internal/app/ob.bound.obi.json is stale against its inputs — run `go generate ./internal/app`")
	}

	// Recipe doc (derived from the bound CLI OBI).
	wantRecipe, err := os.ReadFile("../../docs/bound-cli-recipe.md")
	if err != nil {
		t.Fatal(err)
	}
	if app.GenerateBoundCLIRecipe(cli) != string(wantRecipe) {
		t.Error("docs/bound-cli-recipe.md is stale against its inputs — run `go generate ./internal/app`")
	}

	// Canonical OpenAPI source (contract + route inventory).
	generatedOpenAPI, err := app.GenerateServeOpenAPI("../../ob.obi.json", "${OB_SERVER_URL}")
	if err != nil {
		t.Fatalf("generate OpenAPI: %v", err)
	}
	wantOpenAPI, err := os.ReadFile("../server/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generatedOpenAPI, wantOpenAPI) {
		t.Error("internal/server/openapi.yaml is stale against the contract or route inventory — run `go generate ./internal/app`")
	}
	generatedAsyncAPI, err := app.GenerateServeAsyncAPI("../../ob.obi.json", "${OB_SERVER_HOST}", "${OB_SERVER_PROTOCOL}")
	if err != nil {
		t.Fatalf("generate AsyncAPI: %v", err)
	}
	wantAsyncAPI, err := os.ReadFile("../server/asyncapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generatedAsyncAPI, wantAsyncAPI) {
		t.Error("internal/server/asyncapi.yaml is stale against the contract — run `go generate ./internal/app`")
	}

	// Bound serve OBI.
	serveBase := fmt.Sprintf("http://127.0.0.1:%d", DefaultServePort)
	serve, err := app.GenerateBoundServe("../../ob.obi.json", "../server/openapi.yaml", "../server/serve.obi.json", serveBase)
	if err != nil {
		t.Fatalf("generate bound serve: %v", err)
	}
	genServe := filepath.Join(tmp, "serve.obi.json")
	if err := app.WriteInterfaceFile(genServe, serve); err != nil {
		t.Fatal(err)
	}
	gotServe, err := os.ReadFile(genServe)
	if err != nil {
		t.Fatal(err)
	}
	wantServe, err := os.ReadFile("../server/serve.obi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotServe, wantServe) {
		t.Error("internal/server/serve.obi.json is stale against its inputs — run `go generate ./internal/app`")
	}
}
