# Changelog

All notable changes to `ob` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.1] - Unreleased

**Spec:** implements OpenBindings 0.1 (unchanged).

### Changed

- `ob serve` now listens on HTTP and HTTPS simultaneously by default. HTTP on
  `--port` (default 20290), HTTPS on port+1 (default 20291). Clients pick
  whichever matches their page protocol. `--no-tls` still available to skip
  the HTTPS listener and CA trust setup entirely (useful in CI and sandboxed
  environments).
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
