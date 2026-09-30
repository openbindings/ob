# Iteration 0.2-06: installed source workflow verbs

Spec pin: sibling `spec` at `ccfe0b6`. Round 1 missed all four installed
source workflow verbs on first attempt, but several task cards used words
that primed the alternatives (`acquire`, `interpret`, `refresh`, and
`proposed`). This pass challenges the vocabulary with neutral, paired task
wording. It does not change the 0.2 Core model or make handlers mandatory.

## Hypothesis and task cards, recorded before changing the CLI

The current `import`, `inspect`, `pull`, and `synthesize` verbs may be less
predictable than `acquire`, `interpret`, `refresh`, and `propose`. Both trees
must keep the same arguments, flags, result types, inertness, and output
contracts. Each tree has exactly one spelling per action.

1. Make source `supplier` from `./supplier-openapi.yaml` using the installed
   `acme.openapi@1` handler.
2. Turn a local service definition file `./orders.yaml` into source `orders`
   using the installed handler for `acme.openapi@1`.
3. Ask the installed handler for a kind-specific report about existing source
   `catalog` in `warehouse.obi.json`.
4. Learn what the locally installed handler can tell you about the stored
   source `catalog`, beyond its raw JSON.
5. Update stored content of source `catalog` from the handler's external
   origin, retaining the same source key.
6. Reobtain the handler-managed content for source `catalog` without
   rebuilding unrelated OBI entries.
7. Generate a draft JSON Patch of operation and binding entries suggested by
   source `catalog`, leaving the OBI unchanged.
8. Generate and apply the handler's suggested operation and binding entries
   from source `catalog`.

The first two cards need an input file and kind. Cards 3–8 use only the OBI
locator and source key, except card 8 adds `--apply`. All use
`warehouse.obi.json`. The wording intentionally omits the eight competing
verbs and does not contain a command example.

| Current tree | Plain-workflow candidate |
| --- | --- |
| `source import OBI KEY INPUT --kind KIND` | `source acquire OBI KEY INPUT --kind KIND` |
| `source inspect OBI KEY` | `source interpret OBI KEY` |
| `source pull OBI KEY` | `source refresh OBI KEY` |
| `source synthesize OBI KEY [--apply]` | `source propose OBI KEY [--apply]` |

Candidate selector: `OB_SURFACE_VARIANT=plain-workflows`. Deterministic
gates run before a blind comparison. Each independent reviewer seals the
first path and full invocation before help, then uses only the assigned CLI.
The outcome will decide whether to promote the family, retain the current
family, or redesign the information architecture. A small pilot cannot by
itself satisfy the 30-task stop gate.

## Pilot outcome

The two reviewers had no repository or source access. They sealed their
first commands, then used only the frozen inert executable and at most two
help/error hops for each card. The candidate's source help ordering was
imperfect because the renamed leaves retained their old sort positions;
this can affect recovery but not sealed first guesses.

| Metric | Current verbs | Plain-workflow verbs |
| --- | ---: | ---: |
| Semantically correct first paths | 3/8 | 1/8 |
| First complete invocations parsed | 0/8 | 0/8 |
| Recovered within two help hops | 8/8 | 8/8 |
| Operational false-success exits | 0 | 0 |

Both reviewers initially chose `source add` for one handler-construction
goal. That path exists but is **semantically wrong**: generic `add` stores
caller-supplied kind/content and does not run a handler. It is therefore not
a first-path hit. Both groups frequently invented `--obi` and `--name`
flags where the CLI uses positional `<obi> <key>` values. The current group
guessed `import`, `inspect`, and `pull` once each. The plain group guessed
`refresh` once; its other guessed verbs included `import`, `inspect`,
`update`, `plan`, and `apply`.

**Decision:** retain `import`, `inspect`, `pull`, and `synthesize` in the
default tree. The alternative remains selectable for comparison, with its
help ordering corrected after this frozen trial. The result gives no basis
for adding synonyms or claiming better first-command usability. The
neutral wording also shows that round 1's cards sometimes primed specific
alternative verbs; the full-round first-path score remains a valid stop
failure, but it is not a reliable ranking of those two verb families.
