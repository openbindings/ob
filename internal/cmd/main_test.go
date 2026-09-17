package cmd

import (
	"os"
	"testing"

	"github.com/openbindings/ob/internal/testenv"
	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(testenv.Run(m))
}
