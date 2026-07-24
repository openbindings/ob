# OpenBindings Primer for AI Agents

## Status and purpose

This is an informative orientation to OpenBindings for language models and
other automated development agents. It explains the project's mental model,
authority boundaries, and safe working rules. It is not a substitute for the
normative [core specification](openbindings.md), a selected binding
specification, or the artifact and protocol specifications that binding
specification incorporates.

This primer describes the OpenBindings 0.2 working draft. Before changing or
implementing behavior, identify the exact core version and binding-specification
identifier in use and read those authorities. If this primer conflicts with a
normative specification, the normative specification wins.

For any task, establish three facts before acting:

1. **Which layer owns the question?** Core document model, operation contract,
   binding specification, incorporated upstream authority, or implementation
   policy.
2. **Which exact versions are in force?** Core version, binding-specification
   identifier, and upstream artifact or protocol edition.
3. **Which capability is being claimed?** Reading, validation, synthesis,
   invocation, bridging, or something narrower. Do not imply capabilities or
   coverage the implementation does not exercise.

## OpenBindings in one paragraph

OpenBindings is a portable interface description format. An OpenBindings
interface document (OBI) describes protocol-independent operations, with
optional JSON Schema contracts for each input and output value, and connects
those operations to concrete realizations through sources and bindings. A
binding specification governs how a source family such as OpenAPI, AsyncAPI,
gRPC, GraphQL, MCP, or a CLI descriptor is interpreted and acted upon.
OpenBindings does not replace those artifacts or protocols. It adds an
operation-level layer that can survive across them.

The useful separation is:

```text
what a capability means                     how it is realized
──────────────────────────────────────      ─────────────────────────────
operation key, aliases, description,        source, bindingSpec, ref,
per-value input/output schemas              transforms, concrete interaction
```

One operation may have several bindings. Those bindings are author-declared
realizations of the same operation contract; OpenBindings does not prove that
arbitrary protocol operations are semantically equivalent.

## The conceptual stack

```text
upstream artifact or live protocol surface
                 │
                 │ synthesis or inspection
                 │ governed by a binding specification
                 ▼
        OpenBindings interface document
      operations + sources + bindings + transforms
                 │
                 │ resolution and invocation
                 │ governed by the same binding specification
                 ▼
          concrete protocol interaction
```

The binding specification is the semantic hinge in both directions. A
synthesizer uses it to project source targets into operations and bindings. An
invoker uses it to resolve those bindings and perform interactions. The two
tools need not share code, but they must agree on the binding specification's
meaning.

This is not a serialization round trip. Synthesis does not copy an entire
upstream artifact into a new universal protocol, and invocation does not
reconstruct the original server. The fidelity target is that every represented
binding identifies the intended target and can be acted upon with the
interaction semantics, value boundary, and success classification its binding
specification defines. Synthesis coverage is a separate question: a tool must
not imply that unrepresented source targets were faithfully preserved.

## Core concepts and their owners

### OBI

An OBI is a JSON document governed by the core specification. It is a target
artifact that tools may author, synthesize, validate, index, transform, invoke,
or bridge. It is not a source language that compiles protocols from scratch.

### Operation

An operation is a named, protocol-independent unit of capability. Its `input`
and `output` schemas describe each value crossing the caller-facing boundary.
They do not declare stream cardinality, framing, timing, completion, or
transport. The selected binding owns those interaction properties.

An operation key and its aliases form one flat namespace. An alias can assert
correspondence with a shared interface's operation name, but the assertion does
not prove compatibility, ownership, trust, or substitutability.

### Source

A source names an exact, opaque binding-specification identifier in
`bindingSpec` and supplies `location`, `content`, or both as that specification
permits. The source may point at an artifact, embed one, address a live surface
with no separate artifact, or combine an embedded artifact with a service
address.

### Binding

A binding connects one operation to one source and identifies a target through
`ref` when the binding specification requires one. It may carry input and
output transforms that bridge value-shape differences. A transform does not
redefine transport lifecycle or repair an interaction that the binding
specification cannot represent.

### Binding specification

A binding specification gives one source family stable OpenBindings meaning.
It owns accepted source representations, address semantics, `content`,
composition of `content` and `location`, `ref` syntax and resolution, target
identity, interaction mechanics, operation-boundary correspondence, and
success classification.

