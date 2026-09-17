# Moving to role-scoped delegates

**Candidate guide — not yet an available upgrade procedure.** The storage and
conversion implementation has isolated tests, and role-aware routing and native
diagnostics are connected. Manager mutation/conversion commands and HTTP handlers
are still being migrated. Do not
edit your real configuration to imitate its private storage format. Executable
preview/apply/rollback examples must be added and tested when those surfaces are
available. Nothing in this guide authorizes conversion of an existing environment.

## Routing diagnostics available in this candidate

`ob delegate resolve --role invoke --binding-spec openbindings.usage@1` explains
ordinary ranked invocation routing. Add `--path native-first` to explain the
frame entrypoint; synthesis and inspection default to native-first. Add
`--registration <id>` to check only that exact enrolled registration, with no
built-in or alternate-provider fallback. The result labels this path `explicit`.
It checks support, not workload, and is not a reservation for a future invocation.
Do not interpret a source's old `x-ob.delegate` hint as a registration ID.

The equivalent authenticated HTTP operation is `POST /delegates/resolve` with
`{"role":"invoke","bindingSpec":"openbindings.usage@1"}` and optional `path`
or `registrationId`. A completed negative assessment returns `available:false`;
invalid inputs/state or failed provider assessment are errors. No provider OBI,
locator or credentials are included. Old operation-keyed resolution and
`resolve-binding-spec` commands/routes have been removed, not silently retargeted.
The OBI operation is OB-native `openbindings.ob.resolveRoleDelegate`, not a
shared Delegate Manager operation.

`ob binding invoke` and `ob operation invoke` remain native commands. Their
unary inputs/results do not implement the shared bidirectional frame protocol,
so `ob --openbindings` retains the abstract frame operations **unbound** over
Usage. The served OBI retains its real AsyncAPI streaming bindings. CLI-based
unary authoring/support operations remain bindable; a callable invocation-role
provider needs a faithful frame realization, not just compatible schemas.

## What changes

Each registration now retains an actual OBI value under a manager-issued ID,
with an explicit set of application roles. A URL is not its identity. OB does
not fetch a provider from a legacy location merely because it occurs in a
conversion plan. Two registrations may retain the same OBI and still have
different identities, roles, preferences and recipient-specific context.

OB's initial roles are `invoke`, `synthesize` and `inspect`. Each requires a
complete accepted interface, not just one operation with a familiar name. A
provider with additional capabilities is not automatically enrolled for them.
Role discovery describes the accepted interfaces and OB's selection policy.
Admission and having executable bindings are separate questions.

Shared preferences apply to a registration and role. OB also retains its native
binding-spec override, scoped to a registration, role and exact binding-spec
identifier. These are numeric preferences, not permissions. Built-in handling
is available without a registration; it is not an editable row in the delegate
list. A negative preference does not disable a provider.

Registration does not authorize a local executable. Previously explicit
executable authorizations are preserved during conversion, but an old delegate
location is not turned into a new authorization. Enrollment also does not
promise that a remote provider or its referenced artifacts are immutable.

## A deliberate conversion

1. **Select one environment and preview it.** Preview retains every original
   delegate row in its original order. It records the source document's identity,
   target environment, storage format and role catalogue. It neither changes the
   environment nor fetches providers, guesses roles or activates a registration.
2. **Review every row.** Choose `convert` or `exclude`, with a review explanation.
   For conversion, supply the recovered OBI value and explicit roles. All required
   operations must pass admission. A missing or changed legacy provider-content
   pin additionally requires a provider-review explanation. Reviewing the OBI is
   not proof of the provider's behavior or an execution-security approval.
3. **Resolve information that has no safe mapping.** Known old preferences map
   to the selected roles and OB's native binding-spec overrides without rounding.
   Unknown fields or unmappable preferences need explicit archival dispositions;
   they are not silently dropped. Rows using the retired `formats` or
   `formatPreferences` shape require exclusion and separate recovery. Exclusion
   keeps the original row in the reviewed plan and source backup, but does not
   enroll it. The converter does not infer capabilities from stale cached lists.
