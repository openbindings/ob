# Iteration 0.2-03: exact invocation and structural comparison

Spec pin: sibling `spec` at `ccfe0b6`. This is still an inert CLI facade.
The 0.2 Core enables acting on bindings but defines no invoker, automatic
binding selection, interaction pattern, failure vocabulary, runtime context,
or compatibility algorithm. The hypothesis is that an exact-binding invoke
lane can be useful without claiming any of those Core semantics. A purely
structural diff is a separate document utility; it must not be called
compatibility.

## Task cards written before the change

1. Act on the exact binding `get.http` in `api.obi.json`, sending one
   caller-facing JSON input value `{"id":1}`.
2. Send explicit JSON `null` to that binding; distinguish it from omitting
   `--input`.
3. Stream caller-facing input values from stdin only if the handler supports
   a stream interaction; report result events without confusing stderr
   diagnostics with value output.
4. Ask for an exact binding that exists in a conformant OBI but whose kind
   this installation cannot invoke. Report unsupported local capability,
   not document non-conformance.
5. Compare `before.obi.json` and `after.obi.json` for structural changes,
   without declaring one compatible with the other.
6. Try `-o` and `-F yaml` with invocation. A stream's stdout contract must
   reject both instead of silently switching format.
7. Handle an unsupported 0.2 document version distinctly from validation
   failure and from invocation failure.
8. Decide whether `compat` or `conform` belongs in the default tree before
   a separately versioned ob policy defines what they mean.

## Competing placement

| Task | Binding-first | Direct-action |
| --- | --- | --- |
| Exact invocation | `ob binding invoke <obi> <binding-key>` | `ob invoke <obi> <binding-key>` |

Both accept exactly the same input and context flags and emit the same
machine stream. The binding-first trial used `OB_SURFACE_VARIANT=object` and
the direct-action trial used `OB_SURFACE_VARIANT=invoke-root`. There is one
canonical invocation path per tree; no operation-level auto-selection path
or alias is added. The binding key is required, so the CLI never invents a
selection policy. The locally installed kind handler still decides if and
how it can invoke the binding's target.

## Proposed tool contracts

| Command | Result and behavior |
| --- | --- |
| `invoke` candidate | `--input JSON\|@file\|-` supplies exactly one caller-facing JSON value; omission supplies no caller-facing value. `--input-stream -` supplies NDJSON values only for a handler advertising stream input. The two flags are mutually exclusive. `--context NAME` selects local ob configuration; it is not an OBI reference and samples show no secrets. |
| Invocation stdout | NDJSON envelopes, one object per line, with `type`, `value` or `error`, and `binding`; `complete` marks finished interaction. This is an ob protocol proposal, not Core. Diagnostics and sample disclaimers go to stderr. `-F` and `-o` are rejected on invoke; redirection is the capture mechanism. |
| `ob diff <before> <after>` | Exact JSON document difference only. Text by default; `-F json` gives a machine report. No compatibility, semantic equivalence, or binding selection claim. |

Planned real exits: 0 completed; 2 usage; 3 non-conformant OBI; 4 missing
local kind capability; 5 invocation or acquisition failure; 6 partial stream
after one or more events; 7 unsupported specification version refusal.
The facade executes nothing: all correctly shaped ordinary calls exit 2
with a placeholder message, and `--sample-output` is visibly illustrative.
Exit categories are policy proposals to test, not implemented behavior.

`compat` and `conform` are excluded from this iteration's tree. They would
need versioned ob policies with explicit per-value, name-resolution, and
kind-specific rules. A placeholder spelling before that contract would make
their apparent value larger than what the draft warrants. Persistent
contexts and codegen are likewise not added merely to grow command count;
the invocation flag states the narrow context boundary for later testing.

## Pilot outcome and decision

Two fresh agents recorded first guesses on the eight tasks before CLI
access. Each then used only one candidate's inert binary help/errors. The
direct root path was much easier to guess; the exact binding key position
and stream flag still need a wider trial.

| Measure | Binding-first | Direct `invoke` |
| --- | ---: | ---: |
| First command path | 2/8 | 6/8 |
| First full invocation | 1/8 | 3/8 |
| Operational false-success exits | 0 | 0 |

**Choose `ob invoke <obi> <binding-key>` as the default path.**
`OB_SURFACE_VARIANT=binding-invoke` preserves the losing shape as a separate
candidate, not an alias. The default tree has no automatic operation-level
selection. The trial exposed a concrete missing task: checking local
invocability by binding key cannot be done directly with `kind check`
without first finding its source kind. It also confirmed that `compat` and
`conform` cannot be soundly advertised from the current 0.2 Core alone.

The facade now has a machine-readable result/destination contract for every
leaf and an illustrative output for every leaf. Invocation rejects `-o`
and `-F`; its help omits those inherited flags. OBI-writing routes reject
`-F text` and reject the ambiguous `-F yaml -o` combination. This remains
an internal pilot. Neither candidate reaches the full stop gate, and no
operation or comparison runs.
