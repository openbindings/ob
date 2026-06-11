# OpenBindings CLI (`ob`)

The OpenBindings CLI (`ob`) creates, syncs, validates, and invokes OpenBindings Interface (OBI) documents. An OBI describes what a service can do (operations) and how to access it (bindings to OpenAPI, gRPC, MCP, AsyncAPI, CLI specs, and more) -- all in one format-agnostic document.

**Spec version:** implements OpenBindings 0.2. Run `ob info` to see the exact range this build supports.

## Install

```bash
bash ob/scripts/dev-install.sh
```

Defaults to `~/.local/bin`. Override with `OB_BIN_DIR="$HOME/bin"`.

## Demo

See OpenBindings in action:

```bash
ob demo
```

Starts OpenBlendings, a coffee shop demo service exposing five operations across six protocols simultaneously (REST, Connect, gRPC, MCP, GraphQL, SSE). One interface, six protocols, same operations. The server prints endpoints and example commands. `Ctrl+C` to stop.

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
ob sync interface.json
```

The OBI now has operations from both the REST API and the CLI, each with their own bindings pointing to the appropriate source.

### 3. Check for drift

Your team updates the OpenAPI spec. Check if the OBI is out of date:

```bash
ob status interface.json
```

```
myservice v1.0.0  (openbindings 0.1.0)

Sources (2)
  openapi    openapi@3.1    ./openapi.json       drifted (synced 3d ago)
  cli        usage@2.0      ./cli.usage.kdl      current (synced 3d ago)

Operations (8) -- 8 managed, 0 hand-authored
Bindings (10) -- 10 managed, 0 hand-authored

1 source(s) drifted. Run 'ob sync interface.json' to update.
```

### 4. Sync

```bash
ob sync interface.json
```

`ob` re-reads the drifted sources, updates operations and bindings, and preserves any hand-authored operations you added manually.

### 5. Invoke an operation

```bash
ob operation invoke interface.json getMenu
```

`ob` finds the binding for `getMenu`, resolves the source (OpenAPI spec), makes the HTTP call, and returns the result. You never write protocol-specific code.

### 6. Generate a typed client

```bash
ob codegen interface.json --lang typescript -o ./src/client.ts
ob codegen interface.json --lang go -o ./generated/client.go
```

Produces a typed, transport-agnostic client with methods for each operation. The client uses the OBI's bindings at runtime to route calls through the appropriate protocol.

### 7. Publish a clean OBI

```bash
ob sync interface.json -o published.json --pure
```

Strips `ob`'s internal metadata (`x-ob` fields), producing a clean OBI suitable for distribution or committing to a repo.

## Core Concepts

### OpenBindings Interface (OBI)

An OBI is a JSON document with:
- **operations** -- what the interface can do (methods, events, with input/output schemas)
- **sources** -- where the binding artifacts live (OpenAPI specs, proto files, usage specs, etc.)
- **bindings** -- which operation is exposed through which source, and how to find it (`ref`)
- **security** -- what authentication methods each binding requires (optional)

OBIs are format-agnostic. The same operation can be bound to an OpenAPI endpoint, a gRPC method, an MCP tool, and a CLI command simultaneously.

### Managed vs. Hand-Authored

`ob` tracks which objects it manages via `x-ob` metadata:

- **Managed** (`x-ob: {}` present): created by `ob create` or `ob source add`. Sync can overwrite them when the source changes.
- **Hand-authored** (no `x-ob`): added manually. Sync never touches them.

Remove an object's `x-ob` to detach it from sync. The object stays; `ob` just stops managing it.

## Code Generation

`ob codegen` generates typed clients from OBIs:

```bash
ob codegen interface.json --lang typescript -o client.ts
ob codegen interface.json --lang go -o client.go --package myapi
```

The generated client has a typed method for each operation. At runtime, the client uses the OBI's bindings to route calls through the appropriate binding invoker -- your code calls `client.getMenu()`, the invoker handles HTTP, gRPC, or whatever protocol the binding uses.

You can also point `codegen` at a URL. If it's not an OBI, `ob` tries to synthesize one:

```bash
ob codegen https://api.example.com/openapi.json --lang typescript
```

## Role Conformance

`ob conform` scaffolds operations in your OBI to fulfill a contract interface
(such as one of the spec's published roles). Correspondence is expressed
through the operation key+alias namespace — an operation fulfills a contract
operation by carrying its name as the key or an alias (spec OBI-T-12); there
is no separate roles/satisfies layer.

```bash
ob conform openbindings.context-store.json my-service.obi.json
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

Delegates extend `ob` with binding format support. A delegate is any program that implements the `openbindings.binding-invoker` and/or `openbindings.interface-creator` roles. When `ob` encounters a binding format, it asks its registered delegates which one handles it and routes `createInterface` / `invokeBinding` calls there. Credentials and context flow through the same `ContextStore` pipeline as in-process execution.

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
ob format list   # should now include the formats the delegate handles

