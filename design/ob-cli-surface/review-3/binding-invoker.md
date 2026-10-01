# Binding Invoker

A binding invoker knows how to invoke bindings governed by specific binding specifications. Given a source (bindingSpec + location/content), a selector within that source, and a way to receive input, it makes the protocol-specific call — as the source's governing binding specification defines it — and exposes a typed I/O channel for the caller to write inputs and read outputs.

The binding specification is the semantic authority; any artifact or protocol
authority applies only to the extent that specification incorporates it. This
interface does not require every named specification to be complete. An
invoker may supply local behavior where one is silent, but that completion is
implementation-defined: it must not be represented as portable meaning or
cross-implementation conformance under the identifier. No new frame or core
field is needed to carry that distinction; it belongs in the implementation's
support documentation and claims.

This is the protocol boundary in the OpenBindings model: callers exchange operation values while the invoker interprets the binding artifact and performs the concrete interaction.

## Binding-specification support

`checkBindingSpecs` is the authoritative support surface. It accepts exact,
opaque identifier tokens and returns one verdict for each unique token, in
first-occurrence order. A `supported: true` verdict is a conformance warrant:
this implementation implements the specification that exact string denotes,
as published. The boolean is strict — there is no maybe or partial state — and
the result is deterministic for a given implementation version and free of
side effects. A consumer may base binding selection, refusal, or delegation on
this operation and no other support signal.

An implementation may use an internal pattern to compress its warranted set,
but never to extend it. In particular, a prefix or regular expression cannot
make an unpublished or future revision supported merely because its spelling
resembles an implemented identifier. Future optional verdict fields may
annotate the answer, never qualify or soften `supported`.

`listBindingSpecs` is the advisory glance: identifiers the implementation can
name for display, documentation, or pickers. Presence is a warrant, but
**absence from the list carries no information**. It cannot say no and no tool
may treat it as if it answers the support question. The one law connecting the
surfaces is `listed ⊆ supported`: every listed identifier MUST receive
`supported: true` from `checkBindingSpecs`.

This advisory/authoritative division already has a precedent in this contract:
`preflightBinding` is advisory pre-flight, while the live `CONTEXT_REQUIRED`
challenge is authoritative. The same division now applies on the
binding-specification-support axis.

## Why it's called a *binding* invoker

A binding invoker takes a `(source, selector)` directly — not an OBI document, and not a binding key. It invokes **by value**: you hand it the entire realization, and it needs no interface document to act. So, strictly, it isn't handed "a binding" in the document sense; it's handed a binding's invocable essence (the operation label and key that an OBI binding entry adds are discovery metadata the wire never needs).

The name still fits, and is the clearest available, for one reason: the `(bindingSpec, selector)` pattern only exists *because* OpenBindings defines sources and bindings. Outside the OpenBindings model you would not address a call as "a selector into a declared source," so naming it for that model is exactly right. Its peer — the one that takes an interface and a *key*, resolving an operation or a binding **by reference** — is the [operation invoker](../operation-invoker/).

## What an invoker does

When a binding invoker receives a `BindingInvocationInput`, it follows this lifecycle:

1. **Artifact interpretation.** Resolves the source artifact from `location` or `content`, per its governing binding specification's carriage rules. Loading and caching strategy are implementation details.
2. **Context consumption.** Reads the context supplied for this invocation without mutating the caller's input. The contract neither requires nor exposes a context store.
3. **Context application.** Applies credentials, headers, cookies, and other context to the interaction exactly as the governing binding specification defines.
4. **Invocation.** Interprets the selector within the source artifact, maps writes to the concrete interaction, and emits outputs through the invocation handle.
5. **Context negotiation.** If the binding cannot proceed because required context is missing, emits `CONTEXT_REQUIRED` before output or effects. A surrounding runtime may resolve the requirements and start a new attempt with augmented context.

## Context

A binding invocation usually needs more than the operation input. Credentials, headers, cookies, environment variables, session state, consent flags, custom invoker-specific values: all of it is **context**. Context is opaque to the contract and broader than auth. Credentials are one common kind, not the whole concept.

### Context carriage and lifecycle

