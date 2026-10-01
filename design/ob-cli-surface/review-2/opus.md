# Findings

## Blocking

**B1. Most refusals contradict the exit-status table** (every edit, `merge`, `remove`, `rename`). `ob operation add tasks.obi.json x --input-schema '{"type":42}'` prints "refused: ... nothing was written" and exits 1. The same happens with `operation remove createTask` (bindings use it), `schema remove Problem`, `merge tasks.obi.json other.obi.json`, rename collisions, and `dependency set` removing the last kind. `invoke` exits 3 for the same kind of refusal. `operation show nope` exits 1, but `invoke nope` exits 3. A script that trusts the table printed at the top of `ob --help` misreads every refused edit. Change: anything ob declined, with nothing written or sent, exits 3. Add a test that covers every command.

**B2. `ob mcp` and `ob codegen` cannot call an operation that has more than one binding.** `ob mcp tasks.obi.json` offers `createTask` and "calls it the way ob invoke does", and invoke refuses with ERR_BINDING_SELECTION_REQUIRED. `mcp` has no `--binding`. Codegen says generated calls choose "the way ob invoke does" and offers no selection option. The case that fails is the spec's own §4 example: one operation over two protocols. Change: add `mcp --binding OPERATION=KEY` (repeatable and ordered, the interface's `selection`), list operations it cannot resolve under "Skipped", and give generated clients a selection option.

## Major

**M1. `operation remove` deletes through an alias.** `ob operation remove tasks.obi.json acme.tasks.createTask --cascade` removes createTask and both its bindings, with only a parenthetical note. `rename` refuses an alias for exactly this reason. Change: refuse, and point to `operation set --remove-alias`.

**M2. `source pull` and `status` report success for sources ob cannot read.** `ob source pull tasks.obi.json grpcApi` prints "up to date" and exits 0, while `source inspect grpcApi` says this ob cannot inspect example.grpc@1. `status` leaves grpcApi out entirely: in text, in `-F json`, and in `--exit-code`. CI goes green without checking that source. Change: report "cannot check" for each such source and exit 4.

**M3. `source pull` makes one artifact the authority over the operation contract.** "Update changed ones" rewrites `listTasks.output` from httpApi. createTask is bound over both HTTP and MCP, so pulls from its two sources would overwrite each other, and any hand-authored schema is replaced silently. "New target" also has no baseline in the document: `status` calls the archive target new but not GET /health, which is just as unbound. Change: by default, pull adds unbound targets and updates binding content only. Contract drift is reported and applied only to operations named with a flag such as `--update-operation`, refusing conflicts the way merge does.

**M4. `source pull --target` cannot bind to an existing operation.** It creates `getHealth` and `getHealth.http` from the handler's suggestion. Binding the archive target to my own `archive` operation means writing kind content by hand. `--target` also rejects `#/paths/~1health/get`, the value it writes into content, and accepts only the label "GET /health". Change: add `--operation` and `--name`, accept both identifiers, and show both in `inspect -F json`.

**M5. `invoke` exits 0 after dropping input.** `printf '{"title":"a"}\n{"title":"b"}\n' | ob invoke tasks.obi.json createTask --binding createTask.http --input -` sends the first value, notes the drop on stderr, and exits 0. The second task never exists. Change: exit 1 when values were left unsent after the binding closed its input.

**M6. `delegate roles` contradicts binding-invoker.** It requires `openbindings.binding-invoker.listSupportedKinds` and `checkKindSupport`, but the interface defines `listBindingSpecs` and `checkBindingSpecs`. A delegate built to the published interface fails the check in `delegate register`. Relatedly, `kind list` presents delegate kinds as a complete list, although absence from `listBindingSpecs` "carries no information". Change: use the interface's operation names, and say that `kind check` is the authoritative answer.

**M7. OAuth stops at pasting a token.** createTask.mcp's challenge is `auth.oauth2`, and the only remedy offered is `context set --access-token -`. No command runs the flow the requirement names (grantType, authorizeUrl, tokenUrl, scopes) or keeps a refresh token, so stored tokens expire and scripts break. `--token-provider` expects a token-minting contract that neither interface defines. Change: add `ob context login <scope>` to run the challenge's flow, plus `--refresh-token`, and name the interface a token provider must implement.

**M8. Bare-origin lookup rests on a convention nothing defines.** Commands that read a document rewrite `https://api.example.com` to `/.well-known/openbindings` ("OpenBindings HTTP Discovery"). The spec leaves acquisition out (§1.2) and registers no well-known URI (§11), and neither interface defines it. File-name lookup was rejected on the same ground. Change: cite a published definition, or fetch URLs as given and add an explicit `fetch --discover`.

**M9. `patch` states a stricter rule than every other edit:** "the result must still be a conformant document". That blocks patch's main use, repairing a broken document one violation at a time. It also blocks every write while conformance is undetermined. Change: apply and state the shared rule.

**M10. `init` breaks the settled rule for commands that create files.** `ob init -o tasks.obi.json` fails with "unknown shorthand flag". init takes the file as a positional argument and refuses to overwrite without `--force`. `synthesize`, `fetch`, and `codegen` take `-o` and say nothing about overwriting. Change: `init [-o FILE]`, with one overwrite rule and `--force` on every command that takes `-o`.

**M11. `ob start` installs a local certificate authority the first time it runs, by default.** Changing the system's trust should be opt-in, through `--tls` or a one-time `--install-ca`.

**M12. Two common jobs have no command.** 0.1.0 is the spec's latest release, yet a 0.1 document gets only version refusals, and there is no `migrate`. "Did this change break callers?" can be answered by `compat next.json current.json`, but `diff` has no breaking-change view and nothing tells users about compat. Change: add `ob migrate`, which shows the diff and refuses what it cannot translate. Document compat as the breaking-change check, or add `diff --breaking`.

