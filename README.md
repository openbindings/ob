# OpenBindings CLI (`ob`)

The OpenBindings CLI (`ob`) creates, syncs, validates, and invokes OpenBindings Interface (OBI) documents. An OBI describes what a service can do (operations) and how to access it (bindings to OpenAPI, gRPC, MCP, AsyncAPI, CLI specs, and more) -- all in one format-agnostic document.

**Spec version:** implements OpenBindings 0.2. Run `ob describe` to see the exact range this build supports.

## Install

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

Starts OpenBlendings, a coffee shop demo service exposing six operations across six protocols simultaneously (REST, Connect, gRPC, MCP, GraphQL, SSE), including one composed operation defined purely as an operation graph. One interface, six protocols, same operations. The server prints endpoints and example commands. `Ctrl+C` to stop.

## Getting Started

### 1. Create an OBI from an API spec

Given an OpenAPI spec at `./openapi.json`:

```bash
ob create openapi.json -o interface.json
```

This reads the spec, extracts operations and schemas, and writes an OBI. The format is auto-detected. To be explicit:

```bash
ob create openapi@3.1:./openapi.json -o interface.json
```

### 2. Add a second source

Your service also has a CLI described by a usage spec:

```bash
ob source add interface.json usage@2.0:./cli.usage.kdl
ob source pull interface.json
```

The OBI now has operations from both the REST API and the CLI, each with their own bindings pointing to the appropriate source.

### 3. Check for drift

Your team updates the OpenAPI spec. Check if the OBI is out of date:

```bash
ob status interface.json
```

```
myservice v1.0.0  (openbindings 0.2.0)

Sources (2)
  openapi    openapi@3.1    ./openapi.json       drifted (synced 3d ago)
  cli        usage@2.0      ./cli.usage.kdl      current (synced 3d ago)

Operations (8) -- 8 managed, 0 hand-authored
Bindings (10) -- 10 managed, 0 hand-authored

1 source(s) out of sync. Run 'ob source pull interface.json' to update.
```

### 4. Pull

```bash
ob source pull interface.json
```

`ob` re-reads the drifted sources, updates operations and bindings, and preserves any hand-authored operations you added manually.

### 5. Invoke an operation

```bash
ob operation invoke interface.json getMenu
```

`ob` finds the binding for `getMenu`, resolves the source (OpenAPI spec), makes the HTTP call, and returns the result. You never write protocol-specific code.

### 6. Generate a typed invoker

```bash
ob codegen interface.json --lang typescript -o ./src/invoker.ts
ob codegen interface.json --lang go -o ./generated/invoker.go
```

Produces a typed, transport-agnostic invoker with a method for each operation. The invoker uses the OBI's bindings at runtime to route calls through the appropriate protocol.

### 7. Publish a clean OBI

```bash
ob source pull interface.json -o published.json --pure
```

Strips `ob`'s internal metadata (`x-ob` fields), producing a clean OBI suitable for distribution or committing to a repo.

## Core Concepts

### OpenBindings Interface (OBI)

An OBI is a JSON document with:
- **operations** -- what the interface can do (methods, events, with input/output schemas)
- **sources** -- where the binding artifacts live (OpenAPI specs, proto files, usage specs, etc.)
- **bindings** -- which operation is exposed through which source, and how to find it (`ref`)
- **schemas / transforms** -- shared shapes and JSONata reshaping referenced by operations and bindings (optional)

OBIs are format-agnostic. The same operation can be bound to an OpenAPI endpoint, a gRPC method, an MCP tool, and a CLI command simultaneously. Authentication is deliberately absent from the document: credentials and other prerequisites are negotiated at invocation time and stored as context (see `ob context`).

### Managed vs. Hand-Authored

`ob` tracks which objects it manages via `x-ob` metadata:

- **Managed** (`x-ob: {}` present): created by `ob create` or `ob source add`. Sync can overwrite them when the source changes.
- **Hand-authored** (no `x-ob`): added manually. Sync never touches them.

Remove an object's `x-ob` to detach it from sync. The object stays; `ob` just stops managing it.

## Code Generation

`ob codegen` generates typed invokers from OBIs:

```bash
ob codegen interface.json --lang typescript -o invoker.ts
ob codegen interface.json --lang go -o invoker.go --package myapi
```

The generated invoker has a typed method for each operation. At runtime it uses the OBI's bindings to route calls through the appropriate binding invoker -- your code calls `invoker.getMenu()`, the binding invoker handles HTTP, gRPC, or whatever protocol the binding uses.

You can also point `codegen` at a URL. If it's not an OBI, `ob` tries to synthesize one:

