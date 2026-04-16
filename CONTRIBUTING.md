# Contributing to ob

## Workflow

1. Branch from `main`: `git checkout -b <type>/<short-description>`.
   Types: `fix`, `feat`, `docs`, `chore`, `refactor`.
2. Commit and push.
3. `gh pr create --fill --base main`.
4. Squash-merge when CI is green (`gh pr merge --squash --auto --delete-branch`).

All changes land on `main` via squash-merged PRs. No direct commits to `main`.

## Testing

```bash
go test ./...
go build ./...
go install ./cmd/ob  # installs to $GOPATH/bin (usually ~/go/bin)
```

## Releasing

Tag from `main`: `git tag vX.Y.Z && git push origin vX.Y.Z`.
`.github/workflows/release.yml` runs goreleaser, publishes a GitHub release, and
updates the `openbindings/homebrew-tap` cask automatically.

See [CHANGELOG.md](./CHANGELOG.md) for release history. Pre-1.0, minor versions
may include breaking changes; document under **Changed** or **Removed**.

## Architecture

The CLI does not include a terminal UI (TUI). PRs adding terminal UI functionality will not be accepted.

The web-based experience is provided by [Panjir](https://panjir.com), which connects to a locally running `ob serve` instance. Interface browsing, execution, and workspace management happen through Panjir's web UI powered by the ob serve HTTP API.

## Command structure

- **`internal/app/`** — transport-agnostic domain logic. All operations live here.
- **`internal/cmd/`** — Cobra CLI bindings. Thin adapters over app functions.
- **`internal/server/`** — HTTP server infrastructure for `ob serve`.
- **`internal/mcpbridge/`** — MCP server bridge for exposing OBI operations as MCP tools.

When adding a new operation, implement it in `internal/app/`, then add both a CLI command in `internal/cmd/` and an HTTP handler in `serve_routes.go` or `serve.go`.
