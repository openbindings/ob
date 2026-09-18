package app

import (
	"os"
	"testing"

	"github.com/openbindings/ob/internal/testenv"
	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	// Tests explicitly selecting the keychain backend must still never reach
	// the OS keychain. Process children use the isolated file backend instead.
	keyring.MockInit()
	os.Exit(testenv.Run(m))
}
