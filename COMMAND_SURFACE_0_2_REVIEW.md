# Command-surface review against the OpenBindings 0.2 draft

Review basis: the local `spec` repository at `ccfe0b6` on `release/0.2`
(2026-09-26), especially `openbindings.md`, `openbindings.schema.json`, and
`MIGRATING-0.1-TO-0.2.md`. The 0.2 spec is an unreleased working draft; the
latest release remains 0.1.0. This is a review of the inert hybrid facade,
not a claim that any command operates correctly. The previous surface review
assessed an earlier 0.2 model and its scores no longer measure fidelity to
this draft. No fresh 0.2 first-command trial was run for this review.

## Verdict

The command skeleton remains useful: direct `new`, `show`, `validate`,
`operation`, `source`, `binding`, `dependency`, `patch`, and `invoke` paths cover
recognizable tasks. The current facade is **not yet a credible 0.2 interface**.
Its primary source, binding, and dependency authoring forms describe removed
fields; its illustrative 0.2 OBI fails the current structural schema; and
`validate` help now misstates a core conformance rule. This is a version-model
reset, not a reason to discard the whole tree.

## Findings, in priority order

| Priority | Finding | Evidence in facade | 0.2 consequence and direction |
| --- | --- | --- | --- |
| P0 | The showcased `0.2.0` OBI is structurally invalid. | `sampleOBI()` writes source `bindingSpec`/`location` and binding `selector`; `source show --full` repeats them. `TestSurfaceFullSampleIsConformantOBI` checks against the older `openbindings-go` dependency. | A 0.2 source requires `kind`; source and binding have closed core fields. Validate every sample against the pinned 0.2 schema and semantic rules, and do not call it conformant based on the old SDK. |
| P0 | Generic authoring cannot express the new core shape directly. | `source add` teaches `[format:]path`, top-level `location`/`content`, and old `bindingSpec` machine input. `binding add` requires the generic `--selector` concept and offers `--input-transform`/`--output-transform`. `operation bind` also requires a selector and offers transform stubs. `dependency add` uses repeated `--binding-spec`. | Core uses source `kind` plus optional arbitrary JSON `content`, binding optional arbitrary JSON `content`, and dependency `kinds`. Give each a lossless first-class authoring path, including omitted content versus explicit JSON `null`. Keep artifact detection and target picking as kind-aware conveniences. |
| P1 | Validation help teaches a false conformance boundary. | `validate --strict` says unknown non-`x-` fields are only extra lint; help still promises transform validity and “operation kinds.” | Unknown non-`x-` fields already violate OBI-D-02 in 0.2; transforms are removed and operations do not have kinds. Base validation should report the applicable 0.2 document rules, distinguish non-conformant, undetermined, and version refusal, and not fetch external resources to decide document conformance. Any additional ob lint needs its own clearly named check. |
| P1 | The installed-capability utility is named for the superseded document vocabulary and returns too little information. | `source binding-specs list/check` and the root recovery hints ask for binding-specification IDs and a yes/no result. | The core token is an exact opaque `kind`. Unsupported kinds do not make a document non-conformant. A local utility may report support, but it should answer what this installation can **do** with a kind (for example inspect, synthesize, or invoke), without implying a core registry or a published specification exists. Prototype `kind`/`support` command placements; do not gate generic authoring or validation on installed support. |
| P1 | Compatibility and conformance authoring still read like the removed 0.1 comparison model. | `compat` describes “Method input,” “Method output,” and “Event payload”; `conform` proposes schema replacement; `synthesize` cites OBI-D-05 as though it governed a source `location`. | Core 0.2 defines no comparison, matching, or selection algorithm. These can remain valuable **ob policies**, but need a separately specified policy version and per-value semantics. OBI-D-05 now governs OBI-defined schema references, not source content. |
| P2 | Runtime and source-maintenance help assumes one universal artifact/URL/selector model. | `source inspect/pull` promise bindable targets from a “format”; `context set` describes URL-scoped automatic matching; `binding invoke` describes removed transforms. | Retain these as optional ob workflows where a kind and installed capability make them meaningful. Help should state that source/binding content, target identity, interaction, adaptation, and runtime context are kind/tool concerns, not core semantics. |

The `patch` escape hatch can represent the new document shape, but it does not
make the main authoring commands truthful or easy to discover. Several good
decisions survive: independent binding keys, neutral operations, named
dependencies, alias support, exact preference bounds, explicit ob-policy
labeling on `compat`, JSON artifact output, and a distinct `conformance
undetermined` exit proposal.

## Proposed 0.2 authoring contract to prototype

These are design sketches, **not implemented commands**:

```sh
ob source add api.obi.json api --kind example.openapi@1 --content @source-content.json
ob binding add api.obi.json get.http --operation get --source api --content @binding-content.json --preference 10
ob dependency add api.obi.json billing --operation charge --kind example.openapi@1 --kind private.rpc@1
ob validate api.obi.json -F json
```

`--content` would accept any JSON value, including `null`; omitting it would
omit the member. `--kind` would accept any non-empty exact string, with no
registry, spelling decomposition, or implied support check. A separate
artifact-import convenience may detect a kind and construct its content; it
must not define a universal meaning for a path or URL in source content.
The installed-kind capability lookup should be evaluated as a separate task,
possibly with a capability selector, before choosing its final name or home.

## Provisional grade against this draft

The same weights as the earlier internal rubric give **5.8/10** for the
current hybrid proposal. These are design-review scores, not runtime or fresh
blinded-UX results.

| Criterion | Score | Reason |
| --- | ---: | --- |
| Spec fidelity | 2.0 | Primary examples and authoring forms conflict with current 0.2 fields. |
| Core model expressivity | 5.5 | `patch` is lossless, but direct source/binding/dependency commands are not. |
| Command value and coverage | 7.5 | Most task areas remain useful if their contracts are rewritten. |
| Names and hierarchy | 7.4 | Carried over provisionally; new kind terminology needs a fresh trial. |
| Help and onboarding | 4.0 | Clear prose currently teaches superseded concepts and false validation claims. |
| Output and composability | 7.5 | The lanes are coherent, while illustrative documents and machine inputs are stale. |
| Error clarity and safety | 6.0 | Good path recovery, but wrong 0.2 guidance and invalid samples are material. |
| Presentation and density | 7.5 | Task grouping survives, though source support is buried under the wrong noun. |
| Scripting and exits | 7.0 | Exit design is useful; version refusal and capability-scoped reports need contracts. |
| Alias and migration policy | 10.0 | Canonical-path discipline remains sound. |

**Internal S-tier gate: failed.** Before another blind trial, revise the
facade's 0.2 authoring contract and samples, make validation claims accurate,
and write the ob-specific comparison/capability policies. Then test first
command choice with fresh tasks that include a private kind, absent versus
null content, an unsupported kind, a dependency with two kinds, and an
inconclusive validation result.
