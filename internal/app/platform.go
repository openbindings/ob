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
// the composition of the binding-invoker and key-value-store interfaces. When a
// binding raises CONTEXT_REQUIRED, the resolver derives a store key from the
// challenge's target and first consults the CLI context store under it; if the
// stored context can't satisfy the challenge, it prompts for the missing
// credentials (the first satisfiable alternative), persists them under the
// key, and returns the resolved context scoped to the challenge (ScopeContext:
// only the satisfied alternative's credentials plus non-secret config, never
// other stored credentials).
// It declines (returns nil) when no prompt is possible (e.g. not a TTY), so the
// challenge surfaces to the caller unchanged.
func CLIContextResolver() openbindings.ContextResolver {
	store := NewCLIContextStore()
	return func(ctx context.Context, details *openbindings.ContextRequiredDetails) (map[string]any, error) {
		// The challenge reports the target the binding addresses; derive the
		// store key from it the same way the SDK's StoreContextResolver does,
		// so keys match across the CLI and the in-process resolver.
		key := openbindings.NormalizeEndpoint(details.Target)

		// 1. Try the stored context first.
		stored, _ := store.Get(ctx, key)
		if stored != nil && openbindings.ContextSatisfies(stored, details) {
			return openbindings.ScopeContext(stored, details), nil
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
				// 3. Persist only the durable portion under the challenge key.
				// Non-durable context (e.g. a short-lived token) MUST NOT be
				// written to disk/keychain; it is re-acquired each call.
				if persistable := durableSubset(alt, candidate); len(persistable) > 0 {
					_ = store.Set(ctx, key, persistable)
				}
				// Least privilege: hand back only what this challenge needs.
				return openbindings.ScopeContext(candidate, details), nil
			}
		}
		return nil, nil
	}
}

// requirementField maps a requirement family to the context field it
// populates (mirrors promptForAlternative and the SDK's field mapping).
var requirementField = map[string]string{
	"auth.bearer": "bearerToken",
	"auth.apiKey": "apiKey",
	"auth.basic":  "basic",
	"auth.oauth2": "accessToken",
}

// durableSubset returns a copy of candidate with the fields contributed by
// non-durable requirements of alt removed, so only persistable context is
// stored. A requirement is durable unless it explicitly sets Durable=false.
func durableSubset(alt openbindings.ContextAlternative, candidate map[string]any) map[string]any {
	out := make(map[string]any, len(candidate))
	for k, v := range candidate {
		out[k] = v
	}
	for _, req := range alt.Requirements {
		if req.Durable != nil && !*req.Durable {
			if field, ok := requirementField[req.Type]; ok {
				delete(out, field)
			}
		}
	}
	return out
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
