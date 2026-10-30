# Rust command surface

This phase makes the designed CLI surface executable in Rust without
implementing its domain operations. Help, argument parsing, shell completion
and the argument grammar are real; every operational command exits 3 and
performs no operation. The surface can be reviewed, and felt from a terminal,
independently of the SDK and binding-kind work.

## Source of the surface

The surface is the ob CLI draft in the project's design records
(`design/ob-cli-surface-rethink-2026-10-22/surface/` in the
openbindings-project container repository): its spine (`README.md`, with the
root help, the help topics and the command tree) and its help pages
(`help/documents.md`, `help/parts.md`, `help/sources.md`, `help/service.md`),
one page per command, which are the full `--help` text as drafted. The draft
was reviewed over seven rounds, its Sources group rewritten for the 0.2 model
in which sources returned to the document (the fifth version of its
`sources-0.2-decisions.md`, 2026-10-30), and its words follow the
specification, the published interfaces (Operation Invoker, Binding Invoker,
Source Puller, Kind Descriptor, the context document, Delegate Manager,
Software Descriptor) and the schema-comparison profile.

The draft's generator, `tools/gen_ob_surface.py` beside it, writes:

- `src/commands/{documents,parts,sources,service}.rs`: one clap `Command` per
  page, with its summary, its usage line and its page text, and its positional
  arguments and options as the page lists them;
- `src/commands/help.rs`: the root help, the three help topics, and the `-h`
  summary of every page (its usage, first paragraph, commands, options,
  examples and exit status);
- `tests/fixtures/surface.json`: the frozen surface, every path with its
  arguments and flags;
- `tests/fixtures/command-examples.json`: the argv of every example on every
  page, tokenized with the standard shell tokenizer and never executed;
- `tests/fixtures/PROVENANCE.json`: the hashes of the pages the generation
  read.

The generated files are checked in and are the source of truth from then on;
regenerate only when deliberately adopting a changed draft. `src/commands/mod.rs`
holds the helpers the generated code calls and assembles the root;
`src/syntax.rs` holds the argument grammar; `src/main.rs` has the removed-command
hints, the `-h` cut and the one placeholder dispatch boundary. Nothing else is
generated.

## What works

- Root, group, leaf and topic help: `ob --help`, `ob source`,
  `ob help source pull`, `ob source pull --help`, `ob help documents`. `--help`
  and `ob help <command>` print the drafted page; `-h` prints its summary cut.
- The 44 domain command paths, their positionals, options, short forms,
  required options, fixed choices (`-F text|json`, `--check-as`, `--supports`,
  `--job`), and the examples on their pages.
- Repeated values in order; explicit booleans such as `--idempotent=false`;
  signed integer `--preference`; bounded Go-style duration syntax for
  `--timeout`; `--force` alone or `--force=<entry>`, repeatable.
- Argument-only relationships: validate's mode selection, compat's
  `--dependency`, merge's `--onto` and `--ours`/`--theirs`, fmt's `--canonical`,
  source pull's `--check` and the two `--force` forms, invoke's `--context`
  source and `--frames` with `--preflight`, context set's value source,
  delegate set's job changes, `-F json` on a document read from standard
  input, and more than one reader of standard input.
- Removed and renamed commands (`ob synthesize`, `ob source status`,
  `ob kind check`, `ob operation add`, `ob version`, ...) get the draft's
  fixed hint and exit 2.
- Version output and Bash, Zsh, Fish, and PowerShell completion scripts.

Payload text remains opaque. Inline JSON is not parsed or rounded by the CLI
shell; `@FILE` is not opened; URLs are not fetched; standard input is not read.
Accepting command syntax does not validate a document, schema, context object,
name, or kind.

| Exit | Current meaning |
| --- | --- |
| `0` | Help, version, or completion output succeeded |
| `1` | Output I/O failed (a closed output pipe is handled quietly) |
| `2` | Argument parsing or argument-only validation failed, or a removed command was named |
| `3` | A domain command reached the unimplemented boundary; no operation ran |

All accepted domain requests leave stdout empty and explain the placeholder on
stderr. This also applies to `--quiet`, `--dry-run`, `-F json`, `--check`,
`--preflight` and `describe`. There are no sample success records. Exit codes
described in the operational help are future behavior, not current
capability. Help carries a conspicuous notice about that distinction.

## Deliberate limits and differences

- Clap parses; the drafted page is printed as written for `--help`, so help is
  byte-faithful to the draft, while usage errors use clap's own wording with
  the page's usage line.
- A repeated singleton option is an error; repeatable options append in order.
- Stdin conflicts count actual input sources. A literal `-` in a description
  or output path does not consume stdin.
- Interactive prompts (a kind's source schema, a context challenge, the
  confirmation before a check that executes) are described, not implemented.
- Value validity, document semantics, URL policies, the context store, and
  runtime capability checks belong to future handlers. No permissive parser
  result establishes their policy.

## Internal shape and next phase

`CommandId` identifies each domain leaf, and `main.rs` contains one dispatch
boundary. There is no SDK, async runtime, HTTP client, document codec, context
store, or server dependency yet.

When an operation is ready to implement, introduce its typed request and
handler at that boundary. Keep parsing and help available without initializing
host services. The CLI/application layer will compose the Rust SDK with the
optional discovery and evaluation companions and binding-kind adapters. The
next useful implementation slice is a read-only document workflow (`show`,
`validate`, the `list` and `show` commands of each part), then `source add`
and `source pull` over the Kind Descriptor and Source Puller.

## Verification

`cargo test --locked` checks the Rust tree against the frozen surface, runs all
154 help-example command lines and verifies that they collectively reach all 44
placeholders, and checks grammar, explicit false values, ordered repetitions,
exact payload spellings, help lengths, removed-command hints, completions,
secret-free usage errors, unchanged files, no connection to a supplied
loopback URL, and prompt exit while stdin remains open. Rust CI runs these on
Linux, macOS, and Windows.

The checked-in fixtures are frozen source evidence; change them by
regenerating from a deliberately adopted draft revision.

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full Rust check commands.
These checks qualify the shell, not SDK integration, operational behavior,
cross-platform distribution, performance targets, or a Rust release.

## History

The first Rust surface (2026-10-09) transcribed the frozen Go surface of the
`codex/cli-surface-lab` branch, 73 commands, as the canonical development CLI.
That transcription was replaced on 2026-10-30 by the generated draft surface
described above. The Go implementation is preserved on `legacy/go-cli`; see
[the preservation record](legacy-go.md).
