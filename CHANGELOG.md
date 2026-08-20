# Changelog

All notable changes to `ob` will be documented in this file.

Section conventions (Added / Changed / Fixed / Removed) follow
[Keep a Changelog](https://keepachangelog.com/); versions follow
[Semantic Versioning](https://semver.org/). The next release accumulates
under a `(working draft)` heading, which gains its date when the release
is tagged.

## 0.2.0 (working draft)

**Spec:** implements OpenBindings 0.2.0 (working draft).

A 0.1.1 patch release was prepared in April 2026 but never tagged or
released; its changes (TLS setup bulletproofing, the newer-version notice)
ship as part of 0.2.0.

### Changed

- **OpenAPI auto-detection now selects current `openbindings.openapi@1`.**
  Exact schema-omitted OAS 3.0 non-JSON request and response representations
  cross the protocol-independent boundary as canonical Base64. The CLI keeps
  exact revisions 6, 5, 4, 3, 2, and 1 for invocation and synthesis
  compatibility. Declaration-complex JSON and explicitly dynamic object
  bodies retain their protocol-neutral operation shapes and binding-private
  routing. Core is unchanged.

- **`ob start` no longer modifies system trust or accepts arbitrary HTTPS
  origins by default.** The zero-configuration server is loopback HTTP only.
  `--tls` adds HTTPS without installing trust, while `--trust-local-ca`
  explicitly authorizes trust-store installation and implies TLS. Remote
  browser origins now require an exact `--allow-origin`; loopback origins
  remain available by default. HTTP and WebSocket use the same origin policy.

- **`ob start` now serves an embedded OpenBindings workbench at its root.**
  The framework-neutral OBI explorer, operation detail, and invocation
  elements are built from the separate `openbindings/elements` workspace.
  The browser discovers the server OBI and fulfills its published Operation
  Invoker dependency through `ob`; raw API URLs can be synthesized and
  invoked without bundling protocol-family implementations in the page.
  Session authentication and target invocation context remain separate, the
  URL-fragment token is scrubbed after bootstrap, and generated assets are
  protected by the server's no-store and content-security policies.

- **`ob start` now has a browser-first startup and workbench experience.**
  Interactive startup prints a clickable authenticated workbench URL, `--open`
  launches it directly, normal output is quiet, and `--verbose` restores
  request diagnostics. The URL fragment is consumed before discovery and kept
  only in tab-scoped session storage. The workbench separates local-session
  and target credentials, preflights standard context requirements without
  inventing protocol-specific configuration, supports explicit binding
  selection, derives conservative input starters from schema evidence, and
  pairs concise failures with exact technical details.

- **WebSocket frame endpoints now complete a browser-clean close handshake.**
  The application-frame reader stops before the WebSocket library reads the
  peer close response, and final cleanup no longer sends a second close frame.
  This removes the browser-visible abnormal `1006` / “Close received after
  close” result while retaining exactly one application terminal frame.

- **The generic `ob mcp` adapter now preserves the complete abstract
  OpenBindings boundary without binding-family guesses.** Explicit object
  inputs remain direct; every other JSON input uses an optional, reversible
  `{"input": ...}` envelope so the per-value schema does not invent input
  cardinality.
  Results use one structured and text-compatible `{"outputs": [...]}` envelope
  so zero, one, many, and explicit-null outputs remain distinct, and terminal
  errors retain partial outputs plus their OpenBindings code, message, and
  details. Startup reports and excludes operations with unusable wiring or
  unresolved binding ambiguity; repeatable `--select-binding` supplies
  explicit selection. `--context` and `--configuration` expose ordinary
  invocation context without guessing carriers. Raw-artifact acquisition now
  retains synthesis coverage in both SDKs; `--coverage-report` persists it and
  `--require-complete-coverage` can gate bridge startup.

- **`ob mcp` now preserves the represented MCP-native surface instead of
  flattening it.** Exact, untransformed `openbindings.mcp@1` bindings retain
  original tool names and primitive families (tools, static resources,
  resource templates, prompts), complete descriptors from embedded listings,
  complete success and application-error results, progress, and cancellation.
  Resource-template reads use a narrow same-protocol forwarding lane because
  expanded RFC 6570 URIs are not generally reversible to operation variables.
  A real differential test exercises MCP server → synthesized OBI → bridge
  across descriptors, structured/multimodal content, metadata, errors,
  progress, resources, prompts, and cancellation. Transformed MCP bindings
  remain abstract operations and use the generic tool adapter.

- **CLI and served-API correspondence now includes GraphQL authoring.** The
  cross-surface conformance lane inspects and synthesizes the same embedded
  GraphQL introspection artifact through the bound CLI OBI and the `ob start`
  OBI, comparing canonical operation results just as it already does for
  OpenAPI.

- **The experimental Workers RPC registration was removed.** `ob` no longer
  advertises a token backed only by a Go stub that could neither invoke nor
  synthesize its source family. The CLI now reports only complete native
  binding implementations.

- **GraphQL is now a first-class native `openbindings.graphql@1`
  implementation.** The CLI no longer advertises or emits the legacy
  versionless token. Its demo uses canonical lower-case refs, exact
  executable-document configuration, and output transforms that explicitly
  unwrap the binding specification's complete GraphQL response envelope.
  `ob op invoke` and `ob op prepare` accept `--configuration` as an inline
  JSON object, `@file`, or stdin, carrying the same named binding-spec
  interpretation points that `ob start` accepts through
  `context.configuration`.

- **Delegate frame-endpoint resolution moved behind the SDK's asyncapi
  seam.** Resolving a delegate's advertised `invokeBinding` endpoint from
  its AsyncAPI document now uses the format package's exported resolution
  (`asyncapi.ParseDocument` / `Document.ResolveEndpoint`) instead of a
  hand-rolled parser copy inside ob — one owner for the
  `openbindings.asyncapi@1` §9.2 server-selection and address rules. ob
  keeps only what is its own: the document fetch policy and the ws(s)
  upgrade-scheme spelling.

- **Renamed the `ob serve` command to `ob start`.** The local server (HTTP +
  WebSocket) now starts with `ob start`, aligning the command with the
  `startServer` operation it realizes and reflecting that it boots ob's full
  local surface rather than serving a single artifact. The `OB_SERVE_*`
  environment variables are correspondingly renamed `OB_START_*`. No `serve`
  alias is retained (pre-1.0).
- **`ob start` no longer serves a built-in `/mcp` endpoint.** ob's served
  interface is exposed as an MCP server by pointing the bridge at a running
  server — `ob mcp http://127.0.0.1:20290` — which dogfoods the same OBI→MCP
  path ob offers for any interface. This removes a hand-maintained set of MCP
  tools that duplicated the REST handlers (and could drift from them); the MCP
  surface now tracks the REST surface automatically. The served OBI
  (`serve.obi.json`) consequently carries only `openapi` + `asyncapi` sources.
- **`ob mcp` is now single-interface.** It bridges one OBI URL (was `<url>...`);
  to expose several services as one MCP server, compose them into an aggregate
  OBI (`ob merge`) and bridge that. MCP tool names are now the operation key
  sanitized to the tool-calling charset (`[A-Za-z0-9_-]`, ≤64) — e.g.
  `openbindings.ob.describe` → `openbindings_ob_describe`, with a numeric suffix
  on the rare sanitize collision. This stays faithful to the operation identity
  (the bridge assumes no key convention) and replaces the previous
  `<interface>.<full.op.key>` scheme, which double-namespaced and used dots that
  some agent tool-calling APIs reject.
- **Migrated to the SDK's cardinality-agnostic Invocation handle** (the 0.2
  invoker model: write inputs until done, read outputs until done; one shape
  for unary, streaming, and bidirectional bindings).
  - `ob codegen --lang go` emits **operation signatures**, not a bound invoker:
    the per-operation input/output structs plus an `OperationSignatures`
    namespace (one `invoke.OperationSignature[I, O]` per operation, built
    via `NewOperationSignature`). Callers invoke with the free verb,
    `invoke.Invoke(ctx, invoker, obi, sig, ...)`, and drive the
    cardinality-agnostic handle (`invoke.Single` for one-shot outputs).
    No per-operation methods, no bound invoker struct, no embedded contract: the
    interface is supplied at call time, and named schemas shared across
    operations are emitted once. `--lang typescript` emits the same shape: an
    `OperationSignatures` const of branded `OperationSignature<I, O>` values
    (built via `operationSignature`), invoked with the invoker method
    `invoker.invoke(obi, sig)` and driven with `single(call.outputs)`. Both
    emitters reuse a schema shared across operations as one type. Emitted headers
    declare the SDK range they target.
  - The app layer owns CONTEXT_REQUIRED negotiation for its binding-level
    calls: challenges raised before any output are resolved through the
    configured resolver and re-driven with merged context. The CLI's resolver
    composes the context store (under the challenge key) with interactive
    per-requirement prompts, persisting acquired credentials; `ob mcp`'s
    bearer token flows as invocation context.
  - `ob start`'s `/bindings/invoke` speaks the `openbindings.binding-invoker`
    frame protocol over WebSocket: the caller streams `open`/`input`/`close`
    frames, the server streams `output`/`input_closed` frames and exactly one
    terminal `complete`/`error` frame. One connection per invocation; every
    cardinality crosses the wire. The session token rides the upgrade request
    (`Authorization` header, or an unpadded-base64url
    `openbindings.bearer.<token>` request subprotocol for browsers, alongside
    the selected non-secret `openbindings.frames.v1` protocol); URL query
    credentials are rejected and credential protocols are never echoed. The
    legacy WS event envelope and the unary `POST /bindings/invoke` route are
    gone. `POST /bindings/prepare` implements the now-required
    `prepareBinding` preflight.
  - Delegate-backed binding invocation speaks the same frame protocol over
    WebSocket when a delegate advertises an asyncapi-bound `invokeBinding`
    with an http(s) location, exposing a normal `Invocation` handle (with
    `ERR_TRANSPORT_CLOSED` synthesized when the transport closes without a
    terminal frame); delegates advertising only a usage (CLI) binding keep
    the unary CLI realization. The `execute --as-delegate` fallback is
    removed.
  - Error codes follow the SDKs' new SCREAMING wire values (`ERR_CANCELLED`,
    `CONTEXT_REQUIRED`, ...).

