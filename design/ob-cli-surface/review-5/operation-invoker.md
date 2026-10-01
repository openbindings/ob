# Operation Invoker

An operation invoker invokes an operation described by an OpenBindings interface document. Given an interface and a **key** — an operation key, or a specific binding key — it dereferences that key against the document, selects a binding, validates against the operation's schemas, applies its transforms, and drives the underlying [binding invoker](../binding-invoker/).

This is a reusable invocation contract, not a requirement of core OpenBindings. Conformance is claimed and versioned independently of core conformance and of every binding specification.

It is the **by-reference** peer of the binding invoker. The binding invoker invokes *by value* (a self-contained `source + selector`, no document); the operation invoker invokes *by reference* (an interface plus a key it resolves). Both share one frame protocol and bottom out in the same wire engine. They differ only in how the call is addressed.

## By value vs by reference

This axis, not "binding vs operation," is the real distinction between the two interfaces:

| | Binding invoker | Operation invoker |
|---|---|---|
| **Addresses by** | `source` + `selector` (the realization itself) | `interface` + `operation` **or** `binding` key |
| **Needs an OBI?** | No | Yes (the key is meaningless without the document) |
| **Knows the schemas?** | No, values are opaque | Yes, validates input/output, applies transforms |
| **Selects a binding?** | No, it's given one | Yes (when addressed by operation key) |
| **Context** | Consumes supplied context and may challenge for missing requirements | Forwards supplied context and propagates binding challenges |

"Invoke a binding by key" lives here, not in the binding invoker, because **keys need the document**, and the document is this interface's whole premise.

"By reference" names how the call is addressed — a key, resolved against a document — not how the document travels. The `interface` in the open frame is the document itself, carried inline, never a pointer into a store or registry. An interface that is stored nowhere (synthesized mid-pipeline, held only in memory) invokes exactly like a published one.

Because the document is a value, the caller also decides how much of it to send. Nothing requires the full document: a slice that keeps the top-level fields, the operation being invoked, and everything it transitively references — its bindings, their sources, the reachable schemas and transforms — is itself a valid OpenBindings interface, and the key resolves against it to the same binding, the same schemas, the same transforms. A caller invoking one operation of a large document may slice before sending; the invoker has no way to know a fuller document existed. When the invoker is remote, the slice is also a boundary: the far side sees the operation it is performing, not the caller's whole interface.

## What an operation invoker does

When it receives an `OperationInvocationInput` (carried by the `open` frame), it:

1. **Resolves the key.** An `operation` key resolves to the operation and a selected binding; a `binding` key resolves to that binding, and the operation is derived from it.
2. **Resolves a binding** (operation-key case). The candidate set is the operation's bindings whose governing binding specification the invoker can act on. The contract follows caller policy and binding-specification authority without inventing a ranking:
   - an explicit `binding` key is used directly;
   - when the caller supplies an ordered `selection`, the first listed binding the invoker can act on is used. The list passes over only what the invoker cannot act on, the one fact a caller may not know. Every listed key must name one of the operation's bindings, and the list never falls back to a binding it does not name: a key that names no binding of the operation, or a list with none the invoker can act on, fails with `ERR_BINDING_NOT_FOUND`;
   - without `binding` or `selection`, a sole invocable candidate is used;
   - zero candidates fail with `ERR_BINDING_NOT_FOUND`;
   - several candidates fail with `ERR_BINDING_SELECTION_REQUIRED`.

   `preference`, `deprecated`, key order, source order, and implementation registration order do not silently choose among alternatives. An application may apply any policy it owns, then express the result through an explicit binding or an ordered selection.
3. **Validates and transforms.** Input values are validated against the operation's input schema, outputs against its output schema (where declared), and the binding's input/output transforms are applied. This is the layer the binding invoker lacks. Validating is a claim, and the claim carries the core's semantics ([OBI-T-16](https://github.com/openbindings/spec/blob/main/openbindings.md#103-tool-rules)): success only against the complete statically reachable schema graph, `format` as annotation, per value — a mismatch is `ERR_OPERATION_VALIDATION_FAILED`, an unresolvable schema graph is reported distinctly, and neither is ever papered over with partial validation.
4. **Drives the binding invocation,** forwarding caller context down. It preserves the binding invoker's *frame sequence* — the same `output` / `input_closed` / terminal shape, one-for-one — but the output *payloads* it relays are the values after the operation's output transform and output-schema validation have run (step 3 is applied to this stream, not bypassed). An unsuccessful terminal frame remains unsuccessful, interface-owned data such as a `CONTEXT_REQUIRED` challenge remains intact, and an opaque application-authored failure value in `data` is relayed unchanged without protocol reinterpretation, output transformation, or output-schema validation. Binding-native evidence does not cross this boundary. The frames this layer may add are terminal ones of its own mechanics, such as `ERR_OPERATION_VALIDATION_FAILED` when an output fails the schema claim. "Relayed" means the envelope and ordering are the binding's; the carried values are this layer's transformed, validated ones.