ob create thrift@1.0:./service.thrift -o interface.json
ob operation invoke interface.json getUser
```

For `exec:` and local-path delegates, `ob` invokes `<delegate> --openbindings` at registration time to read its OBI and probe `listFormats`, so `ob format list` immediately reflects what it handles. HTTP delegates are not probed today — they participate in invocation but you'll need to know which formats they handle. Streaming operations don't cross delegate boundaries; subscriptions only run against in-process invokers.

### Building a delegate

The simplest path: scaffold the binding-invoker role into a new OBI and implement the operations.

```bash
ob conform openbindings.binding-invoker.json my-delegate.obi.json --yes
```

A minimal `exec:` delegate is a CLI that:

1. Responds to `--openbindings` by printing its OBI to stdout.
2. Binds `listFormats` via a `usage@…` source so `ob` can enumerate supported format tokens at registration.
3. Implements `invokeBinding` (and optionally `createInterface`) per its declared role.

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

## Drift Detection and Sync

`ob` hashes each source artifact at sync time (`x-ob.contentHash`). `ob status` compares the current file against the stored hash to detect changes.

```bash
ob status interface.json      # check for drift
ob sync interface.json        # update from drifted sources
ob sync interface.json usage  # sync just one source
ob sync interface.json -o dist/interface.json --pure  # publish clean
```

## Command Reference

### Getting Started

| Command | Description |
|---------|-------------|
| `ob demo` | Start the OpenBlendings coffee shop demo |
| `ob create <sources...>` | Create an OBI from binding source artifacts |
| `ob status [obi]` | Show environment status or OBI drift report |
| `ob info` | Show ob identity and metadata |
| `ob fetch <url>` | Download an OBI from a URL or host |

### Interface Authoring

| Command | Description |
|---------|-------------|
| `ob source add <obi> <format:path>` | Register a source reference |
| `ob source list <obi>` | List source references |
| `ob source remove <obi> <key>` | Remove a source reference |
| `ob sync <obi> [sources...]` | Sync sources from x-ob references |
| `ob diff <obi>` | Show structural differences between OBI and sources |
| `ob merge <obi>` | Selectively apply changes from one OBI into another |
| `ob conflicts <obi>` | List merge conflicts between local edits and source changes |
| `ob conform <role> <target>` | Scaffold operations to satisfy a role interface |
| `ob codegen <source> --lang <lang>` | Generate a typed client (typescript, go) |

### Operations

| Command | Description |
|---------|-------------|
| `ob operation invoke <obi> <op>` | Invoke an operation via its binding |
| `ob operation list <obi>` | List operations |
| `ob operation add <obi> <name>` | Add a new operation |
| `ob operation remove <obi> <name>` | Remove an operation and its bindings |
| `ob operation rename <obi> <old> <new>` | Rename an operation |

### Serving

| Command | Description |
|---------|-------------|
| `ob serve` | Run `ob` as a local HTTP/HTTPS service exposing every CLI operation over a stable API |
| `ob mcp <url>...` | Expose one or more OBI URLs as an MCP server for AI agents |

### Environment

| Command | Description |
|---------|-------------|
| `ob init` | Initialize an OpenBindings environment |
| `ob context set <url>` | Set credentials/headers for a service |
| `ob context get <url>` | View stored context for a service |
| `ob delegate add <location>` | Register a delegate |
| `ob delegate list` | List registered delegates |
| `ob delegate remove <location>` | Remove a delegate |
| `ob format list` | List all format tokens this `ob` instance handles |

### Validation

| Command | Description |
|---------|-------------|
| `ob validate <obi>` | Validate an OBI document |
| `ob diff <a> <b>` | Compare two OBIs structurally |
| `ob compat <target> <candidate>` | Check interface compatibility |

## Serve and integrate

`ob` is more than a CLI — it can run as a local service that exposes every CLI operation over HTTP and WebSocket (`ob serve`) or over the Model Context Protocol (`ob mcp`). Other applications, browser UIs, and AI agents can use those endpoints instead of shelling out to the CLI.

### `ob serve` — local HTTP/HTTPS service

```bash
ob serve                 # http://localhost:20290 + https://localhost:20291
ob serve --port 18000    # custom port
ob serve --no-tls        # HTTP only
ob serve --token-file ~/.ob/serve.token  # supply bearer token instead of random
```

On startup `ob serve` prints the address it bound and a random bearer token. The HTTPS listener uses a local CA installed into the system keychain (first run prompts for `sudo`; subsequent runs are silent).

**Authentication.** Every endpoint except `/`, `/healthz`, `/.well-known/openbindings`, `/openapi.yaml`, `/asyncapi.yaml`, `/oauth/authorize`, and `/oauth/token` requires `Authorization: Bearer <token>`. The token comes from one of:
- `--token` or the `OB_SERVE_TOKEN` env var (static)
- `--token-file` (loaded once at startup)
- Auto-generated at startup if neither is provided (printed once, lost on restart)
- An OAuth2 access token obtained via `/oauth/authorize` + `/oauth/token` (PKCE flow)

**CORS.** `ob serve` accepts requests from any origin allowed by `--allowed-origin` (repeatable). Private Network Access preflights (`Access-Control-Request-Private-Network: true`) are honored when the origin is allowlisted, so browser apps served from `https://app.example.com` can reach `https://localhost:20291`.

