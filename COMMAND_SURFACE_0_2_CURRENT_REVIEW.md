# Current internal review of the 0.2 facade

Review basis: `codex/cli-surface-lab`, pinned sibling spec commit
`ccfe0b6df87cca0a46955e5aeeff550797a23cf8`, default inert
`NewV02SurfaceRoot`, eight focused iteration records, one 30-task blind
round, the focused read/workflow trials, a four-reviewer architecture
pilot, and a subsequent four-reviewer fresh diagnostic after the recovery
fixes. These are
**facade** scores. No operational command runs, so runtime correctness is
ungraded. The full-round metrics measure the frozen tree *before* the read,
source-list, repeated-format, and Boolean-flag revisions; they are the last
qualified blind baseline, not fresh scores for the current executable.

## Stop decision

**Internal S-tier gate: failed.** In the 30-task round, reviewers chose the
correct first path for 13/30 and 12/30 tasks, versus the 75% overall stop
threshold. Their complete first invocations met intent for 7/30 and 6/30,
versus 65%. Common Core first-path counts were 8/12 each, versus 85%.
Positive-syntax recovery was 53/56 across reviewers, just under 95%; the
strict per-reviewer criterion also failed for reviewer B. No operational
false-success exit was observed. The post-round improvements address several
full-invocation misses but do not change the main noun hierarchy, so they
cannot justify another qualification score without a fresh held-out round.

The fresh post-fix diagnostic scored **21/64 task-correct first paths** and
**20/64 complete first invocations** across four reviewers. A single root
help exposure raised complete invocations to **48/64**, and targeted help
recovered **64/64** within budget, with zero false-success task exits. This
16-card result is not a 30-task qualification round, and it does not erase
the failed stop decision above. See `COMMAND_SURFACE_FRESH_BLIND_0_2_01.md`.

The current default is substantially more faithful to 0.2 than the inherited
0.1 facade: sources use exact opaque `kind` and optional arbitrary JSON
`content`; bindings have independent keys and arbitrary optional content;
dependencies use any-of `kinds`; operation aliases, optional Boolean
idempotency, and arbitrary JSON Schemas are represented. Samples pass the
pinned 0.2 structural schema and the covered local relationship checks.
Unknown kinds remain valid documents; installed ability is a separate ob
concern. `compat` and `conform` are withheld because no 0.2 Core algorithm
defines their results and this branch has no versioned ob policy for them.

## Provisional grades

The weights use the established internal rubric. A 10 means no known flaw
within the dimension's facade scope. Grades are design judgments, not
measured runtime performance.

| Criterion | Weight | Grade | Main limiting evidence |
| --- | ---: | ---: | --- |
| Spec fidelity | 18% | 9.2 | Core-facing help and samples were reset to 0.2; the schema gate is strong, but the semantic sample check is local rather than a complete independent conformance runner. |
| Core model expressivity | 12% | 9.4 | Common core fields have direct paths and `patch` covers arbitrary fields; updating an existing nested core field still depends on generic patch. |
| Command value and coverage | 15% | 8.0 | The 32 leaves have distinct tasks, but installed capability lookup by a binding key and versioned ob comparison policies are still unresolved. |
| Names and hierarchy | 15% | 6.7 | The held-out round found only 12–13/30 correct first paths; `binding`, `dependency`, `kind`, and source-handler workflows remained hard to predict. |
| Help and onboarding | 10% | 8.5 | Most tasks recovered in two hops; a few local-kind tasks needed three. Some longer pages rely on spec knowledge. |
| Output and composability | 10% | 8.1 | Exact reads, `--json`, canonical JSON OBI output, and NDJSON invoke lanes are specified; text report samples outside `diff` still render as JSON fixtures. |
| Error clarity and safety | 8% | 8.6 | Invalid inputs fail, repeated format selection is now rejected, and accepted ordinary calls report inertness; wrong domain nouns still receive uneven suggestions. |
| Presentation and density | 5% | 8.2 | Root help is scannable; eight source actions in one group require more task-oriented explanation. |
| Scripting and exits | 5% | 7.8 | Result/destination contracts exist for all leaves, but many proposed real exit distinctions and stream failure cases have no executable contract beyond inert exit 2. |
| Alias and migration policy | 2% | 9.6 | One canonical spelling per action in the default tree; variants are separate, and no synonym collection inflates first-hit scores. |
| **Weighted** | **100%** | **8.3** | Below the 9.0 stop threshold and the 8.5 floor for every dimension. |

## What the loop changed

1. Replaced the inherited 0.1 preview with an independent inert 0.2 tree
   and pinned its samples to the draft schema.
2. Chose object-first core authoring, root exact-binding `invoke`, and
   `init` from competing blind pilots.
3. Moved complete stored values to the default `show` result, added
   `--json` for report lanes and `source list --full`, and rejected repeated
   `-F` selections after the read pilot.
4. Kept the original `import`, `inspect`, `pull`, and `synthesize` verbs after
   a neutral-wording competitor lost 3/8 to 1/8 on semantically correct first
   paths. A focused Boolean `--idempotent` revision now preserves absent,
   true, and false without requiring a value for true.
5. Tightened the loop: future task banks pair neutral paraphrases, separate
   semantic path choice from parser recognition, and never count an inert
   placeholder as task execution.

6. Compared the Core-noun tree with a coherent task-language tree that moved
   document actions under `document`, renamed `binding`/`dependency` to
   `realization`/`consumption`, and renamed local `kind` ability to `handler`.
   First-contact reviewers got 2/16 correct first paths on both; spec-primed
   reviewers got 4/16 and 3/16 respectively. The alternative stays a
   comparison variant. The trial exposed and prompted corrected plural-noun
   recovery and an explicit `kind check --action` flag.

## Current bottleneck

The architecture pilot did not identify a better canonical noun tree.
First guesses varied even after a spec primer: one reviewer used plural
Core nouns, another used singular Core nouns, and first-contact reviewers
invented different task nouns. This makes zero-help first-path rate a noisy
single-reviewer measure, although all four rates were far below the current
stop gate. The next internal review should test **learnability after a brief
root-help exposure** and singular/plural recovery with balanced tasks, while
retaining zero-help guesses as a separate diagnostic. Recalibrate the stop
criterion only prospectively, before a fresh 30-task round; do not reinterpret
the failed round as a pass. The fresh diagnostic found that root help leaves
only four repeatable command-shape misses: `binding get`, `dependency get`,
and two local-kind action checks. Each recovered through targeted help;
that is the narrowest next facade question. No outside human review has been
run.
