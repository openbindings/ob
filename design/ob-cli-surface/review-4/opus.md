## Findings

There are no blocking findings: 7 major and 10 minor.

### Major

**1. `invoke` drops input values the user supplied and still exits 0.**
- `ob invoke tasks.obi.json completeTask --input '{"id":"a"}' --input '{"id":"b"}'` completes `a`, prints "ob stopped reading after the first" on stderr, and exits 0.
- `printf '{"title":"a"}\n{"title":"b"}\n' | ob invoke tasks.obi.json createTask --binding createTask.http --input -` creates one task and exits 0.
- A script that streams values through `--input -` loses data and is told it succeeded.
- Proposal: when values the user gave were never written because the binding closed its input, finish the exchange but exit 1 ("1 of 2 values written"). Leave the frames unchanged.

**2. Removals contradict the tool's own exit table.**
- The exit table says an edit that is already true is "no change", exit 0. `--add-alias` on an alias the operation already has behaves that way.
- These removals of something absent exit 2 instead:
  - `operation set tasks.obi.json createTask --unset tags` ("there is no tags to remove")
  - `--remove-alias nope` and `--remove-tag nope`
  - `binding set … createTask.mcp --unset preference`
  - `dependency set … notifier --remove-kind example.mcp@1`
  - `delegate set d_91c2 --remove-role synthesize`
- `operation rename tasks.obi.json createTask createTask` exits 3.
- The `operation set` help calls an absent alias "refused", which would be exit 3, but it exits 2.
- Idempotent scripts fail on their second run.
- Proposal: removing something already absent is "no change", exit 0. Keep exit 2 for a part name that is not in the document.

**3. `context` field names do not round-trip.**
- `context show` and `context list` print `bearerToken`, `headers.X-Client` and `configuration.server`.
- `context set --unset headers.X-Client` exits 2 ("takes … header.NAME").
- Even a successful `--unset header.X-Client` reports "removed headers.X-Client".
- Proposal: text output uses the flag vocabulary (`bearer-token`, `header.X-Client`, `config.server`), as the `--unset` ruling implies. JSON keeps the interface's field names and says so.

**4. JSON reports change shape.**
- `validate --operation createTask --input A -F json` returns `{"fits":true,"problems":[]}`. With two `--input` flags it returns `{"results":[{"index":1,"fits":true},…]}`.
- `--examples` reports `"input":"fits"` as strings rather than booleans.
- `schema list -F json` mixes `{name,…}` records and `{uri,external,…}` records in one array.
- `status` uses `binding` for a suggested key. `source inspect` uses `binding` for content and `boundBy` for the key.
- Proposal: one documented, versioned shape per report, whatever the number of values, with one shared vocabulary.

**5. `compat`'s summary contradicts OB-2020-12, the profile whose identifier it stamps on its reports.**
- The help gives exit 4 only when "nothing is known to be incompatible". So incompatible plus undecided exits 1, "not compatible". The profile collapses results as indeterminate > incompatible > compatible.
- It uses "not compatible" and "undecided" where the profile says incompatible and indeterminate.
- The JSON has a boolean `satisfied` with no issue kind (`missing`, `input_incompatible`, `output_incompatible`) and no reason.
- Rows are not in sorted key order, which the profile requires (`acme.events.deliver` should come first).
- Proposal: follow the profile's collapse, words, issue kinds, reasons and ordering.

**6. `schema bundle` can fail its "no `$ref` changes" promise and still report success.**
- The help says a fetched schema keeps its own `$id` if it declares one. The document's `$ref` names the URI it was fetched from.
- Example: `person.json` is served declaring `"$id":"…/v2/person.json"`. The embedded copy answers only to the v2 URI, so the reference still needs the network.
- A schema with no `$schema` that was written for draft-07 is silently re-read as 2020-12 once embedded (§5.2).
- Proposal: refuse such a URI by name, explain why, and keep listing it as external.

**7. Edit refusals do not consistently name the rule, which a Decided point requires.**
- `operation add tasks.obi.json acme.tasks.createTask` exits 3 without naming OBI-D-04.
- `binding add … 'bad name'` exits 2 without naming OBI-D-03, and `set --interface-version ""` also exits 2.
- A draft-07 `$schema` produces three OBI-D-02 lines ("'anyOf' failed", "got object, want boolean"). It never mentions OBI-D-06 or the §5.2 remedy (remove `$schema`).
- The guide calls `context set --bearer-token abc` "refused", yet it exits 2, not 3.
- Proposal: every edit that would break a document rule exits 3, names the rule in spec terms, and suppresses validator branch noise.

### Minor

**8. Named credentials are missing.** Two API keys required together need `credentials[name]`. Refresh tokens and cookies also reach the store only through `--value`, which replaces the whole context. The `--context @context.json` remedy that invoke prints gives no field names. Proposal: `--api-key NAME=-`, `--bearer-token NAME=-`, `--unset credential.NAME`, and a field list in `ob context --help`.

**9. Scopes that can never match are accepted silently.** `ob context set api.example.com --bearer-token -` and `https://API.example.com/` both succeed, but invoke keeps refusing because the binding asks for `https://api.example.com`. Proposal: warn when a scope is not one ob has seen requested, or is not a normalized origin.

**10. `--config POINT=VALUE` types values by accident.** Because VALUE is "JSON or a bare string", `--config port=8443` stores a number and `debug=true` stores a boolean. OpenAPI server variables are strings, so the binding then rejects them. Proposal: `POINT=STRING` and `POINT:=JSON`.

**11. `context show` masks by keeping four characters (`••••3f9a`).** The binding-invoker contract allows structural redaction but "never the secret value". Proposal: mask fully or print a fingerprint, and use `{"masked":true}` in JSON instead of glyph strings.

