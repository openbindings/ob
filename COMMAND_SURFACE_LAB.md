# ob command-surface lab

This branch is an executable, **inert** OpenBindings 0.2 CLI proposal. Its
standalone `NewV02SurfaceRoot` tree does not import production `NewRoot` or
run operational handlers. Valid ordinary calls parse and exit 2 with
`command-surface placeholder; no operation ran`. Illustrative output uses
`--sample-output`, warns on stderr, and never reads or writes a locator.

The 0.2 source of truth is the unreleased sibling spec draft at commit
`ccfe0b6` on `release/0.2`. Public 0.1 documentation and the older Go SDK
still describe a different source/dependency model. The current 0.2 samples
are checked against the draft schema independently of that SDK.

## Explore the current proposal

From this worktree:

```sh
GOWORK=off GOCACHE=/private/tmp/ob-surface-gocache go build -o ./bin/ob ./cmd/ob
./bin/ob --help
./bin/ob init api.obi.json --name "Acme API" --sample-output
./bin/ob source add --help
./bin/ob source import --help
./bin/ob invoke --help
./bin/ob source add api.obi.json api --kind my-team.http@1 --content null --sample-output
./bin/ob invoke api.obi.json get.http --input '{"id":1}' --sample-output
./bin/ob show api.obi.json --json --sample-output
./bin/ob source list api.obi.json --full --json --sample-output
```

The default starts with `init` and uses object-first authoring: `source add`, `binding add`,
`dependency add`, `operation add`, and `schema add`. Sources carry exact
opaque `kind` strings and arbitrary optional JSON `content`; bindings have
optional arbitrary `content`; dependencies have repeatable any-of `--kind`.
`source import`, `inspect`, `pull`, and `synthesize` are optional installed
kind workflows, separate from generic authoring. `kind list/check` reports
local tool ability, not OBI conformance. `invoke` acts on one exact binding
through an installed handler. `diff` reports structural changes only.
`show` returns complete stored values by default, preserving absent versus
explicit `null`; `show --summary` opts into a short overview. `--json` is an
explicit machine-output spelling for reports. `source list --full` returns
the exact stored source map instead of its brief key/kind list.

Other candidate trees are selected with an environment variable:

```sh
OB_SURFACE_VARIANT=action ./bin/ob --help
OB_SURFACE_VARIANT=capability ./bin/ob --help
OB_SURFACE_VARIANT=binding-invoke ./bin/ob --help
OB_SURFACE_VARIANT=new ./bin/ob --help
OB_SURFACE_VARIANT=read-summary ./bin/ob show --help
OB_SURFACE_VARIANT=plain-workflows ./bin/ob source --help
OB_SURFACE_VARIANT=idempotent-value ./bin/ob operation add --help
OB_SURFACE_VARIANT=task-language ./bin/ob --help
```

`action` moves add leaves to `add source`, `add binding`, and so on.
`capability` renames the root `kind` group. `binding-invoke` nests exact
invocation under `binding`. `new` preserves the earlier creation form for
comparison against default `init [<obi|->]` or `init -o <obi|->`.
`read-summary` preserves summary-first reads. `plain-workflows` compares
alternative installed-handler verbs; a neutral-wording pilot favored the
default verbs.
`idempotent-value` preserves the previous string-valued flag for comparison;
the default Boolean flag distinguishes omitted, true, and false.
`task-language` puts document actions under `document`, renames `binding`
to `realization`, `dependency` to `consumption`, and local `kind` abilities
to `handler`. A four-reviewer blind pilot found no first-contact advantage,
so the Core-noun tree remains the default.
These are competing trees, never aliases in one tree. The legacy 0.1-based
`broad`, `compact`, and `hybrid` previews remain explorable through the same
environment variable for historical comparison; they are not 0.2 candidates.

## Review loop and current evidence

`COMMAND_SURFACE_LOOP_0_2.md` defines the contract, deterministic, blind,
and stop gates. Iteration records are in
`COMMAND_SURFACE_ITERATION_0_2_01.md` through `_08.md`. The formal 30-task
round is recorded in `COMMAND_SURFACE_ROUND_0_2_01.md`. The 0.2 reset audit
is `COMMAND_SURFACE_0_2_REVIEW.md`, and the current internal grade and stop
decision are in `COMMAND_SURFACE_0_2_CURRENT_REVIEW.md`. The post-fix blind
diagnostic is in `COMMAND_SURFACE_FRESH_BLIND_0_2_01.md`. Earlier 0.1-era reviews and trials are
retained as history, not current scores.

Run the deterministic 0.2 gate with:

```sh
GOWORK=off GOCACHE=/private/tmp/ob-surface-gocache go test ./internal/cmd -run TestV02 -count=1
```

The current facade has not passed the internal S-tier stop rule. Blind
pilots found meaningful first-command misses, especially local kind ability
lookup and source refresh/proposal names. No outside human review has been
requested or run. The draft has no Core compatibility or automatic binding
selection algorithm, so `compat` and `conform` are not advertised without
a separate versioned ob policy.