```bash
ob codegen https://api.example.com/openapi.json --lang typescript
```

## Interface Conformance

`ob conform` scaffolds operations in your OBI so it satisfies another
interface (such as one of the spec's published interfaces). Correspondence is
expressed through the operation key+alias namespace — an operation satisfies a
contract operation by carrying its name as the key or an alias (spec
OBI-T-12).

```bash
ob conform kv-store.json my-service.obi.json
```

For each operation in the contract interface:
- **Missing**: scaffolded under the contract's operation name with its schemas
- **Present but incompatible**: offers to replace the schema (declaring the
  correspondence with an alias when the keys differ)
- **Compatible**: reports "in sync"

Use `--yes` for CI, `--dry-run` to preview:

```bash
ob conform host.json my-service.obi.json --yes
ob conform host.json my-service.obi.json --dry-run
```

## Delegates

Delegates extend `ob` with binding format support. A delegate is any program that satisfies the `binding-invoker` and/or `interface-synthesizer` interfaces. When `ob` encounters a binding format, it asks its registered delegates which one handles it and routes `synthesizeInterface` / `invokeBinding` calls there. Credentials and context flow through the same `ContextStore` pipeline as in-process execution.

`ob` itself is a delegate. A fresh `ob init` registers two default delegates: `exec:ob` (this binary, which provides OpenAPI, AsyncAPI, gRPC, Connect, MCP, GraphQL, and usage-spec) and `http://localhost:8787` (a conventional local host). Removing a default with `ob delegate remove` records it under `removedDefaultDelegates` so a later `ob init` doesn't bring it back; re-adding clears that record.

### Adding a delegate

`ob delegate add` accepts three location forms:

| Form | Example | Notes |
|------|---------|-------|
| `exec:` | `ob delegate add exec:thrift-ob-delegate` | Runs the named binary as a subprocess |
| Local path | `ob delegate add ./my-delegate` | Auto-prefixed with `exec:` |
| HTTP(S) | `ob delegate add https://delegate.example.com` | Runs over HTTP for execution |

```bash
ob delegate add exec:thrift-ob-delegate
ob delegate list
ob format        # should now include the formats the delegate handles

ob create thrift@1.0:./service.thrift -o interface.json
ob operation invoke interface.json getUser
```

For `exec:` and local-path delegates, `ob` invokes `<delegate> --openbindings` at registration time to read its OBI and probe `listFormats`, so `ob format` immediately reflects what it handles. HTTP delegates are not probed today — they participate in invocation but you'll need to know which formats they handle. Streaming operations don't cross delegate boundaries; subscriptions only run against in-process invokers.

### Building a delegate

The simplest path: scaffold the binding-invoker interface into a new OBI and implement the operations.

```bash
ob conform binding-invoker.json my-delegate.obi.json --yes
```

A minimal `exec:` delegate is a CLI that:

1. Responds to `--openbindings` by printing its OBI to stdout.
2. Binds `listFormats` via a `usage@…` source so `ob` can enumerate supported format tokens at registration.
3. Implements `invokeBinding` (and optionally `synthesizeInterface`) as its OBI declares.

## Source Resolution

When adding a source, `--resolve` controls how it appears in the OBI:

### `location` (default)

Stores a path or URI to the source artifact:

```bash
ob source add interface.json openapi@3.1:./api.yaml
```

With `--uri`, the output location differs from the input path:

```bash
ob source add interface.json openapi@3.1:./api.yaml --uri https://cdn.example.com/api.yaml
```

### `content`

Embeds the source content directly in the OBI:

```bash
ob source add interface.json usage@2.0:./cli.kdl --resolve content
```

JSON/YAML formats embed as native objects. Text formats (KDL, protobuf) embed as strings.

## Drift Detection and Pull

`ob` hashes each source artifact when it is pulled (`x-ob.contentHash`). `ob status` compares the current file against the stored hash to detect changes.

```bash
ob status interface.json             # check for drift
ob source pull interface.json        # update from drifted sources
ob source pull interface.json usage  # pull just one source
ob source pull interface.json -o dist/interface.json --pure  # publish clean
```

## Command Reference

### Getting Started

| Command | Description |
|---------|-------------|
| `ob demo` | Start the OpenBlendings coffee shop demo |
| `ob create [sources...]` | Create an OBI from binding source artifacts |
| `ob environment` | Show the active OpenBindings environment (alias: `ob env`) |
| `ob describe` | Show ob identity and metadata |
| `ob fetch <url-or-host>` | Download an OBI from a URL or host |

### Interface Authoring

| Command | Description |
|---------|-------------|
| `ob source add <obi> <source>` | Register a source reference |
| `ob source pull <obi> [source-keys...]` | Derive operations and bindings from registered sources |
| `ob status <obi>` | Report an OBI's drift against its sources (read-only) |
| `ob source list <obi>` | List source references |
| `ob source remove <obi> <key>` | Remove a source reference |
| `ob diff <obi> --from-sources` | Show structural differences between an OBI and its sources |
| `ob merge <target> [source]` | Selectively apply changes from one OBI into another |
| `ob conflicts <obi>` | List merge conflicts between local edits and source changes |
| `ob conform <contract-interface> <target-obi>` | Scaffold or update operations to fulfill a contract interface |
| `ob codegen <source> --lang <lang>` | Generate a typed invoker (typescript, go) |

### Operations

| Command | Description |
|---------|-------------|
| `ob operation invoke <obi> [operation]` | Invoke an operation via its binding |
| `ob operation list <obi>` | List operations |
| `ob operation add <obi> <name>` | Add a new operation |
| `ob operation remove <obi> <names...>` | Remove operations and their bindings |
| `ob operation rename <obi> <old> <new>` | Rename an operation and update all references |
| `ob binding invoke` | Low-level: invoke a resolved binding (delegate plumbing) |

### Serving

| Command | Description |
|---------|-------------|
| `ob start` | Run `ob` as a local HTTP/HTTPS service exposing every CLI operation over a stable API |
| `ob mcp <url>...` | Expose one or more OBI URLs as an MCP server for AI agents |

### Environment

| Command | Description |
|---------|-------------|
| `ob init` | Initialize an OpenBindings environment |
| `ob context set <url>` | Set context (credentials, headers, environment) for a service |
| `ob context get <url>` | View stored context for a service |
| `ob context list` | List all stored contexts |
| `ob context remove <url>` | Remove stored context for a service |
| `ob delegate add <location>` | Register a delegate |
| `ob delegate list` | List registered delegates |
| `ob delegate remove <location>` | Remove a delegate |
| `ob delegate resolve <format>` | Show which delegate handles a format |
| `ob format` | List all format tokens this `ob` instance handles |

### Validation

| Command | Description |
|---------|-------------|
| `ob validate <locator>` | Validate an OBI document |
| `ob diff <baseline> [comparison]` | Compare two OBIs structurally |
| `ob compat <target> <candidate>` | Check interface conformance between two interfaces |

## Serve and integrate

`ob` is more than a CLI — it can run as a local service that exposes every CLI operation over HTTP and WebSocket (`ob start`) or over the Model Context Protocol (`ob mcp`). Other applications, browser UIs, and AI agents can use those endpoints instead of shelling out to the CLI.

### `ob start` — local HTTP/HTTPS service

```bash
ob start                 # http://localhost:20290 + https://localhost:20291
ob start --port 18000    # custom port
ob start --no-tls        # HTTP only
ob start --token-file ~/.ob/start.token  # write the session token to a file (for scripts)
```

On startup `ob start` prints the address it bound and a random bearer token. The HTTPS listener uses a local CA installed into the system keychain (first run prompts for `sudo`; subsequent runs are silent).

**Authentication.** Every endpoint except `/`, `/healthz`, `/.well-known/openbindings`, `/openapi.yaml`, `/asyncapi.yaml`, `/oauth/authorize`, and `/oauth/token` requires `Authorization: Bearer <token>`. The token comes from one of:
- `--token` or the `OB_START_TOKEN` env var (static, caller-supplied)
- Auto-generated at startup otherwise (printed once, lost on restart). `--token-file` writes that session token to a file instead of stderr, so scripts can read it; it does not supply a token.
- An OAuth2 access token obtained via `/oauth/authorize` + `/oauth/token` (PKCE flow)

**CORS.** `ob start` accepts requests from any origin allowed by `--allow-origin` (repeatable). Private Network Access preflights (`Access-Control-Request-Private-Network: true`) are honored when the origin is allowlisted, so browser apps served from `https://app.example.com` can reach `https://localhost:20291`.

#### HTTP endpoints

The complete API is described by [`internal/server/openapi.yaml`](https://github.com/openbindings/ob/blob/main/internal/server/openapi.yaml) (also served at `GET /openapi.yaml`). Headline endpoints:

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` | GET | Health probe (no auth) |
| `/.well-known/openbindings` | GET | OBI for `ob start` itself (no auth) |
| `/describe` | GET | Identity and version metadata |
| `/formats` | GET | Format tokens this `ob` can handle |
| `/delegates` | GET | Registered delegates |
| `/status` | GET | Environment status |
| `/contexts` | GET | List per-host context entries |
| `/contexts/{url}` | GET / PUT / DELETE | Inspect, set, or clear one host's context |
| `/bindings/invoke` | GET (WebSocket) | Invoke a binding via the binding-invoker frame protocol |
| `/bindings/prepare` | POST | Preflight a binding's context requirements (`prepareBinding`) |
| `/interfaces/create` | POST | Create an OBI from a binding source |
| `/sources/inspect` | POST | Enumerate refs in a source |
| `/resolve` | POST | Fetch an OBI from a URL (synthesizes if served raw) |
| `/validate` | POST | Validate an OBI |
| `/diff` | POST | Structural diff between two OBIs |
| `/compatibility` | POST | Compatibility check |
| `/oauth/authorize` + `/oauth/token` | GET / POST | OAuth2 Authorization Code + PKCE |
| `/spec/{name}` | GET | Embedded spec resources |
| `/openapi.yaml`, `/asyncapi.yaml` | GET | Self-description specs (no auth) |

Each POST endpoint accepts and returns JSON. Example:

```bash
TOKEN=$(cat ~/.ob/serve.token)
curl -X POST https://localhost:20291/bindings/prepare \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source": { "format": "openapi@3.1", "location": "https://api.example.com/openapi.json" },
    "ref":    "#/paths/~1users/get"
  }'
```

#### Binding invocation (WebSocket frame protocol)

`GET /bindings/invoke` upgrades to a WebSocket speaking the `binding-invoker` frame protocol — one connection per invocation, one shape for unary, server-streaming, client-streaming, and bidirectional bindings. The complete protocol is described by [`internal/server/asyncapi.yaml`](https://github.com/openbindings/ob/blob/main/internal/server/asyncapi.yaml).

1. Client opens a WebSocket to `wss://host/bindings/invoke`, presenting the session token on the upgrade request: `Authorization: Bearer <token>`, or the `token` query parameter for browsers (which can't set headers on upgrades).
2. Client streams input frames: exactly one `{"kind": "open", "input": {source, ref, context?}}` first, then zero or more `{"kind": "input", "value": …}`, then one `{"kind": "close"}`.
3. Server streams output frames: zero or more `{"kind": "output", "value": …}`, an `{"kind": "input_closed"}` once the binding stops accepting input (later `input` frames are ignored; the invocation continues), and exactly one terminal frame — `{"kind": "complete"}` or `{"kind": "error", "error": {code, message, details?}}` — after which the connection closes.

Missing runtime context surfaces as a terminal `error` with code `CONTEXT_REQUIRED` whose `details` enumerate the requirements, before any output and any side effect; resolve them (typically via `/contexts`) and retry. `POST /bindings/prepare` reports the same requirements proactively when they are statically knowable.

### `ob mcp` — Model Context Protocol bridge

`ob mcp` exposes one OBI URL as an MCP server. AI agents (Claude Desktop, Cursor, etc.) that speak MCP connect and call the interface's operations as MCP tools, with `ob` translating the MCP requests into native binding invocations. Tool names are the interface's operation keys, sanitized to the MCP/LLM charset (`[A-Za-z0-9_-]`, ≤64) — e.g. `openbindings.ob.describe` becomes `openbindings_ob_describe`, a bare key like `createCharge` is unchanged. The bridge assumes no key convention.

```bash
ob mcp https://api.example.com           # stdio transport (for Claude Desktop, Cursor)
ob mcp --transport http --port 9100 https://api.example.com    # HTTP transport
ob mcp --token "$API_TOKEN" https://api.example.com  # bearer credential for the target API
ob mcp --token-file ~/.ob/start.token http://127.0.0.1:20290   # bridge a running `ob start`
```

Use `--token-file` or `OB_TOKEN` to avoid putting credentials on the command line.

To expose **several** services as one MCP server, compose them into a single aggregate OBI first (e.g. `ob merge`), then bridge that — collisions and naming are resolved deliberately in the composed contract rather than guessed at runtime.

This is also how you get an MCP server for `ob` itself: point `ob mcp` at a running `ob start` (last line above). `ob start` exposes no MCP endpoint of its own; ob's served interface becomes an MCP server through the same bridge it offers for any interface, so the MCP surface stays in lockstep with the REST surface automatically.

When invoked through MCP, an operation's input schema is exposed as the tool's input schema; the response body is returned as the tool result.

### When to use which

- **`ob start`**: another process (a browser app, a worker, a server-side host) needs to make binding invocations and you want REST/WS access. Use it when the consumer can speak HTTP.
- **`ob mcp`**: an AI agent (Claude, Cursor) needs to discover and call your service's operations. Use it when the consumer speaks MCP.
- Both can run at once — they're independent processes.
