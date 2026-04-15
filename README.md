# OpenBindings CLI (`ob`)

The OpenBindings CLI (`ob`) creates, syncs, validates, and executes OpenBindings Interface (OBI) documents. An OBI describes what a service can do (operations) and how to access it (bindings to OpenAPI, gRPC, MCP, AsyncAPI, CLI specs, and more) -- all in one format-agnostic document.

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

### 5. Execute an operation

```bash
ob operation exec interface.json getMenu
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

The generated client has a typed method for each operation. At runtime, the client uses the OBI's bindings to route calls through the appropriate binding executor -- your code calls `client.getMenu()`, the executor handles HTTP, gRPC, or whatever protocol the binding uses.

You can also point `codegen` at a URL. If it's not an OBI, `ob` tries to synthesize one:

```bash
ob codegen https://api.example.com/openapi.json --lang typescript
```

## Role Conformance

`ob conform` scaffolds operations in your OBI to satisfy a role interface:

```bash
ob conform openbindings.context-store.json my-service.obi.json
```

For each operation in the role interface:
- **Missing**: scaffolded with schemas and a `satisfies` reference
- **Present but incompatible**: offers to replace the schema
- **Compatible**: reports "in sync"

Use `--yes` for CI, `--dry-run` to preview:

```bash
ob conform host.json my-service.obi.json --yes
ob conform host.json my-service.obi.json --dry-run
```

## Delegates

Delegates extend `ob` with binding format support. A delegate is any program that implements the `openbindings.binding-executor` and/or `openbindings.interface-creator` roles. When `ob` encounters a binding format, it asks its registered delegates which one handles it and routes `createInterface` / `executeBinding` calls there. Credentials and context flow through the same `ContextStore` pipeline as in-process execution.

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
ob operation exec interface.json getUser
```

For `exec:` and local-path delegates, `ob` invokes `<delegate> --openbindings` at registration time to read its OBI and probe `listFormats`, so `ob format list` immediately reflects what it handles. HTTP delegates are not probed today — they participate in execution but you'll need to know which formats they handle. Streaming operations don't cross delegate boundaries; subscriptions only run against in-process executors.

### Building a delegate

The simplest path: scaffold the binding-executor role into a new OBI and implement the operations.

```bash
ob conform openbindings.binding-executor.json my-delegate.obi.json --yes
```

A minimal `exec:` delegate is a CLI that:

1. Responds to `--openbindings` by printing its OBI to stdout.
2. Binds `listFormats` via a `usage@…` source so `ob` can enumerate supported format tokens at registration.
3. Implements `executeBinding` (and optionally `createInterface`) per its declared role.

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
| `ob operation exec <obi> <op>` | Execute an operation via its binding |
| `ob operation list <obi>` | List operations |
| `ob operation add <obi> <name>` | Add a new operation |
| `ob operation remove <obi> <name>` | Remove an operation and its bindings |
| `ob operation rename <obi> <old> <new>` | Rename an operation |

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
