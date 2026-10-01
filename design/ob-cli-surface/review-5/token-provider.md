# Token Provider

Operations that produce, examine, and end short-lived bearer tokens: `mint`
exchanges a credential for a new short-lived token, `refresh` exchanges a
refresh token for its successor, `introspect` describes a token, and
`revoke` ends one. The contract is flow- and protocol-universal — an OAuth
token endpoint, a secrets vault, a cloud CLI, and a venue's token service
can all carry these names — and deliberately kind-specific: it produces
bearer tokens only, permanently (see Evolution).

This contract is the supply half of a seam whose demand half the project
also publishes: the binding-invoker and operation-invoker contracts define
how a runtime discovers that an invocation *needs* a credential; this
contract names the operations that *produce* one, so a runtime holding that
need can resolve any corresponding provider. Neither half depends on the
other — the contract mentions no other interface, and a service can adopt
it having never heard of either.

*Provenance: discovered during the Panjir venue's authentication build,
designed and ratified through this repository's process; Panjir is
implementation #1, by alias, with no authority over this vocabulary.*

## Inputs, not context

Per this repository's authoring convention, tokens are this contract's
subject matter, so the credentials and tokens acted on are ordinary
operation inputs and outputs — never carried as any runtime's context or
prerequisite machinery. What a concrete manifestation additionally requires
to perform an operation (authentication of its caller, configuration)
remains that manifestation's prerequisite, exactly as for any operation.

## Secrecy

**The `Secret.` marker, defined once:** every value this contract marks
`Secret` — `accessToken`, `refreshToken`, mint's `credential`, refresh's
`refreshToken`, introspect's and revoke's `token` — MUST be kept
confidential wherever operation values travel. That includes logs,
diagnostics and provenance envelopes, caches, validation-failure output
that echoes the offending instance, record/replay fixtures, generated
examples, command lines and argv, URL paths and query strings, and
tool-call transcripts. `expiresAt` and everything in `TokenInfo` are not
secret; every `TokenInfo` property — required, optional, or added by a
provider — MUST be non-secret metadata, so anything secret rides `Token`,
never `TokenInfo`.

The marker is prose today. A machine-readable secret annotation (covering
inputs and outputs symmetrically) is planned so that binding
specifications, transform auditing, and diagnostics redaction can enforce
what these sentences state; until it lands, conformance to the marker is an
implementation obligation tooling cannot check.

## Trust and resolution

The namespace conveys no endorsement: no key, under any publisher, is ever
itself a reason to route a secret. Carrying these operation names is not a
reason to receive a credential — correspondence is author-asserted, and
registration in any delegate registry is not trust.

For consuming runtimes, the load-bearing rules:

- Which provider a credential or token is given to is **caller
  configuration, established out of band**. A resolver MUST NOT select the
  recipient of a secret by key correspondence, delegate preference,
  discovery, or any generic substitution policy.
- Refresh tokens are in the protected class: any token that can produce
  successors is guarded by the same rule.
- A resolver that falls back from a failed refresh to mint MUST target only
  the provider pinned for that credential — never a substitute that happens
  to carry the mint key. (An unsuccessful refresh from a hostile provider
  must not become a lever for summoning the primary credential elsewhere.)
- Acceptance check for any auto-resolving runtime: register a decoy
  interface aliasing `openbindings.token-provider.mint`; the runtime must
  refuse or ignore it. A runtime that fails this test is a
  credential-phishing machine.

## Anti-scanning

Any string can be given to the string-accepting operations, so a surface
reachable without authenticating its caller is a token-scanning oracle —
and a successful introspect amplifies it, returning `subject` and `scopes`.
RFC 7662 §2.1 makes caller authorization a MUST for HTTP introspection
endpoints for exactly this reason; this contract's SHOULD is deliberately
protocol-neutral (a sidecar on a local socket defeats scanning by boundary
rather than caller auth), not weaker in intent. The contract states the
SHOULD on introspect, the amplified case; the same consideration
generalizes to every string-accepting operation, and providers are
expected to issue unguessable tokens. The one-bit failure design (below)
is the other half of the defense.

## Unsuccessful completions

The contract deliberately defines no failure vocabulary; refusals surface
as ordinary unsuccessful completions. Two standing rules:

