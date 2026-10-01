# Delegate Manager

Pre-launch working draft, version `0.1.0`, targeting OpenBindings Core `0.2.0`.
The unbound interface is [0.1.json](0.1.json). This replaces the earlier public
pre-launch location-based draft; it is a breaking design change, not evidence
that existing implementations already conform.

## Purpose

A Delegate Manager advertises the responsibilities an application can delegate,
the interfaces it accepts for each responsibility, and manages the interfaces
registered to fulfill them. An arbitrary searchable interface collection is
not sufficient to implement this capability: advertised roles must correspond
to responsibilities the consuming application actually knows how to use.

The design center is **purpose-scoped enrollment and expressed preference**,
not a catalogue, discovery engine, universal scheduler, or invocation service.
This supports applications with one stable storage provider, several specialized
invokers, or multiple notification providers without prescribing the same
selection procedure for all of them. A consumer managing registrations should
be able to discover what is accepted, enroll a value, inspect it, express its
relative preference, and remove it through the same small interface.

The shared management interface is independent of the managed responsibilities.
It contains no invocation, storage, identity, notification, or provider-specific
operation dependency. An application may implement this interface and consume
additional operations in its own OBI. The same machinery can manage storage
delegates in one application and synthesizers or invokers in another.

This is a non-normative, opt-in interface above OpenBindings Core, not an
addition to Core. There is no universal role vocabulary, mandatory transport,
database, local environment, account model, or secret store.

## Five operations

Short names below abbreviate `openbindings.delegate-manager.<name>`.

| Operation | Input | Output |
| --- | --- | --- |
| `listRoles` | None | `{ roles: DelegateRole[] }` |
| `registerDelegate` | `{ interface, roles, rolePreferences?, id? }` | `DelegateRegistration` |
| `listDelegates` | `{ role? }` | `{ delegates: DelegateRegistration[] }` |
| `setDelegatePreference` | `{ id, role, preference: number \| null }` | `null` |
| `unregisterDelegate` | `{ id }` | `null` |

The optional `id` lane replaces a known record's complete interface/role set
without deleting and recreating its identity. It avoids a separate replacement
operation; omitting `id` always requests a fresh registration.

## Values, not locations

Both expected and supplied interfaces are actual OBI objects. The shared
`OpenBindingsInterface` schema is identical to the existing invocation
interfaces' carrier. It is deliberately an open object: semantic conformance
to the OBI specification is an additional check, not a locally maintained copy
of the entire Core meta-schema.

There is no `location`, `contract`, URL resolver, fetch policy, remote refresh,
or interface-name lookup on this management surface. A caller can fetch an OBI,
read one from disk, synthesize it, or build it in memory before passing it in.
A native CLI can accept a filename as a convenience while publishing this
by-value operation through its appropriate adaptation; file retrieval is not
the portable enrollment operation.

Passing an OBI by value does not make all of its dependencies or source
artifacts self-contained, offline, trusted, or immutable. References *inside*
the OBI retain their ordinary semantics. Do not invent a document base from a
display name or previously fetched URL. Missing reference context or an
unresolved schema is reported as what it is: neither compatibility nor a
contradiction. Admission proceeds on the provider's correspondence claim and
refuses only on a contradiction the comparison establishes.

## Role discovery and expected interfaces

A role contains:

- `id`: a nonempty opaque identifier local to this manager's scope;
- `description`: the purpose and application-owned admission/use behavior,
  including whether and when preferences affect selection and how ties work;
- `acceptedInterfaces`: a nonempty array of complete expected OBI values.

An expected interface has operations and their relevant schema graph, but no
bindings and no dependency entries. It must contain at least one operation.
All its operations form one accepted requirement set. To require only a subset
of a larger shared interface, supply an ordinary self-contained slice retaining
the relevant schemas and other referenced document values.

**Unbound is not the opposite of dependency-bearing.** An OBI can be unbound
and have dependencies. Here the expected interface describes capabilities a
delegate must provide; adding dependencies would instead describe consumption
by the component represented in that OBI. The consuming application's own OBI
may declare its actual dependencies separately. The role relationship belongs
to this management interface, not to invented Core fields.

### Alternatives and matching

