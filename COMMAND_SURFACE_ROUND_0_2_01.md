# OpenBindings 0.2 command surface: held-out round 1

## Method and scoring

Two independent model reviewers received the same 30 natural-language task cards below. Each sealed its unrevised first command path and complete invocation before using help or running the frozen executable, `/private/tmp/ob_v02_round1`. Neither saw the other's guesses. After the first invocation, each could use at most two targeted help/error hops per task. The executable was an inert command-surface preview; no operation ran, and this round did not inspect source, repository files, or design notes.

An exit-2 `command-surface placeholder; no operation ran` is **syntax acceptance only**, never task execution. An operation-shaped exit 0 would be a false success; none occurred. A06 deliberately requests a forbidden shared-stdin call, and A07 deliberately supplies malformed JSON. Their correct outcomes are the specified exit-2 diagnostics, not placeholder acceptance. The other 28 cards have recoverable positive syntax. For A08, syntax acceptance does not establish the validation verdict; for K02 and A05, it does not establish installed support.

The `P` prefix in every command table expands to the complete executable path `/private/tmp/ob_v02_round1`. A checkmark under “first invocation” means the exact first call achieved the requested parser outcome, including A07's diagnostic. “Within 2” counts observed recovery, not what the reviewer could have inferred earlier. Reviewer B disclosed that K01, K02, and A05 used three help calls after the first invocation; all three are failures under the strict two-hop rule despite eventual syntax acceptance.

## Frozen task bank

| ID | Card |
|---|---|
| C01 | Create empty `warehouse.obi.json`, titled `Warehouse API`, described as `Inventory contract`, with interface version label `2026-09`. |
| C02 | Display every stored field of `warehouse.obi.json` as JSON, rather than its summary. |
| C03 | List operation keys and aliases in `warehouse.obi.json`. |
| C04 | Define `findItem` in `warehouse.obi.json`, alias `getItem`, input schema `{"type":"object","required":["sku"]}`, output schema `{"type":"object"}`, and an author claim of idempotence. |
| C05 | Retrieve the complete stored operation selected by alias `getItem` from `warehouse.obi.json`. |
| C06 | Store reusable schema `Item` with value `{"type":"object","required":["sku"]}` in `warehouse.obi.json`. |
| C07 | List the reusable schema keys in `warehouse.obi.json`. |
| C08 | Add source `catalog` to `warehouse.obi.json` with exact kind `acme.http@1` and content `{"baseUrl":"https://catalog.example.test"}`. |
| C09 | Add realization `findItem.http` of `findItem` through `catalog`, content `{"path":"/items/{sku}"}`, author preference `10`, in `warehouse.obi.json`. |
| C10 | Declare consumption point `billing` for existing operation `charge` in `warehouse.obi.json`, without a kind constraint. |
| C11 | Check `warehouse.obi.json` for OpenBindings 0.2 document conformance, with a JSON report. |
| C12 | Report exact changed JSON paths and values between `before.obi.json` and `after.obi.json` as JSON. |
| K01 | Find exact kinds for which the local installation advertises any source-handling or invocation ability. |
| K02 | Determine whether the installation supports invoking exact kind `acme.http@1`. |
| K03 | Have the installed `acme.openapi@1` handler acquire `./supplier-openapi.yaml` as source `supplier` in `warehouse.obi.json`. |
| K04 | Ask the installed handler to interpret source `catalog` in `warehouse.obi.json`. |
| K05 | Refresh source `catalog` content in `warehouse.obi.json` through its installed kind handler. |
| K06 | From `catalog`, get proposed operation and realization entries for review without applying them. |
| K07 | From `catalog`, apply the handler's proposed operation and realization entries. |
| K08 | Execute exact realization `findItem.http` with input `{"sku":"A-17"}` and local context `staging`. |
| K09 | Execute exact realization `health.http` with explicit JSON null caller input. |
| K10 | Apply RFC 6902 patch `owner.patch.json` to `warehouse.obi.json` to add an `x-owner` extension, retaining conformance checking. |
| A01 | Add source `secret_bus` with exact private kind `corp.internal.bus@7` and content `{"channel":"orders"}`, even without an installed handler. |
| A02 | Add realization `noop.null` of `noop` through `catalog` with an explicitly present JSON null content member. |
| A03 | Add `lookup.backup` for `lookup` through `shared`, which already has `lookup.primary`. |
| A04 | Declare consumption point `payments` for `charge` accepting either `acme.http@1` or `corp.rpc@3`. |
| A05 | Find whether this installation can refresh `corp.internal.bus@7`; a negative answer is not document non-conformance. |
| A06 | In one call, define `normalize` using both input and output schemas from the same stdin stream (`-`); determine whether the collision is rejected. |
| A07 | Try to store reusable schema `Broken` using malformed inline JSON `{"type":`; report the diagnostic without claiming a change. |
| A08 | Assess `partial.obi.json` under its declared specification version, preserving an `undetermined` conclusion if some rules cannot be checked, with machine-readable results. |

