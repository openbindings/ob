# Contributing to ob

## Workflow

Resolve the current integration branch from the
[project repository catalog](https://github.com/openbindings/project/blob/main/repositories.json)
and its [working loop](https://github.com/openbindings/project/blob/main/working-loop.json).
The current destination for `ob` is **`release/0.2`**, not `main`.

1. Refresh the remote and create a feature branch from the declared integration
   ref, using a clean worktree when another checkout has ongoing work.
2. Implement and run the checks for the affected implementation.
3. Commit, push, and open a PR against the declared integration ref.
4. Squash-merge after review and the required checks pass. A source merge does
   not authorize a tag, publication, or deployment.

## Rust command surface

The Rust toolchain is pinned in `rust-toolchain.toml`. No Go toolchain or
neighboring SDK checkout is required for these commands:

```sh
cargo fmt --all -- --check
cargo test --locked
cargo clippy --locked --all-targets -- -D warnings
cargo build --locked --release
```

One process test opens an ephemeral loopback listener to verify that a supplied
URL is not contacted. Run the tests in an environment that permits loopback
sockets; the test must not silently skip that assertion.

The Rust command tree lives in `src/commands/`; argument-only rules live in
`src/syntax.rs`. `src/main.rs` has a single placeholder dispatch boundary.
The frozen Go surface is test evidence, not a runtime schema or an authority
over future design. See [the migration notes](docs/rust-command-surface.md).

For this phase, keep operational commands explicit placeholders. Do not make a
stub print a successful-looking result, read a credential store, load documents,
or start a service. Implementation phases will replace placeholders one job at
a time, with real consumer tests and matching capability documentation.

CI runs formatting, Clippy, tests, release builds, and release-binary smoke
checks on Linux, macOS, and Windows. The required `ci` result fails if any
mandatory host job fails, is cancelled, or is skipped. CI needs only this
repository and its locked Cargo dependencies; it does not check out Go SDKs or
trigger a project-wide integration run.

The Go implementation, its tests, and its former release process are preserved
on `legacy/go-cli`. They no longer define this branch's checks. See
[the preservation record](docs/legacy-go.md) when consulting or recovering that
code. Do not infer operational parity from the Rust shell's passing tests.

## Architecture and releases

The CLI does not include a terminal UI (TUI). Terminal commands remain native
CLI experiences. Domain behavior will use the maintained SDK, optional
companions, and binding-kind adapters as appropriate; protocol engines and
application policy do not belong in the SDK core.

[RELEASING.md](RELEASING.md) records the publication hold and the work required
before a Rust release. The Cargo package remains unpublished (`publish = false`),
and the Go release workflow and GoReleaser configuration are retired.
