# OpenBindings Quick Reference

An OpenBindings Interface (OBI) is a JSON document that defines operations and binds them to existing protocols.

## Core Structure

```json
{
  "openbindings": "0.1.0",
  "name": "My API",
  "operations": {
    "getUser": {
      "description": "Get a user by ID",
      "input": { "type": "object", "properties": { "id": { "type": "string" } }, "required": ["id"] },
      "output": { "type": "object", "properties": { "name": { "type": "string" } } }
    }
  },
  "sources": {
    "rest": { "format": "openapi@3.1", "location": "./openapi.yaml" }
  },
  "bindings": {
    "getUser.rest": {
      "operation": "getUser",
      "source": "rest",
      "ref": "#/paths/~1users~1{id}/get"
    }
  }
}
```

## Key Concepts

- **Operation**: named unit of capability with input/output JSON Schemas
- **Source**: reference to a binding artifact (OpenAPI doc, proto file, MCP server)
- **Binding**: mapping from an operation to a specific entry point in a source
- **Role**: published interface a service can declare it satisfies

## Format Tokens

| Format | Token | Ref Format |
|--------|-------|------------|
| OpenAPI | `openapi@3.1` | JSON Pointer: `#/paths/~1users/get` |
| AsyncAPI | `asyncapi@3.0` | JSON Pointer: `#/operations/sendMessage` |
| gRPC | `grpc` | `package.Service/Method` |
| Connect | `connect` | Same as gRPC |
| MCP | `mcp@2025-11-25` | `tools/name`, `resources/uri` |
| GraphQL | `graphql` | `Query/field`, `Mutation/field` |
| Operation Graph | `openbindings.operation-graph@0.1.0` | Native composition |

## Transforms

Bindings can have `inputTransform` and `outputTransform` using JSONata:

```json
{
  "inputTransform": {
    "type": "jsonata",
    "expression": "{ \"userId\": id }"
  }
}
```

## Security

The `security` section declares named security entries with methods in preference order:

```json
{
  "security": {
    "api-auth": [
      { "type": "oauth2", "authorizeUrl": "...", "tokenUrl": "..." },
      { "type": "bearer" }
    ]
  }
}
```

Bindings reference security entries: `"security": "api-auth"`

## Well-Known Security Types

- `bearer`: token in Authorization header
- `oauth2`: Authorization Code + PKCE (authorizeUrl, tokenUrl, scopes, clientId)
- `basic`: username + password
- `apiKey`: key in header, query, or cookie (name, in)

## Compatibility

- Outputs: covariant (provided must be at least as specific as required)
- Inputs: contravariant (provided must accept at least what required accepts)
- Three-strategy operation matching: direct key, satisfies, aliases
