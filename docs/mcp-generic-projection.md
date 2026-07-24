# Generic OBI-to-MCP projection

`ob mcp` adapts the OpenBindings operation boundary to MCP's tool boundary.
It does not inspect or imitate the operation's original protocol. An OpenAPI,
gRPC, GraphQL, AsyncAPI, Connect, usage, or operation-graph binding therefore
uses the same projection once the selected binding has produced the abstract
OpenBindings input/output values.

This contract is intentionally separate from the
[MCP-origin round-trip lane](mcp-round-trip.md). An exact, untransformed
`openbindings.mcp@1` binding can preserve native MCP primitive families and
result objects. Every other binding is exposed as a generated tool.

## Admission and binding selection

The bridge advertises a generated tool only when the operation can be routed
honestly by the installed invokers:

- an operation with no binding is excluded;
- a binding whose specification has no installed invoker is excluded;
- a binding that references a missing source is excluded; and
- several invocable bindings are excluded unless the caller supplies an
  ordered binding selection.

Use repeatable `--select-binding <binding-key>` to provide that selection. The
order is copied to `context.configuration.selection`, using the same
first-invocable rule as ordinary OpenBindings invocation. Exclusions are
reported on stderr at startup. If no operation can be advertised, startup
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
    "message": "upstream stream failed",
    "details": {"offset": 1}
  }
}
```

The MCP result is marked as an error, and the OpenBindings error code, message,
and details are preserved. An invocation that does not complete before
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
