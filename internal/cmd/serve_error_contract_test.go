package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/openbindings/ob/internal/servecontract"
)

// TestServeErrorCodesAreDeclared prevents a handler typo from accidentally
// becoming a new public convention. Runtime serialization also fails closed,
// but catching it here gives the author the exact source location.
func TestServeErrorCodesAreDeclared(t *testing.T) {
	files, err := filepath.Glob("serve*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "start.go", "../server/server.go")

	fset := token.NewFileSet()
	used := map[string]bool{}
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			}
			if name != "writeErrorJSON" && name != "writeServerError" {
				return true
			}
			literal, ok := call.Args[2].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Errorf("%s: invalid error-code literal: %v", fset.Position(literal.Pos()), err)
				return true
			}
			if !servecontract.IsErrorCode(servecontract.ErrorCode(value)) {
				t.Errorf("%s: error code %q is absent from the ob start catalog", fset.Position(literal.Pos()), value)
			}
			used[value] = true
			return true
		})
	}
	for _, code := range servecontract.ErrorCodes() {
		// internal_error is the fail-closed result for a programmer mistake,
		// so no handler should deliberately emit it.
		if code != string(servecontract.CodeInternal) && !used[code] {
			t.Errorf("error code %q is published but no handler emits it", code)
		}
	}
}
