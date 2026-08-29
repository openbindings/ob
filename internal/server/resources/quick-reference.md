# OpenBindings Quick Reference

An OpenBindings Interface (OBI) is a JSON document that defines operations and binds them to existing protocols.

## Core Structure

```json
{
  "openbindings": "0.2.0",
  "name": "My API",
  "operations": {
    "getUser": {
      "description": "Get a user by ID",
      "input": { "type": "object", "properties": { "id": { "type": "string" } }, "required": ["id"] },
      "output": { "type": "object", "properties": { "name": { "type": "string" } } }
    }
  },
  "sources": {
    "rest": { "bindingSpec": "openbindings.openapi-3.1@1", "location": "https://api.example.com/openapi.yaml" }
  },
  "bindings": {
    "getUser.rest": {
      "operation": "getUser",
      "source": "rest",
      "selector": "#/paths/~1users~1{id}/get"
    }
  }
}
```

## Key Concepts

- **Operation**: the portable contract — a named unit of capability with input/output JSON Schemas, independent of any protocol
- **Source**: carries or points at a source artifact (OpenAPI doc, proto file, MCP server) under a named binding specification (`bindingSpec`)
- **Binding**: links one operation to one source at a specific entry point (`selector`), optionally with a transform
- **Alias**: an additional name for an operation, equal in standing to its key; by adopting a published interface's operation name as its key or an alias, an operation **corresponds to** that operation. The claim is an author assertion — it does not establish compatibility, ownership, or trust (OBI-T-12 resolves keys and aliases identically)

## Binding Specifications

A source names the binding specification that governs its artifact via its `bindingSpec` field. Project specifications use `openbindings.<name>@<rev>`, where `<rev>` is an integer revision of the specification (artifact and dialect versions self-identify in the artifact, never in the identifier). Every current family document is an unreleased first `@1` candidate; none has been published.

The binding specification is sovereign: another artifact or protocol authority
applies only where the specification incorporates it. A specification may
instead narrow, extend, override, or define the governed domain itself. An
implementation may complete a silent case locally, but that completion is
implementation-defined rather than portable meaning under the identifier. The
`openbindings.*` candidates choose close upstream deference and must close such
gaps before publication.

| Format | `bindingSpec` identifier | `selector` shape |
|--------|--------------------------|-------------|
| OpenAPI 2.0 | `openbindings.openapi-2.0@1` | JSON Pointer to the operation object: `#/paths/~1users~1{id}/get` |
| OpenAPI 3.0 | `openbindings.openapi-3.0@1` | JSON Pointer to the operation object: `#/paths/~1users~1{id}/get` |
| OpenAPI 3.1 | `openbindings.openapi-3.1@1` | JSON Pointer to the operation object: `#/paths/~1users~1{id}/get` |
| OpenAPI 3.2 | `openbindings.openapi-3.2@1` | JSON Pointer to the operation object: `#/paths/~1users~1{id}/get` |
| AsyncAPI | `openbindings.asyncapi@1` | JSON Pointer: `#/operations/sendMessage` |
| gRPC | `openbindings.grpc@1` | `<fully-qualified-service>/<method>` |
| Connect | `openbindings.connect@1` | `<fully-qualified-service>/<method>` |
| MCP | `openbindings.mcp@1` | `tools/<name>` for tools declaring `outputSchema` |
| usage (CLI) | `openbindings.usage@1` | space-separated command path (absent `selector` = root) |
| GraphQL | `openbindings.graphql@1` (latest) | `query/<field>` or `mutation/<field>` |
| GraphQL | `openbindings.graphql@1` (compatibility) | `query/<field>`, `mutation/<field>`, or `subscription/<field>` |
| Operation Graph | `openbindings.operation-graph@1` | JSON Pointer to a graph definition |

The four OpenAPI siblings accept Swagger 2.0, OpenAPI 3.0.0–3.0.4,
3.1.0–3.1.2, and 3.2.0 respectively. Their SDK adapters use the standalone
OpenAPI client engine. Synthesized OpenAPI bindings emit an `inputTransform`
that maps the operation input to the `{parameters?, body?}` caller envelope.

## Transforms

Bindings can have `inputTransform` and `outputTransform` using JSONata. Inline transforms are bare JSONata expression strings, or `{"$ref": "#/transforms/<name>"}` to reference a named transform.

```json
{
  "inputTransform": "{ \"userId\": id }",
  "outputTransform": { "$ref": "#/transforms/unwrap" }
}
```

Named transforms in `#/transforms` are plain JSONata strings keyed by name:

```json
{
  "transforms": {
    "unwrap": "data.user"
  }
}
```

## Authentication & Context

An OBI document carries **no** security, credentials, or auth field: the operation contract and its bindings stay abstract. Authentication is a property of the source artifact and the binding specification that governs it (an OpenAPI document's own security schemes, for example), not something the OBI restates.

Credentials and other runtime context are supplied by the runtime at invocation, never baked into the document. When a binding cannot be invoked because required context is missing, the invoker returns a `CONTEXT_REQUIRED` challenge *before* any side effect, listing alternatives (disjunctive) each of whose requirements (conjunctive) the runtime resolves from its context store, then retries. Well-known authentication requirement types:

- `auth.bearer`: token in the Authorization header
- `auth.basic`: username + password
- `auth.apiKey`: key in a header, query parameter, or cookie
- `auth.oauth2`: OAuth 2.0 (access/refresh tokens)

Runtimes MAY define further requirement families (e.g. `approval.user`, `config.value`); an unrecognized requirement type simply makes its enclosing alternative unselectable.

## Compatibility

- Outputs: covariant (provided must be at least as specific as required)
- Inputs: contravariant (provided must accept at least what required accepts)
- Operation matching: one flat namespace of each operation's key plus its aliases, resolved with key and alias matches equally authoritative (OBI-T-12)