Each role accepts **any one** of its expected interfaces. Within one alternative,
**all** required operations must correspond and be directionally schema-compatible
with the supplied interface. Use the existing operation-key/alias relationship,
not display names, version labels, schema similarity alone, or a caller's claim
that an interface has a certain type. Extra provider operations are allowed.
They do not become dependencies, role memberships, or permissions automatically.

For example, an application may understand an earlier Document Store interface
and a future, differently shaped one. It can advertise both actual values.
This draft does not invent or claim that a second Document Store version exists.
Nor does the declaration teach the application how to use an unfamiliar version.

Do not assemble a fictional match from one operation in the first alternative
and another in the second. An accepted role can impose coherent state/namespace
requirements that shape comparison cannot prove. Admission may therefore also
require separately established application-specific qualifications. A structural
match never proves durability, behavior, availability, or security.

A provider's name or alias correspondence is its compatibility claim. The
manager proceeds on that claim and refuses admission only on a contradiction
its comparison establishes; a comparison it cannot decide leaves the claim
standing and is reported as such, never counted as a failed check. No
particular comparison algorithm is mandated by this interface. Each interface keeps its own schema
graph and reference environment during comparison. Unknown binding support is
not the same question as shape compatibility.

If several alternatives match, array order is not a selection mechanism.
The application must define how it chooses a supported consumption path and
preserves any required affinity. Matching every alternative is not required.

### Compatibility reference

