# Internal command-surface review

**Historical review.** The OpenBindings 0.2 working draft changed after this
assessment. Its spec-fidelity scores and S-tier decision are superseded by
[`COMMAND_SURFACE_0_2_REVIEW.md`](COMMAND_SURFACE_0_2_REVIEW.md), reviewed
against spec commit `ccfe0b6`.

This reviews the inert facade on `codex/cli-surface-lab`, not the production
implementation. The OpenBindings specification defines an OBI model and tool
obligations, not command names or a required CLI. No outside reviewer was
contacted. Scores below are an internal assessment informed by three blinded
Codex agent trials and one Claude Opus 5.5 trial; agents are not a substitute
for outside users.

An earlier review compressed nearly every score into the 8s because it used
unfinished runtime behavior as a reason to discount *facade* qualities. That
was a rubric error. Runtime usefulness remains unassessed; the scores here
use observable command-surface evidence. A 10 means the named criterion has
no known in-scope flaw, not that the entire tool is finished.

## Internal S-tier gate

Run separate model, task-path, shell-contract, and adversarial passes after
each surface iteration. Stop internal iteration only when:

1. The weighted score is at least **9.0/10**, every dimension is at least
   **8.5**, and no major finding remains.
2. Every spec relationship has a discoverable direct path or a documented
   lossless escape hatch. Help makes no false core-spec claim.
3. The full task bank parses through the intended command, every executable
   leaf has an output/exit contract, and help has no dead example or flag that
   silently does nothing.
4. Adversarial tasks cover opaque third-party binding specifications,
   multiple bindings through one source, stdin collisions, output streams,
   secrets, malformed arguments, and automation.
5. A fresh internal task set produces the same conclusion without reusing
   command paths as hints. Record first choice, help lookups, and wrong turns.

This gate determines readiness to seek outside feedback. Outside user testing
is a later stage and was not performed here.

## Designs compared

The **broad** variant keeps most tasks directly on the root: 29 top-level
commands and 78 lines of root help. The **compact** variant groups comparison,
edits, configuration, and processes: 16 top-level commands and 60 lines.
The new **hybrid** keeps daily tasks direct and moves specialist commands
under `source`, `operation`, and `serve`: 24 top-level commands and 66 lines.
Set `OB_SURFACE_VARIANT=compact` or `OB_SURFACE_VARIANT=hybrid` to explore one.
All three contain the same 69 inert leaves, flags, parsers, and output modes.

The compact layout improves the first screen. Its `edit` boundary is weak:
`ob edit patch` and `ob operation add` both edit the OBI, while one is nested
and the other is not. `config binding-specs` also hides an informational
command under a configuration label. Moving `status` under `source` is precise
about drift but makes a likely daily check harder to guess. The broad layout
keeps those paths visible but makes the root expensive to scan. The hybrid
keeps direct `status`, `diff`, `compat`, and `patch`; it places `inspect` and
`binding-specs` under `source`, `conform` under `operation`, and foreground
processes under `serve`. This ownership rule is clearer than the compact
tree's generic `edit` boundary. It is now the leading variant.

## Internal passes and evidence

### 1. Specification and command value

Reviewed the command model against `../spec/openbindings.md` §§1, 3, 5, 6,
and 10. Operations remain neutral contracts; `binding add` gives bindings
independent keys and allows two through the same source; `dependency add`
represents named consumption points with exact opaque any-of binding-spec
identifiers; `source` admits artifactless live surfaces; `patch` covers fields
without a dedicated command. `compat` is explicitly ob policy. `validate
--strict` is described as extra ob lint. `strip-ob-metadata` is packaging,
not a conformance repair. Invocation is presented as ob's interface, since
Core does not define an invoker or interaction lifecycle.

The facade addresses every Core document class without requiring a built-in
format, provider, or implementation-specific relationship:

| Core document concept | Facade path |
| --- | --- |
| Version and document labels | `new`, `meta set`, or `patch` |
| Neutral operation contracts and schemas | `operation add/set/output-schema`, `operation alias`, or `patch` |
| Sources carrying `content`, `location`, or both | `source add`, including full `--input`, or `patch` |
| Independently keyed bindings and selectors | `binding add` or `patch` |
| Named consumption points with opaque acceptable specs | `dependency add` or `patch` |
| Shared schemas, transforms, examples, and extension fields | RFC 6902 `patch` |

This is a 10 for *addressability in the command grammar*. It does not assert
that the inert implementation preserves or validates those fields correctly.