The interface carries one opaque context object on each invocation. Where its values came from — a caller, a credential broker, a durable store, a short-lived session, or a composition of those — is outside the contract. A runtime that combines several sources decides precedence before calling the invoker and supplies the resulting object.

This separation is intentional. A stateless remote invoker, an in-process invoker with application-managed credentials, and a runtime backed by a [`document-store`](../document-store/) can all implement the same interface. None must expose storage to the invoker.

### Targets and context reuse

A `CONTEXT_REQUIRED` challenge reports a **target**: an opaque identifier for the concrete destination or context scope the invoker is about to use. A runtime may use that value to scope resolution or reuse, but key derivation, normalization, persistence, hierarchy, and cross-target sharing are runtime policy rather than this interface's semantics.

The target is an invoker assertion, not self-authenticating proof. A resolver
that does not include the invoker in its trust boundary MUST independently
validate the asserted target before releasing a reusable secret. An in-process
resolver may deliberately treat its co-located invoker as trusted; a delegate
or hosted invoker normally requires an independently derived comparison. This
contract does not pretend that a generic resolver can validate an opaque
identifier without binding-family knowledge.

When a runtime does derive storage keys from network locations, excluding userinfo and other secret material is a security requirement. Host normalization can also improve reuse across binding families. Those are implementation concerns, not a universal promise that every target is a URL or that every runtime has a store.

### Well-known context fields

Context is an opaque object, but these well-known field names provide cross-invoker interoperability:

