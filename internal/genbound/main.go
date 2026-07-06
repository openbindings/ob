// Command genbound regenerates ob's bound OBIs from the unbound contract
// (ob.obi.json), so the bound realizations stay conformant with the contract
// instead of drifting as hand-maintained files:
//
//   - the bound CLI OBI (internal/app/ob.obi.json), from contract + usage.kdl;
//   - the bound serve OBI (internal/server/serve.obi.json), from contract +
//     openapi.yaml (REST) + the WS invoke binding.
//
// It is invoked via `go generate ./internal/app` (the go:generate directive in
// internal/app/openbindings.go), whose working directory is internal/app — so
// the paths below are relative to internal/app.
package main

import (
	"fmt"
	"os"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/cmd"
)

func main() {
	const contractPath = "../../ob.obi.json"

	// Bound CLI OBI: contract + usage.kdl, emitted as an openbindings.usage
	// wrapper source (the artifact-version pin rides the wrapper's
	// spec.format, tracked from usage.MaxTestedVersion inside boundgen).
	cli, err := app.GenerateBoundCLI(contractPath, "../cmd/usage.kdl")
	if err != nil {
		fmt.Fprintln(os.Stderr, "genbound: cli:", err)
		os.Exit(1)
	}
	if err := app.WriteInterfaceFile("ob.obi.json", cli); err != nil {
		fmt.Fprintln(os.Stderr, "genbound: write cli:", err)
		os.Exit(1)
	}
	fmt.Printf("genbound: regenerated ob.obi.json (%d operations, %d bindings)\n",
		len(cli.Operations), len(cli.Bindings))

	// Bound serve OBI: contract + openapi.yaml (REST) + WS invoke. MCP is not a
	// served transport here; it is produced by pointing the bridge at a running
	// server (`ob mcp <url>`), so the served OBI carries no mcp source/bindings.
	// Sources point at this server's own live spec endpoints. The committed file
	// carries the default-port base so it's a valid OBI-D-05 document; handleOBI
	// rewrites it to the actual request address at serve time, so this default is
	// never what a client sees. Derived from the one port constant
	// (cmd.DefaultServePort), not a re-typed literal.
	const servePath = "../server/serve.obi.json"
	serveBase := fmt.Sprintf("http://127.0.0.1:%d", cmd.DefaultServePort)
	serve, err := app.GenerateBoundServe(contractPath, "../server/openapi.yaml", servePath, serveBase)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genbound: serve:", err)
		os.Exit(1)
	}
	if err := app.WriteInterfaceFile(servePath, serve); err != nil {
		fmt.Fprintln(os.Stderr, "genbound: write serve:", err)
		os.Exit(1)
	}
	fmt.Printf("genbound: regenerated serve.obi.json (%d operations, %d bindings)\n",
		len(serve.Operations), len(serve.Bindings))
}