- **`introspect` and `revoke` failures are one bit, forever.** Expiry,
  revocation, and non-recognition are indistinguishable by design — the
  indistinguishability is the anti-oracle property (it implements RFC 7662
  §2.2's inactive-token minimization). Any future change that adds detail
  to these operations' failures is a security regression, not an
  improvement. Callers therefore MUST NOT read an unsuccessful introspect
  as proof the token is invalid — provider faults land in the same bucket;
  the correct caller response is renew, with backoff.
- For `mint` and `refresh` refusals, implementations MAY use the code
  `ERR_TOKEN_REFUSED` — an open, non-portable implementation identifier in
  the sense of this repository's error-code registry (owned by none of its
  three authorities) — to mean *the presented value is not accepted and
  re-presenting it cannot succeed*: the retry-vs-surrender bit and nothing
  more. No reason taxonomy: dead credential, policy refusal, and unknown
  value stay indistinguishable to the holder. Whether a published contract
  may one day own such a code is an open registry design question, tracked
  separately.

## Realization notes

**OAuth 2.0 / OIDC token endpoints.** One endpoint realizes both `mint` and
`refresh`: two bindings to the same operation reference, each injecting its
`grant_type` constant, is the intended pattern, not a workaround. Client
authentication is a manifestation prerequisite (it rides the binding's
credential machinery, not these inputs). RFC 7009 revocation maps directly:
its 200-for-invalid-token is exactly this contract's vacuous success. RFC
7662 introspection needs one adapter obligation and it is a **conformance
trap**: `active: false` arrives as HTTP 200 and MUST map to an unsuccessful
completion — mapping it to a successful completion with information reopens
the scanning oracle and breaks the postcondition.

**Providers with expiring primary credentials.** The mint expiry bound
("known to or determinable by") does not reward ignorance: arranging not to
examine a determinable expiry is not conformance-relevant inability.

**Ambient-identity providers** (a CLI's login, a sidecar's bootstrap) carry
`mint` with the `credential` input absent. Note the expiry ceiling has no
operand on this path — the minted token is bounded only by provider policy
— so ambient realizations SHOULD document their tokens' lifetimes.

**Providers that require a credential** still declare the contract's
optional `credential` input and refuse at runtime when it is absent;
declaring `required: ["credential"]` on their own surface would make them
input-incompatible with the contract.

**Multi-part credentials** (a key id plus a secret) pack into the single
`credential` string in the provider's own documented format. Define the
packing explicitly; ad hoc packing is how secrets end up in argv and logs.

**Stateless-token providers** omit `revoke` (a no-op revoke would be a
lie); correspondence is per-operation throughout this contract, so partial
adoption is normal and honest.

## Caller guidance

- Treat `expiresAt` as authoritative and renew before it with a skew
  margin (non-normatively: tens of seconds absorbs ordinary clock drift); near end-of-life credentials mint near-dead tokens by design (the
  decay bound), so end-of-life handling is the caller's policy.
- `introspect` evaluates acceptance at processing time; it is a
  point-in-time answer, not a lease, and distributed providers are not
  required to be linearizable about very recent revocations.
- `mint` carries no idempotency marking deliberately: it is safe to retry
  (the credential is never consumed) but each retry mints a distinct live
  token, so neither `idempotent: true` nor `false` would be honest. Retry
  freely; expect distinct tokens.
- `refresh` is marked `idempotent: false` and means it: retrying middleware
  that re-presents a refresh token after an ambiguous outcome is the
  reuse-detection family-kill trigger.

## Evolution

- **Inputs stay strict.** The input schemas of these operation keys remain
  `additionalProperties: false` in every future version. Caller-directed
  narrowing (audience, scopes), if ever published, arrives as a **distinct
  operation key**, never as a field added to `mint` — an added optional
  restriction that old providers silently ignore while reporting success
  would invert the caller's security expectations, and this repository's
  unversioned operation keys make that inversion undetectable. Strictness
  makes it structurally impossible instead.
- **Bearer-only, permanently.** `accessToken`'s anti-downgrade rule (a
  token whose acceptance depends on anything beyond bearer presentation
  MUST NOT be returned) means sender-constrained tokens (DPoP- or
  mTLS-bound, per current OAuth BCP direction) are excluded rather than
  silently stripped of their constraint. Non-bearer credential kinds are a
  sibling contract's ground, never a widening of this one.
- A provider that carries an operation but refuses every value (vacuous
  carriage) is conformant and a bad citizen; the contract does not
  legislate good faith.
