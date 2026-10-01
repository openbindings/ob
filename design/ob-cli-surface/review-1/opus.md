Round 1 CLI surface review (Opus subagent, read-only, same brief as Astra). The full report is in the session transcript; this file records its verdict, rankings, and findings index.

Verdict: does not meet the bar; one blocking finding; 6th of 8 overall.

Blocking
- B1 Credentials are not tied to any service: the default context applies to every invoke target, including OBIs fetched from anywhere (spec §9). Peers scope by host (gh, cargo, Terraform, kubectl). Change: contexts name the origins or sources they apply to; invoke refuses to attach a credential to a target nothing matches; default never matches everything implicitly.

Major
- M1 Edits write non-conformant documents without warning (missing $ref target OBI-D-12, relative $ref OBI-D-05, {"type":42} OBI-D-10), while patch requires a conformant result. One rule: refuse edits that add a violation, name the rule, allow edits that add none, --force; schema add should detect root-relative refs and offer --id.
- M2 Three-way results collapse into two: validate exits 0 for undetermined; value vs absent schema has no status; compat promises "undecided" but JSON and exit status are two-way. Distinct "no verdict" status (e.g. 4) for validate and compat; top-level conclusion in JSON.
- M3 operation rename rejects aliases (show/set/remove resolve them, OBI-T-07); renaming to an own alias should swap.
- M4 operation example has only add and remove.
- M5 Nothing sets the document's name, version, description after init: ob set <obi> --name/--interface-version/--description/--unset.
- M6 invoke input: one value only (client/bidi streams impossible); omitting --input undefined. --input - reads a sequence, each checked (OBI-T-08); document omission.
- M7 Binding selection: where undeclared preferences rank is unstated (§5.3 omission is not zero); help writes --preference as if an invoke flag.
- M8 compat checks only the provider direction; consumed operations (dependencies, §5.5) need the reverse; say which contract names count and how absent schemas are judged.
- M9 merge collision policy unclear; contract key match silently replaces schemas; plain merge adds acme.tasks.listTasks beside listTasks. Refuse differing collisions, --prefer ours|theirs, list close matches and suggest --add-alias.
- M10 source pull and status depend on a baseline the document does not hold ("changed upstream"); document where it lives (lock file or x- record) or make pulling explicit (--add <target>); report conflicts with hand edits.
- M11 No path from an inspected target to a binding; inspect -F json field names (sourceRef, operationKey) match neither text nor spec vocabulary; add binding content to JSON and binding add --target.
- M12 --output means a file (-o in fetch, synthesize, codegen) and a value (validate, example add). Use -o, --out.

Minor
- Empty long help on source/binding/dependency/schema rename; typo "Rename a operation".
- Exit statuses for unknown names differ (invoke/validate 2, show/set/binding add 1); no exit table in ob --help.
- Misleading errors: invoke --binding nope; codegen --lang rust says required; root shown as at ''.
- No-op edits say "changed" (--remove-alias absent, --add-kind existing).
- dependency set --remove-kind on the last kind silently accepts any kind.
- Uneven -F/JSON: yaml only on show/list; delegate resolve no -F; diff -F json drops detail; kind check -F json drops handler; context show -F json opaque strings.
- kind check without --role always exits 0.
- fmt - prints a message not the document; no --dry-run; should promise exact values and refuse duplicate keys.
- Edits accept a URL as <obi>; refuse and point to fetch -o.
- Overlaps: status vs source pull --dry-run; delegate resolve vs kind check --role.
- delegate verbs (register/unregister/prefer) and generated IDs break the shared vocabulary; delegate list lacks kinds.
- Unsupported spec versions documented only on validate (§8.1 display allowance).
- binding --content help narrower than §5.3.
- compat cites profile OB-2020-12 nothing defines for the user.
- codegen skips operations without bindings, so contracts and dependencies generate nothing.
- No shell completion; --json has no hint to -F json; --preference and --port typed as string.
- --dry-run diff is not a standard unified diff; offer JSON Patch.

Rankings (1 = best of 8)
- Learnability 4th: cargo, gh, Redocly, ob, Terraform, buf, kubectl, git
- Consistency 5th: gh, cargo, kubectl, buf, ob, Redocly, Terraform, git
- Coverage 7th: git, kubectl, gh, Terraform, cargo, buf, ob, Redocly
- Editing 3rd: kubectl, cargo, ob, gh, Terraform, git, buf, Redocly
- Output and scripting 5th: kubectl, gh, Terraform, git, ob, cargo, buf, Redocly
- Errors and recovery 4th: cargo, gh, Terraform, ob, buf, Redocly, kubectl, git
- Fidelity 4th: git, kubectl, buf, ob, Terraform, cargo, Redocly, gh
- Economy 5th: Redocly, buf, cargo, Terraform, ob, gh, kubectl, git
- Overall 6th: cargo, gh, kubectl, Terraform, buf, ob, Redocly, git

To reach top three: fix B1, M1 to M8 (M3, M4, M5, M12 cheap), and M11; then 3rd or 4th, higher with a default document and shell completion.
