package server

import (
	"embed"
	"io/fs"
	"net/http"
)

// workbenchDist is produced from the separately publishable
// openbindings/elements packages. The Go binary embeds only compiled browser
// assets; it carries no Node or framework runtime.
//
//go:embed workbench/dist
var workbenchDist embed.FS

// WorkbenchIndex returns the generated ob-start workbench entry document.
func WorkbenchIndex() []byte {
	raw, err := workbenchDist.ReadFile("workbench/dist/index.html")
	if err != nil {
		panic("embedded ob start workbench is missing index.html: " + err.Error())
	}
	return raw
}

// WorkbenchAssets serves the generated workbench tree rooted at its dist
// directory. Route registration remains in cmd, beside the rest of ob start's
// application surface.
func WorkbenchAssets() http.Handler {
	root, err := fs.Sub(workbenchDist, "workbench/dist")
	if err != nil {
		panic("embedded ob start workbench is invalid: " + err.Error())
	}
	return http.FileServer(http.FS(root))
}
