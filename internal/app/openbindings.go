package app

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/openbindings/openbindings-go"
)

// ob.bound.obi.json is the bound CLI realization, generated from the unbound
// contract (../../ob.obi.json) + usage.kdl. Regenerate with
// `go generate ./internal/app`; do not hand-edit. The
// TestBoundCLIConformsToContract guard fails if it drifts.
//
//go:generate go run ../genbound
//go:embed ob.bound.obi.json
var cliInterfaceJSON []byte

var (
	cliInterface     openbindings.Interface
	cliInterfaceOnce sync.Once
	cliInterfaceErr  error
)

// OpenBindingsInterface returns the OpenBindings CLI's own interface definition,
// loaded from the embedded ob.bound.obi.json file.
func OpenBindingsInterface() (openbindings.Interface, error) {
	cliInterfaceOnce.Do(func() {
		cliInterfaceErr = json.Unmarshal(cliInterfaceJSON, &cliInterface)
	})
	return cliInterface, cliInterfaceErr
}
