# Interface Synthesizer

An interface synthesizer produces an OBI from a source artifact governed by a binding specification. Given a source artifact (an OpenAPI document, an AsyncAPI document, a protobuf descriptor set, or anything else a supported binding specification governs), it extracts operations, schemas, sources, and bindings into an OpenBindings interface document. The named binding specification is the semantic authority; the artifact's own specification applies only on the terms that binding specification incorporates.

This is what powers OBI synthesis from a raw source artifact: source-driven authoring (register a source, then pull it to derive operations and bindings), on-the-fly synthesis when a consumer is handed a spec it has no OBI for, and any tool that needs to bootstrap OBI adoption from existing specs.

## Binding-specification support

`checkBindingSpecs` is the authoritative support surface. It accepts exact,
opaque identifier tokens and returns one verdict for each unique token, in
first-occurrence order. A `supported: true` verdict is a conformance warrant:
this implementation implements the specification that exact string denotes,
as published. The boolean is strict — there is no maybe or partial state — and
the result is deterministic for a given implementation version and free of
side effects. A consumer may base source acceptance, refusal, or delegation on
this operation and no other support signal.

An implementation may use an internal pattern to compress its warranted set,
but never to extend it. In particular, a prefix or regular expression cannot
make an unpublished or future revision supported merely because its spelling
resembles an implemented identifier. Future optional verdict fields may
annotate the answer, never qualify or soften `supported`.

`listBindingSpecs` is the advisory glance: identifiers the implementation can
name for display, documentation, or pickers. Presence is a warrant, but
**absence from the list carries no information**. It cannot say no and no tool
may treat it as if it answers the support question. The one law connecting the
surfaces is `listed ⊆ supported`: every listed identifier MUST receive
`supported: true` from `checkBindingSpecs`.

The binding-invoker contract already uses this division: `preflightBinding` is
advisory pre-flight while the live `CONTEXT_REQUIRED` challenge is
authoritative. The same division now applies on the
binding-specification-support axis.

## Two synthesis surfaces

This interface exposes two forms of the same derivation:

- `synthesizeInterface` is the convenient strict surface. It returns the OBI
  directly and never uses a transient diagnostic to excuse an omitted target
  that the governing binding specification admits.
- `synthesizeInterfaceWithCoverage` returns the OBI together with durable,
  machine-readable dispositions for the source interactions considered by that
  same call and an explicit exhaustiveness claim. It may return a sound partial
  OBI when every omission is accounted for; it never turns disclosure into
  permission to emit an unsound binding.

The coverage form is substrate, not an API registry or crawler. It defines the
evidence such products can consume without defining discovery, storage,
ranking, trust, health checking, or invocation policy.

## Authentication is not extracted

An OBI document carries no authentication or `security` section, so a synthesizer does not extract credentials, security schemes, or auth requirements into the OBI. Authentication is a runtime prerequisite, not interface metadata: the binding invoker may negotiate it at call time via a `CONTEXT_REQUIRED` challenge (see the [`binding-invoker`](../binding-invoker/) interface).

A source artifact's security metadata (for example OpenAPI's `securitySchemes`) is therefore not mapped into the document. At most it can inform what the invoker asks for at invocation time; it is never baked into the static OBI.

## Multi-source composition is implementation-defined

The input's `sources` is an array, but how many sources one call composes
into the resulting OBI is the implementer's capability decision — a
service-level synthesizer may merge many artifacts; a single-family one
legitimately handles a single artifact. What no implementation may do is
answer for a subset silently: an implementation that does not compose all
supplied sources MUST refuse the multi-source input rather than synthesize
from some and drop the rest.

## Synthesis without sources

`sources` is optional, and omitting it is a legitimate call rather than a
malformed one. The result is a valid OBI with no operations, no sources, and
no bindings: the skeleton an author fills in by hand, or extends later by
registering sources against it.