- **Roles/satisfies removed** per spec 0.2.0: correspondence to a shared
  contract is the operation key+alias namespace (OBI-T-12).
  - `ob conform` scaffolds under the contract's operation name and declares
    correspondence with aliases; `--role-key` removed.
  - `ob validate` no longer performs role-conformance fetching; `--skip-roles`
    removed.
  - `ob compat` and `ob diff`/comparison pair operations by key+alias only
    (the `paired_via.satisfies` counter is gone from comparison reports).
  - ob's own OBI documents (`ob.obi.json`, `serve.obi.json`) drop their
    `roles`, `satisfies`, and `security` fields.
  - The comparison fixture corpus moved into this repo
    (`internal/app/testdata/comparison-corpus`), per the spec repo's decision
    that comparison semantics are a tool concern.

- **CLI commands renamed** to align with the OpenBindings spec 0.2.0 "executor → invoker / invoke" rename. Pre-1.0 hard rename, no deprecated aliases.
  - `ob op exec` → `ob op invoke`. The `execute` alias was removed.
  - `ob binding exec` → `ob binding invoke`. Source file `internal/cmd/binding_exec.go` → `binding_invoke.go`.
  - SDK identifier consumers updated throughout: `OperationExecutor` → `OperationInvoker`, `BindingExecutionInput` → `BindingInvocationInput`, `ExecuteBinding(...)` → `InvokeBinding(...)`, `ExecuteOperation(...)` → `Invoke(...)`, `AddBindingExecutor` → `AddBindingInvoker`, per-format `NewExecutor` → `NewInvoker`, `ExecutionOptions` → `InvocationOptions`, `ExecuteError` → `InvocationError`, `ExecuteOutput` → `InvocationOutput`.
  - App-level types: `ExecuteOperationInput`/`Output` → `InvokeOperationInput`/`Output`; `ExecuteOBIOperation` → `InvokeOBIOperation`; `ExecuteBindingInput`/`Result` → `InvokeBindingInput`/`Result`; `DefaultExecutor` → `DefaultInvoker` (and helpers).
  - OBI files: role `openbindings.binding-executor` → `openbindings.binding-invoker` (path + URL) in `internal/app/ob.obi.json` and `internal/server/host.obi.json`; `satisfies[*].operation` value `executeBinding` → `invokeBinding`.
  - `usage.kdl`: subcommand `cmd "exec"` (under both `operation` and `binding` parents) → `cmd "invoke"`.
  - Demo command output (`ob demo`) updated to print `ob op invoke ...` examples.
  - README "Execute an operation" section → "Invoke an operation"; prose "binding executor" → "binding invoker" throughout.

