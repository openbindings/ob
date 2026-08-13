# Generic OBI-to-MCP projection

`ob mcp` adapts the OpenBindings operation boundary to MCP's tool boundary.
It does not inspect or imitate the operation's original protocol. An OpenAPI,
gRPC, GraphQL, AsyncAPI, Connect, usage, or operation-graph binding therefore
uses the same projection once the selected binding has produced the abstract
OpenBindings input/output values.

This contract is intentionally separate from the legacy
[MCP-origin round-trip lane](mcp-round-trip.md). An exact, untransformed
`openbindings.mcp@1` binding can preserve native MCP primitive families and
result objects for compatibility. The latest `openbindings.mcp@1` is already
an application-level operation contract, so it uses this generic projection,
as does every non-MCP binding.

## Admission and binding selection

An operation is **excluded** only when it cannot be routed honestly by the
installed invokers — i.e. invoking it would fail regardless of caller choice:

- an operation with no binding is excluded;
- an operation whose every binding uses a specification with no installed
  invoker is excluded; and
- an operation whose sole binding references a missing source is excluded.

An operation with **several invocable bindings is advertised, not dropped.**
OpenBindings deliberately leaves binding *selection* to the consuming
application (core invariant 2); the bridge is that application, and it resolves
the choice by a spec-loyal deference order — honor, then expose, then refuse
loudly, never silently drop and never silently invent a default:

1. **Honor** an explicit `context.configuration.selection` (repeatable
   `--select-binding <binding-key>`), first-invocable wins — unchanged.
2. **Honor** an author-declared `preference` (spec §5.3): when *every*
   invocable binding of the operation declares one and there is a unique
   maximum, that binding is auto-selected. (All-declared is required because
   §5.3 states omission is not equivalent to zero, so a partial declaration
   cannot be ranked soundly.) A well-authored multi-binding interface therefore
   bridges with zero configuration.
3. Otherwise the call is **refused loudly**: a **structured, pre-dispatch
   error** (`ERR_BINDING_SELECTION_REQUIRED`, `data.bindings` listing the
   keys) in the MCP channel the client actually observes, not on stderr. The
   agent recovers by re-calling with `_binding` set. An unknown `_binding`
   value returns `ERR_UNKNOWN_BINDING` with the same list.

**Honor *and* expose — not honor *or* expose.** Whenever an operation has more
than one candidate binding, its tool carries the optional **`_binding`**
argument enumerating the exact valid keys — *including* when steps 1–2 produced
an automatic choice. In that case the argument's description names the binding
the caller gets by omission, and an explicit `_binding` always outranks the
automatic choice.

This is a fidelity requirement, not a convenience. §5.3 calls `preference` a
*preference*, and the core specification deliberately defines no selection
algorithm. A bridge that acted on that signal while concealing the alternatives
would quietly promote an author's signal into a mandate the author never
declared, and would leave a caller unable to see which of several protocols it
is about to dispatch over. Acting on a declaration is loyal; foreclosing the
choice is not.

`--select-binding` remains available for launch-time selection and startup
logs still report admission on stderr, but stderr is never the *only* place a
refusal appears. If no operation can be advertised at all, startup
fails instead of presenting an unusable server.

This is structural readiness, not a promise that credentials are already
available or that the remote service is healthy. Those facts can vary between
calls.

## Input projection

MCP tool arguments always have an object root. OpenBindings operation inputs
are per-value JSON Schemas and may describe any JSON value.

- An input schema whose resolved root is explicitly `type: object` remains the
  tool input schema. The MCP arguments object is the OpenBindings input value.
- Any other input schema is preserved under one optional adapter member:
  `{"input": <value>}`. For example, an array input is called with
  `{"input": [1, 2]}`.
- An omitted operation input contract uses the same envelope with an
  unconstrained `input` value.

The envelope member is optional because an operation schema constrains each
input value but never asserts that a value exists. `{}` sends no OpenBindings
input value; `{"input": null}` sends one explicit JSON `null` value. This also
means `input: false` permits the zero-input form while rejecting every explicit
input value. The bridge does not coerce a scalar or array contract into an
object contract or invent protocol-family parameter rules.

For a direct object schema, an omitted MCP `arguments` member sends no input
value; an explicit arguments object, including `{}`, sends one object value.

Each MCP tool call can supply at most one OpenBindings input value. This is an
MCP request-shape limit, not a claim about the binding: client-streaming and
bidirectional bindings remain invocable through OpenBindings consumers that
can drive the full input stream.

## Output and error projection

OpenBindings deliberately keeps cardinality out of the operation. The selected
binding decides whether an invocation produces zero, one, many, or an
unbounded sequence of output values. The bridge therefore never guesses that a
particular binding family is unary.

Every successful generated-tool result has this stable shape:

```json
{
  "outputs": []
}
```

`outputs` is the complete ordered sequence, and its `items` schema is the
operation's per-value output schema. Zero, one, and many outputs remain
distinguishable; an explicit `null` output remains `[null]`, not an empty
sequence. The same JSON object is supplied as MCP structured content and as
JSON text content for clients that only consume text.

If an invocation emits values and then fails, already-emitted values are not
discarded:

```json
{
  "outputs": [{"partial": true}],
  "error": {
    "code": "ERR_RESPONSE_ERROR",
    "data": {"offset": 1}
  }
}
```

The MCP result is marked as an error, and the OpenBindings error code and
optional application-authored data are preserved. An invocation that does not complete before
`--tool-timeout` is cancelled and returns the same error form with any partial
outputs. This makes terminating server streams usable while refusing to
pretend an unbounded subscription can complete as a request-scoped MCP tool.

## Invocation context

The bridge does not guess credential carriers or binding-spec interpretation
points. Supply them explicitly:

```bash
ob mcp api.obi.json \
  --context @context.json \
  --configuration @binding-configuration.json \
  --select-binding orders.grpc
```

`--context` accepts the SDK context vocabulary, including headers, cookies,
named API keys, basic credentials, bearer credentials, and extensions.
`--configuration` is merged into its `configuration` member.
`--token`, `--token-file`, and `OB_TOKEN` are bearer-token conveniences.
Conflicting declarations are refused rather than overwritten. Because stdio
is the default MCP transport, `-` is not accepted for these JSON flags; use an
`@file`. The interface locator likewise cannot be `-` with stdio transport,
because that same stream carries the MCP session; use a file/URL locator or
`--transport http`.

## Raw-artifact synthesis evidence

When the locator is a raw artifact rather than an OBI, acquisition retains the
synthesizer's coverage evidence. `ob mcp` logs whether the source inventory was
exhaustive and fully represented and reports every alternative or exclusion.

- `--coverage-report <file>` writes the durable evidence as JSON.
- `--require-complete-coverage` refuses startup unless coverage is both
  exhaustive and fully represented.

These controls answer whether synthesis represented the upstream artifact.
They are independent of the generic projection contract, which answers how
the input/output values that fit MCP's request shape cross without additional
semantic guesses and explicitly identifies the interaction shapes that do not.
