# Changelog

All notable changes to `ob` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - Unreleased

**Spec:** implements OpenBindings 0.2.0 (working draft).

### Changed

- **Renamed the `ob serve` command to `ob start`.** The local server (HTTP +
  WebSocket) now starts with `ob start`, aligning the command with the
  `startServer` operation it realizes and reflecting that it boots ob's full
  local surface rather than serving a single artifact. The `OB_SERVE_*`
  environment variables are correspondingly renamed `OB_START_*`. No `serve`
  alias is retained (pre-1.0).
- **`ob start` no longer serves a built-in `/mcp` endpoint.** ob's served
  interface is exposed as an MCP server by pointing the bridge at a running
  server — `ob mcp http://127.0.0.1:20290` — which dogfoods the same OBI→MCP
  path ob offers for any interface. This removes a hand-maintained set of MCP
  tools that duplicated the REST handlers (and could drift from them); the MCP
  surface now tracks the REST surface automatically. The served OBI
  (`serve.obi.json`) consequently carries only `openapi` + `asyncapi` sources.
- **`ob mcp` is now single-interface.** It bridges one OBI URL (was `<url>...`);
  to expose several services as one MCP server, compose them into an aggregate
  OBI (`ob merge`) and bridge that. MCP tool names are now the operation key
  sanitized to the tool-calling charset (`[A-Za-z0-9_-]`, ≤64) — e.g.
  `openbindings.ob.describe` → `openbindings_ob_describe`, with a numeric suffix
  on the rare sanitize collision. This stays faithful to the operation identity
  (the bridge assumes no key convention) and replaces the previous
  `<interface>.<full.op.key>` scheme, which double-namespaced and used dots that
  some agent tool-calling APIs reject.
- **Migrated to the SDK's cardinality-agnostic Invocation handle** (the 0.2
  invoker model: write inputs until done, read outputs until done; one shape
  for unary, streaming, and bidirectional bindings).
  - `ob codegen --lang go` emits **operation signatures**, not a bound invoker:
    the per-operation input/output structs plus an `OperationSignatures`
    namespace (one `openbindings.OperationSignature[I, O]` per operation, built
    via `NewOperationSignature`). Callers invoke with the free verb,
    `openbindings.Invoke(ctx, invoker, obi, sig, ...)`, and drive the
    cardinality-agnostic handle (`openbindings.Single` for one-shot outputs).
    No per-operation methods, no bound invoker struct, no embedded contract: the
    interface is supplied at call time, and named schemas shared across
    operations are emitted once. `--lang typescript` still emits the typed
    invoker (one method per operation returning `Invocation<I, O>` with per-call
    `InvokerCallOpts`) pending the TypeScript SDK's signature surface: no
    Promise-returning unary wrappers, no `*Stream` twins, no per-event envelope,
    no thrown `OperationError` (terminal failures are `InvocationError` on the
    handle). Emitted headers declare the SDK range they target.
  - The app layer owns CONTEXT_REQUIRED negotiation for its binding-level
    calls: challenges raised before any output are resolved through the
    configured resolver and re-driven with merged context. The CLI's resolver
    composes the context store (under the challenge key) with interactive
    per-requirement prompts, persisting acquired credentials; `ob mcp`'s
    bearer token flows as invocation context.
  - `ob start`'s `/bindings/invoke` speaks the `openbindings.binding-invoker`
    frame protocol over WebSocket: the caller streams `open`/`input`/`close`
    frames, the server streams `output`/`input_closed` frames and exactly one
    terminal `complete`/`error` frame. One connection per invocation; every
    cardinality crosses the wire. The session token rides the upgrade request
    (`Authorization` header, or `token` query parameter for browsers). The
    legacy WS event envelope and the unary `POST /bindings/invoke` route are
    gone. `POST /bindings/prepare` implements the now-required
    `prepareBinding` preflight.
  - Delegate-backed binding invocation speaks the same frame protocol over
    WebSocket when a delegate advertises an asyncapi-bound `invokeBinding`
    with an http(s) location, exposing a normal `Invocation` handle (with
    `ERR_TRANSPORT_CLOSED` synthesized when the transport closes without a
    terminal frame); delegates advertising only a usage (CLI) binding keep
    the unary CLI realization. The `execute --as-delegate` fallback is
    removed.
  - Error codes follow the SDKs' new SCREAMING wire values (`ERR_CANCELLED`,
    `CONTEXT_REQUIRED`, ...).

- **Roles/satisfies removed** per spec 0.2.0: correspondence to a shared
  contract is the operation key+alias namespace (OBI-T-12).
  - `ob conform` scaffolds under the contract's operation name and declares
    correspondence with aliases; `--role-key` removed.
  - `ob validate` no longer performs role-conformance fetching; `--skip-roles`
    removed.
  - `ob compat` and `ob diff`/comparison pair operations by key+alias only
    (the `paired_via.satisfies` counter is gone from comparison reports).
  - ob's own OBI documents (`ob.obi.json`, `serve.obi.json`) drop their
    `roles`, `satisfies`, and `security` fields.
  - The comparison fixture corpus moved into this repo
    (`internal/app/testdata/comparison-corpus`), per the spec repo's decision
    that comparison semantics are a tool concern.