- **Delegates are registered interfaces, not config entries.** A delegate is
  any OBI registered by location (`ob delegate register <location>`); its
  capabilities are detected from its own operations by correspondence
  (operation key + published alias, per the `openbindings.delegate-manager`
  contract) and snapshotted at registration. Selection is preference-ordered
  per operation, with ob itself standing as the self-delegate:
  `ob delegate resolve <operation>` shows the ranking, `ob delegate prefer`
  sets delegate-level or per-offering preference, and
  `ob delegate requirements <capability>` prints the interface a delegate
  must correspond to for a capability. Synthesis, inspection, and binding
  invocation all route through the same selection — operation-invoking the
  delegate's own OBI rather than shelling into a hard-coded plugin seam.
  Wrapper-era registrations and pre-rename registry records are refused
  loudly with a re-register instruction, never silently skipped.
- **The context store is a keyed, hierarchical surface.** Contexts are
  user-scoped and keyed by target URL with hierarchical resolution (exact
  key first, then up the path); credential fields ride the OS credential
  store, everything else the config file. The three context operations
  (`getContext` / `setContext` / `removeContext`) declare correspondence to
  the published `openbindings.document-store` interface (`get`/`set`/
  `delete`) — retargeted from that interface's earlier `key-value-store`
  name — and `context set` is full-replacement per that contract.
