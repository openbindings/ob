# Handoff: the ob CLI command-surface design

Written 2026-10-01 for an agent picking up this work from a Claude Code
session. It assumes no memory of that session. Read it top to bottom once;
after that, the "Next steps" section is the checklist.

## 1. What this work is

The goal, in Matt's words: make the `ob` CLI command surface "S-tier", the
obvious, ubiquitous, unopinionated handler of OpenBindings interface
documents (OBIs), the way git is for repositories, designed against the
OpenBindings 0.2 specification.

This is **design only**. The Go SDK is being rebuilt, so nothing here is
implemented for real. The work lives in a **playable preview**: a build of
`ob` whose every command answers from a built-in sample document (a Task
Manager API) and a pretend installation. Edits print the diff they would
make; `invoke` and `validate` really read and check input values; nothing
is read from or written to disk (beyond the binary itself) and nothing is
called. Matt plays with it to make decisions, and two AI reviewers rank it
against peer CLIs.

**The bar** Matt set: a top-three overall ranking from every reviewer, with
no blocking findings. Current state: Opus meets it (3rd, no blocking);
Astra does not yet (5th, two blocking), but both of Astra's blocking
findings are already fixed in the lab and await re-review. See section 5.

## 2. Where everything lives

All paths are on Matt's machine.

### The lab (the preview)

- Worktree: `/Users/matt/Code/ob-pj/openbindings/ob-cli-surface-lab`
- Branch: `codex/cli-surface-lab` in the `openbindings/ob` repository.
  **Local only, never pushed.** Do not push without Matt's explicit approval.
- Head at handoff: `b62c182`. The working tree is clean.
- Code: `internal/cmd/surface_next*.go` (the tree is `NewNextSurfaceRoot`).
  `cmd/ob/main.go` builds it by default.
  - `surface_next.go`: root, plumbing (`nxCtx`, `nxRun`, exit helpers
    `nxFail`, `nxRefuse` (3), `nxNotFound` (2), `nxUsageErr` (2, no
    banner)), document gate for URLs, edit writing (`wrote`).
  - `surface_next_docs.go`: init, set, show, validate, diff, fmt, patch,
    merge, synthesize, status.
  - `surface_next_parts.go`: operation, source (incl. inspect, pull), binding,
    dependency, schema (incl. bundle).
  - `surface_next_use.go`: fetch, context, codegen, compat, start, ca, mcp,
    kind, delegate.
  - `surface_next_invoke.go`: invoke (the invocation handle).
  - `surface_next_rules.go`: the preview's document-rule checker (which edits
    use to refuse new violations) and schema traversal.
  - `surface_next_fixture.go`: the sample document, the sample contract, the
    pretend installation (kinds, delegates, context store, published
    schemas).
  - `surface_next_schema.json`: embedded copy of the 0.2 derived schema.
  - `surface_next_test.go`: the lab's tests.
- Play guide: `COMMAND_SURFACE_LAB.md` in the worktree root. Its
  **"Decided" section is the single current list of Matt's rulings.** Keep
  it current; review briefs point at it.

Build, play, test (always `GOWORK=off`):

```sh
cd /Users/matt/Code/ob-pj/openbindings/ob-cli-surface-lab
GOWORK=off go build -o ./bin/ob ./cmd/ob
./bin/ob --help
GOWORK=off go test ./internal/cmd -run 'TestNext|TestV02|TestSurface' -count=1
```

The lab tests must pass before every commit. Other tests in the branch (the
pre-existing production `ob` tests) fail and are expected to: `cmd/ob` now
builds the preview, and the sibling SDK and spec checkouts have moved.
Ignore them.

### Reviews

- `/Users/matt/Code/ob-pj/design/ob-cli-surface/review-1` through `review-4`.
  Each holds a frozen read-only copy of the binary (`ob`), the guide, the
  spec texts, `brief.md`, `PINS.txt`, and the two reports (`astra.md`,
  `opus.md`).
- `review-4/brief.md` is the current brief template. It points reviewers at
  the guide's "Decided" section instead of restating rulings.

### The specification and interfaces the CLI serves

- Spec: `openbindings/spec`, branch `release/0.2`, pinned at `de2c20b`
  (`openbindings.md`, and the companion `http-discovery.md`).
- Interfaces: `openbindings/interfaces` (operation-invoker, binding-invoker,
  token-provider, schema-comparison profile OB-2020-12, delegate-manager).
  **None of the interfaces is published**; their `0.1.json` files are drafts
  edited in place.
