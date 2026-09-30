# Iteration 0.2-08: domain nouns versus task-language navigation

Spec pin: sibling `spec` at `ccfe0b6`. The prior 30-task round found repeated
first-path guesses for `document`, `realization`, `consumption`, and
`handler`, whereas the default facade uses direct document commands plus
`binding`, `dependency`, and `kind`. A small source-verb trial also showed
that task wording can prime a candidate name. This iteration changes one
thing: the **information architecture** for these noun families. It leaves
Core fields, source workflows, flags, outputs, and inert exit behavior alone.

## Competing trees

| Area | Current Core-noun tree | Task-language candidate |
| --- | --- | --- |
| Whole OBI | `ob init/show/validate/diff/patch` | `ob document init/show/validate/diff/patch` |
| Concrete operation realization | `ob binding add/show/list/remove` | `ob realization add/show/list/remove` |
| Named consumption point | `ob dependency add/show/list/remove` | `ob consumption add/show/list/remove` |
| Local installed ability | `ob kind list/check` | `ob handler list/check` |
| Exact runtime action | `ob invoke OBI KEY` | `ob realization invoke OBI KEY` |

The alternative uses language that first-contact reviewers reached for in
round 1. It is intentionally coherent, not a set of aliases: each tree has
exactly one path per action. Candidate help must explicitly relate its names
to the exact 0.2 JSON members `bindings`, `dependencies`, and `kind`, and to
the fact that invocation is an ob/handler concern rather than Core behavior.
`OB_SURFACE_VARIANT=task-language` selects the candidate. The default stays
the Core-noun tree until the blind result is adjudicated.

## Frozen neutral task cards, written before CLI changes

Use `warehouse.obi.json` unless another locator is named. The wording avoids
the disputed command nouns and both trees' exact multiword paths.

| ID | Goal | Intended action ID |
| --- | --- | --- |
| D1 | Start a minimal OBI named `Warehouse API` at `warehouse.obi.json`. | `new` |
| D2 | Display the complete stored JSON value of that OBI. | `show` |
| D3 | Check that OBI against the declared OpenBindings version and get a JSON report. | `validate` |
| D4 | Apply `owner.patch.json` as RFC 6902 changes to that OBI. | `patch` |
| R1 | Add `findItem.http` as a concrete way to carry out `findItem` through source `catalog`. | `binding.add` |
| R2 | Display complete stored details for `findItem.http`. | `binding.show` |
| R3 | List all declared ways of carrying out operations. | `binding.list` |
| R4 | Act on the exact `findItem.http` path with input `{"sku":"A-17"}`. | `binding.invoke` |
| C1 | Add named point `billing` where this component requires existing operation `charge`. | `dependency.add` |
| C2 | List such named requirements in the OBI. | `dependency.list` |
| C3 | Show complete stored details for point `billing`. | `dependency.show` |
| H1 | List the source types for which this local installation has any built-in action. | `kind.list` |
| H2 | Check whether this installation can perform invocation for exact source token `acme.http@1`. | `kind.check` |
| H3 | Check whether this installation can update content for exact source token `corp.bus@7`. | `kind.check` |
| X1 | Add source `catalog` with exact type `acme.http@1` and JSON content `{"baseUrl":"https://example.test"}`. | `source.add` |
| X2 | Add portable operation contract `findItem` with output schema `{"type":"object"}`. | `operation.add` |

The last two cards are unchanged-path controls. The phrase "source type" is
ordinary first-contact language, not an instruction to choose either `kind`
or `handler`. Card R4 deliberately tests whether nesting invocation under
the concrete-path noun helps enough to justify its longer route.

## Review protocol and decision rule

Four independent reviewers each see only one frozen binary variant. Two
receive a compact, command-free 0.2 concept primer (spec-primed cohort); two
receive no primer (first-contact cohort). One reviewer in each cohort sees
each tree. Before any CLI access they seal one command path and complete
invocation for all 16 cards. Afterward they may use only assigned CLI help
and errors, with at most two help/error hops per card. Record semantic
first-path hits, complete first invocations, two-hop recovery, false-success
exits, and why the wrong paths looked plausible. A recognized but
semantically wrong command is not a hit. Correctly parsed ordinary calls
exit 2 as placeholders and do not complete tasks.

Promote the candidate only if it makes a material first-path gain in both
cohorts, does not lose the unchanged-path controls, and passes every 0.2
contract gate. A mixed result means retain the current Core-noun tree and
invest in help/recovery rather than adding aliases. This 16-card pilot is
diagnostic, not one of the two 30-card S-tier qualification rounds.

## Frozen blind outcome

Each reviewer sealed all 16 paths and complete invocations before seeing
its assigned CLI. The executable was frozen at `/private/tmp/ob_v02_arch_trial`;
opaque wrappers A/B selected the trees without revealing their names. All
accepted ordinary calls exited 2 with `command-surface placeholder; no
operation ran`. No file was changed and no operational false success occurred.

| Cohort and tree | Correct semantic first path | Correct complete first invocation | Recovered within two hops | False success |
| --- | ---: | ---: | ---: | ---: |
| First-contact, Core nouns | 2/16 | 1/16 | 14/16 | 0 |
| First-contact, task language | 2/16 | 1/16 | 14/16 | 0 |
| Spec-primed, Core nouns | 4/16 | 4/16 | 14/16 | 0 |
| Spec-primed, task language | 3/16 | 2/16 | 16/16 | 0 |

The unchanged-path controls explain both first-contact first-path hits in
each tree: both chose `source add` and `operation add`. Neither first-contact
reviewer guessed a contested noun family correctly. The Core-noun
spec-primed reviewer guessed all four direct whole-OBI paths, but pluralized
`bindings`, `dependencies`, `handlers`, `sources`, and `operations`. The
task-language spec-primed reviewer guessed the exact singular Core nouns
`binding` and `dependency`, plus direct `init/show/validate/patch`, even
though those paths had moved in its assigned candidate. This is genuine
between-reviewer variation, not a controlled within-reviewer improvement.

Three reviewers missed the local ability selector, guessing `--action` or
`--ability` while the frozen CLI used `--for`. The fourth found `--for`
within its allowed budget. The Core-noun review also exposed a separate
recovery bug: plural `bindings` incorrectly pointed to `ob kind` because
an old binding-spec migration hint captured that word.

**Decision:** do not promote the task-language tree. It had no first-contact
gain and lost one spec-primed first-path hit plus two complete first
invocations. Keep it as an explicit comparison variant, not a synonym set
in the default CLI. The result also limits the earlier interpretation of
zero-help first guesses: a short spec primer improved direct document paths
for one reviewer but did not settle singular/plural command conventions.
The next concrete recovery fixes are a correct plural-noun diagnostic and
an explicit `--action` selector for local kind support. They change neither
tree's object model or first-path count, and need their own parser gates.

## Post-trial recovery correction

The frozen binary and scores above were not changed. The current lab now
routes plural first guesses to the appropriate singular command (`bindings`
to `binding`, `dependencies` to `dependency`, and likewise for source,
operation, schema, and local kind ability). The comparison tree points its
Core terms to its own canonical paths. `kind check` now uses `--action`
instead of `--for`; its help explains that `pull` is the installed action
for refreshing stored source content. The no-action sample reports all
locally known actions, matching the help contract. Parser/example/sample
gates pass after these changes. No fresh blind score is claimed for them.
