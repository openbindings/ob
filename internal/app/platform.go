package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"golang.org/x/term"
)

// CLIPlatformCallbacks returns PlatformCallbacks for interactive CLI usage.
// Prompt reads from stdin (using terminal raw mode for secrets), and
// Confirmation asks a y/n question on stderr/stdin.
func CLIPlatformCallbacks() *openbindings.PlatformCallbacks {
	return &openbindings.PlatformCallbacks{
		Prompt:       cliPrompt,
		Confirmation: cliConfirmation,
	}
}

func cliPrompt(_ context.Context, message string, opts *openbindings.PromptOptions) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("interactive prompt unavailable: stdin is not a terminal")
	}
	if opts != nil && opts.Secret {
		fmt.Fprintf(os.Stderr, "%s: ", message)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("reading secret input: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}

	fmt.Fprintf(os.Stderr, "%s: ", message)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func cliConfirmation(_ context.Context, message string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("interactive confirmation unavailable: stdin is not a terminal")
	}
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", message)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}
