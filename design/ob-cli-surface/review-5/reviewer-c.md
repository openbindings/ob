No blocking findings. I found seven major issues and three minor ones.

1. **Major — delegate preferences reject valid contract values.**  
   `ob delegate set d_91c2 --preference invoke=1.5` exits **2**, saying the value is not a whole number. The [delegate-manager contract](/Users/matt/Code/ob-pj/design/ob-cli-surface/review-5/delegate-manager.md) explicitly allows fractional preferences; these are different from an OBI binding’s integer preference. Accept finite JSON numbers and retain their exact values in both `delegate add` and `delegate set`.

2. **Major — duplicate delegate roles produce an invalid registration report.**  
   `ob delegate add d.obi.json --role invoke --role invoke -F json` exits **0** and returns `"roles":["invoke","invoke"]`. The contract requires a duplicate-free role set and explicitly requires rejection of duplicates. Reject this request with exit **2**, naming the repeated role; apply the same rule consistently to role-changing flags.

3. **Major — delegate inspection cannot expose the contracts being managed.**  
   `ob delegate roles -F json` exposes required operation names, but no accepted interface values or schemas. `ob delegate show d_91c2 -F json` exposes offered names, but no retained OBI. Users therefore cannot inspect the enrolled endpoints, export the retained document, or determine the exact admission contract. Include complete accepted interfaces and role descriptions in `roles` JSON, and the retained interface in `show` JSON; readable text can remain summarized.

4. **Major — JSON list reports discard information available in text.**  
   `ob binding list tasks.obi.json -F json` omits preference and idempotency, although text displays them. `source list -F json` omits binding counts and invocation support; `delegate list -F json` omits preferences, including the sample’s `invoke=10`. Scripts must issue additional requests or parse terminal tables to obtain advertised information. Make JSON reports include every substantive field in their text equivalents, preserving absent-versus-explicit values.

5. **Major — revealed context JSON does not round-trip as context.**  
   `ob context show https://api.example.com --reveal -F json` returns flattened keys such as `"headers.X-Client"` and `"credentials.primary"` inside `fields`. Passing `.fields` to `context set another-scope --value -` would create literal dotted properties, rather than the nested `headers` and `credentials` objects the invocation interface consumes. Flattening also becomes ambiguous for custom field names containing dots. Return the actual nested context object under a documented member, and keep local resolver settings separately identifiable.

6. **Major — pure-contract compatibility lacks a way to express the intended direction.**  
   Under the proposed help semantics, `ob compat new.obi.json old.obi.json` checks an unbound operation both ways. Suppose its input changes from `{"type":"string","enum":["a"]}` to `{"type":"string","enum":["a","b"]}`, with unchanged output: the new contract accommodates every old caller input, but the reverse consumer check fails. Protecting both callers and existing providers is a useful default, but differs from the advertised “still serves old callers” question. Add an explicit direction choice for operations with neither bindings nor dependency use, preserve the settled inference rules elsewhere, and clarify the migration example. This finding follows the proposed rule; the preview comparison engine cannot demonstrate it.

7. **Major — invocation behavior changes with input packaging.**  
   `ob invoke tasks.obi.json importTasks --input '{"title":"a"}' --input '{"title":5}' --frames` exits **3**, reporting nothing sent. Piping those same two values through `--input -` exits **1**, reporting that the first value may have taken effect. For unary `createTask.http`, the streamed version succeeds after closing input early, while the literal version rejects the otherwise unused second value. Help acknowledges literal prevalidation, but this conflicts with the guide’s unconditional later-value rule and weakens the “each flag is the next write” abstraction. Parse literal JSON up front, then schema-check values in write order and stop after input closure, consistently across carriers.

8. **Minor — integer flags silently interpret leading zeros as octal.**  
   `ob binding set tasks.obi.json createTask.http --preference 010` proposes **8**, while `--preference 08` exits **2** with `strconv.ParseInt` diagnostics. Decimal-looking author preferences should not acquire an undocumented radix. Parse these flags in base ten and give a plain-language range or syntax error.

9. **Minor — paired value validation is unexpectedly forbidden.**  
   `ob validate tasks.obi.json --operation createTask --input '{"title":"x"}' --output '{}'` exits **2**: “check one side at a time.” Help does not clearly state this restriction, and checking a supplied request/result pair is a natural validation job already supported for stored examples. Allow both sides in one run, with separately identified results and a combined exit status.

