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
(`help/documents.md`, `help/parts.md`, `help/sources.md`, `help/service.md`,
`help/shell.md`), one page per command, which are the full `--help` text as
drafted. The draft was reviewed over eight rounds, its Sources group
rewritten for the 0.2 model in which sources returned to the document (the
fifth version of its `sources-0.2-decisions.md`, 2026-10-30), and its words
follow the specification, the published interfaces (Operation Invoker,
Binding Invoker, Source Puller, Kind Descriptor, the context document,
Delegate Manager, Software Descriptor) and the schema-comparison profile.

The draft's generator, `tools/gen_ob_surface.py` beside it, is run as

```sh
python3 tools/gen_ob_surface.py surface <ob checkout> --spec <openbindings.md>
```

With `--spec`, every `§` citation in a page or topic must name a heading of
that specification text, or the generator fails; `PROVENANCE.json` records
the hash of the text it checked against. It writes:

- `src/commands/{documents,parts,sources,service}.rs`: one clap `Command` per
  page, with its summary, its usage line and its page text, and its positional
  arguments and options as the page lists them;
- `src/commands/shell.rs`: the `ob help` and `ob completion` commands, with
  their drafted pages; they are real commands, not placeholders, and have no
  `CommandId`;
- `src/commands/help.rs`: the root help, the four help topics (`obi`,
  `documents`, `exit-codes`, `output`), and the `-h` summary of every page
  (its usage, first paragraph, commands, options, examples and exit status);
- `tests/fixtures/surface.json`: the frozen surface, every path with its
  arguments and flags;
- `tests/fixtures/command-examples.json`: the argv of every example on every
  page, tokenized with the standard shell tokenizer and never executed;
- `tests/fixtures/PROVENANCE.json`: the hashes of the pages the generation
  read.

The generated files are checked in and are the source of truth from then on;
regenerate only when deliberately adopting a changed draft, then run
`cargo fmt --all`, since the generator does not lay out its Rust as rustfmt
does. `src/commands/mod.rs` holds the helpers the generated code calls and
assembles the root; `src/syntax.rs` holds the argument grammar; `src/main.rs`
has the removed-command hints, `ob help`, `ob completion`, the `-h` cut, the
usage-error style and the one placeholder dispatch boundary. Nothing else is
generated.

## What works

- Root, group, leaf and topic help: `ob --help`, `ob source`,
  `ob help source pull`, `ob source pull --help`, `ob help obi`. `--help`
  and `ob help <command>` print the drafted page, byte for byte the same;
  `-h` and `ob help <command> -h` print its summary cut. `ob help` and
  `ob completion` have drafted pages too: `ob help help` and
  `ob completion --help` print them. Words that name no command or topic
  (`ob help primer`) are a usage error, with a close match when there is
  one. The topics are pages of `ob help` only, never commands, so they are
  never offered as a similar command.
- The 44 domain command paths, their positionals, options, short forms,
  required options, fixed choices (`-F text|json`, `--check-as`, `--supports`,
  `--job`), and the examples on their pages.
- Repeated values in order; explicit booleans such as `--idempotent=false`;
  binding add's integer `--preference`, where `1.0` is the integer 1;
  negative numbers as option values (`--input -1`); bounded Go-style duration
  syntax for `--timeout`; source pull's `--apply` alone or `--apply=<entry>`,
  repeatable.
- Argument-only rules; a command line that breaks one is a usage error
  (exit 2):
  - validate's mode selection, and `--kinds` without `--operation` or
    `--examples`;
  - compat's `--dependency`; merge's `--onto` and `--ours`/`--theirs`;
    fmt's `--canonical`;
  - source pull: `--check` without `--add`, `--apply`, `--prune` or
    `--dry-run`; `--apply` beside `--apply=<entry>`; an empty
    `--apply=`; `--add` with exactly one `<key>`;
  - `--context` on invoke, source add and source pull, and context set's
    `<value>`, take only `@FILE` or `-`, and an inline value is never echoed;
  - invoke's `--frames` with `--preflight`;
  - init's `--interface-version` is not empty;
  - delegate add and set name a job once per option (`--job`, `--add-job`,
    `--remove-job`, `--preference`, `--unset`); `--preference` is
    `JOB=NUMBER`, where JOB is `pull` or `invoke` and NUMBER a JSON number
    (finite, negative and fractional allowed, kept as typed); on add, its
    job is one `--job` requests; `--unset` is `preference.pull` or
    `preference.invoke`; set refuses contradictory changes (`--add-job` and
    `--remove-job`, or `--preference` with `--unset` or `--remove-job`, for
    one job);
  - `-F json` is refused on a document read from standard input when the
    run writes the document, and accepted with `--dry-run` or source pull's
    `--check`, which write nothing; at most one argument reads standard
    input.
- One shape for every usage error, ob's own and clap's: an `error:` line,
  indented detail lines when there are some (clap's possible values, or the
  missing arguments), and one `hint:` line with a command to run, such as
  `hint: ob source pull --help shows the syntax`, led by clap's suggestion
  when it has one (`hint: did you mean --apply? ...`). No usage block,
  "For more information" line or tip follows.
- Removed and renamed commands (`ob synthesize`, `ob source status`,
  `ob kind check`, `ob operation add`, `ob version`, ...) get the draft's
  fixed hint and exit 2, also when named to `ob help`.
- Version output and Bash, Zsh, Fish, and PowerShell completion scripts.

Payload text remains opaque. Inline JSON is not parsed or rounded by the CLI
shell; `@FILE` is not opened; URLs are not fetched; standard input is not read.
Accepting command syntax does not validate a document, schema, context object,
name, or kind.

| Exit | Current meaning |
| --- | --- |
| `0` | Help, a help topic, version, or completion output succeeded |
| `1` | Output I/O failed (a closed output pipe is handled quietly) |
| `2` | Argument parsing or argument-only validation failed, `ob help` was given words that name no command or topic, or a removed command was named |
| `3` | A domain command reached the unimplemented boundary; no operation ran |

All accepted domain requests leave stdout empty and explain the placeholder on
stderr. This also applies to `--quiet`, `--dry-run`, `-F json`, `--check`,
`--preflight` and `describe`. There are no sample success records. Exit codes
described in the operational help are future behavior, not current
capability. Help carries a conspicuous notice about that distinction.

## Deliberate limits and differences

- Clap parses; the drafted page is printed as written for `--help`, so help is
  byte-faithful to the draft. Clap's parse errors keep clap's first-line
  wording, and name values as the pages do (`<number>`, `<JOB=NUMBER>`,
  `<entry>`); ob's own value parsers (an integer, a duration) say what the
  option takes instead of echoing the value.
- An optional-value option given alone, such as `--apply`, carries a NUL
  character, which no argument can contain, so the handler can tell it from
  an explicitly empty `--apply=`.
- Completion offers the help topics and command names after `ob help`, and
  the shells after `ob completion`, in Bash and Zsh; clap_complete's Fish and
  PowerShell scripts do not complete a positional argument's choices.
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
158 help-example command lines and verifies that they collectively reach all 44
placeholders, and checks grammar, explicit false values, ordered repetitions,
exact payload spellings, negative-number values, help lengths, that
`ob help <command>` and its `-h` match `--help` and `-h`, unknown help words,
the usage-error shape, removed-command hints, completions, secret-free usage
errors, unchanged files, no connection to a supplied loopback URL, and prompt
exit while stdin remains open. Rust CI runs these on Linux, macOS, and
Windows.

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
