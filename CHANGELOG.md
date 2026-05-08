# Changelog

All notable changes to `ob` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - Unreleased

**Spec:** implements OpenBindings 0.2.0 (working draft).

### Changed

- **CLI commands renamed** to align with the OpenBindings spec 0.2.0 "executor → invoker / invoke" rename. Pre-1.0 hard rename, no deprecated aliases.
  - `ob op exec` → `ob op invoke`. The `execute` alias was removed.
  - `ob binding exec` → `ob binding invoke`. Source file `internal/cmd/binding_exec.go` → `binding_invoke.go`.
  - SDK identifier consumers updated throughout: `OperationExecutor` → `OperationInvoker`, `BindingExecutionInput` → `BindingInvocationInput`, `ExecuteBinding(...)` → `InvokeBinding(...)`, `ExecuteOperation(...)` → `Invoke(...)`, `AddBindingExecutor` → `AddBindingInvoker`, per-format `NewExecutor` → `NewInvoker`, `ExecutionOptions` → `InvocationOptions`, `ExecuteError` → `InvocationError`, `ExecuteOutput` → `InvocationOutput`.
  - App-level types: `ExecuteOperationInput`/`Output` → `InvokeOperationInput`/`Output`; `ExecuteOBIOperation` → `InvokeOBIOperation`; `ExecuteBindingInput`/`Result` → `InvokeBindingInput`/`Result`; `DefaultExecutor` → `DefaultInvoker` (and helpers).
  - OBI files: role `openbindings.binding-executor` → `openbindings.binding-invoker` (path + URL) in `internal/app/ob.obi.json` and `internal/server/host.obi.json`; `satisfies[*].operation` value `executeBinding` → `invokeBinding`.
  - `usage.kdl`: subcommand `cmd "exec"` (under both `operation` and `binding` parents) → `cmd "invoke"`.
  - Demo command output (`ob demo`) updated to print `ob op invoke ...` examples.
  - README "Execute an operation" section → "Invoke an operation"; prose "binding executor" → "binding invoker" throughout.



- `ob serve` now listens on HTTP and HTTPS simultaneously by default. HTTP on
  `--port` (default 20290), HTTPS on port+1 (default 20291). Clients pick
  whichever matches their page protocol. `--no-tls` still available to skip
  the HTTPS listener and CA trust setup entirely (useful in CI and sandboxed
  environments).
- `ob serve`'s discovery endpoint (`/.well-known/openbindings`) now responds
  with `Content-Type: application/vnd.openbindings+json; charset=utf-8` per
  spec §7.1 / §14.2. Clients accepting `application/json` continue to receive
  the same body.
- The local HTTPS CA is installed into the system keychain on first run, not
  the user-login keychain, so Chrome and every other browser trust it without
  additional setup. Prompts once for the sudo password.
- On every `ob serve` startup, ob verifies the CA is still trusted and
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
