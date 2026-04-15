package execref

import (
	"fmt"
	"strings"

	"github.com/google/shlex"
)

// IsExec reports whether raw uses the exec: scheme.
func IsExec(raw string) bool {
	return strings.HasPrefix(strings.TrimSpace(raw), "exec:")
}

// Parse extracts argv from an exec: reference.
// Uses shell-style lexing to properly handle quoted arguments.
func Parse(raw string) ([]string, error) {
	if !IsExec(raw) {
		return nil, fmt.Errorf("invalid exec reference")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "exec:"))
	if rest == "" {
		return nil, fmt.Errorf("invalid exec reference")
	}
	args, err := shlex.Split(rest)
	if err != nil {
		return nil, fmt.Errorf("invalid exec reference: %w", err)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("invalid exec reference")
	}
	return args, nil
}

// RootCommand returns the root command for an exec: reference.
func RootCommand(raw string) (string, error) {
	args, err := Parse(raw)
	if err != nil {
		return "", err
	}
	return args[0], nil
}
