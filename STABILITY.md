# Stability

This document describes the stability guarantees for `ob` and `ob serve`.

## Versioning

Releases follow [semantic versioning](https://semver.org/). The version is
tracked in `ob info` output and in the `version` field of
`/.well-known/openbindings`.

### Pre-1.0

While the version is `0.x`, minor releases may include breaking changes.
All breaking changes are documented in `CHANGELOG.md` under a **Changed** or
**Removed** heading.

### Post-1.0

After `1.0.0`, breaking changes to the HTTP API or CLI surface require a major
version bump. Non-breaking additions (new endpoints, new optional fields) happen
in minor releases.

## API Contract

The `ob serve` HTTP API is described by two artifacts:

- **`/.well-known/openbindings`** — the OpenBindings Interface document, which
  lists all operations the server supports. Clients should use this for runtime
  capability discovery.
- **`/openapi.yaml`** — the OpenAPI 3.1 specification for the HTTP API.

There is no URL path versioning (no `/v1/` prefix). The OBI document's
`openbindings` field carries the spec version. Clients that need to check
compatibility should inspect the OBI at `/.well-known/openbindings` on connect.

## Deprecation Policy

Features scheduled for removal will be:

1. Marked as deprecated in the `CHANGELOG.md` entry for the release that
   deprecates them.
2. Retained for at least one subsequent minor release before removal.
3. Documented with a migration path when a replacement exists.

## Supported Platforms

`ob` publishes binaries for:

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)