This pass found inherited help citing **retired OBI-T-07/T-08** as runtime
validation rules and overstating adopted operation names as verified
correspondence. The facade now describes direct binding invocation as ob
policy, treats aliases as author assertions, cites current §5.1, and says
unsupported spec versions are refused by explicit support policy. Automated
checks reject those stale claims in all three variants. The remaining risk is
in future implementation: conformance can be **undetermined**, source
selectors belong to the binding specification, and unknown binding specs
must not be rejected solely because ob lacks a delegate.

### 2. Task-path and navigation review

The task bank is: T1 new stdout/file; T2 summary/full OBI; T3 add and pull a
source; T4 two bindings for one operation/source; T5 dependency with two
accepted binding specs; T6 arbitrary extension edit; T7 invoke with one-call
context and explicit binding; T8 ongoing input and bounded output; T9
validate, drift, and directional compatibility; T10 malformed key,
preference, stdin, flag, and command errors; T11 opaque unfamiliar binding
spec. Thirteen valid invocations covering T1–T9 and T11 reached inert
placeholders in the broad and compact variants. T10 cases are exercised in
`TestSurface*`. This establishes parseability, not whether someone would
choose those paths.

The hybrid preserves the broad tree's obvious direct paths and removes five
root choices. A second, fresh 12-task set covered a neutral unbound operation,
name adoption, an opaque third-party source, source inspection, exact support
checking, distribution stripping, drift in CI, masked context output, binding
machine input, a code envelope, server launch, and extension editing. All 12
reached a placeholder or an explicit inert sample. That was path-shape evidence;
the blinded trial below separately tested first-command choice.

### Blinded first-command trial

Three agents with no access to this review, the contract, source, or tests
recorded their first command guesses before viewing CLI help. Each used only
the hybrid executable and inert invocations. The novice, power-user, and
operator task sets had five natural-language tasks each. They then recorded
help pages, wrong turns, and final routes. **All 15 tasks were resolved through
help and reached a placeholder**, with discoverability scores of **8, 8, and
7/10** respectively. These are model-generated users, not a measure of human
adoption.

The exact first guesses, help lookups, and resolved routes are in
`COMMAND_SURFACE_AGENT_TRIAL.md`.

| Trial | First-choice evidence | Help outcome |
| --- | --- | --- |
| Novice | Guessed a nonexistent `obi` group for all five document tasks. Its error only said `unknown command "obi" for "ob"`. | Root help exposed all five routes; `source add --pull` and `strip-ob-metadata` needed leaf help. |
| Power user | Guessed `create` rather than `add`, a `consumption-point` noun, an extension-specific patch path, and a positional binding argument with `--request`. | Noun and leaf help explained every final route, but the machine invocation lane required a correction. |
| Operator | Chose `diff` for replacement compatibility, `auth` for a token, `spec` for binding-spec support, bare `serve` for the API, and `new <filename>` for file creation. | Help resolved all five; `new <filename>` and nonexistent nouns gave only generic errors. |

This meets the gate's requirement to *run and record* a fresh unhinted task
set, but it does not support a top discoverability rating. A wrong noun often
dead-ends at a bare parser error. The most concrete repair target is `new
<filename>`, whose diagnostic should say to use `-o`. `auth` and `spec` are
plausible entry nouns; cross-reference guidance can help without adding more
canonical aliases. No command-shape change was made from this trial alone.

An authenticated **Claude Opus 5.5** pass added five blinded tasks. It first
recorded guesses with tools disabled, then used only hybrid CLI help and inert
invocations. All five routes were found; Claude rated discovery **8/10**. It
correctly guessed `show`, missed `create` versus `new`, `--derive` versus
`--pull`, and `export` versus `strip-ob-metadata`. It exposed a concrete
facade defect: `ob diff <obi>` reaches the placeholder even though the help
requires either a second OBI or `--from-sources`. The production handler
checks this in `RunE`; replacing that handler removed the check from the
facade. `diff --from-sources` and `status` also need reciprocal help links
that distinguish structural diff from the per-source drift report. The exact
Claude log and model setup are in `COMMAND_SURFACE_AGENT_TRIAL.md`.

### Focused discoverability and recovery pass

The facade now validates the two `diff` argument modes before its inert
handler. It rejects a positional argument to `new` with guidance for `--name`
and `-o`. A preview-only command-path preflight runs before Cobra parses flags
or honors `--help`, so invalid `auth --help` and `auth set ... --bearer-token`
fail with the `context` route instead of appearing valid or reporting an
unrelated flag error. Unknown `obi`, `create`, `export`, `auth`, `spec`,
`bindings`, `consume`, `policy`, and related guesses receive task-aware route
guidance. No new aliases or executable leaves were added.

