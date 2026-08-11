# MCP-origin round-trip fidelity

`ob mcp` has two intentionally different adaptation lanes.

For an ordinary OBI operation, the bridge exposes a generated MCP tool. Its
name comes from the operation key, its input schema comes from the operation
contract through MCP's object-only argument boundary, and its complete output
sequence is carried as both structured content and identical JSON text. This
is a protocol adapter for an abstract operation; it does not claim that the
source was MCP. The exact generic contract is documented in
[Generic OBI-to-MCP projection](mcp-generic-projection.md).

For an untransformed binding governed by legacy exact identifier
`openbindings.mcp@1`, the bridge preserves the native MCP family for
compatibility:

- `tools/<name>` becomes the same named tool;
- `resources/<uri>` becomes the same static resource;
- `resourceTemplates/<uriTemplate>` becomes the same resource template; and
- `prompts/<name>` becomes the same named prompt.

Complete successful MCP result objects are re-emitted rather than JSON-wrapped
inside a new text block. Protocol-native tool failures preserve their complete
`CallToolResult`; solicited progress is forwarded and re-correlated; downstream
cancellation cancels the upstream invocation.

## Descriptor fidelity and embedded listings

Operation fields preserve the universal contract, not every protocol-native
display hint. For full MCP descriptor fidelity, synthesize with embedding
enabled. The MCP synthesizer captures the raw, pagination-exhausted listing as
the source's pinned `content`; the bridge then reuses complete Tool, Resource,
ResourceTemplate, and Prompt descriptors, including titles, annotations,
icons, MIME types, and prompt-argument metadata.

Without embedded content, the bridge still preserves represented primitive
identities and invocation behavior, but it can only reconstruct descriptor
fields carried by the OBI operation and binding.

## The fidelity boundary

The target is equivalence for the subset represented by the unreleased first
`openbindings.mcp@1` candidate, not a clone of every MCP server feature. It
intentionally excludes required-task tools, resource subscriptions, sampling,
elicitation, roots, and log streams. These do not silently disappear: they are
outside the binding's represented surface.

The candidate binds only
tools with declared `outputSchema`, emits their conforming `structuredContent`
as the application value, and keeps MCP result and transport facts out of the
ordinary operation boundary. This bridge therefore exposes those
operations as ordinary generated tools; it does not reconstruct a native MCP
result envelope that the OBI intentionally does not contain.

Resource-template reads need special care. MCP gives the downstream bridge an
expanded URI, while the OpenBindings operation takes RFC 6570 variables, and
RFC 6570 does not define a generally reversible match. For an untransformed
MCP-origin binding, `ob mcp` forwards that `resources/read` URI to the same MCP
source. If an author adds an input or output transform, the binding no longer
has the native MCP boundary and is exposed as a generic OBI tool instead.

The executable differential test in `internal/cmd/mcp_test.go` starts a real
MCP server, synthesizes an embedded OBI, bridges it, connects real clients to
both surfaces, and compares:

- tool, resource, resource-template, and prompt inventory;
- complete native descriptors;
- structured, multimodal, metadata-bearing, and error tool results;
- static and template resource reads;
- prompt results;
- progress and final-result order; and
- cancellation propagation.

The broader corpus plan and synthesis-wide lessons are recorded in the spec
repository's `conformance/binding-specs/SYNTHESIS-ROUND-TRIP-REVIEW.md`.
