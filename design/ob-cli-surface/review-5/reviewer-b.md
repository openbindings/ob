1. **Major — `delegate add`: duplicate roles succeed.**  
   `ob delegate add delegate.obi.json --role invoke --role invoke -F json` exits 0 and returns `"roles":["invoke","invoke"]`. The delegate-manager contract explicitly requires duplicate-free role sets and rejection of duplicates. Reject this request with exit 2 before enrollment.

2. **Major — `delegate add`, `delegate set`: preferences incorrectly require integers.**  
   `ob delegate set d_91c2 --preference invoke=0.5` exits 2: “not a whole number.” Delegate preferences permit negative and fractional numbers; they differ from binding preferences, which are integers. Accept numeric preferences faithfully and describe any representation limits explicitly.

3. **Major — `delegate roles`, `delegate show`: stored interfaces cannot be inspected or exported.**  
   `ob delegate show d_91c2 -F json` exposes names, roles, preferences, and offered operation names, but neither the retained OBI nor its bindings. `delegate roles -F json` likewise supplies required names without the expected schemas or admission/use policy. Operators cannot audit the enrolled destinations, recover the retained document, or inspect the complete requirements for writing a delegate. Expose complete registrations and accepted interface values in JSON, including role descriptions.

4. **Major — `context show`: revealed JSON is unsuitable for copying context.**  
   `ob context show https://api.example.com --reveal -F json` represents nested fields as dotted keys such as `"headers.X-Client"` and `"credentials.primary"`. Extracting `.fields` and supplying it to `context set --value -` would store those literal properties, whereas invokers expect `headers` and `credentials` objects. Return the native context hierarchy when revealing JSON, with scope and local resolver settings separately identified.

5. **Major — `source pull`: an unreadable source gets the wrong exit status.**  
   `ob source pull tasks.obi.json grpcApi` exits **3**; pulling all sources exits **4** and names the same unchecked source. Both the ruling and command help require exit 4 when a source cannot be read. Apply that rule consistently to explicitly selected sources too, so scripts can distinguish incomplete checking from other refusals.

6. **Major — proposed no-role `kind check`: a negative check succeeds.**  
   `ob kind check nope@1` prints “no” for every role and exits 0; adding `--role invoke` makes it exit 1. Consequently, `if ob kind check nope@1; then …` enters its success branch despite universally unsupported work. The documented distinction is intentional, but the verb invites a predicate. Require `--role` for `check`, and expose the all-role inspection through an explicit report option or command.

7. **Major — proposed pure-contract `compat`: symmetric checking conflates two useful questions.**  
   The help recommends `ob compat new.obi.json old.obi.json` to check existing callers, but unbound operations are checked both ways. Under that policy, changing an input enum from `["a"]` to `["a","b"]`, with identical output, fails the consumer direction even though every old caller’s input remains accepted. This follows from the proposed policy; the preview does not implement arbitrary schema comparisons. Keep symmetric contract-evolution checking available, but expose an explicit direction for unbound operations and make the old-caller workflow select provider compatibility. Preserve the settled rules for bound and dependency-used operations.

8. **Major — recovery commands across `compat`, `invoke`, `operation rename`, and `source pull`: paths are not consistently shell-quoted.**  
   With document `my tasks.obi.json`, an MCP context refusal prints `ob invoke my tasks.obi.json createTask … --preflight`; copying it splits the document into two arguments. Compatibility remedies and rename/pull recovery commands have the same problem, although some pull guidance already quotes correctly. Use one shell-quoting routine for every generated command.

9. **Minor — `source list`, `delegate list`: JSON drops useful information present in text.**  
   `source list` shows binding counts and invocation support, but its JSON contains only name and kind. `delegate list` shows `invoke=10` for `d_91c2`, but JSON omits preferences. Include these report fields in both formats; automation should not need additional requests to recover information already shown to a person.

10. **Minor — `validate`: help omits the single-side restriction.**  
    Supplying both `--input` and `--output` with `--operation createTask` exits 2: “check one side at a time.” The help presents both flags without stating that restriction. Either support checking both sides with labeled results or document their mutual exclusion prominently.

No blocking findings. I support the other three new recommendations: named credentials, explicit string-versus-JSON configuration syntax, and private per-run connection records. In-place editing, explicit binding selection, and the distinction between document conformance and author claims also work well.

I read the frozen contracts and command helps, exercised successful and failing tasks with exit statuses, and verified every frozen checksum. I made no file changes and used no network. The rankings below judge the promised surface against each peer’s own job; each ordering runs from **1, best, to 8**.

| Criterion | Best → worst | Reasons and what would move ob up one place |
|---|---|---|
| **Learnability in the first hour — ob 4th** | cargo → gh → buf → **ob** → Redocly → Terraform → git → kubectl | Root grouping and explanatory help make the operation/source/binding distinction approachable. Cargo, gh, and buf offer more direct initial workflows; one complete acquisition-to-invocation walkthrough, including explicit binding choice, would move ob to third. |
| **Consistency of names and structure — ob 4th** | buf → cargo → gh → **ob** → Redocly → Terraform → kubectl → git | The repeated noun/verb structure and common edit rules are strong. The check/report distinction and inconsistent JSON completeness introduce exceptions; resolving those would move ob above gh. |
| **Coverage of users’ jobs — ob 6th** | git → kubectl → cargo → gh → Terraform → **ob** → buf → Redocly | Authoring, conformance, reference handling, drift, comparison, and invocation form a broad, connected workflow. Enrollment auditing and context transfer remain incomplete; complete native exports would move ob above Terraform on this criterion. |
| **Editing ergonomics — ob 3rd** | git → kubectl → **ob** → Terraform → gh → cargo → Redocly → buf | Protected edits, reference-updating renames, and targeted pulls provide unusually useful editing primitives. Git and kubectl offer stronger composition of review and change application; a structured JSON dry-run plan would move ob to second. |
| **Output and scripting — ob 6th** | gh → kubectl → cargo → buf → git → **ob** → Terraform → Redocly | Clean stdout, invocation frames, and distinct uncertainty statuses provide a good foundation. Lossy report shapes and the successful negative `kind check` undermine it; native round-trippable JSON and predictable predicates would move ob above git. |
| **Errors and recovery — ob 4th** | cargo → gh → buf → **ob** → Redocly → Terraform → kubectl → git | Typo suggestions, named rules, concrete remedies, and warnings about already-sent values are excellent. Broken copied commands and the source-pull status mismatch prevent third place; fixing both would put ob above buf. |
| **Fidelity to its model — ob 3rd** | git → buf → **ob** → cargo → kubectl → Terraform → Redocly → gh | Opaque kinds, equal-standing aliases, per-value schemas, presence distinctions, and author claims are handled carefully. Delegate management’s restrictions and incomplete inspection weaken the surrounding contracts; repairing those would move ob above buf. |
| **Economy of surface — ob 5th** | buf → cargo → Terraform → Redocly → **ob** → gh → kubectl → git | The breadth largely earns its place, and the Advanced grouping keeps it manageable. Additional report representations create avoidable translation work; reusing native context and registration objects would move ob above Redocly without removing settled commands. |
| **Overall — ob 4th** | cargo → gh → git → **ob** → buf → kubectl → Terraform → Redocly | The coherent document model and strong editing workflow outweigh substantial surface complexity. Inspection, scripting, and recovery gaps keep it below the first three; resolving the major findings would move it to third in my assessment. |

**The surface does not meet the maintainer’s bar in this review:** it has no blocking findings, but ranks **fourth overall**. Fix the major findings, including clarifying the two challenged defaults, and retain the strong core semantics and editing structure; I would then place it in the top three.