| Field | Type | Purpose |
|---|---|---|
| `bearerToken` | `string` | Bearer token (OAuth2, JWT, etc.) |
| `apiKey` | `string` | API key (the single-key convenience) |
| `credentials` | `{ [name]: credential }` | General scheme-scoped credentials keyed by the requirement's artifact-authored `name`: bearer/API-key strings, Basic `{ username, password }`, or OAuth `{ accessToken, refreshToken?, expiresAt?, clientSecret? }` |
| `apiKeys` | `{ [name]: string }` | Historical named-API-key convenience; implementations accept it after `credentials[name]` and before the flat `apiKey` fallback |
| `basic` | `{ username, password }` | HTTP Basic credentials |
| `accessToken` / `refreshToken` / `expiresAt` | `string` | OAuth lifecycle |
| `headers` | `{ [k]: string }` | HTTP headers (per-target) |
| `cookies` | `{ [k]: string }` | HTTP cookies (per-target) |
| `environment` | `{ [k]: string }` | Environment variables (for exec-style invokers) |
| `metadata` | `{ [k]: any }` | Invoker-specific metadata (e.g., gRPC metadata) |
| `configuration` | `{ [point]: any }` | Per-invocation configuration-point values, keyed by point name (a family's decode point, for example); consulted at the first tier of each point's order |

Implementations and callers may add fields for session state, consent, or other family-specific needs. Consumers ignore fields they do not understand unless the governing binding specification says otherwise.

**Context confidentiality.** Bearer and OAuth tokens, API keys, and the password inside `basic` are always secret. Other fields are not inherently non-secret: headers, cookies, environment values, metadata, and configuration can also contain secrets according to their meaning. A runtime MUST protect values classified as secret by their requirement family, governing binding specification, or application policy; it must not expose them in diagnostics or derived keys. Structural redaction may retain non-secret names such as an API-key scheme name, but never the secret value.

### Interactive resolution

Interactive resolution is deliberately outside this contract. An in-process implementation may accept host callbacks, a remote service may drive a flow server-side, and a headless caller may use pre-provisioned values. No callback vocabulary is standardized here because function references and user-interface capabilities do not cross every implementation boundary. Whatever mechanism is used, the resulting values enter the next attempt only through `context`.

## Context negotiation (CONTEXT_REQUIRED)

A binding often needs context the caller has not supplied: credentials, an approval, a configuration value. The OBI document does not declare these. Instead the invoker discovers them at call time and asks for them, so the same mechanism works for every binding family and for prerequisites beyond auth.

When a binding cannot proceed because required context is missing, `invokeBinding` emits a terminal `error` frame with code `CONTEXT_REQUIRED` and a `ContextRequiredDetails` payload, **before** any `output` frame and **before** any observable effect of the requested operation on the target. That pre-execution guarantee is what makes resolve-and-retry safe for non-idempotent operations.

`ContextRequiredDetails` carries:

- `target`: the concrete destination or context scope asserted by the invoker. It is opaque to this contract. A runtime may compare it with independently derived information before releasing secrets; how it verifies, normalizes, or keys that value depends on the binding family and the runtime's trust model.
- `alternatives`: an **OR** of ways to satisfy the requirement. Each alternative carries `requirements`, an **AND** of `ContextRequirement`s. This OR-of-AND shape expresses real auth semantics a flat preference list cannot, e.g. "OAuth2 **OR** (apiKey **AND** clientCert)".

A `ContextRequirement` names a `type` (the resolver family) plus type-specific fields, and an optional `durable` flag:

- `durable: true`: resolved context MAY be persisted, keyed from `target`, and reused for later invocations. This is permission, not a claim that every credential or other value should be stored.
- absent or `durable: false` (the default): resolved context is for the immediate attempt only and MUST NOT be persisted. A one-shot user approval and a short-lived challenge response are not durable.

### Resolve and retry

On `CONTEXT_REQUIRED`, a runtime may:

1. Pick one `alternative` whose every `requirement` it can satisfy.
2. Resolve each requirement into context by whatever mechanism it owns.
3. Persist durable results according to its own storage policy; never persist non-durable ones.
4. Start a new `invokeBinding` attempt with the augmented context.

If it retries, the runtime bounds attempts and does not retry when resolution
made no structural change to the supplied context. A binding MAY issue a fresh
`CONTEXT_REQUIRED` after supplied context proves unusable (for example, an
expired credential) only when its governing rules can still guarantee that no
output or observable operation side effect occurred. A native failure status
alone does not prove that guarantee. When it cannot prove the safe boundary,
the attempt completes unsuccessfully instead of being replayed automatically.

### Least privilege

A `CONTEXT_REQUIRED` challenge is a **scope, not a hint**. When resolving it, the runtime provisions only the context needed to satisfy the **one selected alternative**, and never unrelated stored credentials or configuration. Any resolved value may be sensitive according to its requirement family, binding specification, or application policy. The invoker never gets raw access to a caller's store (no enumeration, no arbitrary reads); it sees only the context supplied by value for this attempt.

This matters most when the invoker is a **separate or third-party service**, such as a delegate or a hosted invoker: it receives only the context its own challenge requires, never the caller's full stored profile. Two runtime-enforced limits produce that bound together — the per-challenge field scoping here (*which fields* for a target) and the target validation under `ContextRequiredDetails` (*which target* at all). Both are the provisioning runtime's responsibility, since only it holds the store and the trust relationship; the bound is a property of what the runtime provisions, not of the invoker's good behavior.

### Requirement types

`auth.*` is the first standard family and resolves into the well-known credential context fields:

| Requirement type | Resolves to | Typical flow |
|---|---|---|
| `auth.bearer` | `credentials[name]`, otherwise `bearerToken` | Prompt for a token. |
| `auth.oauth2` | `credentials[name]`, otherwise the flat OAuth fields beginning with `accessToken` | Drive the flow named by `grantType` (`authorization_code`, `implicit`, `password`, `client_credentials`) from `authorizeUrl` / `tokenUrl` / `scopes`. |
| `auth.basic` | `credentials[name]`, otherwise `basic` (`{ username, password }`) | Prompt for username and password. |
| `auth.apiKey` | `credentials[name]`, historical `apiKeys[name]`, otherwise `apiKey` | Prompt for a key. |

A requirement MAY carry a `name` — the scheme name as the source artifact declares it — which disambiguates two requirements of the same type within one alternative (two ANDed API keys are otherwise indistinguishable) and keys the scheme-scoped credential lookup.

`config.value` is the second standard family. It carries a configuration value a binding needs but the artifact does not supply — a server variable with no default, a channel address a service generates at runtime, a base URL for a document whose only server is the implied `/`. It exists so a missing-but-**resolvable** configuration value becomes a negotiable `CONTEXT_REQUIRED` instead of an ordinary unsuccessful completion caused by source configuration that no runtime can repair. Configuration is not automatically public; its sensitivity follows its meaning. A `config.value` requirement carries:

- `point` — the binding-specification configuration point the value belongs to (`server`, `address`, a family's decode point, …).
- `path` — an RFC 6901 JSON Pointer relative to `configuration[point]`. The
  empty pointer denotes the whole point; `/variables/region` denotes a nested
  member, and `/value` denotes a member literally named `value`.
- `description` — human-readable prompt text.
- `schema` (optional) — an engine-asserted JSON Schema for the value addressed
  by `point` and `path`. It is derived from the source artifact where the
  artifact speaks, supplied by the binding-specification implementation where
  that specification pins a shape, and absent where nothing does; absence
  means the value is unconstrained. An `enum` declares the closed admissible
  set, and satisfaction validates against it; `examples` are advisory
  suggestions, and an off-list value remains valid.

It resolves into the `configuration` context field at the deterministic address
above. `durable` defaults to `false`; an invoker sets `durable: true` only when
reuse is safe. A runtime that cannot satisfy `config.value` simply cannot
select that alternative, exactly as for any other family.

Runtimes MAY define further families (`approval.user`, `account.link`, ...). An unrecognized `type` is simply unsatisfiable by a runtime that has no way to satisfy it; that alternative cannot be selected. An invoker may surface an artifact-defined scheme as an extension requirement only when it knows how the resulting context will be applied faithfully. If the invoker cannot represent or apply a prerequisite, it refuses before dispatch rather than emitting a satisfiable-looking challenge or attempting the interaction without it.

### preflightBinding

`preflightBinding` tells a binding that an invocation of this selection may follow, and lets it report context requirements it can already identify from the source and the supplied context. The result is a `ContextRequiredDetails` in the same shape a `CONTEXT_REQUIRED` challenge carries, or `null`. It is advisory: it may omit requirements, `null` is always conformant, and the live challenge remains authoritative. Invocation never requires a prior preflight. Context supplied to preflight is supplied for that call alone. Preflight never dispatches the requested operation, consumes its input, emits its outputs, or spends an approval for it; the boundary of the requested operation is the governing binding specification's, and anything else a binding does in response is that specification's to require and otherwise the implementation's. Requirements are reported only as the result; an unsuccessful completion means the binding could not answer and carries no prediction. The binding specification governs the boundary of the requested operation; what an implementation does to answer is documented with that implementation.

## Unsuccessful completion

An `error` output frame means that the current invocation did not complete
normally. That structural distinction is the portable contract. It does not
imply a universal ontology for why every present or future binding family can
complete unsuccessfully.

`InvocationError` therefore has a deliberately small shape:

- `code` identifies a reason. Only codes named by a rule of this interface or
  its operation-invoker peer or the governing binding specification have
  portable semantics. Other strings are open implementation or extension
  identifiers; an ordinary caller does not need to interpret them to observe
  unsuccessful completion. Human-readable descriptions of interface-owned
  codes belong to their documentation and local SDK presentation, not the
  interoperable record.
- `data`, when present, is portable JSON data associated with the unsuccessful
  completion. A rule defining `code` may define its structure and semantics;
  otherwise the governing binding specification may use it to preserve an
  opaque application-authored failure value. `CONTEXT_REQUIRED` uses it for a
  `ContextRequiredDetails` object. Presence is significant: an absent member
  means no portable associated value, while `"data": null` is a present JSON
  null. Statuses, headers, trailers, envelopes, raw debugging bytes, exception
  prose, stacks, and implementation evidence do not belong here unless an
  explicit governing rule makes them application data.

The record has no portable presentation string and no diagnostic escape hatch.
A binding implementation may use protocol-native facts internally to determine
the correct code, application value, and lifecycle, but those facts do not
cross this abstraction boundary. Artifact-specific clients, logs, traces, and
protocol tooling remain the places to inspect them.

The binding-invoker-owned codes are exactly those required by its own
mechanics. Their spellings are reserved and a governing binding specification
cannot redefine them with conflicting meaning:

| Code | Meaning |
|---|---|
| `CONTEXT_REQUIRED` | The binding needs the `ContextRequiredDetails` carried in `data` before dispatch. A refusal with a resolution path: like `ERR_REFUSED`, it guarantees no observable effect of the requested operation occurred. |
| `ERR_FRAME_PROTOCOL` | The caller or peer violated this interface's frame protocol. |
| `ERR_TRANSPORT_CLOSED` | The outer transport closed before a terminal frame arrived. |
| `ERR_CANCELLED` | The caller cancelled the invocation. |
| `ERR_REFUSED` | Unsuccessful completion carrying the guarantee that **no observable effect of the requested operation occurred** — the invocation was refused before dispatch. Every "refuses before dispatch" rule in a governing binding specification surfaces portably as this code (or as `CONTEXT_REQUIRED` when the refusal carries a resolution path). An implementation MUST NOT emit it unless the guarantee holds; a caller may treat it as safe to retry once the refusal's cause is addressed. |
| `ERR_EXECUTION_FAILED` | Generic unsuccessful completion when no more specific portable code is owned by a governing rule. It deliberately implies no cause, blame, retry, authentication, or availability semantics, and carries **no dispatch-state claim**: the caller MUST treat the interaction as possibly dispatched. |

The operation-invoker interface and each governing binding specification own
the additional codes they explicitly define. Implementations may use further
codes where those authorities are silent, but such codes are implementation
behavior rather than portable contract meaning. Community convergence around
an underdefined case is a reason to tighten the governing specification, not
to infer a hidden universal taxonomy. This contract assigns additional codes
no portable category, retry disposition, or protocol-status mapping. Retry and side-effect
policy belong to the caller and SDK layer. Binding specifications prove many
refusals to happen **before dispatch**, and that boundary fact is carried
portably by the code space itself: `ERR_REFUSED` (and its special case
`CONTEXT_REQUIRED`) guarantees no observable effect of the requested operation occurred, while
`ERR_EXECUTION_FAILED` makes no dispatch-state claim and MUST be treated as
possibly dispatched. Codes may encode interaction-boundary facts of this
kind — whether dispatch occurred — and never cause, blame, or protocol
category: that line is what keeps the set small and the taxonomy refusal
intact. Artifact runtimes, protocol-native
clients, logs, and traces may preserve native evidence below the OpenBindings
invocation boundary, but the error frame never carries that evidence and
ordinary application behavior never branches on it.

## What a binding invoker must NOT do

- **Understand operations.** It does not know what `getMenu` means. It invokes a binding selector within a source.
- **Select bindings.** That is the operation invoker's job. The binding invoker invokes what it is given.
- **Require a particular state architecture.** The contract supplies context by value and exposes no context store. Caches, pools, sessions, credential brokers, and persistence remain implementation choices so long as their observable behavior honors the contract.
- **Handle transforms.** Input and output transforms are applied by the operation invoker, not the binding invoker.
- **Mutate the caller's input.** Context merging and enrichment MUST operate on a copy.
- **Over-reach for context.** It receives only the context the challenge scoped and applies only what the operation requires (e.g. the security scheme the call declares). It does not read the runtime's store directly, accumulate other targets' credentials, or forward more than a delegate's own challenge requires.

## Cardinality reach depends on the binding family

The binding-invoker interface exposes a bidirectional I/O contract through `invokeBinding`. An implementation can only honor the full contract if its chosen binding family's wire can carry bidirectional message streams. This is a property of the family, not a property of the interface.

| Binding category (examples) | Unary | Server-streaming | Client-streaming | Bidirectional |
|-----------------------------|-------|------------------|------------------|---------------|
| In-process code module (`node-module`, `go-package`) | Yes | Yes | Yes | Yes |
| stdio / subprocess (`usage`) | Yes | No | No | No |
| WebSocket-based (`asyncapi-ws`) | Yes | Yes | Yes | Yes |
| HTTP/2 streaming (`grpc`, `connect`) | Yes | Yes | Yes | Yes |
| HTTP/1.1 + SSE (`openapi` with SSE response) | Yes | Yes | No | No |
| HTTP/1.1 request/response only (`openapi` plain REST) | Yes | No | No | No |

An implementation backed by an HTTP/1.1 binding can only invoke underlying bindings whose cardinality the wire can carry; it cannot proxy a bidi binding. This is fundamental, not a current implementation gap. Implementation authors who want to honor the full contract pick a binding family whose wire supports bidirectional streams. Implementation authors with a constrained binding should document which cardinalities they can carry.

## Why `invokeBinding` returns an `Invocation` handle

`invokeBinding` returns an `Invocation` handle: a typed I/O pair (write side + read side) scoped to one operation invocation, plus lifecycle controls (`close`, `cancel`, terminal state). This shape unifies every cardinality the OpenBindings spec permits (unary, server-streaming, client-streaming, bidirectional) under one signature.

When this invoker is reached remotely — as a delegate or hosted service rather than an in-process module — that same handle *is* the frame protocol: its frames are carried as the ordinary streaming values of whatever binding the invoker's OBI declares for `invokeBinding`, so no dedicated frame transport exists. Any supported streaming binding specification can carry them; `openbindings.asyncapi@1` is the project's current unreleased candidate for one such family.

### The design question

A central design question is whether operations should be modeled as request-response (one input, one output) or as bidirectional streams (zero-or-more inputs, zero-or-more outputs). REST is request-response. SSE and WebSocket receive are server-streaming. File upload protocols are client-streaming. WebSocket bidi and gRPC bidi are full bidirectional. The interface needs to support all of them.

### Alternatives considered

**1. Separate unary and streaming interfaces.** An `invokeBinding` for request-response and `subscribeBinding` for streams. Rejected: forces the caller to know which pattern an operation uses before calling it. That is protocol knowledge leaking through the abstraction. A developer switching a binding from OpenAPI to gRPC should not have to change their calling code.

**2. Single-value return for unary, stream for streaming.** Different return types per operation. Rejected: creates two code paths and the caller must know which to use. Same leak as (1).

**3. Single input + output stream.** Earlier OpenBindings SDKs shipped this: `invokeBinding(input) -> stream of outputs`. It covers unary and server-streaming cleanly. But client-streaming (caller sends N messages) and bidirectional (interleaved sends and receives) cannot be expressed. The earlier SDK acknowledged this gap and skipped gRPC client-streaming / bidi during interface synthesis.

**4. Handle with write + outputs + lifecycle (chosen).** `invokeBinding(input) -> Invocation<I, O>`. The handle exposes:

- `write(input)`: write one input message to the binding's channel (synchronous handoff to a buffer; does not claim transport dispatch).
- `close()`: graceful half-close, signal no more input.
- `cancel()`: abort the whole invocation.
- Output access: iterate `outputs` in TypeScript or call `Read(ctx)` in Go.
- `closed`: terminal-state signal.

The caller drives the handle however the operation demands. Cardinality emerges from how the caller drives it, not from a declared signature.

### How each cardinality looks under the handle

- **Unary** (REST GET, gRPC unary): caller writes one input; binding closes input from its side; output yields one value; close. Caller code: `await call.write(x); for await (const o of call.outputs) return o;`
- **Server-streaming** (SSE, gRPC server-stream): caller writes one input; binding closes input; output yields many values until done. Same caller pattern as unary; the loop just runs more.
- **Client-streaming** (file upload, gRPC client-stream): caller writes many inputs then calls `close()`; binding aggregates, produces one output. Caller owns `close()` because only the caller knows when they are done writing.
- **Bidirectional** (WebSocket, gRPC bidi): caller writes inputs in one async task; reads outputs in another; calls `close()` when done writing. Both sides flow concurrently.
- **No-input** (HTTP GET with no params, "ping"): binding closes its input side immediately; caller never touches input; just iterates outputs.
- **Fire-and-forget**: write one input; close; `closed` resolves. No iteration needed.

The caller's pattern adapts per operation, but the SDK signature is the same. Cardinality is observed at the call site, not declared in types.

### Connection pooling is a binding-specification library concern

Different protocols handle connection reuse differently. HTTP's `http.Client` pools TCP connections automatically. gRPC's `ClientConn` cache multiplexes RPCs on one HTTP/2 connection. MCP's session pool shares one JSON-RPC session across tool calls. AsyncAPI WebSocket pools share one socket across operations on the same channel. This is protocol-specific knowledge that belongs in each binding specification's implementation library. The contract stays clean: `invokeBinding(input) -> Invocation`. That library decides whether to open a new connection or reuse one and routes each invocation's I/O through the appropriate transport.
