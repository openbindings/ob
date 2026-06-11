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

// CLIContextResolver returns the context resolver for interactive CLI usage:
// the composition of the binding-invoker and context-store roles. When a
// binding raises CONTEXT_REQUIRED, the resolver first consults the CLI context
// store under the challenge's key; if the stored context can't satisfy the
// challenge, it prompts for the missing credentials (the first satisfiable
// alternative), persists them under the key, and returns the resolved context.
// It declines (returns nil) when no prompt is possible (e.g. not a TTY), so the
// challenge surfaces to the caller unchanged.
func CLIContextResolver() openbindings.ContextResolver {
	store := NewCLIContextStore()
	return func(ctx context.Context, details *openbindings.ContextRequiredDetails) (map[string]any, error) {
		// 1. Try the stored context first.
		stored, _ := store.Get(ctx, details.Key)
		if stored != nil && openbindings.ContextSatisfies(stored, details) {
			return stored, nil
		}

		// 2. Interactively resolve the first satisfiable alternative.
		resolved := map[string]any{}
		for k, v := range stored {
			resolved[k] = v
		}
		for _, alt := range details.Alternatives {
			candidate := map[string]any{}
			for k, v := range resolved {
				candidate[k] = v
			}
			if promptForAlternative(ctx, alt, candidate) && openbindings.ContextSatisfies(candidate, details) {
				// 3. Persist resolved context under the challenge key.
				_ = store.Set(ctx, details.Key, candidate)
				return candidate, nil
			}
		}
		return nil, nil
	}
}

// promptForAlternative prompts for every requirement in one alternative,
// merging the acquired credentials into `into`. Returns false if any
// requirement can't be satisfied (unknown family, or a prompt failure such as
// no TTY), in which case the caller tries the next alternative.
func promptForAlternative(ctx context.Context, alt openbindings.ContextAlternative, into map[string]any) bool {
	for _, req := range alt.Requirements {
		switch req.Type {
		case "auth.bearer":
			v, err := cliPrompt(ctx, promptLabel(req, "Bearer token"), &openbindings.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			into["bearerToken"] = v
		case "auth.apiKey":
			v, err := cliPrompt(ctx, promptLabel(req, "API key"), &openbindings.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			into["apiKey"] = v
		case "auth.basic":
			u, err := cliPrompt(ctx, promptLabel(req, "Username"), nil)
			if err != nil || u == "" {
				return false
			}
			p, err := cliPrompt(ctx, "Password", &openbindings.PromptOptions{Secret: true})
			if err != nil {
				return false
			}
			into["basic"] = map[string]any{"username": u, "password": p}
		case "auth.oauth2":
			v, err := cliPrompt(ctx, promptLabel(req, "OAuth access token"), &openbindings.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			into["accessToken"] = v
		default:
			// Unknown requirement family — can't prompt for it.
			return false
		}
	}
	return true
}

func promptLabel(req openbindings.ContextRequirement, fallback string) string {
	if req.Description != "" {
		return req.Description
	}
	return fallback
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