Help now distinguishes a structural `diff --from-sources` from the per-source
`status` report, puts credential URL matching at the top of `context set`,
and explains the `binding-specs check` yes/no report and proposed exits.
Errors for the observed `--derive`, `--sources`, `--request`, `--accept`,
`--bearer`, and `--title` guesses point to the intended flags or commands.
Inline secret examples were removed from the facade's context-set help.

Two new blinded Codex agents and a fresh Claude Opus 5.5 session ran five
natural-language tasks each after the first repair set. All **15/15** tasks
found a parsed route; their discovery/recovery grades were **7, 8, and 7/10**.
They exposed invalid paths with `--help` returning root help and flag errors
hiding unknown commands. After those repairs, a second independent set of
two Codex agents and Claude ran 15 more tasks. Again **15/15** found routes;
their grades were **8, 8, and 7/10**. That round exposed a few
correct-area/wrong-flag dead ends and buried credential matching rules. The
last small changes address those observed cases, and focused regressions and
manual CLI checks pass. At that point, the repaired snapshot still needed a
blind task set. The exact guesses
and observations are in
`COMMAND_SURFACE_AGENT_TRIAL.md`.

A final blinded set then gave two new Codex reviewers and Claude Opus 5.5
five tasks apiece. **All 15/15 routes were found, but 0/15 first guesses
parsed.** Their first-command/recovery/overall grades were **1/8/5**,
**0/9/5**, and **3/7/5.5** out of 10. No wrong path appeared to succeed.
The misses were not just spelling: reviewers repeatedly expected an `obi`
container, `bindings` for installed spec support, `consume` for consumption
points, `source status` for drift, and a request envelope under top-level
`invoke`. Recovery also had avoidable detours for `invoke --input` without an
OBI, `init --name`, `source status`, and `diff --tracked`. This pass added
specific corrections and a machine-lane help link for those observed cases;
focused regressions pass. The final corrections have not received another
blind trial. More importantly, recovery hints cannot by themselves improve
the 0/15 first-command hit rate. The next iteration should challenge the
information architecture, not keep adding one-off hints.

### 3. Output, shell, and automation review

An explicit contract classifies all 69 leaves into eight modes. The facade
rejects `-o` on configuration edits and streams, rejects `-o/-F` on servers
and the binding machine lane, requires JSON for uncollected event streams,
rejects text for document results, and rejects result flags with `--quiet`.
`show --full`, `codegen --envelope`, `resolve --with-origin`, and `context get
--reveal-secrets` separate content choice from `-F` rendering. Machine binding
invocation has a one-line JSON sample. See `COMMAND_SURFACE_CONTRACT.md`.

The focused contract pass resolved three ambiguous shapes. `binding invoke
--input` now carries only the binding value in the positional lane; the
complete no-argument machine request has its own `--request` flag. All four
document producers save canonical JSON with `-o` and allow `-F yaml` only
on stdout. `new` has no positional destination; `-o` is its file destination,
and `--force` requires it. Help distinguishes `new` (OBI) from `init` (local
configuration). `resolve --with-origin -o` is explicitly a JSON envelope.

Configuration-edit, stream, and foreground-process help hides rejected result
flags. A proposed cross-command exit table now distinguishes success (0),
negative verdict or unsuccessful invocation (1), error (2), and incomplete or
undetermined result (3). Mixed lanes (`binding invoke` positional versus
machine, and `--quiet` versus normal reports) still rely on explanation in
help. The inert executable cannot demonstrate 1/3, actual partial-stream
failure, or runtime stdout/stderr behavior.

### 4. Adversarial help and naming review

Removed routine shorthand aliases from the proposal. Retained only the
historical `describe` and `purify` migration names; canonical examples now
use `about` and `strip-ob-metadata`. The first sweep caught inherited examples
that still used `ob op` and similar removed aliases; those were repaired.
Automated sweeps opened help for all 69 leaves in each variant and found no
dead command path or stale shorthand in the checked patterns. Tests traverse
every command in all three variants and allow exactly the two migration
aliases. They also cover invalid keys, fractional preferences, duplicate
binding-spec identifiers, stdin conflicts, destination conflicts, quiet mode,
secret reveal selection,
and output mode rejection. These tests cannot exercise actual file, network,
credential, or stream behavior.

