# Iteration 0.2-01: truthful core authoring

Spec pin: `../spec` at `ccfe0b6` (`release/0.2`). This is a facade-only
iteration. The inherited preview advertises old source, binding, and
dependency fields and emits an invalid sample labeled 0.2.0. The hypothesis
is that a clean command declaration with exact `kind` and arbitrary JSON
`content` will close the spec gap without making common authoring harder.

## Task cards written before the facade change

1. Create an empty 0.2 document with a human-readable name and a chosen
   output file.
2. Record a source named `api` under a private kind `my-team.http@1`, with an
   object as its exact content.
3. Record another source whose content is explicitly JSON `null`, and one
   whose content member is absent.
4. Declare one operation and two separately named bindings through the same
   source, each with its own content and author preference.
5. Declare a named consumption point for an operation that accepts either of
   two exact, unfamiliar kinds.
6. Inspect the full stored source, binding, and dependency values.
7. Check a document with an unsupported source kind for Core conformance,
   then separately ask whether the installation can invoke that kind.
8. Check a document with an unknown non-`x-` source field and distinguish its
   conformance failure from an unsupported-version refusal.
9. Replace a less common core field and an `x-` extension without a dedicated
   flag, using a lossless escape hatch.

## Two capability-equivalent command candidates

| Task | Object-first | Action-first |
| --- | --- | --- |
| Add source | `ob source add <obi> <key> --kind K [--content JSON\|@file\|-]` | `ob add source <obi> <key> --kind K [--content JSON\|@file\|-]` |
| Add binding | `ob binding add <obi> <key> --operation OP --source SRC [--content ...]` | `ob add binding <obi> <key> --operation OP --source SRC [--content ...]` |
| Add dependency | `ob dependency add <obi> <key> --operation OP [--kind K]...` | `ob add dependency <obi> <key> --operation OP [--kind K]...` |
| Add operation | `ob operation add <obi> <key> [schema flags]` | `ob add operation <obi> <key> [schema flags]` |

Both candidates have one canonical spelling per action. They share the
same flags, parser checks, help facts, output contract, and task coverage.
Other read commands stay under object nouns. The action candidate tests a
real hierarchy change, not an alias. Neither candidate assumes that an
installed tool supports or can fetch a private kind.

## Contract

- The `kind` flag is an exact non-empty string. The CLI does not parse a
  namespace, version, or URL from it. Source authoring never requires a
  support lookup.
- `--content` accepts one JSON value inline, `@file`, or `-` from stdin.
  Omission omits the member; explicit `null` keeps it present. The inert
  facade parses inline JSON only and never reads a file or stdin.
- Dependency `--kind` is repeatable, exact, unordered any-of. Duplicates and
  empty values are usage errors. No flag means no constraint.
- Binding `--preference` is a signed integer in the interoperable range,
  with higher expressing stronger author preference. It does not select a
  binding or promise a runtime algorithm.
- Sample output is illustrative and emits canonical JSON documents for
  authoring tasks. Samples never read locators or write output files. Every
  sample OBI is checked against the pinned 0.2 schema and applicable local
  rules independently of the old Go SDK.
- Successful samples exit 0. A correctly shaped ordinary invocation reaches
  an inert placeholder and exits 2, visibly saying that no operation ran.
  Invalid paths/flags/arguments also exit 2 but give a specific usage error.
  The proposed real `validate` contract has distinct conformant,
  non-conformant, undetermined, and version-refused result vocabulary;
  the preview does not pretend to validate a supplied locator.

The next review compares first-command choice and recovery on task cards
that do not mention either candidate's path. No score is awarded while a
candidate's examples or samples conflict with the pinned spec.

## Pilot result and decision

The implementation uses a new Cobra tree, independent of production `NewRoot`.
The `object` and `action` variants expose the same 26 inert leaves. Every
advertised help example reaches a leaf and exits 2 with `no operation ran`.
Thirteen illustrative OBIs per variant passed the pinned 0.2 structural
schema and the local relationship checks applicable to those fixtures.
The older sibling Go SDK still embeds the prior source/dependency shape, so
its verdict was deliberately excluded from this 0.2 gate. The `action`
variant's schema-value parser defect was found and fixed during gating.

Four fresh, independent internal agent trials used the nine task cards above:
two agents saw only the object candidate and two separate agents saw only
the action candidate. Before help, they recorded an unrevised full command.
Only then did they use CLI help and inert calls. These are a small pilot,
not the 30-instance, two-round S-tier stop gate.

| Measure | Object-first | Action-first |
| --- | ---: | ---: |
| First command path | 10/18 | 4/18 |
| First full invocation parsed | 7/18 | 1/18 |
| Recovered within two help/error hops | 18/18 | 18/18 |
| Operational false-success exits | 0 | 0 |

**Choose object-first** as the canonical 0.2 authoring shape. The action
variant remains explorable through `OB_SURFACE_VARIANT=action` for comparison,
without aliases in the default tree. The pilot exposes two remaining naming
questions: both object reviewers first tried `init` for creation; both
reached for a consumption-related noun instead of `dependency`. Help and
errors recovered those tasks, but the first-command scores are well below
the stop threshold. Do not call this S-tier. A larger held-out round should
challenge those names after the full task surface exists.

Claude Opus was requested as a second model family using its installed CLI.
Authentication worked outside the sandbox, but the account reported a spend
limit, so this pilot has only one model family. This is a limit on the blind
evidence, not a parser or facade failure.
