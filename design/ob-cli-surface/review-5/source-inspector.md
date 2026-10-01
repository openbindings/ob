# Source Inspector

A source inspector examines a binding source before an OBI is created. It returns bindable targets that tooling can offer to users, optionally including a suggested operation key and operation framing for each target.

This powers tooling that helps users select which operations to include when authoring an OBI without relying on non-normative selector naming conventions.

## When to use it

Source inspection is the right primitive when a caller is authoring an OBI from an existing artifact and wants to choose which targets to bind. Typical surfaces:

- An interactive CLI authoring flow that shows the user a checklist of targets to bind.
- A web tool that lets a user pick endpoints from an uploaded OpenAPI spec.
- A code-generation step that needs to enumerate available bindings.

For non-interactive synthesis ("give me an OBI for everything in this spec"), use the [interface synthesizer](../interface-synthesizer/) directly. Inspection is the discovery step that precedes a targeted synthesis.

## Why this is a separate interface

Source inspection could conceptually be folded into the interface synthesizer as an extra operation. It is a separate interface because the capabilities are independently useful: a source inspector does not need to generate full OBIs, and an interface synthesizer does not need to surface targets to users. Splitting them lets a tool depend on exactly the capability it needs, and lets a service author publish an inspector without committing to full OBI generation.

## The `exhaustive` flag

`SourceInspection.exhaustive` tells the consumer whether `targets` is the complete enumeration of targets admitted by the source's governing binding specification.

- `exhaustive: true` means the inspector has reported every target that could be bound. A "select all" action in the UI is safe.
- `exhaustive: false` means more admitted targets may exist, for example because enumeration was bounded, part of a live source was inaccessible, or the implementation has a known gap. `limitation` states why; partiality is never silently described as completeness.

Inspectors SHOULD prefer `exhaustive: true` whenever the artifact permits complete enumeration. When it is false, `limitation.code` is stable machine-readable evidence and `limitation.message` is diagnostic prose. A private relevance filter does not justify omitting targets unless its criteria are explicit caller input or an extension understood by both parties.

The inventory boundary matches synthesis: a bindable target is a source interaction the governing binding revision admits and for which a conforming binding can be formed. Upstream interactions that revision deliberately excludes are not bindable targets; complete upstream coverage, including exclusions, is the interface synthesizer's coverage surface.

## Operation framing is optional

`BindableTarget.operation` is optional. An inspector that knows enough to suggest input/output schemas, tags, or a description SHOULD include it; an inspector that only knows the selector MAY return targets with just `selector` (and optionally `operationKey`). Consumers MUST treat a missing `operation` as "framing not provided," not as an error. When framing is present, it MUST be a sound projection of the same target under the binding specification and any disclosed implementation-defined completion. Such a completion may make this inspector useful for an underdefined specification, but the projection is not evidence that the completion belongs to the identifier's portable meaning.

## Idempotency

`inspectSource` is declared idempotent: inspection does not intentionally
change the source or external state. For the same observed source state and
implementation configuration, targets and ordering are stable. A live
`location` may change between calls; idempotency does not make an external
resource immutable.