- **Exact binding-specification identifiers replace format tokens.** What a
  source names is a binding specification (`openbindings.openapi@1`,
  `openbindings.usage@1`, …) — an exact, opaque identifier, not a "format"
  with semver-range matching (the range machinery is deleted; matching is
  string equality per spec §6). The wire field renames from `format`/`token`
  to `bindingSpec` across ob's contract schemas, and the CLI adopts the
  native vocabulary: `ob formats` → `ob binding-specs`,
  `ob delegate resolve-format` → `ob delegate resolve-binding-spec`,
  `delegate prefer --source-format` → `--binding-spec`, served
  `GET /formats` → `GET /binding-specs`, operation keys `listFormats` →
  `listBindingSpecs` and `resolveDelegateForFormat` →
  `resolveDelegateForBindingSpec`. Output rendering keeps `--format` (the
  one genuinely format-shaped concept). `ob binding-specs` marks ob's
  pre-promotion drafts `(draft)` — the bare tokens (`graphql`,
  `workers-rpc@^1.0.0`) whose identifiers aren't minted yet.
- **The transform evaluator now runs the gnata JSONata engine**
  (`recolabs/gnata`, pure Go, replacing `blues/jsonata-go`). gnata is
  materially closer to the normative jsonata-js implementation: it preserves
  object member order, closes the reachable `{"v": $}` divergence, and
  evaluates ob's production transforms identically to the reference. A
  differential-conformance gate runs the spec repo's transform corpus
  through the engine — the agreement set must reproduce jsonata-js output
  and the known-divergence catalog must still hold — so silent engine drift
  fails the suite.