- **CLI commands renamed** to align with the OpenBindings spec 0.2.0 "executor → invoker / invoke" rename. Pre-1.0 hard rename, no deprecated aliases.
  - `ob op exec` → `ob op invoke`. The `execute` alias was removed.
  - `ob binding exec` → `ob binding invoke`. Source file `internal/cmd/binding_exec.go` → `binding_invoke.go`.
  - SDK identifier consumers updated throughout: `OperationExecutor` → `OperationInvoker`, `BindingExecutionInput` → `BindingInvocationInput`, `ExecuteBinding(...)` → `InvokeBinding(...)`, `ExecuteOperation(...)` → `Invoke(...)`, `AddBindingExecutor` → `AddBindingInvoker`, per-format `NewExecutor` → `NewInvoker`, `ExecutionOptions` → `InvocationOptions`, `ExecuteError` → `InvocationError`, `ExecuteOutput` → `InvocationOutput`.
  - App-level types: `ExecuteOperationInput`/`Output` → `InvokeOperationInput`/`Output`; `ExecuteOBIOperation` → `InvokeOBIOperation`; `ExecuteBindingInput`/`Result` → `InvokeBindingInput`/`Result`; `DefaultExecutor` → `DefaultInvoker` (and helpers).
  - OBI files: role `openbindings.binding-executor` → `openbindings.binding-invoker` (path + URL) in `internal/app/ob.obi.json` and `internal/server/host.obi.json`; `satisfies[*].operation` value `executeBinding` → `invokeBinding`.
  - `usage.kdl`: subcommand `cmd "exec"` (under both `operation` and `binding` parents) → `cmd "invoke"`.
  - Demo command output (`ob demo`) updated to print `ob op invoke ...` examples.
  - README "Execute an operation" section → "Invoke an operation"; prose "binding executor" → "binding invoker" throughout.



- `ob start` now listens on HTTP and HTTPS simultaneously by default. HTTP on
  `--port` (default 20290), HTTPS on port+1 (default 20291). Clients pick
  whichever matches their page protocol. `--no-tls` still available to skip
  the HTTPS listener and CA trust setup entirely (useful in CI and sandboxed
  environments).
- `ob start`'s discovery endpoint (`/.well-known/openbindings`) now responds
  with `Content-Type: application/vnd.openbindings+json; charset=utf-8` per
  spec §7.1 / §14.2. Clients accepting `application/json` continue to receive
  the same body.
- The local HTTPS CA is installed into the system keychain on first run, not
  the user-login keychain, so Chrome and every other browser trust it without
  additional setup. Prompts once for the sudo password.
- On every `ob start` startup, ob verifies the CA is still trusted and
  auto-recovers (purging stale entries and re-installing) if it isn't.
  Broken installs from earlier versions self-heal on next run.
- If TLS install fails (declined sudo, non-interactive terminal), ob logs a
  clear warning and continues serving HTTP-only. Users are never blocked.

### Added

- CORS middleware now responds to Chrome's Private Network Access preflight
  (`Access-Control-Allow-Private-Network: true`), fixing the
  CSP-shaped error Chrome produced when HTTP pages fetched `http://localhost`.
- `ob info` output now includes the spec version range this CLI supports,
  sourced from the Go SDK's `MinSupportedVersion` / `MaxTestedVersion`.
- `ob compat` now drives a structural OBI comparison feature
  (`internal/app/comparison.go` plus a conformance test corpus) that powers
  cross-document compatibility analysis used by registries and authoring
  workflows.

### Fixed

- `ob codegen` previously generated client code that called `c.Execute(...)`
  in Go and `this.client.execute(...)` in TypeScript. After the spec 0.2.0
  rename those SDK methods became `Invoke` / `invoke`, and the generated
  code no longer compiled against the SDKs. Generated Go now calls
  `c.Invoke(...)` (with helper renamed `execUnary` to `invokeUnary`), and
  generated TS calls `this.client.invoke(...)`.

### Notes for upgraders

- Users who ran v0.1.0's install flow have an orphaned `OpenBindings Local CA`
  entry in their login keychain. `ob serve` on v0.1.1 purges it automatically
  and reinstalls system-wide. No manual cleanup required.

## [0.1.0] - 2026-04-15

### Added

- `ob serve` — local HTTP server exposing ob's full capability surface with
  OAuth2 Authorization Code + PKCE authentication
- `ob mcp` — serve interface URLs as MCP tools for AI agents (Cursor, Claude
  Desktop, etc.) via stdio or HTTP transport
- Delegate system — pluggable binding executors (`exec:ob`, HTTP delegates)
  configured in `config.json`
- Context management — global credential and header storage for API
  authentication, accessible via CLI and `ob serve`
- SSRF protection on `/resolve` endpoint
- OpenBindings Interface discovery at `/.well-known/openbindings`
- `ob validate`, `ob diff`, `ob compat` — interface authoring and comparison
  tools
- `ob sync` — synchronize OBI documents with binding sources

### Changed

- Delegates and contexts are now stored in the environment-level `config.json`,
  replacing the previous workspace model
- `ob mcp` now takes interface URLs as positional arguments instead of reading
  from a workspace file
- OAuth tokens are validated through a single store with TTL eviction (fixes
  memory leak in earlier builds)

### Removed

- **Workspaces** — the workspace concept (`ob workspace`, `ob target`,
  `ob input`) has been removed entirely. Delegates and contexts are managed
  directly in the environment configuration.
- TUI browser — replaced by [Panjir](https://panjir.com), a dedicated web UI
  that connects to `ob serve` as an OpenBindings host