- Open PR: [openbindings/interfaces#36](https://github.com/openbindings/interfaces/pull/36),
  branch `fix/strict-binding-selection` (pushed, head `5e24312`). It moves
  the operation invoker's ordered binding `selection` out of
  `context.configuration` into `OperationInvocationInput` beside `binding`,
  and makes it strict (unknown or foreign keys and a list with nothing
  invocable fail with `ERR_BINDING_NOT_FOUND`; no fallback). Matt approved
  opening it; **it is not merged**, and merging needs his word. Interfaces
  `main` CI has been red since 2026-09-22 for unrelated reasons. Still reading
  the old `context.configuration.selection`: production ob
  (`internal/app/invoke.go`, `internal/mcpbridge/selection.go`,
  `internal/cmd/mcp.go`) and the TS SDK (`packages/invoke`); the rebuilt Go
  SDK has no invoke layer yet. A scratch worktree of the branch sat in the
  Claude session's temp directory and may be gone; recreate from origin if
  needed.

## 3. How Matt works (rules for this work)

- **Current focus** (Matt, 2026-09-30): only the spec, the Go SDK, and the ob
  CLI surface design matter. Everything else (TS parity, JSONata, binding
  specs, elements, web, the project cohort) can be ignored, and red CI
  elsewhere is fine.
- **Design decisions about OpenBindings belong to Matt.** Fixes that follow
  from an existing ruling, from the spec, or from an interface can be made
  directly. Anything that chooses between designs goes to Matt, as a short
  story from zero with a recommendation, never a terse jargon menu.
- **Never present a reviewer's point, or your own proposal, as Matt's
  ruling.** Briefs list only real rulings.
- **Check every reviewer finding against the spec and interfaces before
  relaying it.** Several findings in past rounds were wrong because the
  reviewer lacked a file; reject those with the reason.
- **No invented conventions.** ob never relies on something the spec does
  not define (no `*.obi.json` file discovery; the spec registers no file
  extension). State spec semantics in the spec's own words (for example, a
  dependency without `kinds` "declares no kind constraint", not "accepts any
  kind").
- **One irreducible thing per command; no macros.** (A command that applies
  one job to a whole document, like `schema bundle`, is fine.)
- **Never push or deploy without explicit approval in the current turn.**
  Commit locally freely.
- **No Co-Authored-By or AI attribution lines** in commits or PR bodies.
- **Avoid em dashes** in anything Matt reads.
- Read a file before overwriting it. Use worktrees; Matt works in repos live.
- Reviewer method: rank among named successful peers, never grades.

## 4. The design, in brief

Read the guide's "Decided" section for the full list. The load-bearing
ideas:

- **Explicit document argument**; edits change the file in place
  (`--dry-run` previews, `-` reads stdin and prints the result, and a `-`
  filter always passes the document on).
- **One grammar for every part** of a document: add, set, rename, remove,
  list, show. `rename` renames (no alias side effects; refuses an alias).
  `--unset` takes a flag's own name. Edits refuse to write a document that
  breaks a document rule it did not already break, and name the rule.
- **One exit table**: 0 done or yes (an already-true edit is "no change"),
  1 failed or no, 2 usage error (including a name not in the document),
  3 refused (understood and declined; nothing written or sent), 4 no
  verdict, 130 cancelled.
- **`ob invoke` is one invocation as a Unix process**, following the
  operation-invoker interface: each `--input` is the next write (values,
  `@FILE` streams, or `-` for stdin), end of input closes, outputs print as
  they arrive, `--frames` prints the interface's frames, Ctrl-C cancels, the
  exit status is the terminal state. Binding choice follows the
  interface's selection rule; `--binding` never falls back. Values are always
  checked (no `--no-check`).
- **Context** follows the ratified context resolution pattern: a binding
  asks by an exact scope; ob files and fetches by that exact scope, reuses
  only durable values, sends only what is needed. Delegates never get ob's
  context store. Away from a terminal, a sign-in refusal prints the same
  invoke with `--preflight`. Secrets never go on the command line.
- **Sources**: `source pull` refreshes bindings and adds nothing unasked;
  `status` separates drift from targets left unbound.
- **`compat`** checks each operation in its role (provider if bound,
  consumer if a dependency calls it, spec §5.5) under the comparison profile
  OB-2020-12, whose verdict order is indeterminate > incompatible >
  compatible.
- **`ob mcp` is a placeholder** until an MCP binding specification exists
  for 0.2 (the existing `binding-specs/mcp` candidate predates the kinds
  model and counts as nothing). Principle to keep: `ob mcp` should be the
  inverse of the MCP binding.
- **`ob schema bundle`** embeds external schemas by `$id`, changing no
  `$ref` (JSON Schema's own bundling, spec §7.4). It is not "pull": bundling
  cuts the link, and the document records no provenance.

## 5. Review history

| Round | Astra | Opus | Notes |
| --- | --- | --- | --- |
| 1 | 6th | 6th | before the invocation rework |
| 2 | 6th, 2 blocking | 4th, 2 blocking | refusal exit codes; mcp/codegen binding choice |
| 3 | 4th, none blocking | 4th, none blocking | |
| 4 | 5th, 2 blocking | **3rd, none blocking (meets bar)** | Astra's blockers were preview-checker bugs, fixed after the freeze |

Peers used every round: git, gh, kubectl, cargo, Terraform CLI, buf,
Redocly CLI. Criteria: learnability in the first hour, consistency, coverage
of jobs, editing ergonomics, output and scripting, errors and recovery,
fidelity to the model, economy of surface, overall.

### Fixed since the round-4 freeze (lab `29d7de2`), not yet re-reviewed

- `d8d27ee`: **Astra's two blocking findings.** The preview checker now
  follows spec §7: it walks only the keywords that hold schemas (never
  `const`, `enum`, `examples` data), stops at `$id` resource boundaries,
  percent-decodes fragments, resolves `#/operations/<op>/input` pointers and
  plain names; OBI-D-10 is a meta-schema check that resolves no references
  (spec text: it "resolves none of the document's references").
- `a1b733d`: `compat` states the profile's verdict order and uses its words
  (compatible, incompatible, indeterminate).
- `b62c182`: `validate` JSON has one shape (`results` array); renaming to
  the same name is "no change"; `Authorization`, `Proxy-Authorization`, and
  `Cookie` headers refuse literal values; `--config` takes `@FILE` and `-`;
  `--token-binding` keys apply per operation.
- Continuation, 2026-10-01: Matt approved the revised source-pull naming
  design after a panel of three AI reviewers supported it with changes.
  `--operation` remains an existing-operation lookup; `--new-operation`
  makes creation explicit, and `--binding-key` names a binding. The lab
  models exact retries, occupied-key refusals, additional bindings for an
  already covered target, and atomic bulk naming refusals. Bulk dry runs
  show choices before the edit; post-edit guidance uses `binding set`.
  The guide's "Decided" section records the current rules. This was a
  focused design review, not the full round-5 ranking.

## 6. Pending work

### A. Agreed fixes (applied in the continuation, 2026-10-01)

From the round-4 reports, verified and applied in the preview. The lab suite
includes focused regression checks; these changes await the round-5 freeze
and fresh independent reviewers. This list records the fixes, not pending
decisions:

1. **Name the rule in every edit refusal.** Add OBI-D-03 (names match
   `^[A-Za-z0-9_][A-Za-z0-9_.-]*$`), OBI-D-06 (`$schema` must be the 2020-12
   URI), and OBI-D-13 (no duplicate plain names or `$id`s) to the preview
   checker. A bad part name, an empty version label, and a draft-07
   `$schema` should be refused with exit 3 naming the rule, instead of exit 2
   or OBI-D-02 validator noise. `operation add` with a name that is already
   an alias should name OBI-D-04.
2. **`context` text output uses the flag vocabulary** (`bearer-token`,
   `header.X-Client`, `config.server`) so it round-trips with `--unset`; JSON
   keeps the interface's field names, and the help says so.
3. **JSON shapes**: `validate --examples` reports booleans; `schema list`
   entries share one shape; `status` and `source inspect` stop using
   `binding` for two different things.
4. **`compat` follows the rest of the profile**: per-operation issue kinds
   (`missing`, `input_incompatible`, `output_incompatible`) with reasons, in
   sorted operation-key order.
5. **`schema bundle` refuses what breaks its promise**: a URI whose fetched
   schema declares a different `$id` (references would still need the
   network), or a non-2020-12 `$schema`; such a URI stays listed as external,
   with the reason.
6. **`context show` masks values completely** (binding-invoker: structural
   redaction, "never the secret value"); JSON marks them masked.
7. Smaller:
   - `source import` and `synthesize` help point to `source pull --target`
     or `--all-targets` as the next step;
   - `ob mcp` warns at startup about bindings that need context nobody has
     stored;
   - `context set` notes a scope no binding has asked for (a hint, never a
     refusal, never normalization);
   - `ob help nope` exits 2 with a suggestion;
   - `--token-credential` can rotate on its own when a provider is pinned;
   - shell completion of operation, binding, source, and schema names;
   - `kind list` JSON reports handlers per role;
   - fix the `operation set` help that calls removing an absent alias
     "refused" (it is exit 2), and the guide line that calls
     `--bearer-token abc` "refused".

### B. Decisions waiting on Matt (with the recommendations he was given)

The source-pull naming decision is settled in the continuation above.
The following five decisions remain open.

Continuation: their recommended sides are implemented as **reviewable
proposals** in the default preview, awaiting Matt's answers. They are listed
under the guide's Open decisions, outside Decided, and remain open for the
round-5 reviewers to challenge. Their implementation is not approval.

1. **Named credentials.** Recommendation: `--credential NAME=-|@FILE`,
   `--cookie NAME=…` (like `--header`), `--refresh-token -|@FILE`, matching
   `--unset` names, and `ob context --help` listing every field a context can
   hold.
2. **`--config` typing.** Recommendation: `POINT=VALUE` is always a string;
   `POINT:=JSON` gives a typed value (httpie's convention).
3. **`kind check` without `--role`.** Recommendation: without `--role` it is
   a report (exit 0); with `--role` it is a yes/no check. Changes a round-2
   fix.
4. **`compat` for an operation with neither bindings nor dependencies** (every
   operation of a pure contract). Recommendation: check it both ways. Changes
   the round-3 "as a provider".
5. **How local tools get `ob start`'s access token.** Recommendation: `ob
   start` writes its address and token to a file only the user can read; ob's
   own commands read it; `ob start -F json` prints the same as one line.

### C. Findings rejected, with reasons (do not redo)

- **`invoke` should exit non-zero after the binding drops extra input
  values** (raised twice by Opus). Matt ruled that the exit status is the
  terminal state, and the binding-invoker contract says input after
  `input_closed` is ignored while the invocation continues. ob notes the
  dropped values on stderr. Matt may reopen it.
- **Removing something absent should exit 0** (Opus). Matt's round-3 ruling
  makes naming something not there a usage error (exit 2); adding what is
  already there is "no change" (exit 0).
- **`status --exit-code` should keep 4 over found drift** (Astra, round 3).
  ob's own status lets a known answer outrank no verdict, as `validate` does
  per spec §10.4. (`compat` is different: its profile defines
  indeterminate > incompatible.)
- **HTTP Discovery is invented** (round 2): it is the spec's companion
  `http-discovery.md`.
- **Delegate operation names contradict binding-invoker**: the lab follows
  Matt's ruled kinds rework (`listSupportedKinds`, `checkKindSupport`), not
  yet applied to the interface files.
- **`operation show -F json` should include the resolved key**: `-F json`
  prints the exact stored part, like every `show`.
- **`fmt --check` should fail on the sample**: a preview artifact; the
  sample is formatted.

## 7. Next steps

1. Check Matt's answers on the five remaining decisions in 6B. Until he
   answers, the recommended defaults remain proposals, open for review.
   Only his rulings go into the guide's Decided section.
2. The agreed fixes and recommended proposals are applied. Commit in small
   steps, tests green each time; update the proposals when Matt answers.
3. Run the guide's command list end to end and confirm each exits as the
   guide says (a loop over its `./bin/ob` lines does it).
4. Freeze round 5: create `design/ob-cli-surface/review-5/` with the built
   binary, the guide, `openbindings.md` and `http-discovery.md` from spec
   `de2c20b`, the interface READMEs (operation-invoker and binding-invoker
   from the PR #36 branch; token-provider; schema-comparison), a `PINS.txt`,
   and a `brief.md` copied from `review-4/brief.md` with the round number
   changed. Make the frozen files read-only.
5. Run fresh independent reviewers here in parallel, with the same brief
   and frozen inputs. Matt explicitly excluded Claude for this round on
   2026-10-01. Use fresh Codex reviewer agents, not the completed source-pull
   panel; each works read-only and returns its full ranking report. Save
   their reports separately, then adjudicate them against the pinned texts.
6. Adjudicate: verify every finding against the spec and interfaces; sort
   into rejected (with reasons), fixes that follow from rulings, and
   decisions for Matt (stories with recommendations). The bar is met when
   every reviewer ranks ob top three with no blocking findings.

## 8. Gotchas learned the hard way

- **The preview checker is part of the surface's credibility.** Reviewers
  judge refusals as if they were real. Keep it faithful to spec §7 and
  §10: walk schemas only through schema keywords, stop at `$id` boundaries,
  decode fragments, and never evaluate or resolve references for OBI-D-10.
- `GetStringArray` drops empty strings; read repeatable flags with
  `c.strs`, which uses the flag's own slice.
- Never give a flag a `NoOptDefVal` if it must also take `-` as a separate
  argument (`--basic -` broke that way).
- The example test runs every `ob ...` line in help examples, including the
  `ob` command after a `printf '...' |` pipe (fed that stdin). Examples must
  succeed (anything but exit 2), so the pretend installation is arranged to
  make them work (for example, the sample has the certificate authority
  installed).
- Pretend services for document URLs: `private.example.com` asks for
  sign-in, `nothing.example.com` publishes no OBI, `old.example.com` serves
  an OpenBindings 0.1 document. Published schemas under
  `https://schemas.example.com/...` come from a pretend store, so checks and
  `schema bundle` run offline.
- The preview binary pauses about 5 seconds per run in the Claude desktop
  app's terminal panel: a library linked in from the old ob code queries the
  terminal's background color at startup, and that terminal does not answer.
  Not a surface issue.
- The Opus subagent delivers its report as its final message, not a file;
  save it to `opus.md` yourself.
