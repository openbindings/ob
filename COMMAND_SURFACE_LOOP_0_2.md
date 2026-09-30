# Development loop for an S-tier `ob` command surface

This loop designs an executable **facade**: command paths, arguments, flags,
help, errors, sample output, and output/exit contracts. Operational handlers
are out of scope until the facade passes the internal gate. The target is a
ubiquitous, unopinionated tool for working with OBIs. Internal success means
an **S-tier facade candidate**, not proof that the implemented CLI works or
that outside users will adopt it.

## Correct the review mechanism first

The current facade clones the production `NewRoot` tree and replaces its
handlers. This imported 0.1 assumptions into 0.2 help and flags. It also let
a sample labeled `0.2.0` pass a test against an older SDK while failing the
current 0.2 schema. The next facade should declare its own command tree and
contract, using production code only as reference. Its examples and fixtures
must be checked against the pinned 0.2 draft independently of that SDK.

Pin each review to an exact spec commit. At the start of every iteration,
compare that commit with the prior pin and classify changes as document shape,
semantics, tool obligations, or editorial. A relevant spec change reopens the
affected gate and invalidates scores based on the older rule. Rule IDs are
version-scoped; never use an earlier version's meaning by number alone.

## One iteration

| Step | Work | Exit evidence |
| --- | --- | --- |
| 1. Select one hypothesis | State the concrete flaw and the user task it harms. Pick one design variable, such as generic source authoring, kind support discovery, or invocation lane naming. | A one-paragraph hypothesis and a small set of task cards, written before changing the facade. |
| 2. Specify the contract | Describe the OBI fields, command shape, output, exit codes, help, and near-miss errors. Mark each claim as Core 0.2, kind-specific behavior, or ob policy. | A compact contract table with exact commands and expected results, including absent-versus-null values and unsupported kinds where relevant. |
| 3. Build two honest candidates | Keep both candidates capability-equivalent. One may favor core object nouns and the other a task workflow; neither may add a hidden capability or use aliases to inflate first-hit counts. | Two inert CLI trees whose help and parser can be explored. Keep changes within the facade. |
| 4. Run deterministic gates | Check samples, every help example, argument and flag parsing, invalid paths, output shapes, exit contracts, and command inventory. | Machine-readable results with zero critical failures. A parsed placeholder is recorded as syntax acceptance, never as a completed action. |
| 5. Run fresh blind trials | Give independent internal reviewers natural-language goals and concrete values. Before any help, record the first command path and complete invocation. Then allow only CLI help/errors and inert calls. Reviewers do not see source, design notes, or other scores. Pair paraphrases for contested names and counterbalance task wording across cohorts. | For each task: semantically correct first path, first invocation, wrong turns, help hops, accepted route, false success, and reviewer explanation. Use a new held-out set each cycle. |
| 6. Adjudicate and decide | Compare both candidates on the same tasks. Fix any factually wrong Core claim before interpreting UX results. Choose, revise, or reject the hypothesis. | A short decision record: winning shape, reasons, regressions, open findings, scores, and next hypothesis. |

Run the cheap deterministic gates after every edit. Run blind trials only
after a coherent candidate passes them. One iteration should change one
contract area; otherwise a score change cannot be attributed to a design
decision.

## Deterministic gates

**Spec truth is a hard gate, not a weighted score.** For the pinned 0.2 draft:

1. Every illustrative OBI must pass the published 0.2 structural schema and
   all applicable local document rules. A validator unable to check a rule
   reports it inconclusive; a sample is never called conformant on partial
   evidence. Test the schema with an independent 0.2 tool or spec conformance
   runner, not only the CLI's current Go dependency.
2. Generic authoring must represent every core field without forcing a
   particular kind, artifact, registry, or installed implementation. Test
   arbitrary exact kind strings;
   source and binding `content` omitted, `null`, boolean, number, string, array, and
   object; dependency `kinds` absent and nonempty unique any-of; boolean and
   absent operation schemas; alias equality and preference bounds.
3. Unknown kinds must not invalidate a document. An installed-capability
   report may say a particular action is unavailable, but must not redefine
   the document's kind constraint or claim a global registry.
4. Core validation help must state the 0.2 rules accurately: unknown
   non-`x-` fields are a conformance failure; unsupported versions are
   refused, not judged non-conformant; no network fetch decides document
   conformance; inconclusive rules preclude an unqualified positive verdict.
5. A forbidden old *core* member or claim in generic help/samples is a failure:
   top-level source `bindingSpec`/`location`, binding `selector` or transforms,
   dependency `bindingSpecs`, source priority, or a Core comparison/selection
   rule. These words can appear inside kind-owned `content` or explicitly
   labeled migration guidance; the check must be context-aware.
