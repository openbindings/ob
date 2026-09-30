package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/openbindings/ob/internal/cmd"
	"github.com/spf13/cobra"
)

func main() {
	// This branch is a command-surface lab. The production command tree and
	// handlers remain in internal/cmd, but this executable exposes only a
	// non-operational preview so its names and shapes can be explored safely.
	//
	// OB_SURFACE_VARIANT selects a tree:
	//   (unset)          the proposed surface
	//   filter-edits     proposed, but edits print the result instead of editing in place
	//   binding-invoke   proposed, but invoke takes an exact binding key
	//   correspond       proposed, but adopt is named correspond
	//   v02-lab[:<v>]    the previous 0.2 lab tree, optionally one of its variants
	//   broad, compact, hybrid   the 0.1-era previews
	variant := os.Getenv("OB_SURFACE_VARIANT")
	var root *cobra.Command
	var preflight func(*cobra.Command, []string) error
	switch {
	case variant == "broad":
		root, preflight = cmd.NewSurfaceRoot(), cmd.SurfacePreflightArgs
	case variant == "compact":
		root, preflight = cmd.NewCompactSurfaceRoot(), cmd.SurfacePreflightArgs
	case variant == "hybrid":
		root, preflight = cmd.NewHybridSurfaceRoot(), cmd.SurfacePreflightArgs
	case variant == "v02-lab" || strings.HasPrefix(variant, "v02-lab:"):
		root, preflight = cmd.NewV02SurfaceRoot(strings.TrimPrefix(strings.TrimPrefix(variant, "v02-lab"), ":")), cmd.V02PreflightArgs
	default:
		cobra.EnableCommandSorting = false
		root, preflight = cmd.NewNextSurfaceRoot(variant), cmd.NextPreflightArgs
	}
	if err := preflight(root, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ob: "+err.Error())
		os.Exit(2)
	}
	if err := root.Execute(); err != nil {
		var coded interface{ ExitCode() int }
		if errors.As(err, &coded) {
			if msg := err.Error(); msg != "" {
				fmt.Fprintln(os.Stderr, "ob: "+msg)
			}
			os.Exit(coded.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "ob: "+err.Error())
		os.Exit(2)
	}
}
