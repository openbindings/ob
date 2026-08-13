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

// CLIContextResolver returns ob's optional stored and interactive realization
// of binding-invoker context challenges. When a
// binding raises CONTEXT_REQUIRED, the resolver derives a store key from the
// challenge's target and first consults the CLI context store under it; if the
// stored context can't satisfy the challenge, it prompts for the missing
// credentials (the first satisfiable alternative), persists only values whose
// requirements explicitly permit reuse under the key, and returns the
// resolved context scoped to the challenge
// (ScopeContext: only fields named by the satisfied alternative).
// It declines (returns nil) when no prompt is possible (e.g. not a TTY), so the
// challenge surfaces to the caller unchanged.
func CLIContextResolver() openbindings.ContextResolver {
	store := NewCLIContextStore()
	return func(ctx context.Context, details *openbindings.ContextRequiredDetails) (map[string]any, error) {
		// The challenge reports the target the binding addresses; derive the
		// store key from it the same way the SDK's StoreContextResolver does,
		// so keys match across the CLI and the in-process resolver.
		key := openbindings.NormalizeEndpoint(details.Target)

		// 1. Try the stored context first, but never turn an empty or
		// unkeyable challenge target into a shared storage bucket.
		var stored map[string]any
		if key != "" {
			stored, _ = store.Get(ctx, key)
		}
		if stored != nil {
			if reusable := durableDetails(details); reusable != nil && openbindings.ContextSatisfies(stored, reusable) {
				return openbindings.ScopeContext(stored, reusable), nil
			}
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
				if persistable := durableSubset(alt, candidate); key != "" && len(persistable) > 0 {
					_ = store.Set(ctx, key, persistable)
				}
				// Least privilege: hand back only what this challenge needs.
				return openbindings.ScopeContext(candidate, details), nil
			}
		}
		return nil, nil
	}
}

// requirementField maps an unnamed requirement family to the context field
// populated by promptForAlternative.
var requirementField = map[string]string{
	"auth.bearer": "bearerToken",
	"auth.apiKey": "apiKey",
	"auth.basic":  "basic",
	"auth.oauth2": "accessToken",
}

// durableSubset positively projects only values contributed by requirements
// that explicitly permit reuse. Starting empty prevents unrelated stored or
// interactive fields from entering persistence by accident.
func durableSubset(alt openbindings.ContextAlternative, candidate map[string]any) map[string]any {
	out := map[string]any{}
	for _, req := range alt.Requirements {
		if req.Durable == nil || !*req.Durable {
			continue
		}
		if req.Name != "" && strings.HasPrefix(req.Type, "auth.") {
			credentials, _ := candidate["credentials"].(map[string]any)
			value, present := credentials[req.Name]
			if !present {
				continue
			}
			scoped, _ := out["credentials"].(map[string]any)
			if scoped == nil {
				scoped = map[string]any{}
				out["credentials"] = scoped
			}
			scoped[req.Name] = value
			continue
		}
		if field, ok := requirementField[req.Type]; ok {
			if value, present := candidate[field]; present {
				out[field] = value
			}
		}
	}
	return out
}

// durableDetails retains only complete alternatives whose every requirement
// explicitly permits persistence and reuse. An AND-set is indivisible: a
// store may not partially satisfy one by dropping its non-durable members.
func durableDetails(details *openbindings.ContextRequiredDetails) *openbindings.ContextRequiredDetails {
	if details == nil {
		return nil
	}
	out := &openbindings.ContextRequiredDetails{Target: details.Target}
	for _, alt := range details.Alternatives {
		whollyDurable := len(alt.Requirements) > 0
		for _, req := range alt.Requirements {
			if req.Durable == nil || !*req.Durable {
				whollyDurable = false
				break
			}
		}
		if whollyDurable {
			out.Alternatives = append(out.Alternatives, alt)
		}
	}
	if len(out.Alternatives) == 0 {
		return nil
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
			setPromptedCredential(into, req, "bearerToken", v)
		case "auth.apiKey":
			v, err := cliPrompt(ctx, promptLabel(req, "API key"), &openbindings.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			setPromptedCredential(into, req, "apiKey", v)
		case "auth.basic":
			u, err := cliPrompt(ctx, promptLabel(req, "Username"), nil)
			if err != nil || u == "" {
				return false
			}
			p, err := cliPrompt(ctx, "Password", &openbindings.PromptOptions{Secret: true})
			if err != nil {
				return false
			}
			setPromptedCredential(into, req, "basic", map[string]any{"username": u, "password": p})
		case "auth.oauth2":
			v, err := cliPrompt(ctx, promptLabel(req, "OAuth access token"), &openbindings.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			value := any(v)
			if req.Name != "" {
				value = map[string]any{"accessToken": v}
			}
			setPromptedCredential(into, req, "accessToken", value)
		default:
			// Unknown requirement family — can't prompt for it.
			return false
		}
	}
	return true
}

func setPromptedCredential(into map[string]any, req openbindings.ContextRequirement, field string, value any) {
	if req.Name == "" {
		into[field] = value
		return
	}
	existing, _ := into["credentials"].(map[string]any)
	credentials := make(map[string]any, len(existing)+1)
	for name, stored := range existing {
		credentials[name] = stored
	}
	credentials[req.Name] = value
	into["credentials"] = credentials
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