A binding specification may incorporate an upstream artifact or protocol,
define its own artifact and interaction model, or admit an artifactless source
mode. It should add only the semantics needed to make that family actionable
through OpenBindings.

Project-published identifiers use integer revisions such as
`openbindings.openapi@1`. That revision identifies the binding specification,
not the core OpenBindings version and not the OpenAPI edition. Accepted
artifact editions identify themselves inside their artifacts and are listed by
the binding specification.

### Processor, synthesizer, invoker, and bridge

- A **processor** is any tool that reads or acts on an OBI. Its obligations
  follow the capabilities it claims and exercises.
- A **synthesizer** creates an OBI projection from one or more governed sources.
- An **invoker** acts on a selected binding. The core enables invocation but
  does not prescribe one universal invoker API.
- A **bridge** exposes OBI operations through another surface, such as MCP. A
  bridge can preserve only what the OBI and governing binding specifications
  represent.
- The project's **published interfaces** are optional reusable software
  contracts for interoperability among implementations. They are not mandatory
  parts of the core document model.

## Authority boundaries

Use this ownership model when specifications appear to overlap:

| Question | Authority |
| --- | --- |
| Is the OBI structurally conformant? How do OBI-defined references resolve? | Core OpenBindings specification |
| What does an operation's input or output value mean at its caller-facing boundary? | The operation contract in the OBI |
| What source forms are accepted? What does `ref` identify? How is the interaction performed and classified? | The named binding specification |
| What does an incorporated OpenAPI, protobuf, GraphQL, MCP, or other declaration mean? | The incorporated upstream authority, as scoped by the binding specification |
| Which binding should be selected? Where are credentials stored? Should values be runtime-validated? How are retries, caching, and policy handled? | The implementation or consuming application |

Do not promote an implementation choice into core or binding-specification
doctrine merely because both reference SDKs currently make that choice.
Likewise, do not weaken a specification to match incomplete code; record and
fix the implementation gap.

## Binding-specification doctrine

When adapting an existing artifact or protocol, apply this deference order:

1. **Incorporate** what the artifact or protocol defines.
2. **Preserve alternatives** when it permits several valid choices.
3. **Expose an interpretation choice** when invocation requires a choice the
   artifact does not make.
4. **Refuse or exclude** behavior OpenBindings cannot represent faithfully.
5. **Define a default only as a last resort**, label it as the binding
   specification's convention, and expose a named choice where consumer choice
   remains meaningful.

The shorthand is **incorporate → preserve → configure → refuse → default**.
“Refuse rather than invent” is the governing correction. A loud, pre-dispatch
refusal is better than silently losing information, identifying distinct
artifact values as one value, or approximating an interaction the artifact did
not declare.

Completeness does not require every upstream behavior to be supported. It
requires a definite answer for every part of the binding boundary: an
incorporated rule, a rule defined by the binding specification, a named
configuration point, or an explicit exclusion. See the
[binding-specification guide](binding-specs/README.md) for the full
completeness test and authoring template.

## What OpenBindings provides—and what it does not

OpenBindings provides:

- a common operation and per-value contract model across source families;
- explicit correspondence from an operation to one or more concrete targets;
- portable documents suitable for discovery, indexing, invocation, code
  generation, bridging, and comparison;
- a place for shape transforms without absorbing protocol mechanics into the
  operation contract; and
- decentralized extension through independently published binding
  specifications and shared interfaces.

OpenBindings does not by itself provide:

- semantic equivalence among arbitrary bindings;
- complete coverage of every feature in every upstream protocol;
- a universal interaction lifecycle or failure vocabulary;
- binding selection, credential storage, prompting, retries, rate limiting, or
  trust policy;
- a central registry of binding specifications or operation names;
- proof that synthesized operations cover an entire source artifact; or
- recovery of upstream information that a synthesizer omitted.

Authentication declarations remain in their governed artifacts. Credentials
and other runtime prerequisites are supplied as invocation context; they are
not copied into the OBI.

Core document conformance is offline-decidable. If validation needs a binding
specification the processor does not have, the binding-specific conclusion is
unverified rather than automatically non-conformant. Do not confuse “this tool
cannot verify or invoke this binding” with “this OBI is invalid.”

