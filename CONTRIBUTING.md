# Contributing to ob

## Architecture

The CLI does not include a terminal UI (TUI). PRs adding terminal UI functionality will not be accepted.

The web-based experience is provided by [Panjir](https://panjir.com), which connects to a locally running `ob serve` instance. Interface browsing, execution, and workspace management happen through Panjir's web UI powered by the ob serve HTTP API.

## Command structure

- **`internal/app/`** — transport-agnostic domain logic. All operations live here.
- **`internal/cmd/`** — Cobra CLI bindings. Thin adapters over app functions.
- **`internal/server/`** — HTTP server infrastructure for `ob serve`.
- **`internal/mcpbridge/`** — MCP server bridge for exposing OBI operations as MCP tools.

When adding a new operation, implement it in `internal/app/`, then add both a CLI command in `internal/cmd/` and an HTTP handler in `serve_routes.go` or `serve.go`.
