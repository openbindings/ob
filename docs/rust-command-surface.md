# Rust command-surface migration

This phase makes the designed CLI surface executable in Rust without
implementing its domain operations. It can be reviewed independently of the
ongoing SDK and OpenAPI client API work.

## Source of the surface

The current default design is `NewNextSurfaceRoot("")` on
`codex/cli-surface-lab`, at commit
`11883f42ca92aa0f7743e499b3db644c3b0cb04a`. The migration branch starts from
`release/0.2` at `103087368e1363bae66841487a038616ab7a8f85`.
The older `NewV02SurfaceRoot` and alternative lab variants are not the selected
surface. Neither is the older Go runtime's command tree.

[The metadata snapshot](../tests/fixtures/go-surface.json) preserves the
selected tree, help, flags, argument counts, and examples.
[Provenance](../tests/fixtures/PROVENANCE.json) records source hashes and
deliberate differences. Extraction constructed the Cobra tree and inspected
metadata and argument-count validators; it did not run operational handlers.
The Rust binary uses ordinary static Rust command definitions, not this JSON.

The Go tree is preserved on `legacy/go-cli` at the integration base above;
the original surface lab is preserved on its own branch. The active tree
contains Rust and its tests, without Go runtime or release machinery. The removed agent primer
is not reintroduced: integration commit `6c8aeb0` removed it after the lab pin.

## What works

- Root, group, and leaf help, including `ob help source pull` and `-h`.
- The 73 domain command paths, their positionals, flags, short forms, required
  options, fixed choices, and intended examples.
- Repeated values in order; explicit booleans such as `--idempotent=false`;
  signed integer flags; bounded Go-style duration syntax for `--timeout`.
- Argument-only relationships such as source-pull mode selection, validation
  input/output selection, credential source syntax, and multiple stdin readers.
- Version output and Bash, Zsh, Fish, and PowerShell completion scripts.

Payload text remains opaque. Inline JSON is not parsed or rounded by the CLI
shell; `@FILE` is not opened; URLs are not fetched. Accepting command syntax
does not validate a document, schema, context value, name, or binding kind.
Document-derived completions also await operational implementation.

| Exit | Current meaning |
| --- | --- |
| `0` | Help, version, or completion output succeeded |
| `1` | Output I/O failed (a closed output pipe is handled quietly) |
| `2` | Argument parsing or argument-only validation failed |
| `3` | A domain command reached the unimplemented boundary; no operation ran |

All accepted domain requests leave stdout empty and explain the placeholder on
stderr. This also applies to `--quiet`, `--dry-run`, `-F json`, `describe`,
`start`, and credential commands. There are no sample success records. Exit
codes described in the operational help are future behavior, not current
capability. Help carries a conspicuous notice about that distinction.

## Deliberate limits and differences

- Clap provides Rust-native parsing, help layout, suggestions, and static
  completion generation. Help is not a byte-for-byte Cobra rendering.
- A repeated singleton option is an error; repeatable options append in order.
- Stdin conflicts count actual input sources. A literal `-` in a description,
  tag, or output path does not consume stdin.
- The metadata's hidden `init/set --version` flag produces the intended
  `--interface-version` guidance; the binary's version is `ob --version`.
- The lab's simulated state, sample handlers, alternate trees, fixture-derived
  name completion, and custom near-miss messages are not copied.
- Value validity, document semantics, field-edit conflicts, URL policies,
  credential storage, and runtime capability checks belong to future handlers.
  No permissive parser result establishes their policy.

The lab includes proposals about named credentials/configuration typing,
`kind check` without a role, compatibility direction, and `start` run records.
Their surface text is preserved as design material. This migration does not
ratify those behavioral policies. The placeholder `codegen --lang` choices
remain `go` and `typescript`; rewriting the CLI in Rust does not implement a
Rust client generator.

## Internal shape and next phase

`src/commands/{documents,parts,service}.rs` defines the surface using Clap's
builder API. Small helpers keep common flags consistent. `src/syntax.rs`
contains only argument grammar. `CommandId` identifies each domain leaf, and
`main.rs` contains one dispatch boundary. There is no SDK, async runtime,
HTTP client, document codec, credential store, or server dependency yet.

When an operation is ready to implement, introduce its typed request and
handler at that boundary. Keep parsing and help available without initializing
host services. The CLI/application layer will compose the Rust SDK with optional
discovery/evaluation support and binding-kind adapters. An OpenAPI adapter may
use the standalone OpenAPI client; neither that client nor application policy
belongs in SDK core. A shared Rust application library can be extracted when
implemented commands and another real consumer justify it.

The next useful implementation slice is a read-only document workflow, with
real input/output and error contracts. It should be chosen after the SDK API
work settles enough to consume it. This branch does not constrain that API or
race the ongoing SDK work. Rust CI now qualifies this shell on Linux, macOS, and Windows. Rust binary
packaging and publication remain future work; the Go release path is retired.

## Verification

`cargo test --locked` checks the Rust inventory against the independent frozen
Go metadata, executes all 127 help-example command lines, and verifies that
they collectively reach all 73 placeholders. Further tests check grammar,
explicit false values, ordered repetitions, exact payload spellings, help,
completions, credential error messages, unchanged files, no connection to a
supplied loopback URL, and prompt exit while stdin remains open.

The example argv fixture is derived with Python's standard-library shell
tokenizer. To regenerate it from the preserved metadata:

```sh
python3 scripts/extract_surface_examples.py
```

That script never executes the shell examples, redirects files, expands globs,
or feeds the example credentials anywhere. Neither the script nor Python is
required for ordinary Rust builds or tests. The checked-in fixtures are frozen
source evidence; change them only when deliberately adopting a new surface.

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full Rust check commands.
These checks qualify the shell, not SDK integration, operational parity,
cross-platform distribution, performance targets, or a Rust release.

## Canonical source cutover

The maintainer approved making this surface the canonical development CLI on
2026-10-09. `release/0.2` remains the integration branch and becomes the GitHub
default, so new readers see Rust. Go source, runtime descriptors, browser
bundles, Go qualification scripts, and Go release machinery move out of the
active tree together; their exact prior tree survives in
[the legacy record](legacy-go.md). Existing release tags and the historical
`main` branch are not rewritten.

This cutover deliberately replaces the implementation before operational
parity. The new checks qualify only the stated Rust command surface. Legacy
Go operational tests remain preserved with their implementation; their old
results are not evidence for Rust behavior. No SDK/OpenAPI changes or historical
cohort promotion are part of this cutover.