4. **Stop incompatible writers before applying.** Stop old OB processes and any
   other writer using this environment, then explicitly confirm quiescence. A
   new writer's lock cannot protect against an older binary that ignores it.
   Review must be repeated if the environment, original row order/content or
   role catalogue has changed. Do not bypass a stale-plan refusal.
5. **Apply and inspect the receipt.** The implementation validates the whole plan,
   preserves unrelated environment fields, writes private recovery files, and
   atomically replaces the registry under its shared configuration lock. The
   receipt maps original row positions to fresh registration IDs; excluded rows
   have no registration. Equal OBIs are not collapsed and retained row order is
   preserved. Verify the resulting enrollments before using them.

The private recovery directory is `.delegate-migrations/<plan-digest>/` inside
the selected environment. `original.json` contains the exact prior configuration;
`plan.json` contains the reviewed plan. These files may contain sensitive provider
configuration and must not be published or committed. Do not change their contents
to bypass a verification failure. Normal registry configuration is an implementation
detail, not a second management API.

## Retrying is not the same as registering twice

A failed response can occur after the configuration was replaced. Do not assume
that an error means nothing happened. Inspect current state and its receipt.
Retrying the same native conversion plan consults the matching receipt rather
than enrolling duplicate records or undoing subsequent management edits. Invalid
mixed legacy/new state is refused rather than hidden behind that receipt.

This conversion behavior does **not** make ordinary shared registration
idempotent: registering without an ID allocates a fresh ID even for an identical
OBI. Re-enrollment with a known ID replaces the complete OBI and role set
atomically. An omitted preference map preserves explicit preferences only for
retained roles; `{}` clears them. The role preference setter's `null` removes
one explicit preference. Removed IDs cannot be recreated by re-enrollment.

## Guarded rollback

Quiesce incompatible writers again. Rollback verifies the reviewed plan, exact
source backup, matching receipt and current configuration. If the current state
still matches the conversion result, it can restore the original configuration
bytes. A repeated completed rollback is recognized without a second mutation.

If anything changed after conversion—including unrelated environment settings—
rollback preserves a private recovery copy and refuses to overwrite those changes.
Keep the files and reconcile explicitly; do not force-copy `original.json` over
newer work. A rollback response can also be uncertain after replacement, so inspect
the current state before retrying. Reapplying a conversion after rollback allocates
a new issuance namespace; do not reuse registration IDs from the abandoned result.

Unregistering a delegate is not rollback and does not promise cancellation of
in-flight invocations, credential revocation, or reversal of provider side effects.

## Bounds and qualification

OB's candidate limits are 1 MiB per retained OBI, 4 MiB for the full registry/list,
256 registrations, and 8 MiB for migration inputs/plans. These are application
capacity limits, not OpenBindings validity rules. Oversized input is refused;
the converter does not truncate it. Migration inputs must be regular files.
Supported mutations use one stable OS-backed lock with a bounded five-second
acquisition wait; do not remove the lock file to force progress.

The current evidence covers local-filesystem process termination/recovery on
macOS. It does not certify network filesystems, distributed locking, power-loss
durability or runtime behavior on other operating systems. Cross-compilation is
not a substitute for those tests. Public CLI/HTTP journeys and live delegated
streaming remain required before upgrade qualification.

Implementation and regression anchors:

- [Conversion, receipt and rollback implementation](../internal/app/delegate_migration.go)
- [Preview, exact mapping, reviewed dispositions, uncertain completion and mixed-state tests](../internal/app/delegate_migration_test.go)
- [Process termination and cold-recovery tests](../internal/app/delegate_migration_process_test.go)
- [Registry identity and preference implementation](../internal/app/role_registry.go)

These anchors are internal implementation evidence, not public Go APIs or
instructions to invoke unexported functions against a real environment.
