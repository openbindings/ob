# Findings

I found no blocking findings.

## Major

1. **`invoke` and `validate` silently keep only the last of a repeated `--input` or `--output`.** `ob validate tasks.obi.json --operation createTask --input '{"title":5}' --input '{"title":"x"}'` prints "The input value fits" and exits 0. `ob invoke tasks.obi.json listTasks --input {} --input {}` sends one value. Input values are writes and `--binding` can be repeated, so users will expect each `--input` to be one write. Change: treat each repeated `--input` or `--output` as the next value, in order. Otherwise exit 2.

2. **Exit 1 means both "no" and "nothing by that name" in read-only commands that give a verdict.** `ob validate tasks.obi.json --operation nope --input '{}' -q` exits 1, the same as a value that does not fit. In CI, a typo looks like bad data. Change: when a name resolves to nothing in a read-only command, exit 2, and keep 1 for the verdict.

3. **`source pull` and `status` cannot serve a curated document.** Say an author leaves `GET /health` unbound on purpose. `status --exit-code` then returns 1 forever, and every pull adds it back. `ob source pull tasks.obi.json httpApi --update-operation listTasks` also adds `archiveTask` and `getHealth`, as its diff shows. Change:
   - make `--update-operation` act alone, as `--target` already does;
   - add a pull mode that only refreshes bound targets;
   - have `status --exit-code` count drift only in what is bound, and report uncovered targets separately.

4. **`compat` checks every operation as a provider.** Spec §5.5 makes a dependency's claim the reverse: the consumer sends only what `input` describes and accepts whatever `output` describes (§1.1, "either direction"). So a consumed-only operation such as `events.deliver`, whose input is narrower than the contract's, is reported "not compatible" even though every call it makes is acceptable. Change: check bound operations as providers and depended-on operations as consumers, both when an operation is both, and say which in the report.

5. **Secrets can be given on the command line.** `--bearer-token`, `--api-key`, `--access-token`, `--token-credential`, `context set --value` and `invoke --context` all accept literal secrets; `ob context set https://api.example.com --bearer-token abc` succeeds. token-provider.md requires mint's `credential` to be kept out of command lines and argv. Change: secret-valued flags accept only `-`, `@file` or a prompt. A literal exits 2 and shows the safe form.

6. **Documents read from URLs have no way through gated discovery.** DISC-S-03 lets a publisher answer 401/403, and DISC-C-03/04 keep absence, gating and version refusal apart. `fetch`, `show`, `invoke`, `compat`, `merge`, `codegen` and `mcp` all accept URLs, but none takes a credential for the document request, and none documents these outcomes. Change: add `--header` or `--context` to URL reads, and document the results:
   - 404 exits 1, "no OBI published";
   - 401/403 exits 3, "gated";
   - a version refusal exits 3 with its own message (`fetch` may still save the document).

7. **`start` says nothing about who may call it.** It serves ob's operations on localhost "for browsers", including `invokeOperation`, which runs with ob's stored context. Any local process, or a web page using DNS rebinding, could spend those credentials. Change: print a per-run access token at start, check the Host header, add `--allow-origin`, and say all this in the help.

## Minor

8. `-F yaml` exists on every part's `list` and `show`, and on `context`, `kind` and `delegate`. But `ob show -F yaml` exits 2 because "an OBI is JSON", while `ob schema show tasks.obi.json Task -F yaml` prints OBI members as YAML. Drop YAML at least where stored members are printed.
9. `ob kind check example.grpc@1 --role inspect` says "example.grpc@1 is a different kind". It should say the kind is handled for invoke and synthesize, but not inspect.
10. Equivalent edits end with different exit statuses:
    - `--add-alias` with the operation's own key exits 2; with another operation's key it exits 3;
    - removing an absent alias, tag or kind exits 3;
    - `--unset` of an absent member, and `--add-kind` of a kind already listed, exit 0 with "no change".

    Pick one rule.
11. `ob set --unset name` is refused, even though `name` is optional. Allow it.
12. `delegate` breaks the verb family: it uses generated IDs (`d_7f3a`), the verbs `register`, `unregister`, `prefer` and `--replace`, and has no `show`. Use a name the user chooses, with add/set/remove/list/show.
13. `compat new old` ignores binding keys, yet ob exposes them to callers through strict `--binding`, codegen binding lists and `mcp --binding`. `other.obi.json` drops `createTask.mcp`, and scripts that name it break without any report. Report removed or renamed binding keys, and say so in `binding rename --help`.
14. The `merge` remedy that `compat` prints also copies the contract's bindings and sources into your document, which means someone else's endpoints. Print it with `--no-bindings`.
15. `invoke --help` leaves out several things:
    - what happens with an absent schema (values pass unchecked, OBI-T-08);
    - the exit status of `ERR_SCHEMA_UNRESOLVED` and of a binding's `ERR_REFUSED`, which should both be 3;
    - that "offers to store the result" applies only to durable requirements (binding-invoker forbids persisting the rest);
    - any deadline, although §5 names a deadline as context.