## Aggregate result and stop gate

| Reviewer | Category | First path | First complete invocation | Within 2 hops | False-success exits |
|---|---|---:|---:|---:|---:|
| A | Core, 12 | 8 | 4 | 12 | 0 |
| A | Kind/policy, 10 | 1 | 1 | 10 | 0 |
| A | Adversarial, 8 | 4 | 2 | 8 | 0 |
| **A** | **All, 30** | **13** | **7** | **30** | **0** |
| B | Core, 12 | 8 | 4 | 12 | 0 |
| B | Kind/policy, 10 | 0 | 0 | 8 | 0 |
| B | Adversarial, 8 | 4 | 2 | 7 | 0 |
| **B** | **All, 30** | **12** | **6** | **27** | **0** |

First-try placeholder syntax acceptance was 6/30 for A and 5/30 for B. The first-complete-invocation counts above additionally credit each reviewer for A07's requested malformed-JSON rejection. Positive syntax recovery, excluding A06 and A07, was A **28/28** and B **25/28**; combined **53/56 = 94.6%**. Strict recovery over every card, including the two diagnostic probes, was A **30/30** and B **27/30**; combined **57/60 = 95.0%**. The positive-syntax 95% stop gate **fails**. B also fails it individually.

## Per-task scores

| ID | A first path | A first invocation | A within 2 | B first path | B first invocation | B within 2 |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| C01 | – | – | ✓ | – | – | ✓ |
| C02 | – | – | ✓ | – | – | ✓ |
| C03 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| C04 | ✓ | – | ✓ | ✓ | – | ✓ |
| C05 | ✓ | – | ✓ | ✓ | – | ✓ |
| C06 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| C07 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| C08 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| C09 | – | – | ✓ | – | – | ✓ |
| C10 | – | – | ✓ | – | – | ✓ |
| C11 | ✓ | – | ✓ | ✓ | – | ✓ |
| C12 | ✓ | – | ✓ | ✓ | – | ✓ |
| K01 | ✓ | ✓ | ✓ | – | – | – |
| K02 | – | – | ✓ | – | – | – |
| K03 | – | – | ✓ | – | – | ✓ |
| K04 | – | – | ✓ | – | – | ✓ |
| K05 | – | – | ✓ | – | – | ✓ |
| K06 | – | – | ✓ | – | – | ✓ |
| K07 | – | – | ✓ | – | – | ✓ |
| K08 | – | – | ✓ | – | – | ✓ |
| K09 | – | – | ✓ | – | – | ✓ |
| K10 | – | – | ✓ | – | – | ✓ |
| A01 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| A02 | – | – | ✓ | – | – | ✓ |
| A03 | – | – | ✓ | – | – | ✓ |
| A04 | – | – | ✓ | – | – | ✓ |
| A05 | – | – | ✓ | – | – | – |
| A06 | ✓ | – | ✓* | ✓ | – | ✓* |
| A07 | ✓ | ✓* | ✓* | ✓ | ✓* | ✓* |
| A08 | ✓ | – | ✓ | ✓ | – | ✓ |

`*` denotes the requested rejection diagnostic, not placeholder syntax acceptance. A06 recovered to `stdin (-) can supply only one input per command`; A07's first invocation returned `--value: expected one JSON value, @file, or - for stdin` for both reviewers.

## Sealed first guesses

The invocations below are the complete, unrevised first calls. `P` has the fixed path defined above. No reviewer saw the other's table before sealing their own.