6. Every help example must parse through its intended leaf in each variant.
   Every leaf needs a distinct task, output type, destination rule, and exit
   contract. Invalid shapes and commands with `--help` must not look
   successful. Sample output must visibly say it is illustrative.

The task bank covers the ordinary jobs (`new`, `show`, `validate`, edit an
operation/source/binding/dependency, inspect, invoke), kind-dependent jobs
(import, synthesize, pull, capability discovery), and ob policies (`compat`,
`conform`, context, codegen). Include adversarial cases: an unfamiliar private
kind, unsupported installed capability, absent versus explicit `null`
content, two bindings through one source, two acceptable dependency kinds,
stdin collisions, secret handling, streamed output, malformed input, and
partial validation. `patch` is tested as the lossless escape hatch but does
not substitute for a discoverable route to common core tasks.

## Blind evaluation and scores

Use two independent internal reviewer perspectives and at least two model
families when available. Give each reviewer tasks without command names or
examples in their wording. Keep a fixed regression bank for comparability
and a held-out bank that changes every iteration. Keep the implementation
agent away from the held-out prompts until results are recorded. Use at least
30 task instances per blind round, including 12 common core tasks, 10
kind-aware or policy tasks, and 8 adversarial tasks. Compare candidates on
the same task mix with separate reviewer cohorts or counterbalanced order so
using the first tree does not teach the second.

Before freezing a bank, audit wording for lexical priming: a task that says
“refresh” cannot by itself establish that `refresh` beats `pull`. Score a
recognized command as a first-path hit only if it can satisfy the task's
semantic intent; generic `source add` does not satisfy a request to use an
installed handler. Report parser acceptance separately from task success.

When reviewers disagree on the domain vocabulary itself, run a small
diagnostic with two cohorts: first-contact users and reviewers given a
command-free spec primer. In each cohort compare one tree per reviewer, then
measure a separate root-help-exposed route-finding task. Keep zero-help
first guesses visible, but do not let a single wording or reviewer determine
a naming decision. Any change to the formal stop thresholds must be stated
before the next held-out qualification round and cannot turn a prior failed
round into a pass.

Measure these separately:

| Metric | Meaning | Internal S-tier threshold |
| --- | --- | ---: |
| First path | Reviewer chooses the canonical command path before help. | At least 85% of common core tasks and 75% overall, in each of two fresh rounds. |
| First invocation | Initial full command parses with the intended shape. | At least 75% of common core tasks and 65% overall, in each round. |
| Recovery | Reviewer reaches the right route after help/errors. | At least 95% within two help/error hops; all critical tasks recover. |
| False success | A wrong command or impossible mode appears successful. | Zero. |
| Spec and contract | Core claims, samples, examples, parser, and output/exit checks. | All hard gates pass; zero open P0/P1 findings. |

Keep subjective grades as diagnosis, not substitutes for these measures.
Score spec fidelity, model expressivity, task value, names/hierarchy, help,
output/composability, error safety, presentation, scripting/exits, and alias
policy using concrete evidence. The weighted score must reach 9.0/10 and
every dimension 8.5/10; spec fidelity must reach 9.5/10. A score of 10 means
no known flaw within that dimension's facade scope. Do not grade runtime
correctness on an inert branch.

The **stop rule** requires two consecutive iterations on fresh held-out task
sets meeting all thresholds, no critical regression from the prior winner,
and a final spec-delta check against the then-current pinned draft. If the
spec changes materially, re-run affected gates. Passing this rule names an
internal S-tier facade candidate; outside human review can follow later, at
the user's direction.

## First three passes

1. **0.2 validity reset.** Replace inherited 0.1 command declarations in the
   preview with an independent tree. Make `new`, `show`, source, binding,
   dependency, and validation samples and help 0.2-true. Compare two generic
   authoring shapes for `kind` and arbitrary `content`. Do not score hierarchy
   while either candidate emits an invalid 0.2 sample.
2. **Information architecture.** Compete placements for installed kind
   capabilities and distinguish generic source authoring from artifact
   import/pull. Test first-command choice and near-miss recovery with private
   kinds and unknown installed support. Preserve one canonical spelling per
   action unless a deliberate migration alias is needed.
3. **Policies and automation.** Specify `compat`/`conform` as versioned ob
   policies and invocation/context behavior as tool or kind capability.
   Audit machine request envelopes, stream/partial-result exits, secret
   presentation, and stdout/stderr. Then run the full blind and adversarial
   gate across the entire tree.

No command-count target is useful by itself. A leaf earns a place when it
has a distinct valuable task and a clear result; a commonly guessed path
earns attention when it is repeatedly missed. Repeated wrong guesses should
trigger a hierarchy challenge before another collection of aliases or
special-case error messages.
