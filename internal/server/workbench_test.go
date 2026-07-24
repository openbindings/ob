package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkbenchIndexIsCompiledApplication(t *testing.T) {
	html := string(WorkbenchIndex())
	for _, want := range []string{
		"OpenBindings Workbench",
		"<ob-obi-explorer>",
		"/assets/workbench.js",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("embedded workbench index does not contain %q", want)
		}
	}
	if strings.Contains(html, "/src/main.ts") {
		t.Error("embedded workbench points at an uncompiled TypeScript source")
	}
}

func TestWorkbenchAssetsServeCompiledJavaScript(t *testing.T) {
	req := httptest.NewRequest("GET", "/assets/workbench.js", nil)
	rec := httptest.NewRecorder()
	WorkbenchAssets().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("GET workbench asset status = %d, want 200", rec.Code)
	}
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 1_000 {
		t.Fatalf("compiled workbench JavaScript is unexpectedly small: %d bytes", len(body))
	}
}
