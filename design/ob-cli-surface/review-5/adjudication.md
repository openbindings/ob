# Round 5 adjudication

Round 5 reviewed the surface-design preview at lab commit
`4984a63d18fea62c079d65f1afab025e71923211`. Three fresh independent Codex
reviewers used the same frozen binary, guide, brief, specification, and eight
interface texts. No Claude ran. The input checksums still match.

| Criterion | Reviewer A | Reviewer B | Reviewer C |
| --- | ---: | ---: | ---: |
| Learnability | 3 | 4 | 3 |
| Consistency | 2 | 4 | 3 |
| Coverage | 4 | 6 | 6 |
| Editing ergonomics | 1 | 3 | 2 |
| Output and scripting | 4 | 6 | 5 |
| Errors and recovery | 4 | 4 | 4 |
| Model fidelity | 5 | 3 | 4 |
| Economy | 3 | 5 | 5 |
| **Overall** | **4** | **4** | **4** |
| Blocking findings | 0 | 0 | 0 |

The bar is **unmet**: every reviewer must rank ob in the top three overall
with no blockers. Reviewers praised the coherent document model, granular
edits, alias handling, binding selection, and useful recovery diagnostics.
The remaining weaknesses concentrate on management records, scripting, and
consistency. Rankings are their judgments, not proof that any repair will
produce a particular future rank.

Reports: [A](reviewer-a.md), [B](reviewer-b.md), [C](reviewer-c.md).
Finding labels below refer to each report's numbered findings.

## Verified repairs

These follow from existing contracts, rulings, or a demonstrated mismatch
between help and behavior. They need no new OpenBindings design decision.
Except for the guide clarification noted below, they remain a repair queue
for the lab after round 5.