## The frame protocol

`invokeOperation` is a typed bidirectional I/O operation. The caller streams `OperationInvokerInputFrame` messages (one `open` carrying the `OperationInvocationInput`, then zero or more `input` frames, then `close`); the invoker streams `OperationInvokerOutputFrame` messages back (zero or more `output` / `input_closed`, then exactly one terminal `complete` or `error`). The same shape covers unary, server-streaming, client-streaming, and bidirectional bindings; cardinality is observed by how the caller drives the frames, not declared.

The frame protocol and **every normative frame rule** are identical to [`binding-invoker.invokeBinding`](../binding-invoker/) — first-frame-`open`, single-`open`, input-after-closure handling, exactly-one-terminal, transport-closure synthesis, discriminator dispatch, `additionalProperties` rejection, and caller-cancellation all apply here unchanged. The operation invoker adds the resolution, validation, and transform layer on top of that shared contract.

## Context is forwarded, not reinterpreted

The operation invoker forwards the supplied context to the resolved binding invocation. A `CONTEXT_REQUIRED` challenge from that invocation that the implementation does not resolve propagates unchanged, so a caller can resolve the challenge and start a new operation attempt without learning protocol-specific details. Resolution failure is a local runtime failure, not a declined challenge, and an unchanged resolver result does not trigger another attempt.

The contract does not prescribe where resolution runs, what triggers it, or whether context is stored. A monolithic runtime may compose selection, resolution, and protocol invocation in one process; a distributed system may place them in separate services. The observable requirement is the same: the operation layer does not reinterpret binding-specific context and does not broaden the challenge's scope.

`CONTEXT_REQUIRED` is a negotiation signal, and its position is load-bearing: it arrives **before any `output` frame and before any observable operation side effect**, so a new attempt restarts a call that never happened. A binding may re-challenge after supplied context proves unusable only while its governing rules can still prove that boundary; a native failure status is not sufficient evidence by itself. A necessary consequence is that context cannot be renegotiated **mid-stream**: once a streaming invocation has emitted outputs, a new requirement cannot surface as `CONTEXT_REQUIRED` on that same stream. An implementation may refresh expiring context internally; otherwise the invocation ends and a new one begins.

One well-known context field rides through this layer: **`configuration`**, an
object keyed by configuration-point name. This interface defines no
configuration point; binding specifications define them, and each defining
specification owns the value's meaning and consultation rules. Choosing a
binding is not configuration: it is the caller's instruction for one call,
carried by `binding` or `selection` in the invocation input.

## Operation-invoker-owned errors

This interface owns only the codes required by its resolution, validation,
and transform mechanics. Their spellings and meanings are reserved:

| Code | Meaning |
|---|---|
| `ERR_OPERATION_NOT_FOUND` | The requested operation key or alias does not resolve. |
| `ERR_BINDING_NOT_FOUND` | The explicit binding does not exist, or no invocable binding remains for the operation. |
| `ERR_BINDING_SELECTION_REQUIRED` | Multiple invocable bindings remain and the caller supplied no effective choice. |
| `ERR_UNKNOWN_SOURCE` | The selected binding references no source in the supplied interface. |
| `ERR_OPERATION_VALIDATION_FAILED` | An input or output value violates the operation's governing schema. |
| `ERR_SCHEMA_UNRESOLVED` | The complete statically reachable governing schema graph cannot be established, so validation cannot be claimed. |
| `ERR_TRANSFORM_ERROR` | An operation input or output transform cannot be applied successfully. |

These outcomes are code-only: this interface defines no `data` for them.
Binding-invoker-owned failures and governing-binding-specification-owned
failures relay unchanged. Any other implementation code remains non-portable
under this interface; in particular, this list is not a general failure
vocabulary for bindings or protocols.

### preflightOperation

`preflightOperation` resolves the named operation or binding with invocation's selection rules and preflights the selected binding under the [binding-invoker contract](../binding-invoker/README.md#preflightbinding). A resolution failure completes with this interface's own resolution code and is not the binding's answer. Preflight does not pin a later selection.

## What an operation invoker must NOT do

- **Prescribe context storage or resolution architecture.** It forwards context and propagates challenges; composition around that exchange is external.
- **Invent a binding choice.** Several invocable alternatives require an explicit caller-owned choice.
- **Reimplement the wire.** It drives a binding invoker; it does not speak protocols directly.
- **Bake in cardinality.** The signature never declares unary vs streaming; cardinality is observed at the frames.
- **Mutate the caller's input.** Context forwarding and enrichment operate on a copy.

## Relationship to the binding invoker

The operation-invoker semantics compose with the binding-invoker semantics: after a key resolves to `(source, selector)`, the remaining behavior is binding invocation plus the operation's validation and transforms. An implementation may literally layer the two components or fuse them behind one service. Publishing both interfaces reflects two genuinely different ways to address a call — by value and by reference — not a required process architecture.
