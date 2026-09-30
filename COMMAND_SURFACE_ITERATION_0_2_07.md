# Iteration 0.2-07: author-attested idempotency flag

Spec pin: sibling `spec` at `ccfe0b6`. Round 1's two blind reviewers both
chose the correct `operation add` path for a task with an idempotency claim,
but both wrote bare `--idempotent`. The facade currently declares it as a
string requiring `true` or `false`, unlike the Boolean `--deprecated` flag.
The hypothesis is that a Boolean flag with a presence bit expresses all
three Core states—absent, true, and false—while making the true case natural.

## Task cards before the CLI change

1. Define `findItem` in `warehouse.obi.json`; attest that repeats have the
   intended same effect.
2. Define `charge` in the same OBI; explicitly attest that repeats do not
   have the intended same effect.
3. Define `health` without making any idempotency claim.
4. Attempt a non-Boolean idempotency value and get a usage diagnostic.

Both candidates must emit the exact 0.2 optional Boolean member. Omission
must omit it, and explicit false must remain distinguishable from omission.

| Current `idempotent-value` | Candidate default |
| --- | --- |
| `--idempotent true` | `--idempotent` or `--idempotent=true` |
| `--idempotent false` | `--idempotent=false` |
| omit flag | omit flag |

The candidate changes only parser/help shape; the facade remains inert.
`OB_SURFACE_VARIANT=idempotent-value` preserves the old form for a focused
comparison. Every sample must still pass the pinned 0.2 schema and local
relationship checks.

## Pilot outcome and decision

Two fresh reviewers sealed four first invocations each, then explored one
assigned inert tree. This pilot is **inconclusive as a first-hit comparison**:
the Boolean-tree reviewer guessed `operation define` for all cards, so its
calls never reached the flag parser; the value-tree reviewer guessed
`operation add ... --idempotent true|false`, matching that tree exactly.
Both recovered to the intended syntax within two help hops. Neither tree
produced an operational false success. The value tree's illustrative samples
confirmed true, false, and absent members; the Boolean tree's parser accepted
bare true and explicit false and rejected `--idempotent=maybe`.

The two reviewers in the earlier 30-task round **both** chose the correct
`operation add` path but used bare `--idempotent` on their first invocation.
That prior observation, the spec's optional Boolean type, and consistency
with the existing Boolean `--deprecated` flag favor the Boolean shape.
**Retain the Boolean flag as the default** and the string-value form only as
an explicit comparison variant. A deterministic tri-state sample gate now
checks that omitted, true, and false stay distinct.