## When OpenBindings is relevant

OpenBindings is a good fit when a project needs to:

- discover or index capabilities across unrelated protocol families;
- give one semantic operation several concrete protocol realizations;
- invoke existing APIs through one operation-oriented boundary without
  erasing their protocol authority;
- synthesize portable capability descriptions from artifacts the project does
  not control;
- bridge existing interfaces into agent-facing surfaces such as MCP;
- compare, compose, or generate clients from operation contracts; or
- let independently implemented tools share documents and narrow software
  interfaces.

It is not, by itself, the right tool for designing a new wire protocol,
replacing an upstream API description, generating several protocols from one
authoring language, proving that independently authored operations are
equivalent, or hiding protocol behavior that callers actually need to control.

## Task playbooks

### Consuming and invoking an OBI

1. Validate the document against the declared core version.
2. Resolve the requested operation by its key or alias.
3. Select a binding using application policy; do not infer a universal
   preference from map order.
4. Load the exact binding specification named by the source.
5. Acquire and interpret the source using its `content`, `location`, and
   composition rules.
6. Resolve the binding's `ref` and any required interpretation choices.
7. Obtain credentials or other prerequisites through runtime context without
   mutating them into the OBI.
8. Apply the binding specification's input mapping, interaction, success
   classification, output mapping, and the binding's transforms.
9. Refuse before dispatch if the target cannot be interpreted faithfully.

### Supplying an operation dependency

Use this playbook when a UI component, agent, workflow, plugin, or other
consumer should depend on a capability without choosing its protocol or
deployment:

1. Express the consumer's requirement as an ordinary OBI contract, commonly
   unbound, and select one operation by a typed signature.
2. Let the application supply concrete interfaces and operation invokers. The
   consumer must not own a protocol client, credential store, delegate
   registry, or binding-selection policy.
3. Match per operation by an adopted key or alias, then check directional
   schema compatibility. A name is an author assertion of correspondence, not
   proof of compatibility or trust.
4. Verify invocability through side-effect-free preflight. Treat known context
   requirements as advisory; live `CONTEXT_REQUIRED` remains authoritative.
5. Compose the matches according to application semantics. Use the SDK's
   conservative resolution helper only for route-to-one behavior; aggregation,
   fan-out, race, fallback, and multi-operation affinity remain explicit
   application policy.
6. Invoke the selected concrete interface through the ordinary
   cardinality-agnostic invocation handle. Do not create a second UI-specific
   call model.
7. Install only the binding packages the application actually uses. A thin UI
   framework adapter may make resolution reactive without changing these
   semantics.

The TypeScript SDK names these primitives `operationRequirement`,
`matchOperationRequirement`, and `resolveOperationRequirement`; the Go SDK
publishes the corresponding `NewOperationRequirement`,
`MatchOperationRequirement`, and `ResolveOperationRequirement`. The
non-normative website guide **Operation Dependencies** develops the pattern and
its boundaries.

### Synthesizing an OBI from an upstream artifact

1. Identify a binding specification that explicitly accepts the artifact
   edition or live surface. Do not guess a nearby family.
2. Enumerate bindable targets according to that specification.
3. For each target, either emit an accurately identified operation and binding
   or report a specific exclusion/refusal. Never silently omit a target while
   claiming exhaustive coverage.
4. Preserve the governed source artifact or its stable location as the binding
   specification permits; do not rewrite it into a project-specific dialect.
5. Derive only contracts and metadata justified by the source. Do not invent
   descriptions, schemas, equivalence claims, or protocol defaults.
6. Emit diagnostics or coverage evidence that distinguishes represented,
   unsupported, invalid, and indeterminate targets when the tool claims
   coverage. Such evidence is a useful tool result, not extra OBI semantics.
7. Exercise synthesized bindings through a real invoker. Structural synthesis
   success alone does not prove invocation fidelity.

For large-scale capability indexing, retain source provenance, binding-spec
identity, tool version, coverage evidence, and refusals beside the indexed OBI.
Searchability must not turn partial projection into an implicit completeness
claim.

### Implementing a binding specification

1. Read the exact binding specification and every incorporated authority it
   pins.
