package app

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/openbindings/openbindings-go"
)

//go:embed ob.obi.json
var cliInterfaceJSON []byte

var (
	cliInterface     openbindings.Interface
	cliInterfaceOnce sync.Once
	cliInterfaceErr  error
)

// OpenBindingsInterface returns the OpenBindings CLI's own interface definition,
// loaded from the embedded ob.obi.json file.
func OpenBindingsInterface() (openbindings.Interface, error) {
	cliInterfaceOnce.Do(func() {
		cliInterfaceErr = json.Unmarshal(cliInterfaceJSON, &cliInterface)
	})
	return cliInterface, cliInterfaceErr
}
