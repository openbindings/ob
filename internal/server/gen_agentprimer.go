//go:build ignore

// Command gen_agentprimer refreshes the embedded copy of the canonical agent
// primer from the spec repository.
//
// The primer is authored once, in openbindings/spec. ob embeds a copy so that
// `ob --agent-primer` and `GET /spec/agent-primer.md` work from the binary
// alone, with no sibling checkout. Keeping the two in step used to be a manual
// copy; this generator performs it, and
// TestEmbeddedAgentPrimerMatchesCanonicalSpec fails CI when it has not been run.
//
// Usage:
//
//	go generate ./internal/server/...
//
// The spec checkout is located the same way the guard test locates it: the
// directory holding OB_SPEC_CORPUS when that is set, otherwise the sibling
// ../../../spec. A missing source is a hard error, never a silent no-op.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

const primer = "agent-primer.md"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen_agentprimer: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	specRoot, err := findSpecRoot()
	if err != nil {
		return err
	}

	// go:generate runs in the directory holding the directive, where
	// resources/ sits beside this file. Fail clearly if invoked elsewhere
	// rather than reporting a confusing write error.
	if info, err := os.Stat("resources"); err != nil || !info.IsDir() {
		return fmt.Errorf("resources/ not found in the working directory; run `go generate ./internal/server/...` from the ob module root")
	}

	src := filepath.Join(specRoot, primer)
	want, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read canonical primer: %w", err)
	}

	dst := filepath.Join("resources", primer)
	got, err := os.ReadFile(dst)
	switch {
	case err == nil && bytes.Equal(got, want):
		fmt.Printf("gen_agentprimer: %s already matches %s\n", dst, src)
		return nil
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf("read embedded primer: %w", err)
	}

	if err := os.WriteFile(dst, want, 0o644); err != nil {
		return fmt.Errorf("write embedded primer: %w", err)
	}
	fmt.Printf("gen_agentprimer: updated %s from %s (%d bytes)\n", dst, src, len(want))
	return nil
}

// findSpecRoot mirrors the lookup in TestEmbeddedAgentPrimerMatchesCanonicalSpec
// so the generator and the guard never disagree about which checkout is
// canonical.
func findSpecRoot() (string, error) {
	if corpus := os.Getenv("OB_SPEC_CORPUS"); corpus != "" {
		return filepath.Dir(corpus), nil
	}
	sibling := filepath.Clean(filepath.Join("..", "..", "..", "spec"))
	if _, err := os.Stat(filepath.Join(sibling, primer)); err == nil {
		return sibling, nil
	}
	return "", fmt.Errorf("canonical spec checkout not found: set OB_SPEC_CORPUS, or place the spec repository at %s", sibling)
}