10. **Minor — the proposed bare `kind check` is a misleading predicate.**  
    `ob kind check nope@1` reports three negative answers but exits **0**. This is documented and intentional, but `if ob kind check "$kind"; then …` looks like a support test and succeeds for an entirely unsupported kind. Require `--role` for the predicate and make reporting all roles an explicit report mode.

The named-credential, explicit configuration-typing, and private startup-record proposals are good defaults. All frozen checksums matched; the review remained offline and read-only.

The rankings below judge each CLI against its own job. Orders run from best to worst; these are surface-design judgments.

- **Learnability in the first hour — ob: 3rd.**  
  **Order:** cargo, gh, ob, Redocly, buf, Terraform, kubectl, git.  
  The root’s short starting path and consistent document argument make useful work discoverable, and the part-level help teaches the model without requiring the specification. Cargo and gh introduce fewer concepts before an ordinary successful task; clearer paired-validation help and consistent input-carrier behavior would move ob to **2nd**.

- **Consistency of names and structure — ob: 3rd.**  
  **Order:** cargo, buf, ob, gh, Terraform, Redocly, kubectl, git.  
  The repeated add/set/rename/remove/list/show structure is strong, and argument order is predictable across document parts. Fixing the exceptional numeric grammar, predicate/report distinction, and JSON information gaps would move ob to **2nd**, close to buf’s regularity.

- **Coverage of users’ jobs — ob: 6th.**  
  **Order:** git, cargo, kubectl, gh, buf, ob, Terraform, Redocly.  
  Ob covers the principal OBI lifecycle unusually well: creation, authoring, conformance, comparison, source refresh, discovery, and invocation all have direct paths. Completing delegate introspection, context round-tripping, and directional pure-contract comparison would close the remaining management and evolution workflows and move it to **5th**.

- **Editing ergonomics — ob: 2nd.**  
  **Order:** git, ob, kubectl, cargo, gh, Redocly, buf, Terraform.  
  In-place edits, dry runs, reference-aware renames, explicit creation, and refusal before invalid writes form a coherent authoring experience. Git remains ahead because staging, inspecting, and selectively undoing changes compose exceptionally well; bringing ob’s structured values and configuration snapshots to the same reliable round-trip standard would move it to **1st**.

- **Output and scripting — ob: 5th.**  
  **Order:** git, kubectl, gh, buf, ob, cargo, Terraform, Redocly.  
  JSON documents, streamed output values, terminal frames, quiet checks, and explicit exit meanings give ob a strong foundation. Complete JSON reports and faithful exported context/registration values would move it to **4th**; currently the machine-readable path sometimes knows less than the terminal path.

- **Errors and recovery — ob: 4th.**  
  **Order:** cargo, gh, buf, ob, Terraform, Redocly, git, kubectl.  
  Rule identifiers, near-miss suggestions, binding candidates, and concrete context-recovery commands make most failures actionable, while dispatch-sensitive statuses are valuable. Resolving input-carrier differences, rejecting duplicate roles, and replacing parser internals with useful numeric diagnostics would move it to **3rd**.

- **Fidelity to the model it serves — ob: 4th.**  
  **Order:** git, cargo, buf, ob, Terraform, kubectl, Redocly, gh.  
  Ob handles aliases, schema presence, author claims, opaque kinds, and conformance uncertainty with considerable care. Its supplemental management surfaces are less faithful than its document surface; honoring fractional preferences and exposing complete interface/context values would move it to **3rd**.

- **Economy of surface — ob: 5th.**  
  **Order:** buf, cargo, Terraform, Redocly, ob, gh, git, kubectl.  
  The command count is substantial, but the regular grammar and Advanced grouping make most of it manageable; the breadth largely corresponds to real OBI work. Retaining the settled commands while shortening repeated leaf-help mechanics through a shared syntax reference would move it to **4th**, making the common path feel smaller.

- **Overall — ob: 4th.**  
  **Order:** cargo, buf, gh, ob, git, Terraform, Redocly, kubectl.  
  Ob is unusually coherent for a surface spanning authoring and runtime use, but the leading three have fewer discrepancies between their advertised model and ordinary command behavior. Fixing the seven major findings, then polishing the three minor ones, would move it to **3rd**.

**The surface does not meet the maintainer’s bar in this review:** it has no blocking findings, but ranks fourth overall. The route to third is completion and consistency of the existing surface, particularly faithful management records, directional comparison, and predictable invocation behavior.