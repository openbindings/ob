# Releasing ob

## Versioning

Releases follow [semantic versioning](https://semver.org/). The version is
surfaced by `ob describe` and `ob --version`, and in the `version` field of
`/.well-known/openbindings` when serving.

**Pre-1.0:** minor releases may include breaking changes. All breaking
changes are documented in `CHANGELOG.md` under a **Changed** or **Removed**
heading.

**Post-1.0:** breaking changes to the HTTP API or CLI surface require a
major version bump. Non-breaking additions (new endpoints, new optional
fields) happen in minor releases.

There is no URL path versioning (no `/v1/` prefix). The `ob start` HTTP API
is described by `/openapi.yaml` and `/asyncapi.yaml`; clients discover
capabilities at runtime from the OBI at `/.well-known/openbindings`, whose
`openbindings` field carries the spec version.

### Deprecation

Features scheduled for removal are:

1. Marked as deprecated in the `CHANGELOG.md` entry for the release that
   deprecates them.
2. Retained for at least one subsequent minor release before removal.
3. Documented with a migration path when a replacement exists.

### Supported platforms

`ob` publishes binaries for:

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)

## Changelog conventions

- The next release accumulates on `main` under a `## X.Y.Z (working draft)`
  heading.
- At release time, retitle it `## X.Y.Z — YYYY-MM-DD`, where the date is the
  tag date.
- Released entries are immutable; corrections go in a later entry.

## Prerequisites

`ob` depends on all ten `openbindings-go` modules: the core SDK plus the
nine `formats/*` sub-modules (asyncapi, connect, graphql, grpc, mcp,
openapi, operationgraph, usage). Every one must be **tagged at
the exact version `go.mod` requires before tagging ob** — the release build
sees no `go.work`, and goreleaser's `go mod tidy` hook resolves tagged
versions only. An untagged requirement fails the release build.

Verify from the repo root:

```bash
GOWORK=off go list -m all
```

If that resolves cleanly, the release build will too.

## Cutting a release

1. Retitle the changelog's working-draft heading to `## X.Y.Z — YYYY-MM-DD`
   and land it on `main`.
2. Tag `main` with an annotated tag and push it:

   ```bash
   git tag -a vX.Y.Z -m "ob vX.Y.Z"
   git push origin vX.Y.Z
   ```

3. `.github/workflows/release.yml` runs goreleaser, which publishes the
   GitHub release (archives + `checksums.txt`), updates the
   `openbindings/homebrew-tap` cask, and attests build provenance for the
   release artifacts.
4. Verify the attestation:

   ```bash
   gh attestation verify <artifact> --repo openbindings/ob
   ```

## Supply-chain posture

As of 0.2.0, releases ship sha256 checksums (`checksums.txt`) and GitHub
build-provenance attestations. Cosign signing and SBOMs are deliberately
deferred until 1.0 (decision 2026-07-19).
