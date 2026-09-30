# Iteration 0.2-02: local kind abilities and source workflows

Spec pin: sibling `spec` at `ccfe0b6`. This iteration changes only the
kind-aware command lane. The 0.2 Core treats `kind` as an exact opaque string;
it does not define a registry, artifact location, import, pull, synthesis, or
invocation protocol. These are optional ob/tool capabilities. The hypothesis
is that putting local ability discovery at the root, and kind-aware source
work under `source`, will keep generic authoring unopinionated while making
useful workflows discoverable.

## Task cards written before the change

1. Determine whether this installation can inspect private kind
   `my-team.http@1`, without judging any OBI's conformance.
2. List exact kinds this installation can act on and which actions each
   supports. This is not a global registry.
3. Add a source named `api` with arbitrary JSON content when there is no
   installed handler for its kind.
4. Ask an installed handler to construct a source from `./api.yaml` under
   explicitly chosen kind `example.openapi@1`, then add it to `api.obi.json`.
5. Inspect source `api` in `api.obi.json` through its installed kind handler,
   separately from showing its stored bytes.
6. Refresh source `api` through its kind handler, without treating its
   content as a universal URL or file path.
7. Ask whether the installation could synthesize operation/binding entries
   from source `api`; do not assert Core can do it.
8. Try an unfamiliar kind whose document is valid but for which inspect is
   unavailable. The error must name the unavailable local action, not call
   the OBI invalid.

## Competing capability placements

| Local question | Kind candidate | Capability candidate |
| --- | --- | --- |
| List | `ob kind list` | `ob capability list` |
| One kind | `ob kind check K [--for ACTION]` | `ob capability check K [--for ACTION]` |

All other routes and flags are identical. `OB_SURFACE_VARIANT=capability`
selects the competing spelling; the default remains `kind`. Neither is an
alias in the same tree. Both list/check forms return local installed
capabilities, never conformance. The supported action names are `inspect`,
`import`, `pull`, `synthesize`, and `invoke`; an unavailable action is a
negative capability result, not a document defect.

## Shared kind-aware contract

| Task | Proposed path | Scope and output |
| --- | --- | --- |
| Construct from artifact | `ob source import <obi> <key> <input> --kind K` | ob tool asks installed K handler to make `source.content`; exact K is stored. `input` is an ob acquisition locator, never a Core source field. Result is a JSON OBI. Unsupported K/action is an ob capability error. |
| Interpret stored source | `ob source inspect <obi> <key>` | Handler-defined inspection report. `source show --full` remains the Core read path. No universal target or artifact graph is promised. |
| Refresh stored source | `ob source pull <obi> <key>` | Handler-specific pull/refresh of source content; result is a JSON OBI. No Core URL/path semantics are inferred. |
| Derive entries | `ob source synthesize <obi> <key>` | Handler-specific proposal for operation/binding entries. Proposed output is a machine-readable patch by default and an explicit apply mode for mutation; never claims inferred bindings are spec-verified. |

The preview accepts correct syntax and exits 2 with `no operation ran`.
Illustrative samples must identify themselves as samples. A supported kind
is never required for `source add`, `show`, or `validate`. Installed
capability samples may say unsupported, but cannot infer that from the kind
string alone. The phase-2 gate compares first-command choice and recovery
on the same tasks with separate blinded cohorts after parser/sample checks.

## Pilot outcome

The two trees expose the same 30 inert leaves, and all advertised examples
reach placeholders. The 0.2 schema gate includes the import, pull, and
applied-synthesis illustrative documents. Two fresh blind agents saw the
eight tasks without help; one then explored `kind`, the other `capability`.
This one-agent-per-candidate pilot is too small to justify a final name.

| Measure | `kind` | `capability` |
| --- | ---: | ---: |
| First command path | 2/8 | 3/8 |
| First full invocation | 0/8 | 0/8 |
| Eventually parsed with help | 8/8 | 8/8 |
| Operational false-success exits | 0 | 0 |

`capability` gained one source-workflow first path, not a local-ability
path. Both agents missed the local list/check spelling; one guessed
`kind inspect` and `kinds list`, the other `capabilities check/list`. The
singular noun and `check --for` shape need a larger naming trial. Keep
`kind` as the provisional default because it is the exact 0.2 Core term and
is shorter; retain `OB_SURFACE_VARIANT=capability` as a separate competitor.
The high-value source `import`/`inspect`/`pull`/`synthesize` routes were
found after help, but `pull` versus `refresh` and `synthesize` versus
`propose` remain first-command misses. None of these workflows should be
promoted as Core behavior or real operational support.
