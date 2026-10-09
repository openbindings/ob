# Changelog

All notable changes to `ob` will be documented in this file.

Section conventions (Added / Changed / Fixed / Removed) follow
[Keep a Changelog](https://keepachangelog.com/); versions follow
[Semantic Versioning](https://semver.org/). The next release accumulates
under a `(working draft)` heading, which gains its date when the release
is tagged.

## 0.2.0 (working draft)

This is the canonical Rust command-surface preview. It does not yet implement
OpenBindings document semantics or the operational capabilities of the archived
Go draft. The former Go 0.2 working-draft changelog is preserved on
[`legacy/go-cli`](https://github.com/openbindings/ob/blob/legacy/go-cli/CHANGELOG.md).

### Added

- A Rust `ob` binary with all 73 designed domain commands, help, argument
  parsing, version output, and Bash/Zsh/Fish/PowerShell completion generation.
  Operational commands exit 3 with an explicit unimplemented message and no
  operation performed.
- Tests covering all 127 preserved help examples and checks for argument rules,
  output/exit behavior, unchanged files, no network contact, and unread stdin.
- Required Rust CI on Linux, macOS, and Windows, including an optimized build
  and executable smoke checks.

### Changed

- Rust replaces Go as the maintained source on `release/0.2`. The complete Go
  implementation is preserved at `103087368e1363bae66841487a038616ab7a8f85` on
  `legacy/go-cli`; see [the preservation record](docs/legacy-go.md).
- Current documentation describes the Rust preview. Existing release history
  remains historical and does not establish this preview's capabilities.

### Removed

- Go runtime, modules, generated runtime descriptors, embedded browser bundles,
  legacy qualification tooling, and Go-only CI from the active branch.
- The GoReleaser configuration and automatic Go release workflow. Publication
  is disabled while the Rust implementation is a command-surface preview.

## 0.1.0 — 2026-04-15

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