| Findings | Verification and repair |
| --- | --- |
| A1, B2, C1 | `delegate set d_91c2 --preference invoke=0.5` returns usage error 2. The [delegate-manager preference contract](delegate-manager.md#preferences-intent-scoped-to-a-role) permits fractional and negative values with faithful retention. Accept JSON numbers separately from OBI binding integers; preserve explicit zero versus absence and avoid silent numeric rounding. |
| A2, B1, C2 | Repeating `--role invoke` succeeds and emits duplicate roles. [Registration](delegate-manager.md#registration-and-identity) explicitly rejects duplicate roles rather than deduplicating them. Reject the repeated role before enrollment, with usage error 2. Check every role-changing path. |
| A4, B3, C3 | `delegate show -F json` exposes operation-name summaries without its retained OBI. `delegate roles -F json` gives required names without accepted interface values or admission/use descriptions. The [manager contract](delegate-manager.md) retains complete registrations and advertises complete expected interface alternatives. Expose the stored OBI, complete preference map, accepted interfaces, and each role's admission and consumption policy. Preserve sources, bindings, schema graphs, and extensions. Human text may summarize. |
| A4, B9, C4 | JSON drops fields shown in text: delegate preferences; source binding counts and invocation support; binding preference and idempotency. Commands reproduced these omissions. Include substantive report information in JSON and preserve absence versus an explicit zero or false. Do not replace stored parts with invented defaults. |
| B4, C5 | `context show https://api.example.com --reveal -F json` exports dotted keys such as `headers.X-Client` inside `fields`. Reusing `.fields` through `context set --value` creates literal dotted properties, not the [native Context hierarchy](binding-invoker.md#context). Export nested context values under a documented member, with local token-provider/resolver settings separately identified. Structural masking must never expose secret values. |
| A3, B5 | Explicit `source pull tasks.obi.json grpcApi --dry-run` returns 3; all-source pull returns 4 for the same unreadable source. The frozen guide's Decided list and pull help already prescribe 4. Return no-verdict 4, name the unreadable source, and leave it unchanged. |
| A5, B8 | Recovery commands substitute `my tasks.obi.json` without quoting, breaking copy/paste in compat, invoke, rename, and pull paths. Use a single shell-quoting routine for every substituted argument in generated commands, including structured report remedies. |
| C8 | `binding set ... --preference 010` means 8, while `08` produces a parser diagnostic. The spec requires an integer, but does not require octal CLI syntax. Decimal parsing and a plain numeric diagnostic are a consistent surface repair; retain the declared representation limits. |
| A6, B10, C9 | Both `validate --input` and `--output` fail with “check one side at a time,” but help does not state mutual exclusion. Document that current restriction. Supporting a pair is a useful separate proposal, discussed below. |
| A7, C7 | Literal inputs are all prevalidated before dispatch; streamed inputs are checked incrementally. The guide claimed all later mismatches follow earlier effects. Correct the guide to describe the actual boundary, including early closure of streamed input. This documentation correction is applied in the lab after the freeze, not to the frozen guide. |

## Five open decisions, with updated recommendations

These remain proposals. Reviewer support does not turn them into Matt's
rulings, and the guide's Decided list has not been expanded.

1. **Named credentials: retain the proposal.** A service can request a
   primary and a secondary credential, so a single flat token field cannot
   express both. Recommend repeatable `--credential NAME=-|@FILE`, reading
   JSON strings or Basic/OAuth credential objects; `--cookie NAME=VALUE|-|@FILE`
   for non-secret literal cookies or secret-bearing input; `--refresh-token
   -|@FILE`; matching unset names; complete context field help. All three
   reviewers support it. Repairing nested JSON export is still necessary.

2. **Configuration typing: retain the proposal.** An identifier `001`
   should stay text unless its author requests JSON. Recommend `POINT=VALUE`
   always as a string and `POINT:=JSON` for typed values, with the same rule
   for files and stdin. All three support it.

3. **Bare kind check: revise the recommendation.** A shell user reasonably
   reads `if ob kind check "$kind"` as a support predicate. The frozen
   proposal reports all roles and exits 0 even if each answer is no. A
   supports that documented distinction; B and C find it misleading.
   Recommend `--role ROLE` for a predicate, with an explicit `--report` mode
   for all-role inspection. With neither, return usage error 2 and show
   both paths. The frozen default remains playable until Matt rules.

4. **Pure-contract compatibility: retain a conservative default and add
   explicit direction.** An unbound contract can protect both old callers
   and old providers, but those are different questions. Widening input
   enum `["a"]` to `["a","b"]`, with identical output, still serves old
   callers but fails the reverse check. That follows from the [profile's
   directions](schema-comparison.md#the-question-the-profile-answers), not
   from running the preview's pretend comparison engine. A supports both;
   B and C request an explicit choice. Recommend default `both`, with
   `--direction provider|consumer|both` affecting only operations that have
   neither bindings nor dependency use. An old-caller migration example
   must select provider; protecting existing providers selects consumer.
   Keep settled bound/dependency role inference. The flag is not yet built.

5. **Local startup credentials: retain the proposal.** A local tool needs
   an exact address and run token without scraping terminal text. Recommend
   a private per-port run record, directory 0700 and file 0600, removed on
   shutdown; ob reads it for the exact saved service address. `start -F json`
   prints one startup record for other tools. All three support it. The
   preview describes these effects and performs no real filesystem writes.

## Two additional design choices

**Paired validation (A6, B10, C9).** A person checking a request and response
can reasonably expect one command to accept both. No pinned document rule
requires this CLI convenience. Recommend allowing both, with results labeled
by side and index and a combined exit status consistent with validation's
existing aggregation. Until ruled on, document the current mutual exclusion.

**Literal prevalidation versus identical carrier behavior (A7, C7).** A
prefers retaining upfront validation and fixing the guide; C prefers schema
checking in write order for all carriers. Neither invoker contract requires
the CLI to dispatch already-known invalid literals. The input-after-closure
rule governs values once an invocation is running; it does not force an
invalid literal request through the frontend. C's consistency concern is
valid, but its requested runtime change is a design choice, not an automatic
contract correction. Recommend retaining the useful no-effects refusal for
known literal inputs and making the literal/stream boundary explicit. Matt
can choose identical write-time checking instead. No finding is dismissed
merely because it involves an open proposal.

## Additional audit and post-freeze repair

While verifying findings, the parent found two errors in its new URI checker:

- `$id: "https://example.com/a b"` was accepted even though D-05 requires a
  well-formed URI-reference.
- Distinct IDs `https://user%41@example.com/a` and
  `https://userA@example.com/a` were incorrectly reported as a D-13 duplicate.

[Spec section 7.4 and D-13](openbindings.md#74-other-references) permit only
RFC 3986 section 5.2 resolution and empty-fragment removal before exact
comparison. Go's decoded userinfo exceeded that normalization. The lab now
checks the [RFC URI grammar and resolution algorithm](https://www.rfc-editor.org/rfc/rfc3986)
while retaining authored component spelling. Tests cover the RFC resolution
examples, invalid syntax, unknown bases, case, percent encodings, empty
queries, repeated slashes, and schema-position refusals. The lab suite passes.

This URI repair and the invocation guide clarification are **post-freeze**.
None of the round-5 reviewers assessed them. The frozen binary, guide, spec,
interfaces, pins, brief, and input checksums remain untouched.

## Validation and next boundary

Before freezing, the lab suite and build passed; all 61 guide commands ran
with expected exits (43 success, 9 refusal, 3 usage error, 6 negative result).
The URI correction passes the same lab suite. Nothing was pushed or merged.
This remains surface design in the preview, not production implementation.

Apply the verified queue and Matt's rulings in the lab, then create a new
round-6 freeze for another fresh independent review. Do not overwrite round 5
or claim its panel approved later changes.