**12. `source pull` cannot name a new operation and binds only one target per run.** `--target "POST /tasks/{id}/archive" --operation archive` exits 2. `--all-targets` created `complete_task` beside the existing `completeTask`. Proposal: a repeatable `--target TARGET[=OPERATION]`, where a new name creates the operation. `--all-targets` should list each new operation with the command to bind it to an existing one instead.

**13. Misleading next step in `source import` and `synthesize` help.** Both say to run `ob source pull` to add operations, but pull adds nothing unless asked. Proposal: print `ob source pull <obi> <source> --all-targets`.

**14. `ob mcp` serves tools it can already tell will fail.**
- `ob mcp tasks.obi.json --binding createTask.mcp` offers createTask, though that binding needs a sign-in that cannot happen over stdio. Proposal: preflight each served binding at startup, then skip it or warn with the `--preflight` command.
- The help says `ob mcp` can point at a running `ob start`. That needs start's per-run token, which start prints only as prose. Proposal: give `ob start` a `-F json` startup line or a token file.

**15. `kind` reports are too coarse.** `kind list` shows one handler per kind (`"handler":"built in"`), but handlers are per role. `kind check X` without `--role` exits 0 if any role works, so `ob kind check X && ob invoke …` passes for a kind that can only be synthesized. Proposal: report per role, and require `--role` for the exit status.

**16. `compat` checks an operation with neither bindings nor dependencies only as a provider.** A publisher comparing two contract documents cannot see breaks for existing implementations. Proposal: `--as provider|consumer|both`.

**17. Smaller items:**
- `fmt --check other.obi.json` prints "formatted" and exits 0; it should say "would reformat" and exit 1.
- `--canonical` does not say how a document without a JCS form fails (Appendix A).
- `operation show -F json` drops the resolved key; it appears only on stderr.
- `ob help nope` prints the root help and exits 0.
- `--token-credential` can only be rotated by re-pinning `--token-provider`.
- Nothing completes operation, binding, source or schema names: `ob __complete invoke tasks.obi.json ""` returns nothing.

## Peer rankings (1 = best)

**Learnability: ob 3rd.** Order: cargo, gh, ob, Terraform, Redocly, buf, kubectl, git.
- The root help opens with a four-command path, and every command has runnable examples.
- Near misses (`ob call`, `ob operations`) redirect, and refusals print the exact next command.
- The domain carries more concepts than cargo or gh, and nothing completes names from the document.
- To move up: name and kind completion, and an `operation show` that prints an invoke line built from an example.

**Consistency of names and structure: ob 3rd.** Order: gh, cargo, ob, buf, kubectl, Terraform, Redocly, git.
- One grammar covers every part: add, set, rename, remove, list, show, `--unset` by flag name, `--dry-run`, `-F`.
- One exit table serves every command.
- To move up: fix findings 2, 3, 4 and 7, which are the seams.

**Coverage of users' jobs: ob 6th.** Order: git, kubectl, cargo, Terraform, buf, ob, gh, Redocly.
- Authoring, checking, comparing, source sync, invoking, codegen and serving are all present.
- The gaps are credential shapes, naming targets when binding them, and contract-direction checks.
- To move up: findings 8, 12 and 16.

**Editing ergonomics: ob 2nd.** Order: kubectl, ob, cargo, git, gh, Terraform, Redocly, buf.
- Typed edits for every part come with a diff preview, renames update references, and edits refuse to make a document non-conformant.
- Explicit `null` is kept distinct from absence, and `patch` is the escape hatch.
- kubectl's apply is idempotent; ob's removals are not.
- To move up: finding 2.

**Output and scripting: ob 4th.** Order: gh, kubectl, Terraform, ob, buf, cargo, git, Redocly.
- Separate exit statuses for refused, failed and no verdict are strong, as are frames as JSON lines, a clean stdout/stderr split, and stdin everywhere.
- Report shapes vary, edits and `source pull` have no JSON, and invoke can lose values while exiting 0.
- To move up: findings 1 and 4, plus `-F json` on pull and edits.

**Errors and recovery: ob 2nd.** Order: cargo, ob, Terraform, gh, buf, kubectl, Redocly, git.
- Almost every refusal names its cause and the command that fixes it: sign-in, binding choice, alias removal, version refusal.
- Nothing partial is written.
- To move up: finding 7's validator noise and finding 9's dead scopes.

**Fidelity to the model it serves: ob 2nd.** Order: buf, ob, kubectl, Terraform, Redocly, gh, cargo, git.
- Aliases resolve but edits store keys, kinds compare exactly, and version refusals are reported distinctly.
- Values checked against an absent schema get no verdict.
- Binding selection and context least privilege follow the invoker interfaces.
- `compat` and `schema bundle` depart from settled texts.
- To move up: findings 5 and 6.

**Economy of surface: ob 6th.** Order: buf, Redocly, cargo, Terraform, gh, ob, kubectl, git.
- There are about 26 top-level commands and over 60 subcommands for one document format, with three-mode `validate` and the ruled overlaps.
- `delegate roles` lists three constants.
- To move up: fold `delegate roles` into `describe` or `delegate --help`.

**Overall: ob 3rd.** Order: cargo, gh, ob, Terraform, kubectl, buf, Redocly, git.
- ob leads, or nearly leads, where learners and authors feel it: learning, editing, errors and fidelity.
- It trails on scripting, coverage and size, and is only narrowly ahead of Terraform.
- To move up one place (past gh): fix findings 1 to 7, document stable JSON shapes, and add name completion.

## The bar

From this reviewer, the surface meets the bar: it has no blocking findings and ranks third overall. The margin over Terraform is thin and depends on findings 1 to 7 being fixed before implementation rather than shipping. Fixing them would make third secure and bring second within reach.