| ID | Reviewer A: path → complete first invocation | Reviewer B: path → complete first invocation |
|---|---|---|
| C01 | `document create` → `P document create warehouse.obi.json --title 'Warehouse API' --description 'Inventory contract' --version '2026-09'` | `document init` → `P document init warehouse.obi.json --title 'Warehouse API' --description 'Inventory contract' --interface-version '2026-09'` |
| C02 | `document show` → `P document show warehouse.obi.json --json` | `document show` → `P document show warehouse.obi.json --json` |
| C03 | `operation list` → `P operation list warehouse.obi.json` | `operation list` → `P operation list warehouse.obi.json` |
| C04 | `operation add` → `P operation add warehouse.obi.json findItem --alias getItem --input '{"type":"object","required":["sku"]}' --output '{"type":"object"}' --idempotent` | `operation add` → `P operation add warehouse.obi.json findItem --alias getItem --input '{"type":"object","required":["sku"]}' --output '{"type":"object"}' --idempotent` |
| C05 | `operation show` → `P operation show warehouse.obi.json getItem --json` | `operation show` → `P operation show warehouse.obi.json getItem --json` |
| C06 | `schema add` → `P schema add warehouse.obi.json Item --value '{"type":"object","required":["sku"]}'` | `schema add` → `P schema add warehouse.obi.json Item --value '{"type":"object","required":["sku"]}'` |
| C07 | `schema list` → `P schema list warehouse.obi.json` | `schema list` → `P schema list warehouse.obi.json` |
| C08 | `source add` → `P source add warehouse.obi.json catalog --kind acme.http@1 --content '{"baseUrl":"https://catalog.example.test"}'` | `source add` → `P source add warehouse.obi.json catalog --kind acme.http@1 --content '{"baseUrl":"https://catalog.example.test"}'` |
| C09 | `realization add` → `P realization add warehouse.obi.json findItem.http --operation findItem --source catalog --content '{"path":"/items/{sku}"}' --preference 10` | `realization add` → `P realization add warehouse.obi.json findItem.http --operation findItem --source catalog --content '{"path":"/items/{sku}"}' --preference 10` |
| C10 | `consumption add` → `P consumption add warehouse.obi.json billing --operation charge` | `consume add` → `P consume add warehouse.obi.json billing --operation charge` |
| C11 | `validate` → `P validate warehouse.obi.json --json` | `validate` → `P validate warehouse.obi.json --json` |
| C12 | `diff` → `P diff before.obi.json after.obi.json --json` | `diff` → `P diff before.obi.json after.obi.json --json` |
| K01 | `kind list` → `P kind list` | `handler list` → `P handler list` |
| K02 | `kind show` → `P kind show acme.http@1` | `handler supports` → `P handler supports acme.http@1 --action invoke` |
| K03 | `source acquire` → `P source acquire warehouse.obi.json supplier --kind acme.openapi@1 --from ./supplier-openapi.yaml` | `source acquire` → `P source acquire warehouse.obi.json supplier --kind acme.openapi@1 --from ./supplier-openapi.yaml` |
| K04 | `source interpret` → `P source interpret warehouse.obi.json catalog` | `source interpret` → `P source interpret warehouse.obi.json catalog` |
| K05 | `source refresh` → `P source refresh warehouse.obi.json catalog` | `source refresh` → `P source refresh warehouse.obi.json catalog` |
| K06 | `source preview` → `P source preview warehouse.obi.json catalog` | `source propose` → `P source propose warehouse.obi.json catalog --json` |
| K07 | `source import` → `P source import warehouse.obi.json catalog` | `source sync` → `P source sync warehouse.obi.json catalog --apply` |
| K08 | `realization invoke` → `P realization invoke warehouse.obi.json findItem.http --input '{"sku":"A-17"}' --context staging` | `realization invoke` → `P realization invoke warehouse.obi.json findItem.http --input '{"sku":"A-17"}' --context staging` |
| K09 | `realization invoke` → `P realization invoke warehouse.obi.json health.http --input 'null'` | `realization invoke` → `P realization invoke warehouse.obi.json health.http --input null` |
| K10 | `patch apply` → `P patch apply warehouse.obi.json owner.patch.json` | `patch apply` → `P patch apply warehouse.obi.json owner.patch.json --validate` |
| A01 | `source add` → `P source add warehouse.obi.json secret_bus --kind corp.internal.bus@7 --content '{"channel":"orders"}'` | `source add` → `P source add warehouse.obi.json secret_bus --kind corp.internal.bus@7 --content '{"channel":"orders"}'` |
| A02 | `realization add` → `P realization add warehouse.obi.json noop.null --operation noop --source catalog --content 'null'` | `realization add` → `P realization add warehouse.obi.json noop.null --operation noop --source catalog --content null` |
| A03 | `realization add` → `P realization add warehouse.obi.json lookup.backup --operation lookup --source shared` | `realization add` → `P realization add warehouse.obi.json lookup.backup --operation lookup --source shared` |
| A04 | `consumption add` → `P consumption add warehouse.obi.json payments --operation charge --kind acme.http@1 --kind corp.rpc@3` | `consume add` → `P consume add warehouse.obi.json payments --operation charge --kind acme.http@1 --kind corp.rpc@3` |
| A05 | `kind supports` → `P kind supports corp.internal.bus@7 --refresh` | `handler supports` → `P handler supports corp.internal.bus@7 --action refresh` |
| A06 | `operation add` → `printf '%s\n' '{"type":"object"}' \| P operation add warehouse.obi.json normalize --input - --output -` | `operation add` → `P operation add warehouse.obi.json normalize --input - --output -` |
| A07 | `schema add` → `P schema add warehouse.obi.json Broken --value '{"type":'` | `schema add` → `P schema add warehouse.obi.json Broken --value '{"type":'` |
| A08 | `validate` → `P validate partial.obi.json --json` | `validate` → `P validate partial.obi.json --json` |