#### HTTP endpoints

The complete API is described by [`internal/server/openapi.yaml`](internal/server/openapi.yaml) (also served at `GET /openapi.yaml`). Headline endpoints:

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` | GET | Health probe (no auth) |
| `/.well-known/openbindings` | GET | OBI for `ob serve` itself (no auth) |
| `/info` | GET | Identity and version metadata |
| `/formats` | GET | Format tokens this `ob` can handle |
| `/delegates` | GET | Registered delegates |
| `/status` | GET | Environment status |
| `/contexts` | GET / DELETE | Inspect or clear per-host context entries |
| `/bindings/invoke` | POST | Invoke a binding (unary; see WS variant for streaming) |
| `/interfaces/create` | POST | Create an OBI from a binding source |
| `/sources/inspect` | POST | Enumerate refs in a source |
| `/resolve` | POST | Fetch an OBI from a URL (synthesizes if served raw) |
| `/validate` | POST | Validate an OBI |
| `/diff` | POST | Structural diff between two OBIs |
| `/compatibility` | POST | Compatibility check |
| `/http/request` | POST | Generic HTTP proxy (for clients with CSP/CORS limits) |
| `/oauth/authorize` + `/oauth/token` | GET / POST | OAuth2 Authorization Code + PKCE |
| `/spec/{name}` | GET | Embedded spec resources |
| `/openapi.yaml`, `/asyncapi.yaml` | GET | Self-description specs (no auth) |

Each POST endpoint accepts and returns JSON. Example:

```bash
TOKEN=$(cat ~/.ob/serve.token)
curl -X POST https://localhost:20291/bindings/invoke \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source": { "format": "openapi@3.1", "location": "https://api.example.com/openapi.json" },
    "ref":    "#/paths/~1users/get",
    "input":  { "limit": 10 },
    "context": { "bearerToken": "user-token" }
  }'
```

#### WebSocket streaming

`POST /bindings/invoke` is the unary variant. For streaming operations (SSE, gRPC server-stream, WebSocket subscriptions), upgrade `GET /bindings/invoke` to a WebSocket. The complete WS protocol is described by [`internal/server/asyncapi.yaml`](internal/server/asyncapi.yaml).

WS protocol:
1. Client opens a WebSocket to `wss://host/bindings/invoke`.
2. Client sends one JSON message with the invocation envelope (including a `bearerToken` field — browsers can't set headers on the upgrade request).
3. Server streams back `{type: "event", output: …}` frames as outputs are produced.
4. On error: server sends `{type: "error", error: {message, code}}` and closes with status 1000.
5. On normal completion: server closes the connection.

First-message envelope:

```json
{
  "source": { "format": "asyncapi@3.0", "location": "wss://target.example.com" },
  "ref": "#/operations/subscribeOrders",
  "input": { "customerId": "abc" },
  "context": { "bearerToken": "user-token" },
  "bearerToken": "<ob-serve-token>"
}
```

### `ob mcp` — Model Context Protocol bridge

`ob mcp` exposes one or more OBI URLs as an MCP server. AI agents (Claude Desktop, Cursor, etc.) that speak MCP can connect and call the underlying service's operations as MCP tools, with `ob` translating the MCP requests into native binding invocations.

```bash
ob mcp https://api.example.com           # stdio transport (for Claude Desktop, Cursor)
ob mcp --http --port 9100 https://api.example.com    # HTTP transport
ob mcp --token "$API_TOKEN" https://api.example.com  # bearer credential for the target API
```

Multiple URLs can be passed; all of their operations are merged into a single MCP tool list. Use `--token-file` or `OB_TOKEN` to avoid putting credentials on the command line.

When invoked through MCP, an operation's input schema is exposed as the tool's input schema; the response body is returned as the tool result.

### When to use which

- **`ob serve`**: another process (a browser app, a worker, a server-side host) needs to make binding invocations and you want REST/WS access. Use it when the consumer can speak HTTP.
- **`ob mcp`**: an AI agent (Claude, Cursor) needs to discover and call your service's operations. Use it when the consumer speaks MCP.
- Both can run at once — they're independent processes.
