# Fresh blind diagnostic after the 0.2-08 recovery fixes

This is a **new 16-task diagnostic**, not a 30-task qualification round. It
scores the current default Core-noun facade after plural-noun recovery and
`kind check --action` replaced `--for`. No CLI edit is allowed between the
binary freeze and the reviewers' final reports. The sibling 0.2 spec pin is
`ccfe0b6df87cca0a46955e5aeeff550797a23cf8`.

## Protocol, fixed before review

Four independent internal reviewers use the same frozen binary. Two have no
primer; two receive a command-free 0.2 model primer. None sees repository
files, prior trial results, or other reviewers' answers. Each reviewer:

1. Seals a first command path and complete invocation for all 16 cards
   before running any CLI call.
2. Views root `--help` once, then seals a second path and complete invocation
   for all cards before any targeted help.
3. Tries the second invocations, then uses no more than two targeted
   help/error interactions per card to reach syntax acceptance.

Score semantic first path, complete first invocation, path and complete
invocation after root help, parser acceptance within the targeted budget,
and operational false success. An exit-2 placeholder is parser acceptance,
**not** task completion. `--sample-output` is illustrative and never reads
the named locator. The new cards describe the same action families as the
prior architecture pilot with different wording and values; they are not a
replay of its exact prompt text.

## Held-out task cards

Use `inventory.obi.json` unless another locator is named. These cards avoid
the contested `binding`/`realization`, `dependency`/`consumption`, and
`kind`/`handler` words except where a real JSON field or command value would
have to be named. Reviewers are not shown the intended action IDs.

| ID | Goal | Intended action ID |
| --- | --- | --- |
| D1 | Produce an empty OBI named `Inventory API` at `inventory.obi.json`. | `new` |
| D2 | Print every stored field of `inventory.obi.json` as machine JSON. | `show` |
| D3 | Determine whether `inventory.obi.json` conforms under its declared OpenBindings version; get JSON evidence. | `validate` |
| D4 | Apply RFC 6902 file `metadata.patch.json` to `inventory.obi.json`. | `patch` |
| R1 | Connect operation `lookup` to source `catalog` under concrete key `lookup.http`. | `binding.add` |
| R2 | Retrieve the exact stored object for `lookup.http`. | `binding.show` |
| R3 | Inventory the keys of all declared concrete ways to perform operations. | `binding.list` |
| R4 | Act on exact key `lookup.http` with caller input `{"id":"A-17"}`. | `binding.invoke` |
| C1 | Declare that this component needs operation `charge` at local point `billing`. | `dependency.add` |
| C2 | Inventory named local points that request operations. | `dependency.list` |
| C3 | Read the exact stored data for point `billing`. | `dependency.show` |
| H1 | Which exact source-token types can this local installation do anything with? | `kind.list` |
| H2 | Can this installation perform invocation for source token `acme.http@1`? | `kind.check` |
| H3 | Can this installation obtain fresh stored content for source token `corp.bus@7`? | `kind.check` |
| X1 | Store source `catalog` with exact token type `acme.http@1` and JSON content `{"baseUrl":"https://example.test"}`. | `source.add` |
| X2 | Store operation `lookup` with output JSON Schema `{"type":"object"}`. | `operation.add` |

## Frozen binary and gates

Binary: `/private/tmp/ob_v02_fresh_probe`, SHA-256
`f3536b4cd474505803c7496750442771e498209b152ee93c12309a2b09df5756`.
The default tree was frozen before reviewer access. `TestV02` passed against
the pinned spec, the build passed, and `git diff --check` was clean. No CLI
code changed while reviewers worked.

## Fresh blind score

`F1` and `F2` had no primer; `S1` and `S2` received the command-free 0.2
primer. All four sealed both invocation sets at the prescribed stages.
First-invocation acceptance below requires **both** intended task semantics
and parser acceptance. A wrong but parser-accepted action, such as listing
operations when the task requires concrete ways to carry them out, is not a
hit.

| Reviewer | First semantic path | First complete invocation | After root help: semantic path | After root help: complete invocation | Within two targeted interactions | False-success task exits |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| F1, first-contact | 4/16 | 3/16 | 12/16 | 12/16 | 16/16 | 0 |
| F2, first-contact | 0/16 | 0/16 | 12/16 | 12/16 | 16/16 | 0 |
| S1, spec-primed | 9/16 | 9/16 | 12/16 | 12/16 | 16/16 | 0 |
| S2, spec-primed | 8/16 | 8/16 | 12/16 | 12/16 | 16/16 | 0 |
| **Combined** | **21/64 (32.8%)** | **20/64 (31.3%)** | **48/64 (75%)** | **48/64 (75%)** | **64/64 (100%)** | **0** |

| Task IDs | First correct paths across four | Complete invocations after root help | Within targeted budget |
| --- | ---: | ---: | ---: |
| D1, D2, D3, D4 | 1, 1, 3, 3 | 4 each | 4 each |
| R1, R2, R3, R4 | 2, 0, 2, 0 | 4, 0, 4, 4 | 4 each |
| C1, C2, C3 | 2, 2, 0 | 4, 4, 0 | 4 each |
| H1, H2, H3 | 0, 0, 0 | 4, 0, 0 | 4 each |
| X1, X2 | 2, 3 | 4 each | 4 each |

Every accepted ordinary invocation returned exit 2 with `command-surface
placeholder; no operation ran`. No task was operationally completed. Root
and targeted help returned exit 0 only to display documentation. No task
command falsely returned exit 0 or claimed a document had been changed.

## Interpretation and next gate

Root help consistently gave all four reviewers 12/16 complete invocations.
Every remaining miss was the same shape: `binding get` versus `binding show`,
`dependency get` versus `dependency show`, and guessed local-ability verbs
such as `kind can-invoke`/`can-fetch` versus `kind check --action`. One or two
targeted help pages resolved these routes for every reviewer. All four
recovered the exact `--action invoke` and `--action pull` forms, the area that
had failed for three reviewers in the previous frozen architecture pilot.

This is a fresh **diagnostic** score for the revised default, not a new
30-task qualification score or proof of a causal gain from `--action`: the
task wording, reviewers, and protocol differ from the previous pilot.
Plural-noun recovery was not exercised by these reviewers, though its
deterministic parser tests pass. The formal internal S-tier stop rule remains
failed: zero-help first path is far below its 75% overall threshold, and the
rule also requires two fresh 30-task rounds. The evidence favors a focused
review of post-root-help `get`/`show` and local ability discoverability
before spending another full qualification round. No such round was run.
