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
	variant := os.Getenv("OB_SURFACE_VARIANT")
	root := cmd.NewV02SurfaceRoot(variant)
	legacy := false
	if variant == "broad" {
		root = cmd.NewSurfaceRoot()
		legacy = true
	} else if variant == "compact" {
		root = cmd.NewCompactSurfaceRoot()
		legacy = true
	} else if variant == "hybrid" {
		root = cmd.NewHybridSurfaceRoot()
		legacy = true
	}
	var preflightErr error
	if legacy {
		preflightErr = cmd.SurfacePreflightArgs(root, os.Args[1:])
	} else {
		preflightErr = cmd.V02PreflightArgs(root, os.Args[1:])
	}
	if err := preflightErr; err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
}
