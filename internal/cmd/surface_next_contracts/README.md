# Playable delegate role contracts

These are self-contained expected OBIs for the lab's `delegate roles` report,
not new published revisions of the shared interfaces.

The source is `internal/app/requirements/{binding-invoker,source-inspector,interface-synthesizer}.json`
in ob's `f130c3770321b233f4b625e7ad8bd434cc622d10` base. The operation sets
follow that base's `internal/app/requirements.go` admission catalogue:

- invoke consumes the Binding Invoker list, support query, and invocation.
  Preflight is an independent capability, not required for admission.
- inspect consumes the Source Inspector list and inspection plus the
  Interface Synthesizer's support query. Source Inspector defines no query
  operation of its own.
- synthesize consumes the Interface Synthesizer list, query, and synthesis.
  Synthesis with coverage is an independent capability.

Each slice retains the entire schema graph referenced by those operations.
The lab applies Matt's settled kinds vocabulary (`listSupportedKinds`,
`checkKindSupport`, `kind`, `kinds`, `KindInfo`, `KindVerdict`) and the pinned
0.2 core Source shape (`kind`, optional opaque `content`, description, and
extensions). Operation-level `idempotent` is removed: 0.2 carries that signal
on bindings. The old sentence assigning selection to Context configuration
is removed, following the selection change in interfaces PR #36. Names and
descriptions explicitly identify the slices as lab adaptations.

The shared interface drafts and frozen round-5 inputs remain unchanged. The
role policy text follows ob's existing `internal/app/role_admission.go`:
admission is compatibility evidence; runtime support, authorization, and
executable bindings still matter. Preferences order eligible external
delegates, with registration order resolving ties in ob. This describes ob's
application policy; the delegate-manager contract does not prescribe tie
handling. The preview does not implement the comparison or dispatch engine.