2. Implement observable behavior from those authorities, not from another
   SDK's internal architecture.
3. Keep upstream-permitted alternatives as permitted sets unless effective
   configuration selects one.
4. Expose required interpretation points without prescribing how applications
   store or obtain their choices.
5. Produce actionable, pre-dispatch errors for unsupported or ambiguous cases.
6. Run the shared source fixtures, processor scenarios, synthesis scenarios,
   and adversarial cases. Go and TypeScript implementations should agree at
   the OpenBindings boundary while remaining idiomatic internally.

### Authoring or reviewing a binding specification

Start from the [binding-specification guide](binding-specs/README.md), not from
one implementation. Define the authority pins and all source, address,
composition, reference, target, interaction, value-boundary, and success
semantics. Apply the deference order to every observable choice. Keep the work
as an unpublished candidate if those boundaries cannot yet support an
exemplary, implementable contract.

### Bridging an OBI to MCP

Treat the bridge as a new MCP exposure of represented OBI operations, not as a
claim to recreate the original server's identity or every protocol-native
surface. For an OBI synthesized from OpenAPI, gRPC, GraphQL, or another family,
the bridge is successful when an MCP client can discover and faithfully invoke
the represented operations through their original bindings. For an MCP-origin
OBI, the same standard detects loss: the bridge should not be worse at the
represented capabilities because of avoidable synthesis or invocation loss.
It cannot reconstruct targets or descriptor semantics that synthesis never
captured.

## Common category errors

- **“The operation schema says this is unary.”** It does not. Schemas are
  per-value; the binding defines cardinality and lifecycle.
- **“`bindingSpec` is a URL to fetch.”** It is an exact opaque identifier, not
  a locator.
- **“Both SDKs do this, so the specification requires it.”** Shared behavior
  may still be implementation policy. Find its authority.
- **“The artifact is ambiguous, so choose the most common interpretation.”**
  Preserve, configure, or refuse before defining a visible last-resort
  convention.
- **“Synthesis returned an OBI, so coverage is complete.”** A valid projection
  may be partial. Inspect coverage evidence and refusals.
- **“An extension can carry whatever the core cannot express.”** Extensions do
  not override core invariants or make an unfaithful binding actionable.
- **“Put the API key in the source.”** Credentials are runtime context, never
  OBI content.
- **“An MCP bridge should reproduce every property of the original server.”**
  It should faithfully expose what the OBI represents and clearly delimit what
  it does not.

## Minimal example

```json
{
  "openbindings": "0.2.0",
  "operations": {
    "createTask": {
      "input": {
        "type": "object",
        "properties": {
          "title": { "type": "string" }
        },
        "required": ["title"]
      },
      "output": {
        "type": "object",
        "properties": {
          "id": { "type": "string" }
        }
      }
    }
  },
  "sources": {
    "tasksApi": {
      "bindingSpec": "openbindings.openapi@1",
      "location": "https://example.com/openapi.json"
    }
  },
  "bindings": {
    "createTask.http": {
      "operation": "createTask",
      "source": "tasksApi",
      "ref": "#/paths/~1tasks/post"
    }
  }
}
```

The operation owns the caller-facing values. The OpenAPI source owns its HTTP
declarations. `openbindings.openapi@1` explains how the `ref` resolves, how
values map to the HTTP exchange, and which outcomes produce successful output
values. An invoker supplies runtime context and transport policy. None of those
layers should silently take authority from another.

## Where to verify a conclusion

- [Core specification](openbindings.md): OBI model, invariants, terminology,
  references, and conformance floor.
- [JSON Schema](openbindings.schema.json): structural validation.
- [Binding-specification guide](binding-specs/README.md): doctrine,
  completeness, authoring, and release-quality review.
- The exact published binding specification named by `bindingSpec`: concrete
  source and interaction meaning.
- [Conformance corpus](conformance/README.md): portable evidence and test
  organization.
- [Published interfaces](https://openbindings.com/interfaces): optional
  reusable implementation contracts.

When answering an OpenBindings question, state which of these authorities
supports the answer. If none does, label the conclusion as implementation
policy, a proposed design choice, or an unresolved question rather than
presenting it as OpenBindings behavior.