## Minor

- Flag names and member names differ: `set --interface-version` but `--unset version`; `--input-schema` but `--unset input`.
- There are three ways to ask for an answer through the exit status: `-q` (validate, compat), `--exit-code` (diff, status), and `--check` (fmt). `kind check` has none of them. `-F yaml` works on show and list commands but not on reports.
- `operation show X -F json`, when X is an alias, drops the resolved key and the bindings that the text view shows. Change: print `{key, operation, bindings}`, or add `--raw`.
- `validate --input -` checks one value, while `invoke --input -` streams many. The same NDJSON gets different treatment. Validate each value and report by index.
- In JSON reports, `problems` are prose strings with no location or keyword fields. `diff` JSON says added/changed/removed, while `status` says add/update.
- `dependency add --kind ''` answers "that would remove the last kind".
- `--remove-alias nope` reports "no change" and exits 0 without a warning.
- `operation example add` rejects an example that has only `--description`, but the spec makes every example member optional.
- Some commands overlap. `status` reads like git's but covers only source drift, and it duplicates `source pull --dry-run`. `delegate resolve` overlaps `kind check`, and `fetch` overlaps `show -F json`.
- `show -F yaml` prints the document in a form that is not an OBI, since OBI-D-01 requires JSON.
- Without `--frames`, it is undocumented where an error frame's application `data` goes.
- `--no-check` leaves the operation-invoker's step 3, while `--frames` claims to print exactly the interface's frames. The help should say so, and give the exit status for ERR_SCHEMA_UNRESOLVED.
- `context set --config` cannot address a requirement's `path`. Header values print unmasked, although the interface warns that headers can be secret. `--basic` works only interactively.
- The binding-selection refusal gives no command to copy, unlike the context refusal. A near-miss operation name such as `createtask` gets no suggestion.
- `delegate` commands take only minted IDs. `register` prints the ID in prose and has no `-F json`.
- Merging the whole contract refuses on createTask, which compat reports as already met. The message offers `--theirs` as readily as `--ours`, and `--theirs` would replace createTask's precise anyOf output with `{"type":"object"}`.
- `mcp` does not say how an operation that streams in (importTasks) or streams out (watchTasks) maps to one tool call.
- `--openbindings`, `--usage-spec`, and `--agent-primer` are commands written as flags.

# Rankings (1 = best, of 8)

**Learnability in the first hour: 4th** (cargo, gh, buf, ob, Redocly, terraform, kubectl, git). Every command's help has examples, near-miss commands get hints, and refusals name the next command. What costs it is the number of concepts (kind, source, binding, scope, delegate) and the fact that invoking the flagship example ends in a refusal. Up one: end every refusal with a command to copy, and fix B2.

**Consistency of names and structure: 4th** (cargo, gh, buf, ob, terraform, kubectl, Redocly, git). It has the most regular structure of the eight: add/set/rename/remove/list/show for every part, with `--dry-run`, `-`, and `--unset` everywhere. The exit table, alias handling, flag names, and exit-status flags break that regularity. Up one: fix B1, M1, and the first two minor findings.

**Coverage of users' jobs: 7th** (git, kubectl, cargo, terraform, gh, buf, ob, Redocly). Authoring, checking, syncing, invoking, serving, and codegen are all present. Migration, a breaking-change check, OAuth, serving operations with several bindings, and binding a target to an existing operation are missing. Up one: fix M12 and B2.

**Editing ergonomics: 1st** (ob, kubectl, cargo, gh, git, terraform, buf, Redocly). It has structured edits for every part, renames that update references, a conformance guard, and diff previews. No peer edits its own document this safely. To stay first: fix M1 and M3.

**Output and scripting: 6th** (gh, kubectl, terraform, cargo, git, ob, buf, Redocly). The foundations are good: `-F json` almost everywhere, NDJSON values and interface frames from invoke, and `-` as a filter. Against that, the exit table misreports refusals (B1), and M2 and M5 report success when something went wrong. Up one: fix B1, M2, and M5, and structure `problems`.

**Errors and recovery: 3rd** (cargo, gh, ob, terraform, git, buf, Redocly, kubectl). Refusals name the rule, the conflict, and the command that fixes it (context set, rename's alias guidance, compat's remedies). The false successes hide from users that anything needs recovering. Up one: fix M2 and M5.

**Fidelity to the model: 5th** (kubectl, buf, cargo, git, ob, Redocly, terraform, gh). Much is exemplary: kinds matched exactly, presence kept (explicit null, `--deprecated=false`), aliases stored as keys, three conformance conclusions naming the spec revision, and the interface's frames and codes. M6, M3, and M8 contradict that model. Up one: fix M6 and M3.

**Economy of surface: 5th** (Redocly, buf, cargo, terraform, ob, kubectl, gh, git). It has about 68 leaf commands, made predictable by one grammar. Some commands overlap (status, delegate resolve, fetch, source import), and three root flags act as commands. Up one: fold those in.

**Overall: 4th** (cargo, gh, kubectl, ob, buf, terraform, git, Redocly). Its editing is best in class and its errors are strong, but two blocking findings and results that report false success hold it back. Up one, past kubectl, whose surface sprawls but does not misreport outcomes: fix B1, B2, M2, and M5.

# The bar

The surface does not meet it: it has two blocking findings, and this reviewer ranks it 4th overall. Fixing B1 and B2, then M1 through M6, would move it to 3rd in my ranking. Those cover honest results, alias safety, pull's authority over the contract, and the interface's operation names. Closing M7 and M12 would hold it there.
