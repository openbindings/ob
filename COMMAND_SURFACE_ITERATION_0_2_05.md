# Iteration 0.2-05: exact reads and JSON output

Spec pin: sibling `spec` at `ccfe0b6`. The first full 30-task trial found
repeated `--json` guesses and `show` requests for complete stored values.
The current `show` commands generally give summaries unless `--full` is
present; this may hide the exact absent-versus-null distinction users are
trying to inspect. The hypothesis is that a read command should show its
complete stored value by default and offer a visible JSON output flag.

## Task cards written before the change

1. Display all fields of `warehouse.obi.json` as machine-readable JSON.
2. Display one source `catalog` exactly, including whether `content` was
   absent or present with `null`.
3. Display one binding `findItem.http` exactly, preserving arbitrary
   kind-owned `content`.
4. Display operation `findItem` selected by alias `getItem`, showing its
   entire stored contract.
5. Display dependency `billing` and its any-of `kinds` list exactly.
6. Check conformance of `warehouse.obi.json` and get machine JSON evidence.
7. Compare two OBIs structurally and get machine JSON changes.
8. Ask for a short human summary of a whole document without dropping the
   ability to see its exact stored form.
9. Try `--json` together with `-F yaml`; this must be rejected, not silently
   choose one output representation.

## Competing read contracts

| Current `read-summary` | Candidate `read-direct` |
| --- | --- |
| `ob show OBI --full -F json` | `ob show OBI --json` |
| `ob source show OBI KEY --full -F json` | `ob source show OBI KEY --json` |
| `ob show OBI` is a summary | `ob show OBI` is complete; `--summary` opts into a short report |
| `-F json` for machine reports | `--json` for machine reports; `-F json` remains the general format selector |

Both trees retain the same underlying read capabilities and output modes.
`OB_SURFACE_VARIANT=read-summary` now selects the former read form; the default
uses the direct candidate after blinded comparison. In the direct tree, all noun
`show` leaves return the full stored object by default, and `--json` is a
presentation flag available on report leaves. It does not change whether
the selected output is a full value or a summary. Explicit conflicting
format flags are usage errors. `--json` is a high-frequency convenience,
not a second command spelling.

The preview is inert: correct ordinary calls exit 2 with `no operation ran`;
samples are labeled and never read a locator. Existing 0.2 schema and
command-contract gates must pass before blind trial.

## Blind result and decision

Two independent reviewers sealed nine complete first invocations before
seeing either CLI. Each reviewer then explored only its assigned frozen
binary, help, and errors. This is a small diagnostic pilot, not a full-round
S-tier score. The cohorts chose different verbs (`show` versus `read`), so
their first-path counts cannot isolate the output contract.

| Observation | Summary-first tree | Direct-read tree |
| --- | ---: | ---: |
| First complete invocation accepted by parser | 0/9 | 2/9 |
| Correct target recovered after help | 8/8 positive tasks | 8/8 positive tasks |
| `--json` accepted for reports | No | Yes |
| Complete stored value is the default `show` result | No | Yes |
| Conflicting format flags wrongly accepted | Repeated `-F` accepted | `--json` with `-F` rejected |

The summary-first reviewer guessed `--json` on seven useful tasks and had to
learn `-F json` plus `--full`. The direct-read reviewer guessed `read` instead
of `show` on five tasks, but accepted `--format json` on validate and diff.
Both reviewers discovered that `source list` returned keys/kinds only; that
made exact source catalog inspection require a whole-document read.

**Decision:** promote full-value `show` and `--json` to the default. Add
`source list --full` for exact stored source values. Preserve the old read
shape only as `read-summary` for comparison. Reject repeated `-F` values at
parse time, including the conflict observed in the blind trial. The target
remains inert: an ordinary accepted command exits 2 and does no operation.
The new defaults and sample outputs pass the pinned 0.2 deterministic gates.

The remaining first-command problem is broader than output flags: the first
30-task round had only 12–13/30 correct first paths, especially on installed
kind workflows. This iteration does not satisfy the stop gate or justify a
second 30-task qualification round yet.