The source-less case is also the one place where `name`, `version`, and
`description` are not overrides. Those fields override what would otherwise
be derived from a source artifact, and with no artifact there is nothing to
derive, so an omitted field has no underlying value behind it. What an
implementation substitutes for those three, and for `openbindingsVersion`, is
implementation-defined and carries no cross-implementation meaning. The
determinism rule below still applies, so the same source-less input yields
the same document every time and a scaffold can be diffed against a
checked-in one. A caller that needs a particular identity supplies the fields
rather than relying on any implementation's placeholder.

## Other extraction conventions

Binding specifications govern **interpretation and faithful correspondence**: they define what a bound artifact means and may constrain the soundness of an OBI derived from it. That does not make this Synthesizer interface, its generation strategy, or its reporting product a prerequisite for binding conformance. This optional contract separately defines its own derivation and reporting promises. The principles below are cross-family; per-family strategy detail belongs to each implementation's own reference documentation, subject to the governing binding specification.

- **Operations.** Each callable target in the source becomes one operation. The operation key SHOULD be stable across regenerations: derive it from a source-level identifier (OpenAPI `operationId`, gRPC method name, GraphQL field name) rather than from positional ordering.
- **Schemas.** Resolve `$ref` pointers when the source artifact uses them, so the produced OBI is self-contained. Cycle-protect when the artifact permits cyclical type references.
- **Sources.** Echo the input source's `bindingSpec`, `location`, and source description faithfully; use `name` as the output source key and `outputLocation` as the location written to the result. A local-path authoring convenience may normalize that path to the binding specification's invocable address form. When `embed` is true, preserve a complete accepted source representation as `content` or refuse the request — never ignore the directive or construct a partial discovery pin. Co-present input `content` is authoritative and remains the same JSON value in the result.
- **Bindings.** Each binding entry MUST carry a `selector` that the corresponding binding invoker can resolve back to the source artifact, in the selector form the governing binding specification defines (a JSON Pointer under `openbindings.openapi@1`, a fully-qualified method name under `openbindings.grpc@1`).
- **Aliases (optional).** A synthesizer MAY add operation `aliases` to claim correspondence with a shared contract (for example, a well-known operation name a consumer can target across providers). The name is author-asserted and carries no verification semantics.

## Creation-time soundness

Synthesis is a claim that the emitted interface can be realized through the
governing binding specification. Against the artifact, listing, descriptors,
or discovery state observed by a synthesis call, every emitted binding MUST
resolve to its identified target, fall within the binding revision's supported
subset, and admit at least one faithful invocation path when its declared
runtime prerequisites are available. A synthesizer MUST NOT emit an operation
that the corresponding conforming binding invoker is statically guaranteed to
refuse.

For `synthesizeInterface`, the operation set is complete for the callable
targets the governing binding revision admits in every source input the call
accepts. An implementation may refuse an unsupported artifact or source
representation as a whole; after accepting one, it cannot redefine individual
admitted targets as outside its accepted subset. If it cannot produce a
faithful, bindable operation for one of them, the call MUST fail rather than
silently return a partial interface. A transient warning is not sufficient
notice that an admitted target was omitted.

The coverage surface may return a sound partial interface only by recording
every omission with its actual disposition. In particular,
`implementation-unsupported` is evidence of an implementation gap, not a
successful strict synthesis.

A non-fatal warning remains appropriate when an emitted operation is usable
but its schema is necessarily a conservative or lossy projection. Such a
warning MUST NOT mean that the operation is guaranteed to refuse. Missing
credentials, consumer-selected configuration, unavailable peers or host
capabilities, and caller values that fail validation are runtime conditions,
not creation-time synthesis defects.

## Coverage accounting

Soundness and coverage are independent:

- **Soundness:** every invocation admitted by an emitted OBI operation has a
  faithful path through the binding specification to the source interaction.
- **Coverage:** every upstream interaction or independently selectable
  alternative observed in the source has a recorded disposition, and every
  one admitted by the binding revision is represented.

