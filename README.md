# OpenBindings CLI (`ob`)

The OpenBindings CLI (`ob`) authors, validates, invokes, and serves OpenBindings Interface (OBI) documents. An OBI describes what a service can do (operations) and how to reach it (bindings to OpenAPI, gRPC, MCP, AsyncAPI, GraphQL, CLI specs, and more) — all in one format-agnostic document.

**Spec version:** implements OpenBindings 0.2. Run `ob describe` to see the exact range this build supports.

> **Draft status:** this branch implements the unreleased 0.2 working draft.
> The installation channels below currently resolve the latest released CLI,
> not necessarily the command surface documented on this branch. To evaluate
> the draft, clone `ob` beside `openbindings-go`, follow
> [`CONTRIBUTING.md`](CONTRIBUTING.md), and build from the local workspace.

When handing OpenBindings work to an AI agent, `ob --agent-primer` prints the
version-aligned project primer as Markdown. The same canonical primer is
published at [openbindings.com/agents](https://openbindings.com/agents) and
linked first from the site's `llms.txt`.

## Visual system

Human-facing terminal output and embedded browser pages adapt the stable
official theme from [`openbindings/design`](https://github.com/openbindings/design)
revision `ed8a409`. They also adopt the behavioral boundary from foundations
revision 1 at `3ef2505` without treating its optional visual references as a
closed style guide.

The authorization page keeps its compact local typography and composition and
now collapses transitions when reduced motion is requested. The CLI uses the
terminal expression profile: font, metrics, spacing, and motion remain
terminal-native; ANSI roles respect `NO_COLOR`. JSON, YAML, quiet output, and
files are machine contracts and never receive presentation styling. Consumer
mappings cite their Design revisions so future changes can be reviewed and
migrated explicitly.

## Install

After the 0.2 release, these install the implementation documented here:

```bash
brew install --cask openbindings/tap/ob
```

Or without Homebrew (builds from source, requires Go 1.25+):

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/openbindings/ob/main/scripts/dev-install.sh)
```

The script installs to `~/.local/bin`; override with `OB_BIN_DIR="$HOME/bin"`. Go users can also `go install github.com/openbindings/ob/cmd/ob@latest`.

## Demo

See OpenBindings in action:

```bash
ob demo
```

Starts OpenBlendings, a coffee-shop demo service exposing six operations across six protocols simultaneously (REST, Connect, gRPC, MCP, GraphQL, SSE), including one composed operation defined purely as an operation graph. One interface, six protocols, same operations. The server prints endpoints and example commands. `Ctrl+C` to stop.

## Getting Started

### 1. Author an OBI from an API spec

An OBI starts empty and is populated from sources. Given an OpenAPI spec at `./openapi.json`:

```bash
ob new interface.json                        # empty OBI (name, version, no ops)
ob source add interface.json ./openapi.json  # register the spec as a source
ob source pull interface.json                # derive operations + bindings from it
```

`ob source add` accepts a bare path (`./openapi.json`) and auto-detects the current binding revision, or an explicit binding-spec identifier (`openbindings.openapi-3.1@1:./openapi.json`). `ob source pull` reads the source, extracts operations and schemas, and writes the operations and their bindings back into the OBI.

OpenAPI support is four exact sibling binding specifications, one per source:
`openbindings.openapi-2.0@1` for Swagger 2.0,
`openbindings.openapi-3.0@1` for OpenAPI 3.0.0–3.0.4,
`openbindings.openapi-3.1@1` for OpenAPI 3.1.0–3.1.2, and
`openbindings.openapi-3.2@1` for OpenAPI 3.2.0. Auto-detection reads the
artifact's declared edition and selects that sibling. The Go SDK adapter uses
the standalone [`openapi-client/go`](https://github.com/openbindings/openapi-client/tree/main/go)
engine underneath. Synthesized bindings carry `inputTransform` expressions
that lower their operation input into the OpenAPI caller envelope
`{parameters?, body?}` before invocation.

For a one-shot derivation (no ongoing sync), `ob synthesize` collapses the three steps into one:

```bash
ob synthesize ./openapi.json -o interface.json     # derive a whole OBI in one command
```

Use `synthesize` when you just want the OBI once; use `source add` + `source pull` when you want `ob` to track the source and re-derive on drift.

### 2. Add a second source

Your service also has a CLI described by a usage spec:

```bash
ob source add interface.json ./cli.usage.kdl
ob source pull interface.json
```

The OBI now has operations from both the REST API and the CLI, each with its own binding pointing to the appropriate source.

### 3. Check for drift

Your team updates the OpenAPI spec. Check whether the OBI is out of date:

```bash
ob status interface.json
```

```
myservice 1.0.0  (openbindings 0.2.0)

Sources (2)
  openapi           openbindings.openapi-3.1@1   ./openapi.json       out of sync (synced 3d ago, ob 0.2.0)
    ↳ operations to add: deletePet
    ↳ bindings to add: deletePet.openapi
  cli               openbindings.usage@1     ./cli.usage.kdl      in sync (synced 3d ago, ob 0.2.0)

Operations (8)  — 8 source-owned

Bindings (10)  — 10 source-owned

1 source(s) out of sync. Run 'ob source pull <obi>' to update.
```

`ob status` is a dry pull: the indented `↳` lines are exactly what the next `ob source pull` would add, update, or remove. An operation you wrote by hand (no source) shows in the counts as `hand-authored` and never appears in a source's drift list.

### 4. Pull

```bash
ob source pull interface.json
```

`ob` re-reads the drifted sources, updates the operations and bindings they own, and never touches operations you hand-authored.

### 5. Invoke an operation

```bash
ob operation invoke interface.json getMenu
```

`ob` uses the sole invocable binding for `getMenu`, resolves its source,
performs the protocol interaction, and returns the result. When several
bindings remain, choose one directly with `--binding` or provide repeatable,
ordered `--select-binding` choices. The latter also reaches operations nested
inside an operation graph. You never write protocol-specific code.

When the governing binding specification exposes interpretation points, their
named answers are context, not invocation arguments: invoking without them
raises `CONTEXT_REQUIRED` naming the point, and the standing context store
satisfies it. For example, `openbindings.graphql@1` requires the exact
executable document — set it once for the target, then invoke:

```bash
ob context set https://api.example.com/graphql \
  --value '{"configuration":{"document":"query { getMenu { items { name price } } }"}}'
ob operation invoke interface.json --binding getMenu.graphql
```

Machine callers of the `/operations/invoke` WebSocket API exposed by
`ob start` supply the same object as `context.configuration`. `ob operation
prepare` accepts `--configuration` to preflight with points already known.

If operation-output validation fails, the human CLI reports the safe contract
location (one by default, all available locations with `--verbose`) without
printing the rejected value or changing the machine error record. Use
`ob binding invoke <obi> <exact-binding-key>` deliberately to inspect what the
binding returned below operation validation. Binding keys are opaque exact
selectors; `ob` does not parse protocol meaning from their spelling.

### 6. Generate typed client code

```bash
ob codegen interface.json --lang typescript -o ./src/invoker.ts
ob codegen interface.json --lang go -o ./generated/invoker.go
```

Produces typed, transport-agnostic client code: both languages emit an `OperationSignatures` namespace — Go invokes it with the free `openbindings.Invoke`, TypeScript with the invoker's `invoke` method. Either way it uses the OBI's bindings at runtime to route calls through the appropriate protocol.

### 7. Publish a clean OBI

```bash
ob source pull interface.json -o published.json --pure   # pull + strip in one step
ob purify interface.json -o published.json               # strip an existing OBI
```

Both strip `ob`'s internal metadata (`x-ob` fields), producing a spec-only OBI suitable for distribution or committing to a repo.

## Core Concepts

### OpenBindings Interface (OBI)

An OBI is a JSON document with:
- **operations** — what the interface can do (methods, events, with input/output schemas)
- **sources** — where the binding artifacts live (OpenAPI specs, proto files, usage specs, etc.)
- **bindings** — which operation is exposed through which source, and how to find it (`selector`)
- **schemas / transforms** — shared shapes and JSONata reshaping referenced by operations and bindings (optional)

OBIs are format-agnostic. The same operation can be bound to an OpenAPI endpoint, a gRPC method, an MCP tool, and a CLI command simultaneously. Authentication is deliberately absent from the document: credentials and other prerequisites are negotiated at invocation time and stored as context (see `ob context`).

### Source-Owned vs. Hand-Authored

`ob` tracks provenance via `x-ob` metadata:

- **Source-owned** (`x-ob` marker present): derived by `ob source pull` from a registered source. A later pull refreshes them when the source changes — but never your overlay: correspondence aliases, curated tags, codegen-name overrides, and output-schema overrides all survive the refresh.
- **Hand-authored** (no `x-ob`): added manually with `ob operation add` / `ob operation bind`. Pull never touches them.

`ob operation detach` converts a source-owned operation into a hand-authored one — the operation stays; `ob` just stops refreshing it on pull.

### Context

Credentials, headers, cookies, environment variables: everything an invocation needs beyond the operation input is **context**, optionally stored per target URL (`ob context set/get/list/remove`). Well-known credential fields land in the OS keychain. Structured fields live in owner-readable JSON files; they are not assumed public—headers, cookies, environment values, metadata, and configuration may also contain secrets. At invocation time `ob` resolves stored entries hierarchically—the exact target first, then up the URL path to the most specific stored prefix—and per-call context layers on top.

Context is an open object, but invokers share the well-known fields defined by the [binding-invoker interface](https://openbindings.com/interfaces/binding-invoker): `bearerToken`, `apiKey` (and scheme-scoped `apiKeys`), `basic`, `accessToken`, `headers`, `cookies`, `environment`, `metadata`. How a credential rides the wire is each binding specification's business, read from the source artifact — never from the OBI, which carries no security metadata.

When a binding needs context it doesn't have, invocation stops before any side effect with a `CONTEXT_REQUIRED` error enumerating requirement alternatives. Each requirement's type says what satisfies it:

| Requirement type | Satisfy with |
| --- | --- |
| `auth.bearer` | `ob context set <target> --bearer-token -` |
| `auth.basic` | `ob context set <target> --basic` (prompts) |
| `auth.apiKey` | `ob context set <target> --api-key -`; a requirement carrying a scheme `name` reads `apiKeys[<name>]` first, settable via `--value` |
| `auth.oauth2` | a token obtained through the flow the requirement names, stored as `bearerToken`/`accessToken` (`--bearer-token`, or the full shape via `--value`) |

Then retry the invocation. `--from-curl` imports credentials from a working curl command; `--header`, `--cookie`, `--env`, and `--meta` cover the non-credential fields. The same store backs the HTTP surface (`/contexts`, with `POST /bindings/prepare` reporting requirements proactively) and everything routed through delegates.

### Self-maintained bearer context (token-provider pinning)

Storing a long-lived secret as a target's `--bearer-token` works, but then
that durable credential rides every request. When the target's token service
corresponds to the shared `token-provider` contract (it carries
`openbindings.token-provider.mint` as an operation key or alias), pin it
instead and ob keeps the bearer token minted for you:

```
ob context set https://api.example.com \
  --token-provider https://auth.example.com \
  --token-credential -        # the durable credential, from stdin
```

On a bearer challenge for that target, when no live token is cached (or the
cached one is within 60 seconds of its recorded expiry), ob invokes the
pinned provider's mint with the stored credential as input, caches the
minted `accessToken`/`expiresAt` under the target's context, and satisfies
the challenge with the short-lived token. The durable credential never rides
an ordinary request again.

Rules the behavior lives by:

- **Only the pinned provider is ever contacted.** Correspondence tells ob
  *which* operation on the provider mints — it never selects the provider.
  A delegate, a discovered interface, or any other candidate carrying the
  mint key is never consulted for this: capability never implies use.
- A `bearerToken` you set yourself (one with no recorded expiry) is never
  overwritten; remove it if you want the pin to take over.
- If minting fails, ob warns on stderr and falls back to the ordinary
  ladder (stored context, then interactive prompt), so a misconfigured pin
  degrades loudly, not silently.

Stored context is consulted only for an alternative whose every requirement
explicitly permits reuse with `durable: true`; omission means one-shot. A
freshly resolved one-shot value applies only to that exact invocation attempt
and is never persisted or made ambient to another operation.

#### Headless / CI

By default credentials live in the OS keychain, which is unreachable in CI, containers, and sandboxes (a keychain write there fails, and `ob` tells you the escape hatch below rather than dying opaquely). Set `OB_CREDENTIALS_FILE` to an explicit path to store credentials in a JSON file instead:

```bash
export OB_CREDENTIALS_FILE="$HOME/.config/openbindings/credentials.json"
ob context set https://api.example.com --bearer-token ci-token
# storing credentials unencrypted at /home/you/.config/openbindings/credentials.json at your request
```

The file maps each target-URL key to its credential field-bag:

```json
{
  "https://api.example.com": { "bearerToken": "ci-token" }
}
```

The env var is the only selector: when it is unset the keychain is used, and a keychain failure never silently falls through to a file. Rules:

- The file is created `0600` (owner read/write only). If an existing file is group- or world-accessible, `ob` refuses to read it and tells you to `chmod 600 <path>`.
- On its first write each process prints the notice above once: the file is **unencrypted**, plaintext on disk like `~/.aws/credentials`. Protect it with filesystem permissions and treat it like any other secret file. The OS keychain remains the default for exactly this reason.
- Non-secret context (headers, cookies, environment, metadata) is unaffected — it always lives in the config files under the config directory.

## Code Generation

`ob codegen` generates typed client code from OBIs:

```bash
ob codegen interface.json --lang typescript -o invoker.ts
ob codegen interface.json --lang go -o invoker.go --package myapi
```

Both languages emit the same **operation signatures**: an `OperationSignatures` namespace with one typed `OperationSignature` per operation. Go invokes with the free verb, `invoke.Invoke(ctx, invoker, obi, myapi.OperationSignatures.GetMenu)`; TypeScript with the invoker method, `invoker.invoke(obi, OperationSignatures.getMenu)` (a method because TypeScript can put type parameters on methods and Go currently cannot). Either way, at runtime the OBI's bindings route each call through the appropriate binding invoker, so your code stays protocol-agnostic across HTTP, gRPC, or whatever the binding uses.

### Symbol names

By default the generated symbol is derived from the full operation key, so a namespaced key like `openbindings.binding-invoker.invokeBinding` emits the verbose `OperationSignatures.OpenbindingsBindingInvokerInvokeBinding`. To emit a friendlier name, set a per-operation override:

```bash
ob operation codegen-name interface.json openbindings.binding-invoker.invokeBinding invokeBinding
# -> OperationSignatures.InvokeBinding (Go) / OperationSignatures.invokeBinding (TS)
ob operation codegen-name interface.json invokeBinding --clear   # revert to the default
```

The override is stored in the operation's `x-ob.codegenName` metadata and renames the emitted symbol (and its input/output type names) only. The operation key is untouched: bindings and the wire still reference it verbatim. The override survives `ob source pull` and is stripped by `ob purify` along with the rest of `x-ob`.

You can also point `codegen` at a URL. If it's not an OBI, `ob` tries to synthesize one:

```bash
ob codegen https://api.example.com/openapi.json --lang typescript
```

## Interface Conformance

`ob conform` scaffolds operations in your OBI so it corresponds to another interface (such as one of the project's published interfaces). Correspondence is expressed through the operation key+alias namespace — an operation corresponds to a contract operation by carrying its name as the key or an alias (spec OBI-T-12).

```bash
ob conform document-store.json my-service.obi.json
```

For each operation in the contract interface:
- **Missing**: scaffolded under the contract's operation name with its schemas
- **Present but incompatible**: offers to replace the schema (declaring the correspondence with an alias when the keys differ)
- **Compatible**: reports "in sync"

Use `--yes` for CI, `--dry-run` to preview:

```bash
ob conform host.json my-service.obi.json --yes
ob conform host.json my-service.obi.json --dry-run
```

## Delegates

Delegates extend `ob` with binding-specification support — `ob`'s application of the OpenBindings [delegate pattern](https://openbindings.com/spec/delegate-pattern), and its realization of the published [delegate-manager](https://openbindings.com/interfaces/delegate-manager) interface. A delegate is any referenceable OpenBindings interface; `ob` routes work to delegates that carry the operations it needs (`invokeBinding`, `synthesizeInterface`, `inspectSource`) and support the binding specification at hand. Credentials and context are scoped and supplied by value through the same application pipeline as in-process execution; a delegate never receives direct access to `ob`'s context storage.

`ob` itself is the builtin **self-delegate** (location `"ob"`, providing OpenAPI, AsyncAPI, gRPC, Connect, MCP, GraphQL, and usage-spec in-process). A fresh environment has an empty registry: every external delegate is an explicit registration.

### Registering a delegate

`ob delegate register` (alias: `add`) accepts three location forms:

| Form | Example | Notes |
|------|---------|-------|
| `exec:` | `ob delegate register exec:thrift-ob-delegate` | Runs the named binary as a subprocess |
| Local path | `ob delegate register ./my-delegate` | Auto-prefixed with `exec:` |
| HTTP(S) | `ob delegate register https://delegate.example.com` | Runs over HTTP for execution |

```bash
ob delegate register exec:thrift-ob-delegate
ob delegate list
ob binding-specs  # should now include the binding specifications the delegate handles

ob new interface.json
ob source add interface.json thrift@1.0:./service.thrift
ob source pull interface.json
ob operation invoke interface.json getUser
```

Registration resolves the location to the delegate's OBI (via `--openbindings` for exec:, well-known discovery for http) and records a **snapshot**: the operations it carries, a content digest pinning the resolved document, and the capabilities and formats `ob` derives for routing. **Registration fails when the location cannot be resolved** — a delegate is its interface. When a delegate changes, re-register it: the snapshot refreshes, your preferences persist, and until then invocation detects the drift (digest mismatch) and asks for an explicit re-registration rather than silently running a document you never saw.

How much of an operation's cardinality crosses a delegate boundary depends on the delegate's transport: a delegate that exposes `invokeBinding` over the frame protocol (an `asyncapi` source at an http(s) URL) carries every cardinality — including server-streaming and bidirectional — while a `usage`/CLI delegate is bounded by its one-shot input (no client-streaming or bidi). `ob` prefers the frame transport when a delegate advertises both. Use `ob delegate prefer <location> <n>` to bias selection when several delegates handle the same format (scope with `--operation`/`--capability` and `--binding-spec`; `--clear` removes an entry), and `ob delegate resolve <operation>` to see which delegates carry an operation, best first.

### Building a delegate

The simplest path: scaffold the binding-invoker interface into a new OBI and implement the operations.

```bash
ob delegate requirements invoke > binding-invoker.json   # the exact operation subset ob consumes
ob conform binding-invoker.json my-delegate.obi.json --yes
```

A minimal `exec:` delegate is a CLI that:

1. Responds to `--openbindings` by printing its OBI to stdout.
2. Binds `listBindingSpecs` via an `openbindings.usage@1` source so `ob` can enumerate the binding specifications it supports at registration.
3. Implements `invokeBinding` (and, independently, `synthesizeInterface` or `inspectSource` when desired) as its OBI declares.

## Source Resolution

How a source is stored in the OBI follows from what you point at; `--resolve` overrides.

### `content` (the default for local files)

A local file artifact embeds directly in the OBI: the document is conformant (a relative path can never be, per OBI-D-05) and works from anywhere — registry, stdin, a colleague's clone. The local path is recorded in x-ob metadata as the pull path `ob source pull` refreshes from:

```bash
ob source add interface.json openbindings.openapi-3.1@1:./api.yaml          # embeds by default
ob source add interface.json 'openbindings.openapi-3.1@1:https://example.com/api.yaml?embed'  # fetch and pin a remote artifact
```

JSON/YAML formats embed as native objects. Text formats (KDL, protobuf source) embed as strings and must be self-contained (a `.proto` with imports refuses). Binary artifacts cannot be embedded.

### `location` (the default for URLs and live addresses)

Stores a URI or format-defined address (a gRPC `host:port`, an MCP endpoint) in the spec `location` field:

```bash
ob source add interface.json openbindings.openapi-3.1@1:https://example.com/api.yaml
```

To keep a local working file but publish a pointer, pair `--resolve location` with `--uri`:

```bash
ob source add interface.json openbindings.openapi-3.1@1:./api.yaml --resolve location --uri https://cdn.example.com/api.yaml
```

## Drift Detection and Pull

`ob` hashes each source artifact when it is pulled (`x-ob.contentHash`). `ob status` compares the current file against the stored hash and, for anything that changed, previews the exact add/update/remove lines the next pull would apply — without writing anything. It is a dry pull. `ob source pull` then executes that plan: it re-derives the operations and bindings each drifted source owns and leaves hand-authored objects untouched. Running pull on an already-synced OBI is a no-op.

```bash
ob status interface.json                                    # dry pull: preview what a pull would change
ob source pull interface.json                               # apply: update from drifted sources
ob source pull interface.json usage                         # pull just one source
ob source pull interface.json -o dist/interface.json --pure # publish clean
```

## Command Reference

### Set up a working area

| Command | Description |
|---------|-------------|
| `ob init` | Initialize an OpenBindings environment |
| `ob environment` (`ob env`) | Show the active OpenBindings environment |
| `ob context set/get/list/remove <url>` | Manage binding context (credentials, headers, environment) for a service |

### Browse and interact

| Command | Description |
|---------|-------------|
| `ob demo` | Start the OpenBlendings coffee-shop demo server |
| `ob resolve <url-or-host>` | Resolve an OBI from a URL or host (fetches, or synthesizes from a raw spec) |

### Author an interface

| Command | Description |
|---------|-------------|
| `ob new <obi>` | Create an empty OBI document |
| `ob synthesize [source]...` | Derive a whole OBI from sources in one shot (no ongoing sync) |
| `ob source add <obi> <source>` | Register a binding source reference |
| `ob source pull <obi> [source-keys...]` | Derive operations and bindings from registered sources |
| `ob source list/remove <obi> [key]` | List or remove source references |
| `ob status <obi>` | Report an OBI's drift against its sources (read-only) |
| `ob inspect <source>` | Inspect a binding source and list its bindable targets |
| `ob meta ...` | Manage interface-level metadata (name, version, description, …) |
| `ob diff <obi> [comparison]` | Structural comparison of two OBIs (or `--from-sources`) |
| `ob merge <target> [source]` | Selectively apply changes from one OBI into another |
| `ob conform <contract> <target-obi>` | Scaffold or update operations so the target corresponds to a contract interface |
| `ob codegen <source> --lang <lang>` | Generate a typed invoker (typescript, go) |
| `ob purify <obi>` | Strip `x-ob` vendor metadata, yielding a spec-only interface |

### Operations (`ob operation …`)

| Command | Description |
|---------|-------------|
| `ob operation invoke <obi> [operation]` | Invoke by operation (sole candidate or ordered `--select-binding`) or directly by `--binding`; named interpretation points come from standing context |
| `ob operation list <obi>` | List operations |
| `ob operation add <obi> <name>` | Add a hand-authored operation |
| `ob operation set <obi> <operation>` | Edit an operation's fields |
| `ob operation bind/unbind <obi> …` | Wire an operation to a source selector, or remove the binding |
| `ob operation alias <obi> …` | Manage an operation's correspondence aliases |
| `ob operation rename/remove/detach <obi> …` | Rename, remove, or detach operations |
| `ob operation codegen-name <obi> <operation> [name]` | Set/clear the symbol name `ob codegen` emits for an operation |
| `ob binding invoke <obi> <binding-key>` | Invoke a binding directly, below the operation layer — output exactly as the source produced it, unvalidated (also a machine lane via `--input`) |

### Delegates and formats

| Command | Description |
|---------|-------------|
| `ob delegate register/unregister <location>` | Register or unregister a delegate (aliases: `add`, `remove`) |
| `ob delegate list` | List the registry: every delegate, its operations snapshot, pin, and preferences |
| `ob delegate resolve <operation>` | Resolve an operation to the delegates that carry it, best first |
| `ob delegate resolve-binding-spec <binding-spec>` | Show which delegate ob's routing would select for a binding specification |
| `ob delegate prefer <location> [n]` | Set or clear (`--clear`) a delegate's preference, optionally per-operation |
| `ob delegate requirements <capability>` | Print the interface a delegate must correspond to for a capability |
| `ob binding-specs` | List the binding specifications this `ob` instance can handle |

### Serve

| Command | Description |
|---------|-------------|
| `ob start` | Run `ob` as a local HTTP/HTTPS/WebSocket service exposing every remotely meaningful operation over a stable API |
| `ob mcp <url>` | Serve an interface URL as an MCP server for AI agents |

### Introspection

| Command | Description |
|---------|-------------|
| `ob describe` | Show `ob`'s identity, version, and the OpenBindings spec version it supports |
| `ob --version` | Print the CLI version and its supported spec range (e.g. `ob version 0.2.0 (OpenBindings spec 0.2.0)`) |
| `ob validate <locator>` | Validate an OBI document |
| `ob compat <target> <candidate>` | Check interface conformance between two interfaces |

## Serve and integrate

`ob` is more than a CLI — it can run as a local service that exposes its remotely meaningful operations over HTTP and WebSocket (`ob start`) or over the Model Context Protocol (`ob mcp`). Other applications, browser UIs, and AI agents can use those endpoints instead of shelling out to the CLI. The three foreground process commands (`start`, `mcp`, and `demo`) remain local; the other 50 contract operations are published.

### `ob start` — local HTTP/HTTPS service

```bash
ob start                  # HTTP on http://localhost:20290
ob start --port 18000     # custom HTTP port
ob start --open           # open the authenticated workbench
ob start --verbose        # include request and invocation logs
ob start --tls            # add HTTPS on the next port; no trust changes
ob start --trust-local-ca # explicitly install that CA into system trust
ob start --token-file ~/.ob/start.token  # write the session token to a file (for scripts)
```

In an interactive terminal, `ob start` prints a clickable workbench URL that
contains the active session credential in its fragment. Fragments are not
sent in HTTP requests; the workbench consumes and removes the fragment
immediately, then retains the credential in tab-scoped session storage so
refreshes remain connected. `--open` opens that authenticated URL directly.
Non-interactive output remains stable and does not print a generated secret;
automation should supply `--token` or use `--token-file`.

HTTP over loopback is the default. `--tls` adds an HTTPS listener but does not
modify system trust; `--trust-local-ca` explicitly authorizes system-wide trust
installation, may prompt for administrator credentials, and implies `--tls`.
Local CA trust is security-sensitive because the corresponding private key can
sign certificates trusted by the machine. Review the generated files under
`~/.ob/tls` and prefer plain loopback HTTP when the browser context permits it.

Opening the server root launches the embedded, framework-neutral OpenBindings
workbench. It discovers this server's OBI, resolves or synthesizes a target
URL, explores its operations, and invokes them through the canonical Operation
Invoker capability. Protocol processing remains in `ob`; the browser does not
ship every binding-family SDK. Local session authentication and remote target
credentials are visibly separate. When a target declares recognizable context
requirements, the workbench preflights them into focused credential fields
while preserving alternatives and leaving protocol-specific context to the
advanced JSON control.

The workbench's explicit raw-binding mode resolves the canonical Binding
Invoker capability through the same environment. It requires an exact binding,
forwards a raw JSON value without operation schemas or transforms, and adds no
OpenAPI-specific browser path. Operation and raw-binding editor buffers are
kept separate.

**Authentication.** Every endpoint except `/`, `/assets/*`, `/healthz`, `/.well-known/openbindings`, `/openapi.yaml`, `/asyncapi.yaml`, `/oauth/authorize`, and `/oauth/token` requires `Authorization: Bearer <token>`. The token comes from one of:
- `--token` or the `OB_START_TOKEN` env var (static, caller-supplied)
- Auto-generated at startup otherwise (available through the interactive
  authenticated URL and lost on restart). `--token-file` writes that session
  token to a file instead of printing it, so scripts can read it; it does not
  supply a token.
- An OAuth2 access token obtained via `/oauth/authorize` + `/oauth/token` (PKCE flow)

**CORS.** By default `ob start` accepts only loopback origins.
`--allow-origin` (repeatable) adds an exact remote origin, such as
`https://app.example.com`. The same policy is enforced on WebSocket upgrades.
Private Network Access preflights
(`Access-Control-Request-Private-Network: true`) are honored only for allowed
origins.

#### HTTP endpoints

The complete API is described by [`internal/server/openapi.yaml`](https://github.com/openbindings/ob/blob/main/internal/server/openapi.yaml) (also served at `GET /openapi.yaml`). Headline endpoints:

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` | GET | Health probe (no auth) |
| `/.well-known/openbindings` | GET | OBI for `ob start` itself (no auth) |
| `/describe` | GET | Identity and version metadata |
| `/binding-specs` | GET | Binding specifications this `ob` can handle |
| `/delegates` | GET | Registered delegates |
| `/environment` | GET | Environment status |
| `/contexts` | GET | List per-host context entries |
| `/contexts/{url}` | GET / PUT / DELETE | Inspect, set, or clear one host's context |
| `/bindings/invoke` | GET (WebSocket) | Invoke a binding via the binding-invoker frame protocol |
| `/operations/invoke` | GET (WebSocket) | Invoke an operation or selected binding via the operation-invoker frame protocol |
| `/bindings/prepare` | POST | Preflight a binding's context requirements (`prepareBinding`) |
| `/operations/prepare` | POST | Preflight an inline interface operation's context requirements |
| `/interfaces` | POST | Create an empty interface document |
| `/interfaces/synthesize` | POST | Synthesize an OBI from a binding source |
| `/interfaces/status` | POST | Report an OBI's drift against its sources |
| `/interfaces/metadata`, `/interfaces/purify` | POST | Transform interface metadata or strip implementation extensions |
| `/interfaces/sources/*` | POST | Add, remove, list, or pull document sources |
| `/interfaces/operations/*` | POST | Add, set, detach, rename, remove, list, bind, unbind, or configure document operations |
| `/sources/inspect` | POST | Enumerate bindable targets in a source |
| `/interfaces/resolve` | POST | Fetch an OBI from a URL (synthesizes if served raw) |
| `/interfaces/codegen` | POST | Generate typed client code from an OBI |
| `/interfaces/conform` | POST | Scaffold operations to correspond to a contract interface |
| `/interfaces/validate` | POST | Validate an OBI |
| `/interfaces/compare` | POST | Structural diff between two OBIs |
| `/interfaces/merge` | POST | Merge one OBI into another |
| `/interfaces/compatibility` | POST | Compatibility check |
| `/delegates/*`, `/environment/initialize` | GET / POST | Inspect and manage this server's delegate environment |
| `/oauth/authorize` + `/oauth/token` | GET / POST | OAuth2 Authorization Code + PKCE |
| `/spec/{name}` | GET | Embedded spec resources |
| `/openapi.yaml`, `/asyncapi.yaml` | GET | Self-description specs (no auth) |

The API is document-oriented: authoring requests carry an interface document and return the transformed document, never a server-side file path. Requests use JSON; direct OBI responses use `application/vnd.openbindings+json`. Request bodies and individual WebSocket frames are limited to 10 MiB. The old preview aliases (`/resolve`, `/validate`, `/diff`, `/compatibility`, `/codegen`, `/conform`, and `/merge`) remain callable but are not part of the canonical OpenAPI surface.

Because an inline document has no originating directory, `/interfaces/sources/pull` rejects tracked relative source references instead of resolving them against an arbitrary server working directory. Embed the source or give it an absolute reference before pulling it through the API.

Example:

```bash
TOKEN=$(cat ~/.ob/start.token)
curl -X POST http://localhost:20290/bindings/prepare \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source": { "bindingSpec": "openbindings.openapi-3.1@1", "location": "https://api.example.com/openapi.json" },
    "selector": "#/paths/~1users/get"
  }'
```

#### Invocation (WebSocket frame protocols)

`GET /bindings/invoke` and `GET /operations/invoke` upgrade to WebSockets speaking the binding- and operation-invoker frame protocols. Each connection carries one invocation, with one shape for unary, server-streaming, client-streaming, and bidirectional operations. The operation-level `open` payload carries an inline interface plus exactly one of `operation` or `binding`; the rest of the lifecycle is identical. The complete protocols are described by [`internal/server/asyncapi.yaml`](https://github.com/openbindings/ob/blob/main/internal/server/asyncapi.yaml). That artifact is transport documentation. The unreleased first `openbindings.asyncapi@1` candidate preserves its request/reply intent abstractly; concrete invocation requires a protocol driver that implements the declared WebSocket request/reply session. A built-in driver that cannot do so refuses before establishment rather than discarding the reply stream.

1. Client opens a WebSocket to `ws://host/bindings/invoke` (or `wss://` when
   the explicitly enabled TLS listener is in use), presenting the
   session token on the upgrade request: `Authorization: Bearer <token>` for
   non-browser clients, or the
   `openbindings.frames.v1` and
   `openbindings.bearer.<unpadded-base64url-token>` WebSocket subprotocols for
   browsers (whose WebSocket API cannot set arbitrary headers). The server
   selects only `openbindings.frames.v1`; it never echoes the credential
   protocol. Tokens in URL query parameters are rejected so credentials do not
   leak into logs or copied links.
2. Client streams input frames: exactly one `{"kind": "open", "input": {source, selector, context?}}` first, then zero or more `{"kind": "input", "value": …}`, then one `{"kind": "close"}`.
3. Server streams output frames: zero or more `{"kind": "output", "value": …}`, an `{"kind": "input_closed"}` once the binding stops accepting input (later `input` frames are ignored; the invocation continues), and exactly one terminal frame — `{"kind": "complete"}` or `{"kind": "error", "error": {code, data?}}` — after which the connection closes.

Missing runtime context surfaces as a terminal `error` with code `CONTEXT_REQUIRED` whose `data` enumerates the requirements, before any output and any side effect; resolve it (typically via `/contexts`) and retry. `POST /bindings/prepare` reports the same requirements proactively when they are statically knowable.

### `ob mcp` — Model Context Protocol bridge

`ob mcp` exposes one OBI or supported raw artifact locator as an MCP server. AI agents (Claude Desktop, Cursor, etc.) connect and call the interface through native binding invocations. Ordinary OBI operations become tools whose names are the operation keys sanitized to the MCP/LLM charset (`[A-Za-z0-9_-]`, ≤64) — e.g. `openbindings.ob.describe` becomes `openbindings_ob_describe`, while `createCharge` is unchanged. That ordinary protocol-blind lane includes `openbindings.mcp@1`: its eligible tools expose only their declared application input/output contract. Untransformed bindings governed by legacy exact identifier `openbindings.mcp@1` keep their original MCP family and identity for compatibility: tools, static resources, resource templates, and prompts remain those primitives rather than being flattened into generated tools.

```bash
ob mcp https://api.example.com           # stdio transport (for Claude Desktop, Cursor)
ob mcp --transport http --port 9100 https://api.example.com    # HTTP transport
ob mcp --token "$API_TOKEN" https://api.example.com  # bearer credential for the target API
ob mcp --token-file ~/.ob/start.token http://127.0.0.1:20290   # bridge a running `ob start`
ob mcp --require-complete-coverage --coverage-report coverage.json ./openapi.yaml
```

Use `--token-file` or `OB_TOKEN` to avoid putting credentials on the command line.
For other context, pass `--context` as an inline JSON object or `@file`;
`--configuration` supplies binding-spec interpretation points, and repeatable
`--select-binding` resolves an operation with several invocable bindings.
Conflicting declarations are refused rather than silently overwritten.

To expose **several** services as one MCP server, compose them into a single aggregate OBI first (e.g. `ob merge`), then bridge that — collisions and naming are resolved deliberately in the composed contract rather than guessed at runtime.

This is also how you get an MCP server for `ob` itself: point `ob mcp` at a running `ob start` (the token-file example above). `ob start` exposes no MCP endpoint of its own; ob's served interface becomes an MCP server through the same bridge it offers for any interface, so the MCP surface stays in lockstep with the REST surface automatically.

Generated tools use one protocol-neutral projection. Explicit object-valued
operation inputs remain direct; scalar, array, unspecified, and other input
contracts use an optional, reversible `{"input": ...}` argument envelope
required by MCP's object-only tool boundary. An absent member sends no input
value, because a per-value schema never asserts cardinality. Every result
returns the complete ordered OpenBindings output
sequence as structured content and identical JSON text:
`{"outputs": [...]}`. This preserves zero, one, many, and explicit-null
outputs without inferring cardinality from the binding family. Terminal errors
retain their OpenBindings code, optional application-authored data, and any
outputs emitted before failure. Operations with no usable binding, unresolved binding
ambiguity, or broken source wiring are reported and not advertised.

When a raw source is synthesized, the bridge reports synthesis coverage.
`--coverage-report` persists the evidence and
`--require-complete-coverage` refuses startup if the source inventory was not
exhaustively and fully represented. See
[generic OBI-to-MCP projection](docs/mcp-generic-projection.md) for the exact
adapter contract.

One generated MCP tool call supplies at most one OpenBindings input value.
Client-streaming and truly bidirectional interactions that require several
caller values or interactive exchange remain the province of stream-speaking
OpenBindings consumers; terminating output streams are collected in
`outputs`, while an unbounded stream is cancelled at `--tool-timeout`.

An MCP-origin operation instead preserves the complete native result object,
protocol-native tool errors, solicited progress, and cancellation. Synthesize
the MCP source with embedded content when descriptor fidelity matters: the
pagination-exhausted listing lets the bridge preserve titles, annotations,
icons, MIME types, and prompt-argument metadata as well as behavior. See
[MCP-origin round-trip fidelity](docs/mcp-round-trip.md) for the tested profile
and intentional revision-1 boundary.

For interfaces whose operations take a whole OBI as input — much of `ob`'s own contract does — remember that tool arguments are paid for in model tokens. When the document is the vehicle for a call rather than its subject (invoking one operation of a large interface through `openbindings_ob_invokeOperation`, say), the agent can pass a slice: the operation being invoked plus everything it transitively references is itself a valid OBI, and the invocation behaves identically. Operations whose subject is the document itself (`validateInterface`, `compareInterfaces`) need the real thing.

### When to use which

- **`ob start`**: another process (a browser app, a worker, a server-side host) needs to make binding invocations and you want REST/WS access. Use it when the consumer can speak HTTP.
- **`ob mcp`**: an AI agent (Claude, Cursor) needs to discover and call your service's operations. Use it when the consumer speaks MCP.
- Both can run at once — they're independent processes.
