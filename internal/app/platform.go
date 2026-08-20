package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/openbindings/openbindings-go/invoke"
	"golang.org/x/term"
)

// CLIContextResolver returns ob's optional stored and interactive realization
// of binding-invoker context challenges. When a
// binding raises CONTEXT_REQUIRED, the resolver derives a store key from the
// challenge's target (per requirement family — see the keying rule below)
// and first consults the CLI context store under it; if the
// stored context can't satisfy the challenge, it prompts for the missing
// credentials or configuration (the first satisfiable alternative), persists
// only values whose
// requirements explicitly permit reuse under the key, and returns the
// resolved context scoped to the challenge
// (ScopeContext: only fields named by the satisfied alternative).
// It declines (returns nil) when no prompt is possible (e.g. not a TTY), so the
// challenge surfaces to the caller unchanged.
func CLIContextResolver() invoke.ContextResolver {
	store := NewCLIContextStore()
	return func(ctx context.Context, details *invoke.ContextRequiredDetails) (map[string]any, error) {
		// Keying rule (context-scope model, ratified 2026-08-19): the
		// challenge target is an engine-asserted opaque scope, and the
		// requirement family decides how it keys the store. An alternative
		// consisting solely of config.value requirements is artifact-bound
		// configuration: it files and fetches under the EXACT asserted
		// target, verbatim (no endpoint normalization, no origin scan, no
		// hierarchical walk-up) — one artifact's configuration answers must
		// not resolve another's challenge. A credential-bearing alternative
		// keeps the endpoint-normalized convention, derived the same way as
		// the SDK's StoreContextResolver so keys match across the CLI and
		// the in-process resolver.
		credKey := invoke.NormalizeEndpoint(details.Target)

		// 1. Try the stored context first, per alternative under its
		// asserted key, but never turn an empty or unkeyable challenge
		// target into a shared storage bucket.
		var credStored map[string]any
		if credKey != "" {
			credStored, _ = store.Get(ctx, credKey)
		}
		// 1.5. Token-provider pinning: when this target pins a provider,
		// keep its bearerToken self-maintained (mint on absence or expiry)
		// so the durable credential never rides ordinary requests. Only the
		// pinned provider is ever contacted — see tokenprovider.go.
		// Credential lane only: a configuration challenge never mints.
		if credStored != nil {
			credStored, _ = ensurePinnedToken(ctx, store, credKey, credStored)
		}
		if reusable := durableDetails(details); reusable != nil {
			for _, alt := range reusable.Alternatives {
				altDetails := &invoke.ContextRequiredDetails{
					Target:       details.Target,
					Alternatives: []invoke.ContextAlternative{alt},
				}
				var stored map[string]any
				if configValueOnlyAlternative(alt) {
					if details.Target == "" {
						continue
					}
					stored, _ = LoadContextExact(details.Target)
				} else {
					if credKey == "" {
						continue
					}
					stored = credStored
				}
				if stored != nil && invoke.ContextSatisfies(stored, altDetails) {
					return invoke.ScopeContext(stored, altDetails), nil
				}
			}
		}

		// 2. Interactively resolve the first satisfiable alternative.
		resolved := map[string]any{}
		for k, v := range credStored {
			resolved[k] = v
		}
		for _, alt := range details.Alternatives {
			candidate := map[string]any{}
			for k, v := range resolved {
				candidate[k] = v
			}
			if promptForAlternative(ctx, alt, candidate) && invoke.ContextSatisfies(candidate, details) {
				// 3. Persist only the durable portion under the alternative's
				// asserted key. Non-durable context (e.g. a short-lived
				// token) MUST NOT be written to disk/keychain; it is
				// re-acquired each call. Configuration answers merge
				// point-wise under the exact target; the credential lane
				// keeps its existing store.Set convention.
				if persistable := durableSubset(alt, candidate); len(persistable) > 0 {
					if configValueOnlyAlternative(alt) {
						if config, ok := persistable["configuration"].(map[string]any); ok && details.Target != "" {
							_ = mergeDurableConfiguration(details.Target, config)
						}
					} else if credKey != "" {
						_ = store.Set(ctx, credKey, persistable)
					}
				}
				// Least privilege: hand back only what this challenge needs.
				return invoke.ScopeContext(candidate, details), nil
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
func durableSubset(alt invoke.ContextAlternative, candidate map[string]any) map[string]any {
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
		if req.Type == "config.value" {
			projectDurableConfigValue(req, candidate, out)
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

// projectDurableConfigValue projects a config.value requirement's
// contribution into the persistable subset: only configuration.<point>, and
// within the point only the fragment the requirement's path addresses —
// never the whole configuration map, so unrelated points and members do not
// enter persistence by accident.
func projectDurableConfigValue(req invoke.ContextRequirement, candidate, out map[string]any) {
	point, path, _, ok := configValueCarriage(req)
	if !ok {
		return
	}
	configuration, _ := candidate["configuration"].(map[string]any)
	value, present := configuration[point]
	if !present {
		return
	}
	selected, selectedPresent := configValueAt(value, path)
	if !selectedPresent {
		return
	}
	scoped, _ := out["configuration"].(map[string]any)
	if scoped == nil {
		scoped = map[string]any{}
		out["configuration"] = scoped
	}
	pointValue := configPointValue(path, selected)
	if existing, ok := scoped[point].(map[string]any); ok {
		if fragment, isFragment := pointValue.(map[string]any); isFragment {
			mergeConfigFragment(existing, fragment)
			return
		}
	}
	scoped[point] = pointValue
}

// durableDetails retains only complete alternatives whose every requirement
// explicitly permits persistence and reuse. An AND-set is indivisible: a
// store may not partially satisfy one by dropping its non-durable members.
func durableDetails(details *invoke.ContextRequiredDetails) *invoke.ContextRequiredDetails {
	if details == nil {
		return nil
	}
	out := &invoke.ContextRequiredDetails{Target: details.Target}
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
func promptForAlternative(ctx context.Context, alt invoke.ContextAlternative, into map[string]any) bool {
	for _, req := range alt.Requirements {
		switch req.Type {
		case "auth.bearer":
			v, err := cliPrompt(ctx, promptLabel(req, "Bearer token"), &invoke.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			setPromptedCredential(into, req, "bearerToken", v)
		case "auth.apiKey":
			v, err := cliPrompt(ctx, promptLabel(req, "API key"), &invoke.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			setPromptedCredential(into, req, "apiKey", v)
		case "auth.basic":
			u, err := cliPrompt(ctx, promptLabel(req, "Username"), nil)
			if err != nil || u == "" {
				return false
			}
			p, err := cliPrompt(ctx, "Password", &invoke.PromptOptions{Secret: true})
			if err != nil {
				return false
			}
			setPromptedCredential(into, req, "basic", map[string]any{"username": u, "password": p})
		case "auth.oauth2":
			v, err := cliPrompt(ctx, promptLabel(req, "OAuth access token"), &invoke.PromptOptions{Secret: true})
			if err != nil || v == "" {
				return false
			}
			value := any(v)
			if req.Name != "" {
				value = map[string]any{"accessToken": v}
			}
			setPromptedCredential(into, req, "accessToken", value)
		case "config.value":
			if !promptConfigValue(ctx, req, into) {
				return false
			}
		default:
			// Unknown requirement family — can't prompt for it.
			return false
		}
	}
	return true
}

// promptConfigValue interactively resolves a config.value requirement.
// Bounded on purpose: only an engine-asserted closed set (an `enum` schema)
// prompts — a numbered selection in the style of the credential prompts —
// because a free-form JSON prompt invites typos into persisted
// configuration. For a non-enum schema, or none, the rendered challenge and
// its remedy line (`ob context set … --config`) are the resolution path, so
// this declines and the challenge surfaces unchanged.
func promptConfigValue(ctx context.Context, req invoke.ContextRequirement, into map[string]any) bool {
	point, path, schema, ok := configValueCarriage(req)
	if !ok {
		return false
	}
	members := schemaEnum(schema)
	if members == nil {
		return false
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s\n", promptLabel(req, "Configuration value for "+point))
	for i, member := range members {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, renderConfigChoice(member))
	}
	answer, err := cliPrompt(ctx, fmt.Sprintf("Select [1-%d]", len(members)), nil)
	if err != nil {
		return false
	}
	index, err := strconv.Atoi(strings.TrimSpace(answer))
	if err != nil || index < 1 || index > len(members) {
		return false
	}
	selected := members[index-1]
	configuration, _ := into["configuration"].(map[string]any)
	if configuration == nil {
		configuration = map[string]any{}
		into["configuration"] = configuration
	}
	// Shape the selection into the fragment the path addresses (the SDK's
	// configurationFragment semantics: /url yields {"url": <value>}), and
	// merge rather than replace so sibling members of the point survive.
	pointValue := configPointValue(path, selected)
	if existing, ok := configuration[point].(map[string]any); ok {
		if fragment, isFragment := pointValue.(map[string]any); isFragment {
			mergeConfigFragment(existing, fragment)
			return true
		}
	}
	configuration[point] = pointValue
	return true
}

func setPromptedCredential(into map[string]any, req invoke.ContextRequirement, field string, value any) {
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

func promptLabel(req invoke.ContextRequirement, fallback string) string {
	if req.Description != "" {
		return req.Description
	}
	return fallback
}

func cliPrompt(_ context.Context, message string, opts *invoke.PromptOptions) (string, error) {
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