`synthesizeInterfaceWithCoverage` returns one disposition for every
**interaction unit** in the inventory it claims to have enumerated. A unit is an addressable target
or an independently selectable alternative whose omission would remove a
source-permitted invocation path. A request media alternative is a unit; a
parameter's incorporated serialization keyword is behavior of its parent unit,
not a separate unit. Family implementations document their unit inventory and
MUST NOT choose a coarser unit merely to hide loss. A report MAY additionally
carry `projection` entries for schema or semantic fidelity concerns attached
to represented units; those entries make loss measurable without pretending
that each projection is another independently invocable upstream operation.

Each disposition is one of:

- **`represented`** — the emitted OBI contains the named operation and binding
  path. Runtime prerequisites such as credentials or a required codec are
  carried separately and do not make the unit unrepresented.
- **`excluded`** — the governing binding-specification revision explicitly
  excludes this upstream-valid unit. The disposition names the specification
  rule or section and explains the boundary.
- **`invalid`** — the source unit is malformed or internally contradictory
  under the binding specification or an authority it incorporates.
  Whole-artifact failures may still terminate the call before a report can be
  produced.
- **`lossy`** — the invocation path is represented, but the emitted OBI
  framing cannot express part of the source contract exactly. This is durable
  disclosure of a projection gap, not permission to emit a binding that is
  statically guaranteed to refuse.
- **`implementation-unsupported`** — the binding revision admits the unit but
  this synthesizer cannot represent it. This is an implementation gap, never a
  binding-specification exclusion. A reference implementation has no such
  disposition at release.

`exhaustive: true` means every interaction unit in every accepted input source,
as defined by the governing binding revision's documented inventory, has
exactly one disposition. It does not merely mean "everything the
implementation happened to notice." If the implementation cannot establish
that claim, it reports `exhaustive: false` and includes a machine-readable
`limitation` explaining what may be missing and why. A false flag without that
evidence would disclose uncertainty without making it actionable.

`fullyRepresented: true` additionally means every upstream-valid unit is
represented without a lossy or unsupported disposition; the value is derived
from the entries and MUST NOT contradict them. Exclusion can therefore be
honest and exhaustive without being described as full upstream coverage.

A `represented` entry identifies the emitted source key, operation key,
binding key, and binding selector. These redundant links are intentional evidence:
they let a consumer verify that the input source, output source, binding, and
operation form one path without guessing from naming conventions.
`bindingSelector` is the empty string when the governing binding specification
identifies that target by an omitted `selector` (for example the root command in
`openbindings.usage@1`); empty and absent are not conflated.
An `excluded`, `invalid`, or `implementation-unsupported` entry identifies the
source unit even when no conformant binding selector exists. A `lossy` entry MUST
also identify its emitted source, operation, binding, and selector: loss is a
property of a usable represented path, and tooling must be able to connect the
limitation to that path without inference. Stable family-namespaced reason
codes support corpus measurement; prose messages are diagnostic.

The coverage report is evidence, not proof: a consumer may independently
compare the source, report, and OBI, and an untrusted synthesizer can still make
a false claim. The report is part of the operation result, not a callback. It remains
available when the OBI is persisted or handed to another process. A warning
callback MAY provide immediate authoring feedback, but it is not coverage
evidence.

This is a creation-time invariant, not a temporal-consistency guarantee. A
synthesizer preserves the source's declared embedded-versus-live semantics; it
does not have to embed, hash, refresh, or otherwise pin a mutable artifact or
service. Later artifact or service drift is external lifecycle state. On a
later invocation, the governing binding specification determines how the
current source is interpreted and when drift produces refusal.

## Deterministic output

A synthesizer SHOULD produce byte-stable output for byte-stable input. That means: stable property ordering, stable iteration order over operations, no embedded timestamps, no generated UUIDs. Determinism lets CI compare synthesizer output against checked-in OBIs without spurious diffs.

## Idempotency

Both operations are declared idempotent: calling them does not intentionally
change source or external state. For embedded content, the same input and
implementation configuration produce the same semantic result. A live
`location` may resolve to different content over time; idempotency does not
pretend a mutable external resource is frozen. Deterministic ordering applies
to each observed source state.