## Recovered syntax and diagnostics

These are the final task-shaped calls the reviewers found. Both used equivalent syntax unless noted. All positive calls returned exit 2 with the placeholder message. The calls below were verified against the frozen executable's parser; they did not act on locators.

| IDs | Recovered invocation or diagnostic |
|---|---|
| C01 | `P init warehouse.obi.json --name 'Warehouse API' --description 'Inventory contract' --interface-version '2026-09'` |
| C02 | `P show warehouse.obi.json --full -F json` |
| C03 | `P operation list warehouse.obi.json` |
| C04 | `P operation add warehouse.obi.json findItem --alias getItem --input-schema '{"type":"object","required":["sku"]}' --output-schema '{"type":"object"}' --idempotent true` |
| C05 | `P operation show warehouse.obi.json getItem --full -F json` |
| C06 | `P schema add warehouse.obi.json Item --value '{"type":"object","required":["sku"]}'` |
| C07 | `P schema list warehouse.obi.json` |
| C08 | `P source add warehouse.obi.json catalog --kind acme.http@1 --content '{"baseUrl":"https://catalog.example.test"}'` |
| C09 | `P binding add warehouse.obi.json findItem.http --operation findItem --source catalog --content '{"path":"/items/{sku}"}' --preference 10` |
| C10 | `P dependency add warehouse.obi.json billing --operation charge` |
| C11 | `P validate warehouse.obi.json -F json` |
| C12 | `P diff before.obi.json after.obi.json -F json` |
| K01 | `P kind list` (B added `-F json`) |
| K02 | `P kind check acme.http@1 --for invoke` |
| K03 | `P source import warehouse.obi.json supplier ./supplier-openapi.yaml --kind acme.openapi@1` |
| K04 | `P source inspect warehouse.obi.json catalog` |
| K05 | `P source pull warehouse.obi.json catalog` |
| K06 | `P source synthesize warehouse.obi.json catalog` (B added `-F json`) |
| K07 | `P source synthesize warehouse.obi.json catalog --apply` |
| K08 | `P invoke warehouse.obi.json findItem.http --input '{"sku":"A-17"}' --context staging` |
| K09 | `P invoke warehouse.obi.json health.http --input null` |
| K10 | `P patch warehouse.obi.json owner.patch.json` |
| A01 | `P source add warehouse.obi.json secret_bus --kind corp.internal.bus@7 --content '{"channel":"orders"}'` |
| A02 | `P binding add warehouse.obi.json noop.null --operation noop --source catalog --content null` |
| A03 | `P binding add warehouse.obi.json lookup.backup --operation lookup --source shared` |
| A04 | `P dependency add warehouse.obi.json payments --operation charge --kind acme.http@1 --kind corp.rpc@3` |
| A05 | `P kind check corp.internal.bus@7 --for pull` |
| A06 | `P operation add warehouse.obi.json normalize --input-schema - --output-schema -` → `stdin (-) can supply only one input per command` |
| A07 | `P schema add warehouse.obi.json Broken --value '{"type":'` → `--value: expected one JSON value, @file, or - for stdin` |
| A08 | `P validate partial.obi.json -F json` |

## Repeated wrong turns and candidate fixes

Both reviewers tried a `document` group instead of root `init` and `show`, a `realization` group instead of `binding`, and `consume`/`consumption` instead of `dependency`. Reviewer B also tried `handler` instead of `kind`, contributing to three recoveries outside the two-hop limit. Both guessed `source acquire`, `interpret`, `refresh`, and `preview`/`propose`/`sync` instead of `import`, `inspect`, `pull`, and `synthesize`. Both guessed `--json` instead of `-F json`; both initially shortened the operation schema flags and omitted the required value after `--idempotent`.

Candidate fixes, ordered by the observed error frequency and recovery cost: (1) put a natural-language task-to-command map at root help for `binding`, `dependency`, `kind`, and the source workflows; (2) consider discoverable aliases or error suggestions for the repeatedly guessed nouns and verbs; (3) accept `--json` or explicitly teach `-F json` in more examples; (4) show a complete operation contract example with `--input-schema`, `--output-schema`, and `--idempotent true`. Any proposed change needs a new frozen round; this report records only the current surface.
