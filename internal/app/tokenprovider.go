package app

// Token-provider pinning — ob's policy for self-maintained bearer context.
//
// A stored context may PIN a token provider for its target:
//
//	tokenProvider     the provider interface's locator (an OBI URL/origin)
//	tokenCredential   the durable credential its mint exchanges (secret)
//
// When a binding challenge for that target is bearer-satisfiable and the
// stored bearerToken is absent or inside the renewal skew of its recorded
// bearerTokenExpiresAt, ob invokes the operation on the PINNED provider
// carrying `openbindings.token-provider.mint` (key or alias), stores the
// minted accessToken/expiresAt back under the target's context, and lets the
// ordinary stored-context path satisfy the challenge. The durable credential
// then never rides ordinary requests — only short-lived tokens do.
//
// The trust rule this file exists to uphold: CAPABILITY NEVER IMPLIES USE.
// Correspondence tells ob which operation on a provider mints; it never
// selects the provider. Only the explicitly pinned locator is ever contacted
// with the credential — never a delegate, never a key-matched candidate,
// never a discovery result. (The decoy-delegate test in
// tokenprovider_test.go is the acceptance criterion for this rule.)
//
// This is ob-specific behavior layered on the non-normative invocation
// pattern: the core model knows nothing of context, the pattern defines the
// resolver seam without policy, the token-provider contract names the supply
// operations, and ob composes them — one operation's outputs become other
// operations' prerequisites.

import (
	"context"
	"fmt"
	"os"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
)

// TokenProviderMintIdentifier is the shared contract key ob resolves on the
// pinned provider's interface (as the operation key or an alias).
const TokenProviderMintIdentifier = "openbindings.token-provider.mint"

// tokenRenewalSkew renews a cached token this long before its recorded
// expiry rather than at it.
const tokenRenewalSkew = 60 * time.Second

// mintingContextKey marks a Context while an auto-mint invocation is in
// flight, so the context resolver never recurses into a second auto-mint
// (a provider whose own mint raises challenges surfaces them unchanged).
type mintingContextKey struct{}

// tokenClock is the time source; override in tests.
var tokenClock = time.Now

// mintInvoker indirects the mint invocation through an init-time assignment:
// CLIContextResolver participates in the default invoker's construction, so a
// direct reference to invokeOnInterface here would close an initialization
// cycle (resolver → mint → invoker → resolver).
var mintInvoker func(ctx context.Context, iface *openbindings.Interface, opKey, bindingKey string, input any, config *InvokeConfig) (*ConfiguredInvocation, error)

func init() { mintInvoker = invokeOnInterface }

// ensurePinnedToken guarantees a live bearerToken in the stored context for
// `key` when a token provider is pinned there, minting from the pinned
// provider when the cached token is absent or expiring. It returns the
// (possibly refreshed) context and whether a mint happened. It never prompts,
// never consults delegates, and declines silently into the ordinary ladder
// on any failure (after a stderr warning, so misconfiguration is not
// invisible).
func ensurePinnedToken(ctx context.Context, store openbindings.ContextStore, key string, stored map[string]any) (map[string]any, bool) {
	if ctx.Value(mintingContextKey{}) != nil {
		return stored, false
	}
	provider, _ := stored["tokenProvider"].(string)
	if provider == "" {
		return stored, false
	}
	if tok, _ := stored["bearerToken"].(string); tok != "" {
		if expStr, _ := stored["bearerTokenExpiresAt"].(string); expStr != "" {
			if exp, err := time.Parse(time.RFC3339, expStr); err == nil && tokenClock().Add(tokenRenewalSkew).Before(exp) {
				return stored, false // cached token still fresh
			}
		} else {
			// A bearerToken with no recorded expiry was set by the user, not
			// by this policy; never overwrite it.
			return stored, false
		}
	}

	minted, err := mintFromPinnedProvider(ctx, provider, stored["tokenCredential"])
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: token provider %q pinned for %q but minting failed: %v\n", provider, key, err)
		return stored, false
	}

	refreshed := make(map[string]any, len(stored)+2)
	for k, v := range stored {
		refreshed[k] = v
	}
	refreshed["bearerToken"] = minted.accessToken
	refreshed["bearerTokenExpiresAt"] = minted.expiresAt
	if key != "" && store != nil {
		if err := store.Set(ctx, key, refreshed); err != nil {
			fmt.Fprintf(os.Stderr, "warning: minted token for %q could not be cached: %v\n", key, err)
		}
	}
	return refreshed, true
}

type mintedToken struct {
	accessToken string
	expiresAt   string
}

// mintFromPinnedProvider invokes `openbindings.token-provider.mint` on the
// pinned provider's interface — and only there. The mint invocation runs
// with the re-entrancy mark set and no caller context, so provider-side
// challenges surface instead of triggering resolution loops.
func mintFromPinnedProvider(ctx context.Context, provider string, credential any) (*mintedToken, error) {
	iface, err := ResolveInterface(provider)
	if err != nil {
		return nil, fmt.Errorf("resolving pinned provider interface: %w", err)
	}
	opKey, _, found := openbindings.ResolveOperation(iface, TokenProviderMintIdentifier)
	if !found {
		return nil, fmt.Errorf("pinned provider carries no operation corresponding to %s", TokenProviderMintIdentifier)
	}

	input := map[string]any{}
	if cred, _ := credential.(string); cred != "" {
		input["credential"] = cred
	}

	mintCtx := context.WithValue(ctx, mintingContextKey{}, true)
	run, err := mintInvoker(mintCtx, iface, opKey, "", input, nil)
	if err != nil {
		return nil, fmt.Errorf("invoking %s: %w", opKey, err)
	}
	var out InvocationOutput
	select {
	case ev, ok := <-run.Events:
		if !ok {
			return nil, fmt.Errorf("mint produced no output")
		}
		out = ev
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if out.Error != nil {
		return nil, fmt.Errorf("mint completed unsuccessfully: %s", out.Error.Code)
	}
	value, _ := out.Output.(map[string]any)
	accessToken, _ := value["accessToken"].(string)
	expiresAt, _ := value["expiresAt"].(string)
	if accessToken == "" || expiresAt == "" {
		return nil, fmt.Errorf("mint output carried no accessToken/expiresAt")
	}
	return &mintedToken{accessToken: accessToken, expiresAt: expiresAt}, nil
}