The focused pass fixed an inherited context example missing the bearer-token
value, an obsolete `jq .interface` pipeline, and two relative-location JSON
examples. A permanent parser sweep now sends every command in explicit help
example blocks through a fresh inert root: **140 broad, 139 compact, and 139
hybrid command segments**, all reaching their intended leaves. This proves
the examples' command and flag shapes; it does not prove that their sample
inputs would work with implemented handlers or that prose-embedded snippets
are semantically correct.

## Provisional ratings

Scores rate the facade, with this explicit interpretation of a 10:

| Criterion | A 10 on the command surface means… |
| --- | --- |
| Spec fidelity | Every user-facing model claim matches the current spec and every ob policy is labeled as such. |
| Core model expressivity | Every core OBI relationship and field has a direct or generic patch path, without forced provider or binding-spec choices. |
| Command value and task coverage | Each command earns its place in real tasks and the task bank has no missing or duplicate path. |
| Names and hierarchy | A new user finds the intended path first, and every nested task has an obvious owner. |
| Help and onboarding | Every help page is accurate, self-contained, and has valid examples where needed. |
| Output and composability | Every flag has one meaning; content, rendering, destinations, streams, and machine lanes are explicit and non-conflicting. |
| Error clarity and safety | Invalid inputs fail early with a repairable diagnostic and no silent side effect. |
| Presentation and density | The first screen is scannable while common paths stay visible. |
| Scripting and exits | Every lane has a deterministic stdout/stderr and exit contract, including partial results. |
| Alias and migration policy | Each action has one canonical spelling, migration aliases are deliberate, and all examples use canonical paths. |

The two 10s are reachable without a runtime: core model expressivity
and aliases are properties of the facade. Runtime correctness is a separate
evaluation, not a hidden deduction from these scores.

| Criterion | Weight | Broad | Compact | Hybrid | Main reason |
| --- | ---: | ---: | ---: | ---: | --- |
| Spec fidelity | 18% | 9.1 | 9.1 | 9.1 | Core claims reviewed; ob-only delegate requirements are explicitly labeled as policy. |
| Core model expressivity | 12% | **10** | **10** | **10** | Neutral operations, independent binding keys, opaque specs, dependencies, aliases, and patch escape hatch. |
| Command value and task coverage | 15% | 8.8 | 8.8 | 8.8 | Task paths complete; specialist command value needs stronger evidence. |
| Names and hierarchy | 15% | 7.6 | 7.2 | 7.4 | The final blind set found 0/15 first-command hits despite finding every route after help. |
| Help and onboarding | 10% | 9.0 | 9.0 | 8.8 | 418 example segments parse and every route was found, but the machine lane took multiple help hops. |
| Output and composability | 10% | 9.2 | 9.2 | 9.2 | Machine request, document artifact, YAML view, and new destination each have one clear lane. |
| Error clarity and safety | 8% | 8.5 | 8.5 | 8.7 | Invalid `diff` rejects; observed wrong nouns, help paths, and flags now offer a repair. |
| Presentation and density | 5% | 7.4 | 8.5 | 8.8 | Hybrid shortens root help without burying daily tasks. |
| Scripting and exits | 5% | 8.7 | 8.7 | 8.7 | Machine lane is explicit; partial-stream and verdict exits remain unimplemented proposals. |
| Alias and migration policy | 2% | **10** | **10** | **10** | Exhaustive alias audit across all three variants. |
| **Weighted** | **100%** | **8.8** | **8.8** | **8.8** | Hybrid's recovery is strong, but first-command discovery is well below the gate. |

## Decision and next internal iteration

**Internal S-tier gate: not met. Do not seek outside review yet.** The hybrid
remains the working baseline, but its weighted score is now **8.8** and names
and hierarchy are **7.4**, below the 8.5 floor. The fresh blind set's 0/15
first-command hit rate is stronger evidence than the earlier optimistic
hierarchy score. The next internal comparison should move installed binding
specification support and named consumption points into places that people
expect on first contact, and resolve whether the operation and binding
invocation lanes can be taught at the root. Compare concrete trees with
another task-blind trial; do not add routine aliases merely to improve hits.
`context get` still cannot show which stored URL key matched, so its scope
inspection task remains incomplete at the interface level. The last targeted
recovery fixes need fresh blind verification if this design continues. The
example sweep establishes parser validity, not semantic correctness. Treat
partial-stream behavior and verdict exits as implementation-contract
questions when handlers exist.

Focused `go test ./internal/cmd -run TestSurface` and the preview build pass.
The full `internal/cmd` suite is not a useful gate for this branch because its
end-to-end cases invoke real operations that the facade deliberately disables.
