# Releasing ob

**Publication is currently disabled.** The maintained Rust CLI implements a
command-surface preview, with explicit placeholders for all domain operations.
Merging source into `release/0.2` does not authorize publishing a package,
creating a release tag, updating Homebrew, or replacing an installed binary.

`Cargo.toml` keeps `publish = false`. The active tree has no release workflow
or GoReleaser configuration. The historical GitHub Release workflow is disabled
at the repository level as well, so a tag on the archived Go code does not
silently resume automatic publication. Existing release tags and assets remain
unchanged.

## Before the first Rust release

1. Implement and qualify the operational profile the release will advertise,
   with actual SDK and binding-kind integrations tested as consumers.
2. Define the supported operating systems and architectures for distributed
   binaries. The current Linux/macOS/Windows CI matrix qualifies the shell on
   those runners; it is not a complete release artifact matrix.
3. Review and implement Rust binary packaging, installation/upgrade behavior,
   checksums, build provenance, and the intended distribution channels. Do not
   reactivate the Go publishing job as a Rust release mechanism.
4. Update capability documentation and the changelog, and obtain explicit
   maintainer authorization for publication. Remove the publication hold only
   as part of that reviewed release work.

## Versioning and history

Versions follow semantic versioning. Before 1.0, minor releases may introduce
breaking changes; patches are for compatible fixes. Record breaking changes
under **Changed** or **Removed** in `CHANGELOG.md`. Development accumulates
under `## X.Y.Z (working draft)` on the declared integration branch. A release
entry becomes `## X.Y.Z — YYYY-MM-DD` when its annotated release tag is cut.
Released entries remain immutable.

The build's current preview version is declared in `Cargo.toml` and printed by
`ob --version`. `ob describe` is still a placeholder and makes no compatibility
claim. CLI package versions do not by themselves assert specification support.

The [legacy preservation record](docs/legacy-go.md) links the exact prior Go
release instructions for historical reference. Those instructions are not the
current release process.
