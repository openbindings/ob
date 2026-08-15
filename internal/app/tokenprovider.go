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
	"net"
	"net/url"
	"os"
	"strings"
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

// mintInvoker and bindingResolver indirect into invoke.go through init-time
// assignment: CLIContextResolver participates in the default invoker's
// construction, so a direct reference to either invoke.go function here would
// close an initialization cycle (resolver → mint → invoker → resolver).
var (
	mintInvoker     func(ctx context.Context, iface *openbindings.Interface, opKey, bindingKey string, input any, config *InvokeConfig) (*ConfiguredInvocation, error)
	bindingResolver func(iface *openbindings.Interface, opKey, bindingKey string, input any) (*resolvedBinding, error)
)

func init() {
	mintInvoker = invokeOnInterface
	bindingResolver = resolveBindingAndSource
}

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

	// Best-effort read-modify-write: two concurrent same-target invocations
	// (separate `ob` processes sharing the file store) may each mint and the
	// later Set wins, wasting one token. Acceptable for a CLI — no corruption,
	// bounded to one redundant mint — and a cross-process lock is out of scope.
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

// isLoopbackHost reports whether host is a local-development host exempt from
// the credential TLS floor (localhost, *.localhost, or a loopback IP).
func isLoopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// credentialOrigin classifies a locator or source location for the M5 credential
// transport policy. It returns the normalized origin (host[:port]) when the
// string names a network endpoint, whether that endpoint is PLAINTEXT to a
// non-loopback host (the case a durable credential must never ride), and
// whether it named a network endpoint at all. A file path or opaque scheme is
// not a network endpoint (isNetwork=false); a bare host:port (e.g. gRPC) is a
// network origin whose transport security is a channel/config matter ob cannot
// read from the string, so it carries no plaintext verdict.
func credentialOrigin(s string) (origin string, plaintextRemote bool, isNetwork bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false, false
	}
	// A real URL is identified by "://" — checking the scheme via url.Parse
	// alone misfires, since it reads a bare host:port's colon (gRPC's
	// "host:443") as a scheme and a URL's scheme colon as a host:port.
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return "", false, false // malformed, or file:/// (no host)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "ws":
			return openbindings.NormalizeEndpoint(s), !isLoopbackHost(u.Hostname()), true
		case "https", "wss":
			return openbindings.NormalizeEndpoint(s), false, true
		default:
			return "", false, false // opaque scheme: not an http-family endpoint
		}
	}
	// No scheme: a bare host:port (gRPC-style) is a network origin whose
	// transport ob can't read from the string; anything else is a file path.
	if isHostPort(s) {
		return openbindings.NormalizeEndpoint(s), false, true
	}
	return "", false, false
}

// mintFromPinnedProvider invokes `openbindings.token-provider.mint` on the
// pinned provider's interface — and only there. The mint invocation runs
// with the re-entrancy mark set and no caller context, so provider-side
// challenges surface instead of triggering resolution loops.
func mintFromPinnedProvider(ctx context.Context, provider string, credential any) (*mintedToken, error) {
	// M5 locator floor — before any fetch: never retrieve the provider OBI, nor
	// send a durable credential, over plaintext to a remote host. The OBI
	// describes where the credential goes; fetching it authentically (TLS to a
	// host the caller pinned) is what makes its binding targets trustworthy.
	// Loopback and file-path locators are exempt (local dev; no network fetch).
	locatorOrigin, locatorPlaintext, _ := credentialOrigin(provider)
	if locatorPlaintext {
		return nil, fmt.Errorf("refusing a plaintext token-provider locator %q for a durable credential; pin an https:// locator (loopback exempt for local dev)", provider)
	}

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

	// M5 destination floor + cross-origin transparency, where the mint's source
	// NAMES a network artifact (a located OpenAPI doc, a gRPC host, …). An
	// embedded artifact's dispatch host is inside binding-family knowledge ob
	// does not read here; for it the locator floor above is the backstop, and
	// the binding — authored by a provider whose OBI ob fetched over TLS —
	// owns the target (OB doctrine: ob does not second-guess the binding).
	if resolved, rerr := bindingResolver(iface, opKey, "", input); rerr == nil {
		destOrigin, destPlaintext, destIsNet := credentialOrigin(resolved.source.Location)
		switch {
		case destIsNet && destPlaintext:
			return nil, fmt.Errorf("refusing a plaintext mint destination %q named by pinned provider %q; the artifact describing where the credential goes must be fetched over https", resolved.source.Location, provider)
		case destIsNet && locatorOrigin != "" && destOrigin != locatorOrigin:
			fmt.Fprintf(os.Stderr, "note: pinned provider %q dispatches its mint to %s — the durable credential is sent there\n", provider, destOrigin)
		}
	}

	mintCtx := context.WithValue(ctx, mintingContextKey{}, true)
	run, err := mintInvoker(mintCtx, iface, opKey, "", input, nil)
	if err != nil {
		return nil, fmt.Errorf("invoking %s: %w", opKey, err)
	}
	// Drain any remaining events so the invoker's forwarding goroutine can
	// never block on an unread channel — notably on the ctx-cancelled branch
	// below, which returns before reading.
	defer func() {
		for range run.Events {
		}
	}()
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
	// Validate the expiry before caching it. An unparseable expiresAt would be
	// cached, fail the freshness parse on the next challenge, and re-mint on
	// EVERY subsequent invocation — a silent storm of real provider calls.
	// Reject it here: the failure becomes one stderr warning and a decline to
	// the ordinary ladder, not a poisoned cache.
	if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		return nil, fmt.Errorf("mint output expiresAt %q is not an RFC 3339 instant: %w", expiresAt, err)
	}
	return &mintedToken{accessToken: accessToken, expiresAt: expiresAt}, nil
}