For admission, the **expected interface is the target** and the **supplied
provider interface is the candidate**. This uses the existing
[shared-interface correspondence convention](../README.md#how-these-interfaces-relate):
look up each expected operation's **key** exactly in the provider's flat
key/alias namespace. Aliases on an expected operation remain part of its OBI,
but are neither additional required names nor substitutes for that key in this
whole-interface admission check. For example, expected key
`openbindings.document-store.get` can match provider key `readDocument` carrying
that exact alias. Matching an unrelated display name or merely finding similar
schemas does not establish correspondence.

The directions follow [Core §5.1, Contract directions](https://github.com/openbindings/spec/blob/main/openbindings.md#51-operations):

- **Inputs:** the provider must accommodate the inputs allowed by the expected
  operation; it cannot require a narrower input set.
- **Outputs:** the provider's declared successful outputs must satisfy the
  expected operation's output requirements; it cannot promise a broader set.

The [Schema Comparison Profile's question and directions](../schema-comparison/README.md#the-question-the-profile-answers)
use this same target/candidate terminology. That profile is one separately
specified comparison procedure, not a required dependency of this manager or
a new Core rule. Its [schema-state rules](../schema-comparison/README.md#schema-forms-and-the-unspecified-rule)
also distinguish omitted schemas from present unconstrained schemas. Do not
silently turn an omitted schema into `{}` or treat a skipped comparison as
proof of a claim the application requires. Each implementation remains
responsible for establishing the required compatibility using its declared
procedure and preserving each document's own reference environment.

## Registration and identity

`registerDelegate` receives one complete `interface` and a nonempty `roles` set.
Role arrays are unordered and duplicate-free; duplicates are rejected rather
than interpreted as weights or silently discarded.

Without `id`, the manager allocates a new non-reused ID. A repeat call without
an ID is another enrollment, not an implicit refresh or deduplication. Therefore
the operation has no `idempotent` declaration. After an uncertain result,
listing can help investigation but **cannot reliably correlate the request**:
identical registrations are allowed and another caller may have enrolled one.
A caller needing safe retries must use a separately specified native request
identity/idempotency facility. This draft does not provide one; blind retries
can create duplicates. An OBI's name,
embedded locations, byte representation, and value equality are not its identity.

Distinguish **a definite rejection before commit** from **failure to observe
completion**. Rejection leaves no mutation; a response lost after an atomic
commit leaves the caller uncertain, not entitled to assume rollback. A timeout
or disconnected caller therefore cannot infer that no registration exists.

With `id`, the caller supplies the complete desired replacement interface and
role set for that existing record. An unknown or removed ID fails; this lane
does not resurrect records. Omitted old roles are removed, not retained. An
empty role set is invalid; use `unregisterDelegate` to remove the whole record.
Replacing an interface requires explicit new input, never background refetching.

The optional `rolePreferences` object supplies the complete desired explicit
preference map atomically with enrollment. If supplied, every key must be a
requested role; invalid keys fail the whole enrollment. An empty object clears
all explicit preferences. If omitted on initial enrollment, store `{}`. If
omitted on re-enrollment, preserve current explicit preferences for retained
roles and discard entries for removed roles. Newly added roles have implicit
zero preference unless supplied explicitly. Removing and later re-adding a role
does not restore an old preference. Interface and roles remain full replacement;
preferences have the separately stated omission/preservation behavior.

The manager checks all requested roles against definitions current at commit.
Enrollment and re-enrollment are atomic: every requested role succeeds together,
or none changes. A replacement rejected before commit preserves the previous
record; uncertain completion does not establish rejection. Concurrent
mutations, including preference changes, must not expose mixed records or lose
unrelated preference fields through a read-modify-write race. Preference changes
and re-enrollment must be ordered so preservation uses the state at the atomic
mutation point, not a stale pre-read. This draft does not prescribe a portable
compare-and-swap revision protocol; operators needing stale-write prevention
must use a separately defined facility or serialize administrative changes.

The output is the committed record's `id`, retained `interface`, `roles`, and
complete explicit `rolePreferences` map.
Preserve the supplied document value, including extensions; no silent operation
pruning, source rewriting, numeric rounding, or substitution with a summary.
Object formatting is not significant. A runtime must refuse values it cannot
retain faithfully rather than claim successful enrollment of changed data.

## Many roles and many delegates

One registration can serve several roles. Several registrations can serve one
role, subject to its advertised application rules. Separate records carrying
the same interface are allowed; a host may use them with independent scopes or
configuration. Configuration associated with a role must remain distinguishable
by registration and role, not be keyed only by an interface's embedded URL.

Examples:

- One Document Store OBI enrolled for `contextStorage` and `secretsStorage`.
  This does not imply a shared namespace, credentials, or security qualification.
- Five invoker OBIs enrolled for `invocation`, each eligible for different work.
  Capability discovery and actual selection belong to the role's consuming
  application; passing interface comparison is not proof of binding support.
- One stable storage delegate whose replacement requires an explicit cutover.
  Enrollment alone must not silently move documents or introduce read/write
  inconsistency.

Registration is candidacy under the role's advertised rules, not a universal
activation mechanism. Those rules must explain effects such as whether eligible
registrations are consulted immediately or need an additional activation step.
An application may have built-in implementations; this interface prescribes
neither a mandatory fallback nor a special built-in preference.

## Preferences: intent scoped to a role

Preference belongs to **(registration ID, role ID)**, not to an OBI globally,
an individual provider binding, or a provider's unrequested capabilities.
The registration carries a `rolePreferences` object keyed by exact enrolled
role IDs. It is not part of the carried provider OBI and must not be injected
into or inferred from that OBI's binding preferences.

- Higher numbers express stronger caller preference among registrations in
  the same role and manager scope. Negative and fractional values are allowed.
  Values must be retained faithfully; no silent rounding or clamping.
- Absent entries have effective preference zero. An explicitly supplied zero
  must remain an explicit entry until changed, cleared, or its role membership
  or registration is removed. It has the same effective preference as absence.
- Equal numbers express no caller ordering. They do not prescribe randomness,
  lexical tie-breaking, registration order, or load balancing.
- Differences and ratios have no portable meaning: 20 is preferred over 10,
  not twice as likely, twice as authorized, or twice as capable.
- Cross-role comparisons have no meaning. A provider can be preferred for
  invocation and disfavored for storage.
- Preference is not a permission, hard provider restriction, binding choice,
  activation command, fallback/retry order, or authorization to move data.

`setDelegatePreference({id, role, preference})` sets a number or clears the
entry with `null`. Both setting and clearing require an existing registration
already enrolled in that exact role. Other preferences, roles, and the OBI stay
unchanged. Concurrent writes must not leave an orphan preference after removal.
Existing enrollment in a withdrawn role can be edited for administration, but
the edit does not reactivate the role. The operation is idempotent, not a
conditional write; competing edits to the same preference can overwrite one
another in mutation order.

The application **must document and follow** how each role uses preferences.
It may use them to rank eligible invokers, break otherwise equal choices, or
choose a provider only when assigning a new storage namespace. It may document
that a role does not use them. Supporting this operation means retaining and
reporting the preference, not pretending to execute a universal ranking policy.
A role documenting highest-preference selection must honor that promise within
its documented eligibility and affinity rules. Hard pinning requires separate
role-specific configuration; a large number is not a substitute.

For example, two registrations can have the following explicit preferences:

| Registration | Invocation | Context storage |
| --- | ---: | ---: |
| A | 20 | 0 |
| B | 10 | 30 |

An invocation role using highest-eligible preference would choose A if both
qualify, but B if only B supports the requested work. A storage role may choose
B for a new namespace while retaining A for an existing namespace. Reads and
writes for that namespace stay together; changing the numbers does not migrate
its data. This is an illustrative application policy, not a shared algorithm.

### Worked management sequence

Suppose an application advertises roles `A` (context storage), `B` (secrets
storage), and `C` (template storage). All three accept the actual expected
Document Store OBI in [examples.json](examples.json). Assume their separate
configuration and admission requirements have already been satisfied; this
example neither proves operational security nor invokes a storage service.

Let `P` denote the actual provider OBI object from that file, not a string,
location, or new reference syntax. The following successful sequence assumes
no concurrent administrator. `r1` is the illustrative manager-assigned ID.
This is an expected transcript, not a recording from a running manager.
The **full input/output values**, with `P` expanded as an actual object, are
defined in [management-example.mjs](management-example.mjs) and schema-checked
as portable fixture values. The table abbreviates records to their memberships and
explicit preferences; real registration/list outputs also retain the full OBI.

| Step | Management call | Result or observed explicit state |
| --- | --- | --- |
| Discover | `listRoles()` | Roles A, B, C, each with its accepted OBI value |
| Enroll | `registerDelegate({interface: P, roles: ["A", "B"]})` | ID `r1`; roles `["A", "B"]`; preferences `{}` |
| Prefer A | `setDelegatePreference({id: "r1", role: "A", preference: 20})` | Returns `null`; A now explicitly 20 |
| Set zero B | `setDelegatePreference({id: "r1", role: "B", preference: 0})` | Returns `null`; B now explicitly zero |
| Inspect | `listDelegates({})` | `r1` has roles `["A", "B"]` and preferences `{"A": 20, "B": 0}` |
| Re-enroll | `registerDelegate({id: "r1", interface: P, roles: ["B", "C"]})` | Same ID and OBI; roles `["B", "C"]`; preferences `{"B": 0}` |
| Clear B | `setDelegatePreference({id: "r1", role: "B", preference: null})` | Returns `null`; B's explicit entry is removed |
| Inspect B | `listDelegates({role: "B"})` | `r1` retains both roles `["B", "C"]`; preferences `{}` |
| Remove | `unregisterDelegate({id: "r1"})` | Returns `null`; registration removed |
| Inspect | `listDelegates({})` | Empty delegates array |

In re-enrollment, omission of `rolePreferences` preserves B's explicit zero,
drops A's 20 with its membership, and gives C implicit zero without adding a
stored entry. Clearing B changes stored state without changing its effective
zero preference. No step selects a provider, migrates documents, or changes
the provider OBI. An alternative re-enrollment supplying `rolePreferences: {}`
would clear all explicit preferences at that commit instead.

## Selection and invocation are separate

`listDelegates({role})` reports the relevant recorded candidate pool. The
application then applies current capability, authorization, readiness, affinity,
and selection rules. It may use an independently defined selector. The actual
invocation follows the selected interface's ordinary bindings or an explicitly
configured local implementation mechanism; this interface invents no portable
in-process function binding.

There is deliberately no generic `resolveDelegate(operation)`: a global
operation match loses the purpose distinction. A role-only lookup already
exists as `listDelegates({role})`, and a second operation that merely sorts that
list would add little. A future resolution capability must state a distinct
promise, such as request-specific qualification, without implying that an
operation identifier alone expresses every role's needs.

Independent selection does not inherently require registration or discovery.
A bounded selector can choose among caller-supplied OBI candidates assembled
from memory, configuration, a manager, or another source. A service that instead
searches its own collection and proposes a previously unknown OBI offers a
broader discovery-and-selection capability. Neither belongs in the minimum
Delegate Manager, and neither implies the same input/output or trust rules.

An application may consume a selector through its own operation dependency;
the shared manager OBI does not require every implementation to do so. The
application owns the eligible candidate set, authorized disclosure of selection
inputs, required cross-operation/provider affinity, validation of the answer,
and final invocation. A bounded selector must not invent candidates outside
its supplied set. A discovered provider must undergo the application's usual
admission, configuration, and authorization before use; a selector's
recommendation is not enrollment or permission. The application also establishes
how its selector is chosen without recursively asking selectors to choose
themselves. Neither selection nor preference adjustment invokes provider work
to find out which side-effecting call succeeds.

In particular, the current Operation Invoker interface is not a universal
catalogue of supported binding specifications. A role requiring support queries
must advertise the appropriate additional operations in its expected interface
and implement that consumption. Neither generic role data nor a static OBI
assertion supplies a live capability check automatically. Evaluating candidates
must not mean executing a side-effecting workload until one provider succeeds.

## Scoping, safety, and lifecycle

- Authentication and access to the manager's scope belong to its concrete
  surface. Registration IDs and role IDs are not credentials. No account or
  operator model is baked into this shared interface.
- Registering an OBI or changing its bindings does not authorize disclosure
  to a new endpoint. Credentials, request inputs, and provider documents can
  be sensitive. Resolve prerequisites only for explicitly authorized recipients;
  do not broadcast them to every candidate or inherit trust from a matching name.
- Admission does not invoke provider work. Where authorized resolution of an
  embedded reference is needed for validation, normal resource-access policy
  applies. This is not mandatory discovery of a provider document by location.
- A provider's own dependency entries are carried unchanged. Enrollment does
  not install plugins, satisfy those dependencies, or expose its extra operations.
- Role IDs may not be repurposed for unrelated responsibilities. Role or
  provider changes can invalidate eligibility; an application must not silently
  continue under an obsolete compatibility/authorization assumption. It may
  retain old enrollments for diagnosis, reconfiguration, and removal.
- `listDelegates` reports recorded enrollments, even when a role was withdrawn.
  Filtering by that role still finds them and retains their full role sets and
  explicit preference maps. Lists are complete snapshots, not
  partial results disguised as success; no pagination or ordering is implied.
- Successful removal prevents subsequent registry lookups from offering that
  record. It is not a claim about cancellation, revocation, data erasure, or
  already running work. Those stronger effects need application-owned semantics.
- Definite rejections before commit leave no mutation. An uncertain completion
  may conceal an atomic commit and is not evidence of rollback; no partial
  mutation may commit. Concrete error codes, wire envelopes, limits, and
  transport statuses remain implementation concerns here; no portable
  failure-discrimination vocabulary is invented.

## Chosen scope and remaining tradeoffs

This interface provides the five-operation surface above: role discovery, by-value
multi-role enrollment, inspection, role-scoped preference, and removal. It
uses numeric preferences as a shared ordering of expressed intent without
imposing a shared selection algorithm. It adds no
per-operation preference, discovery, resolution, mandatory selector, or policy
language. No account, transport, provider product, or fixed role vocabulary is
required. This is a recommendation for a useful common capability, not a claim
that every application should implement it or that it is a universal scheduler.

The following costs remain explicit rather than claimed solved:

- Fresh manager-assigned IDs and optional-ID re-enrollment keep identity
  independent of the document, but new enrollment has no portable retry
  deduplication. Listing is not an adequate substitute for request correlation.
- Atomic mutations prevent mixed state, but there is no portable revision/CAS
  mechanism protecting a caller's stale full replacement or same-field writes.
- Complete lists with full OBI values are simple and do not hide pagination,
  but are expensive for large collections. A paged/targeted capability can be
  designed separately when there is a real large-registry consumer; it must not
  silently weaken this operation's completeness promise.
- Admission requires whole-alternative correspondence with no established
  contradiction; the particular comparison procedure and role-specific
  operational qualifications remain implementation concerns. A comparison the
  manager cannot decide leaves the claim standing; it is reported, not counted
  as a contradiction.

None of these tradeoffs requires moving application-specific selection into
the manager. Implementations must qualify their behavior independently.

See [examples.json](examples.json) for actual carried OBI values and
[management-example.mjs](management-example.mjs) for the worked management
sequence. These example values are specifications, not evidence of a running role-aware
delegate manager.
