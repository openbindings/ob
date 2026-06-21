// Command genbound regenerates ob's bound CLI OBI (internal/app/ob.obi.json)
// from the unbound contract (ob.obi.json) plus usage.kdl, so the bound
// realization stays conformant with the contract instead of drifting as a
// hand-maintained file.
//
// It is invoked via `go generate ./internal/app` (the go:generate directive in
// internal/app/openbindings.go), whose working directory is internal/app — so
// the paths below are relative to internal/app.
package main

import (
	"fmt"
	"os"

	"github.com/openbindings/ob/internal/app"
)

func main() {
	const (
		contractPath  = "../../ob.obi.json"
		usagePath     = "../cmd/usage.kdl"
		usageFormat   = "usage@2.13.1"
		storedUsage   = "../cmd/usage.kdl"
		outputPath    = "ob.obi.json"
	)

	bound, err := app.GenerateBoundCLI(contractPath, usagePath, usageFormat, storedUsage)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genbound:", err)
		os.Exit(1)
	}
	if err := app.WriteInterfaceFile(outputPath, bound); err != nil {
		fmt.Fprintln(os.Stderr, "genbound: write:", err)
		os.Exit(1)
	}
	fmt.Printf("genbound: regenerated %s (%d operations, %d bindings)\n",
		outputPath, len(bound.Operations), len(bound.Bindings))
}