16. `diff -F json` and `status -F json` print bare arrays, while `validate` and `compat` print objects with a conclusion. Make them all objects.
17. `validate --examples --operation createTask` is refused, but checking one operation's examples is the usual edit loop.
18. `mcp` offers `importTasks` (a stream in) and `watchTasks` (an unbounded stream out) as tools without saying how a tool call maps onto the exchange. A call to `watchTasks` never returns. Document the mapping, and skip or bound streams.
19. Only `validate` documents version refusal. Say that commands which interpret a document refuse an unsupported line, while `show -F json` and `fetch` may still display it (§8.1).
20. `--token-provider` takes a URL that is resolved later, and offers no way to choose a binding. Pin a copy at `context set`, as `delegate register` does, and accept a binding. Otherwise a provider with two `mint` bindings can never be used.
21. `start --install-ca` has no matching removal. Add `--uninstall-ca`.
22. No command inlines external `$ref` schemas into `schemas` (Redocly's `bundle`). That is the step that makes a document self-contained and checkable offline.

# Rank among peers (1 = best, of eight)

**Learnability in the first hour:** cargo, gh, Redocly, buf, **ob 5**, Terraform, kubectl, git. Every help page has runnable examples, near misses suggest the right command, and refusals print the exact next command. But a newcomer meets about 25 top-level commands and eight new nouns (source, binding, kind, target, scope, delegate, role, preflight) before a first invoke. Up one: start the root help with a five-command path, and group kind, delegate, start, mcp, codegen and describe as advanced.

**Consistency of names and structure:** gh, **ob 2**, cargo, buf, Terraform, kubectl, Redocly, git. ob has one add/set/rename/remove/list/show family for every part, `--unset` by flag name, one `-F` and one exit table. Up one: findings 8, 10 and 12.

**Coverage of jobs:** git, kubectl, cargo, Terraform, buf, **ob 6**, gh, Redocly. Authoring, checking, invocation, serving and codegen are all there. Syncing a curated document, gated discovery, consumer-side compatibility and bundling are not. Up one: findings 3, 4, 6 and 22.

**Editing ergonomics:** **ob 1**, kubectl, cargo, gh, git, Terraform, buf, Redocly. Every part can be edited in place, with a `--dry-run` diff and `-` as a filter. Renames rewrite references, refusals keep the document conformant, and `patch` and `merge --ours/--theirs` cover the rest. ob is already first; findings 10 and 11 would keep it there.

**Output and scripting:** kubectl, gh, **ob 3**, Terraform, cargo, git, buf, Redocly. One exit table with a separate no-verdict status (4) beats every peer, and `--frames` is clean NDJSON. Silently losing `--input` values, the exit-1 collision and mixed JSON shapes cost it. There is also no field selection like `-o jsonpath` or `--jq`. Up one: findings 1, 2 and 16.

**Errors and recovery:** **ob 1**, cargo, Terraform, gh, buf, git, Redocly, kubectl. Refusals name the rule, say that nothing was written or sent, and print remedies you can copy. Context refusals give three ways forward. ob is already first; finding 9 is the only wrong message I saw.

**Fidelity to the model:** git, buf, **ob 3**, kubectl, Terraform, cargo, Redocly, gh. ob carries the model exactly in these areas:
- present-but-null versus absent;
- exact kind matching and alias resolution;
- no verdict when a schema is absent;
- refusals that name the rule;
- the interface's binding selection rule and frames.

Up one: findings 4, 5 and 8.

**Economy of surface:** buf, Redocly, cargo, Terraform, **ob 5**, gh, kubectl, git. The verb family makes the size predictable, but about 25 top-level commands plus three overlaps that were kept is large for a document tool. Up one: fold `synthesize` into `init --from <artifact> --kind`, and `source import` into `source add --from`.

**Overall:** cargo, gh, buf, **ob 4**, Terraform, git, kubectl, Redocly. ob leads on editing and errors and has the most disciplined exit contract of the group. Its scripting traps and workflow gaps still leave buf ahead: buf's lint, breaking and generate are complete CI gates, and ob's `status` and `compat` are not yet. Up one: findings 1 to 7.

# Verdict

The surface does not meet the bar. There are no blocking findings, but I rank it fourth overall, behind cargo, gh and buf. In my judgment, fixing majors 1 to 7 would place it third. The ones that matter most are the two scripting traps (1 and 2), a pull and status that can gate a curated document (3), and a compat that checks each operation in the right direction (4).
