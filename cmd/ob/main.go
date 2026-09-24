package main

import (
	"fmt"
	"os"

	"github.com/openbindings/ob/internal/cmd"
)

func main() {
	// This branch is a command-surface lab. The production command tree and
	// handlers remain in internal/cmd, but this executable exposes only a
	// non-operational preview so its names and shapes can be explored safely.
	root := cmd.NewSurfaceRoot()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
}