- **Exec addresses require explicit operator authorization** (the usage
  specification's USAGE-P-02 policy). An exec source address is dereferenced
  only when the operator authorized it: recorded in the environment config's
  `authorizedExec` list, or standing via delegate registration (the
  registered delegate's own exec location). Operator-typed intake
  (`source add` / `synthesize` / `inspect` positional sources) records the
  address durably with a notice; machine lanes (`--input`, the served
  surface, delegate invocations) never auto-authorize; everything else
  refuses, per the specification's default.
- **`ob op invoke` grew the consumer data face, and every output is
  contract-validated.** The per-invocation knobs are explicit, per-axis
  flags — `--decode json|text|none`, `--ok-exit` (the diff(1) class: exit
  codes that are results, not failures),
  `--route field=argv|stdin|stdin-dash|file`, and `--input` with the house
  grammar (inline JSON | `@file` | `-`) — compiled to per-invocation hooks;
  a typo in a lane or channel refuses loudly. Dispatch is unified under
  delegate selection,
  computed pre-dispatch: when a preferred external delegate owns the hop,
  displaced *flags* refuse (naming the delegate) while displaced *standing
  elections* proceed with a loud attributed warning (stderr, plus
  `x-ob-displaced-elections` in the machine envelope). The app-driven
  binding path now enforces OBI-T-08: every output is validated against the
  operation's declared schema before it reaches the caller. `-F json` emits
  the machine-lane envelopes — one terminal `{outputs, metadata}` object on
  success, the `InvocationError` envelope on failure. A new
  `ob operation output-schema` command makes the output-schema election
  first-class: it writes the elected schema into the operation and stamps an
  `x-ob` election marker so `source pull` re-applies it onto fresh
  derivations (a grown source schema wins and displaces it loudly);
  `ob purify` strips the synthesis floor stamps at every depth. Below the
  operation layer, `ob binding invoke` is the wire lane surfaced as a
  command: document-addressed, `inputTransform` applied, output post-decode
  and pre-`outputTransform`, deliberately unvalidated.

- `ob start` now listens on HTTP and HTTPS simultaneously by default. HTTP on
  `--port` (default 20290), HTTPS on port+1 (default 20291). Clients pick
  whichever matches their page protocol. `--no-tls` still available to skip
  the HTTPS listener and CA trust setup entirely (useful in CI and sandboxed
  environments).
- `ob start`'s discovery endpoint (`/.well-known/openbindings`) now responds
  with `Content-Type: application/vnd.openbindings+json; charset=utf-8` per
  spec §7.1 / §14.2. Clients accepting `application/json` continue to receive
  the same body.
- The local HTTPS CA is installed into the system keychain on first run, not
  the user-login keychain, so Chrome and every other browser trust it without
  additional setup. Prompts once for the sudo password.
- On every `ob start` startup, ob verifies the CA is still trusted and
  auto-recovers (purging stale entries and re-installing) if it isn't.
  Broken installs from earlier versions self-heal on next run.
- If TLS install fails (declined sudo, non-interactive terminal), ob logs a
  clear warning and continues serving HTTP-only. Users are never blocked.

### Added

- CORS middleware now responds to Chrome's Private Network Access preflight
  (`Access-Control-Allow-Private-Network: true`), fixing the
  CSP-shaped error Chrome produced when HTTP pages fetched `http://localhost`.
- `ob describe` output now includes the spec version range this CLI supports,
  sourced from the Go SDK's `MinSupportedVersion` / `MaxTestedVersion`.
- `ob` now notifies on stderr when a newer release is available. The check
  probes the GitHub releases API asynchronously (2s timeout, cached for 24
  hours), skips itself when stderr is not a terminal or the build is
  unversioned (`dev`), and fails silently on any error — it never blocks or
  errors the command you ran. Opt out with `OB_NO_UPDATE_CHECK=1`.
- `ob compat` now drives a structural OBI comparison feature
  (`internal/app/comparison.go` plus a conformance test corpus) that powers
  cross-document compatibility analysis used by registries and authoring
  workflows.
- **New `ob synthesize` command** — one-shot derivation of an OBI from binding
  source artifacts (`ob synthesize openapi.json -o api.obi.json`), the CLI
  realization of the contract's `synthesizeInterface` operation. Sources use
  the same `[format:]path[?options]` syntax as `ob inspect` / `ob source add`.
  Every contract operation now has a CLI binding.
- `ob synthesize` and `ob inspect` accept `-` as a source path to read the
  artifact from stdin (`curl -s …/openapi.json | ob synthesize
  openbindings.openapi@1:-`) — the same filter convention the editing
  family's `<obi-path>` already honors. A bare `-` runs format detection
  over the piped bytes. A stdin artifact is content, not a location: it
  embeds in the document exactly like a wire-supplied content source, with
  no location and no pull path recorded.
- **A full interface-authoring command family.** `ob new` creates an empty
  document and `ob meta set` edits interface-level metadata; `ob inspect
  <source>` lists a binding source's bindable targets; `ob source
  add/pull/list/remove` register binding sources and derive operations and
  bindings from them, with `ob status` reporting drift read-only;
  `ob operation add/set/rename/remove/detach/bind/unbind` edit operations by
  hand, `ob operation alias` manages correspondence aliases, and
  `ob operation codegen-name` sets the symbol name codegen emits (persisted
  as `x-ob.codegenName`, surviving pull, stripped by purify).
  `ob conform <contract> <target>` scaffolds or updates operations so a
  target corresponds to a contract interface; `ob merge` applies changes
  selectively (`--ops-only` / `--no-bindings` / `--no-sources`); `ob purify`
  exports a spec-only interface with all `x-ob` vendor metadata stripped.
  `ob op prepare` and `ob binding prepare` answer the preflight question —
  what context an invocation would require — without invoking. `ob new` and
  `ob synthesize` take `--spec-version` (default: latest tested), and
  `source add` takes `--description`.
- `usage.kdl` now documents command aliases (`ob op ls`, `ob ctx`, `ob env`,
  …) and previously undocumented flags (`init --global`, `mcp --token` /
  `--token-file`, `source add --delegate` / `--yes`, `start --no-tls`). A new
  conformance test (`TestUsageKDLMatchesCommandTree`) cross-references the
  cobra tree, `usage.kdl`, and the root contract so the three CLI surfaces
  can no longer drift apart silently.
- **The bound CLI OBI: ob's own CLI is a fully invocable OpenBindings
  interface.** `ob --openbindings` emits a generated bound OBI
  (`internal/app/ob.bound.obi.json`, regenerated via `go generate` from the
  hand-maintained root contract `ob.obi.json` and guarded by a freshness
  test) whose usage source is a generated `openbindings.usage` binding-unit
  document: the pristine `usage.kdl` embedded verbatim (with artifact pin
  and hash), and one unit per contract operation carrying ob's lane
  elections (stdout JSON everywhere; `exit {ok: [0, 1]}` on
  `diff`/`compat`/`validate`, whose exit 1 is a result, not a failure).
  Adaptation between the wire contract and the CLI lives in the generated
  binding — field renames, flag stringification, stdin routing — never as
  wire sockets on the human surface, so `usage.kdl` stays a pure description
  of the CLI. Every contract operation is conformant over the exec lane:
  read/analysis and document-editing commands run as Unix filters (`-` reads
  the interface document from stdin, and editing commands write the modified
  document to stdout — which IS the contract output — with the human summary
  on stderr), `context set` takes `--value` (`-` reads stdin) so credential
  values never ride argv, wire synthesis warnings go to stderr, and
  `ob codegen` gained a `-F json|yaml` machine lane. Only the
  machine-natured commands keep an `--input` flag (`inspect`, `synthesize`,
  `binding invoke`, `binding prepare`): their wire inputs are nested,
  content-bearing objects with no natural argv shape. The `invokeBinding` /
  `invokeOperation` exec bindings are the contract's documented unary
  realization of its frame streams (frame-true invocation rides `ob start`'s
  WebSocket lane). A wire-conformance harness builds the real binary,
  operation-invokes every contract operation the way a delegate registrar
  would, and validates each output against its contract schema — so
  registering `exec:ob` as a delegate works end to end for the whole
  surface.

- **Operation-family outputs are now wire-shaped.** `ob op list -F json` and
  `ob op alias list -F json` emit the contract's bare arrays (previously
  wrapped in an `{"operations": …}` envelope, and `null` when empty);
  `ob op prepare -F json` emits the requirement details or `null` directly
  (previously wrapped in `{"details": …}`), matching `ob binding prepare`.
  Scoped alias listings emit `"aliases": []`, never `null`. Pre-1.0 breaking
  change for scripts parsing the old envelopes.
- Operation-family text now says **source-owned** (matching the contract and
  `op set`/`op detach`) where it previously said "managed", and no longer
  references the retired `ob sync` command.
- **`ob binding list`** — the read view for an OBI's bindings (all of them,
  or filtered to one operation), closing the gap where `operation bind` /
  `unbind` could write bindings you could not view without opening the raw
  JSON. Human render plus a `-F json` machine lane, realized OBI-first as a
  contract operation (`openbindings.ob.listBindings`).
- **File-backed credentials for headless environments.** Setting
  `OB_CREDENTIALS_FILE=<path>` routes credential storage to a JSON file
  backend so authenticated flows work in CI, containers, and sandboxes with
  no OS keychain. The file is created `0600` (an existing file with
  group/other access is refused with a chmod remediation), and the first
  credential write per process prints a one-line stderr notice that
  credentials are stored unencrypted at your request. With the variable
  unset the keychain is used exactly as before, and a keychain failure never
  silently falls through to a file — the raw error (the bare
  `exit status 154` class) is instead wrapped into a message naming the
  cause and the `OB_CREDENTIALS_FILE` escape hatch.
- `ob mcp --tool-timeout` (default 60s). MCP tool calls are request-scoped,
  so a subscription-style operation that never completes now gets an honest
  `ERR_TIMEOUT` refusal naming the mismatch instead of hanging the agent.
- `ob --version` prints the CLI version and its supported spec range.

### Fixed

- **Source derivation now preserves the synthesizer's complete binding
  contract.** Remapping a synthesized binding to a local source key no longer
  discards its input/output transforms, preference, or descriptive fields.
  Bound server generation composes its contract adaptation with the OpenAPI
  revision-5 private route transform, so dynamic document and context objects
  remain protocol-neutral at the public operation boundary and still reach
  the native request body faithfully.

- **Direct binding invocation now validates operation values with the OBI
  document retained as the schema reference root.** The CLI's pre-dispatch
  input gate and per-output gate therefore accept synthesized recursive
  operation-local `$defs` and other legal document-root references exactly as
  the SDK operation layer does, without exposing binding-protocol details.

- **Delegate frame-endpoint resolution had drifted from the asyncapi
  binding spec.** The retired hand-rolled copy (see Changed: the SDK's
  asyncapi seam now owns the resolution) ignored a channel's declared
  `servers` subset, never substituted server `{variables}` or channel
  address `{parameters}` (literal braces could reach the dial), accepted a
  bare operation key where ASYNC-D-03 pins the `#/operations/<key>` pointer
  (and never decoded its RFC 6901 escapes), and skipped the ASYNC-P-01
  accepted-line check. All now behave per `openbindings.asyncapi@1` §9.2
  via the SDK export; refusals are loud, never guesses.

- **Security: a misreporting delegate could exfiltrate another host's stored
  credentials (confused deputy).** On the delegate path, a
  `CONTEXT_REQUIRED` challenge's `target` is asserted by the untrusted
  delegate, and ob fed it straight to the store-backed resolver — so a
  delegate invoking against one host could name a *different* host's target
  and make ob look up that host's stored credentials and forward them to the
  delegate. ob now derives the source's authoritative target itself and
  validates the asserted target against it before any credential lookup, as
  the binding-invoker contract mandates: a mismatch is refused loudly
  (`ERR_PERMISSION_DENIED`, no lookup, no merge), and an unverifiable target
  (inline content, exec refs) withholds provisioning and lets the delegate's
  own challenge surface. The guard applies only to the external-delegate
  path — ob's in-process invoker runs inside ob's trust boundary and is
  unchanged. Additionally, context forwarded to a delegate on the
  post-challenge retry is now scoped to the challenge (least privilege), so
  a delegate never receives credential material outside its own challenge's
  scope.
- `ob op invoke -v` now prints the resolved binding key on stderr — what its
  help always claimed — instead of echoing back the operation key you typed
  (or nothing in `--binding` mode).
- Delegate invoke-binding selection matched only the bare `invokeBinding`
  operation key, so it never found the operation in ob's own bound OBI
  (full contract keys) or in delegates using the own-key + published-alias
  convention (OBI-T-12). It now resolves the delegate's key by key or alias,
  like the synthesize/inspect router already did.
- `synthesizeInterface` silently dropped `content`-provided sources (the wire
  schema's alternative to `location`) on every machine path, including
  `POST /interfaces/synthesize`: the app-layer input struct had no content
  field. Content sources now synthesize and embed correctly.
- `outputLocation` means spec-level `location` in every synthesis lane. On
  content-lane sources (wire `content` sources and the stdin `-` lane) the
  value accidentally landed in `x-ob.uri` only, while the file lanes wrote
  the spec `location` field. All lanes now write `location` — the published
  pointer pairing with the embedded artifact (spec §6.4), exactly as
  `?embed&outputLocation=` always recorded it — and mirror it in `x-ob.uri`.
  The value is recorded verbatim, as on the file lane; OBI-D-05
  absolute-only enforcement stays at the invoke-time gate, which now covers
  every lane through the one shared field.
- `ob codegen` previously generated client code that called `c.Execute(...)`
  in Go and `this.client.execute(...)` in TypeScript. After the spec 0.2.0
  rename those SDK methods became `Invoke` / `invoke`, and the generated
  code no longer compiled against the SDKs. Generated Go now calls
  `c.Invoke(...)` (with helper renamed `execUnary` to `invokeUnary`), and
  generated TS calls `this.client.invoke(...)`.
- `ob op invoke` resolved an operation by key only, refusing the aliases its
  help promised and every mutation command already accepted. It now resolves
  by key or alias — key and aliases are one namespace with equal standing
  (OBI-T-12).
- `--idempotent` / `--deprecated` (on `operation add` / `operation set`)
  compared their value against the literal string `true`, so every other
  spelling (`yes`, `1`, `TRUE`, `on`) silently stored **false** — at exit 0,
  under an "Updated operation" success message — misreporting whether an
  operation is safe to retry. Values now parse with `strconv.ParseBool`
  semantics, and anything unparseable is a usage error (exit 2) that writes
  nothing: a value the user passed is never silently discarded.
- Operation input is validated against the operation's input schema *before*
  dispatch (OBI-T-07 on the app-driven path): schema-violating input no
  longer executes the side effect and then blames the response.
  Output-validation failures now carry the offending payload (truncated),
  the decode stamp, and the response `Content-Type`; `CONTEXT_REQUIRED`
  errors render the full challenge with a copy-pasteable `ob context set`
  hint; and `ob op prepare` acquires the source document the way invoke
  would, so statically declared auth no longer answers "No context
  requirements" from a cold cache.
- Keychain credentials were saved under the raw URL but read under the
  normalized key, so `ob context set` reported success and invoke re-raised
  the identical challenge forever. Keys now normalize at the store boundary,
  matching the config-file store.
- Comparison soundness, in three passes. The compat walk now descends into
  array items and schema-typed `additionalProperties`, and its identity fast
  paths no longer shortcut while a `$ref` remains in the compared subtree
  (two documents can carry the byte-identical pointer whose targets diverge
  per document's own registry) — a breaking change inside a `$ref`'d
  array-item schema was previously invisible. Schema slots only
  distinguished identical-vs-different, so a breaking output change could
  still report compatible: the SDK's subsumption engine now runs as the
  safety net behind the structural walk, and a cross-implementation test
  pins that `ob compat` and the SDK agree in both directions. And scalar
  type changes other than integer↔number were never classified, so a
  string→integer property swap read as compatible; type changes now
  classify, breaking in both directions.
- `ob conform`'s drift gate and delegate capability detection ran an older
  comparison engine that could disagree with `ob compat`; all three now
  consume the same engine's verdicts and cannot diverge. Unverifiable slots
  count as drift — claiming satisfaction on an unverifiable slot would be a
  silent lie.
- Delegate registry writes (`register`/`unregister`/`prefer`) ride a
  fingerprinted compare-and-swap seam with bounded retries, closing a
  cross-process lost-update race; binding-spec-scoped preferences now
  surface in `ob delegate list` instead of being write-only; and read-only
  `resolveDelegate` is served (`GET /delegates/resolve/{operation}`).
- `source add` with an explicit binding spec routed through source detection
  anyway, letting the gRPC probe dial an arbitrary location as a reflection
  endpoint and hang; explicit-spec adds now route by identifier and never
  probe, and detection probes are bounded at 5 seconds each.
- `source pull -o` wrote the human summary over the just-pulled document;
  the summary now goes to the terminal, never the document's path.
- The hand-authored self-OBI declared a stale `InvocationError` (missing the
  `category`/`effects` members the runtime already emits, and closed to
  additions), so `ob compat` reported ob INCOMPATIBLE with two of the seven
  published interfaces it claims correspondence with. The declaration now
  matches the invoker contracts exactly; all seven claims verify COMPATIBLE.
- Output flags are honored or refused, never dropped: lanes that cannot
  honor `-o`/`-F` (the streaming invoke filter, `mcp`, `demo`, `start`)
  refuse them loudly instead of silently ignoring them, and `op invoke`
  validates `-F` and refuses `-o` (no place on a stdout stream).
- The `[format:]path` source parser split `http://…` URLs on their scheme,
  eating them; `ob mcp` was URL-only and now takes any interface locator
  (local file, URL, or a raw spec it synthesizes from — the documented
  raw-OpenAPI example actually works); and an unreadable `--token-file`
  errors instead of silently continuing with no auth.
- `ob init --global` refused to run whenever the global config directory
  existed — but the context store creates that directory as a side effect,
  so any `ob context set` bricked global init. The existence check now keys
  on the environment marker (`config.json`), and a bare directory is
  completed rather than refused.
- Machine lanes emit contract shapes: `ob init -F json` returns the
  contract's `EnvironmentStatus` (was an ad-hoc result), `listContexts`
  never returns `null`, and the served `/resolve` emits
  `ResolveInterfaceOutput` in place of an ad-hoc five-field shape.
- Demo truthfulness: the banner advertised `ob fetch`, a command that does
  not exist (now `ob resolve`); `operation list` / `alias list` only read
  files and now resolve locators the way invoke does; the demo's
  `placeOrder` wrote its HTTP status before `Content-Type`, so Go sniffed
  `text/plain` and every invocation failed output validation; and the demo
  MCP server returns `structuredContent` with its compatibility text shadow
  per MCP 2025-11-25.
- Cosmetics that lied: command-group parents now error on a stray
  subcommand instead of silently printing help; `ob delegate list` no longer
  renders "ob ob"; `ob validate` prints "No rule violations found" rather
  than an unqualified "Valid" (verification is capability-relative, spec
  §10.5); and `ob compat`'s text render names the actual broken field
  (`…/required/lang`, not `/required/1`).

### Removed

- **`ob sync`** — replaced by the source-pull model: `ob source pull`
  derives operations and bindings from registered sources on demand (with a
  removal-aware detect engine), and `ob status` reports drift read-only.
  The sync/conflict machinery is gone; there is no second write path.
- **`ob create`** — `ob new` creates an empty document; `ob synthesize` is
  the one-shot derivation. What ob does with sources is synthesis, and the
  command vocabulary now says so.

### Notes for upgraders

- Users who ran v0.1.0's install flow have an orphaned `OpenBindings Local CA`
  entry in their login keychain. The first `ob start` on 0.2.0 purges it
  automatically and reinstalls the CA system-wide. No manual cleanup required.

## 0.1.0 — 2026-04-15

### Added

- `ob serve` — local HTTP server exposing ob's full capability surface with
  OAuth2 Authorization Code + PKCE authentication
- `ob mcp` — serve interface URLs as MCP tools for AI agents (Cursor, Claude
  Desktop, etc.) via stdio or HTTP transport
- Delegate system — pluggable binding executors (`exec:ob`, HTTP delegates)
  configured in `config.json`
- Context management — global credential and header storage for API
  authentication, accessible via CLI and `ob serve`
- SSRF protection on `/resolve` endpoint
- OpenBindings Interface discovery at `/.well-known/openbindings`
- `ob validate`, `ob diff`, `ob compat` — interface authoring and comparison
  tools
- `ob sync` — synchronize OBI documents with binding sources

### Changed

- Delegates and contexts are now stored in the environment-level `config.json`,
  replacing the previous workspace model
- `ob mcp` now takes interface URLs as positional arguments instead of reading
  from a workspace file
- OAuth tokens are validated through a single store with TTL eviction (fixes
  memory leak in earlier builds)

### Removed

- **Workspaces** — the workspace concept (`ob workspace`, `ob target`,
  `ob input`) has been removed entirely. Delegates and contexts are managed
  directly in the environment configuration.
- TUI browser — replaced by [Panjir](https://panjir.com), a dedicated web UI
  that connects to `ob serve` as an OpenBindings host
