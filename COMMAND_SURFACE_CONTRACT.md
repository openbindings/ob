# Proposed command-output contract

This contract applies to all three facade variants. The 69 executable leaves are
classified individually in `internal/cmd/surface_contract.go`; tests fail if a
leaf lacks a classification or a variant loses a capability. Commands are
inert, so this is a **proposed** interface, not evidence of runtime behavior.

| Mode | Leaves | Result and destination | Format rule |
| --- | ---: | --- | --- |
| OBI document | 4 | `new`, `resolve`, `synthesize`, and `delegate requirements` print a document. `-o` saves canonical JSON. `new` accepts no positional path. `resolve --with-origin -o` saves a JSON envelope rather than an OBI artifact. | `-F yaml` is a stdout-only view and cannot combine with `-o`; text is rejected. |
| OBI edit | 23 | Update the input OBI by default; `-o` writes a resulting JSON OBI elsewhere. Print an edit summary. | `-F` renders the summary, never changes the edit. |
| Report | 27 | Print a selected report; `-o` saves it. | `-F` renders the same report. A `--quiet` report emits no result and rejects `-o`/`-F`. |
| Configuration edit | 8 | Update the active environment and print a summary. `-o` is rejected. | `-F` renders the summary. |
| Operation stream | 2 | Print one JSON value per event to stdout; redirect stdout to save it. `--envelope` collects into a bounded object. `-o` is rejected. | Uncollected streams allow JSON only. `--envelope` permits text, JSON, or YAML. |
| Binding invocation | 1 | Positional form takes an optional binding value with `--input` and may save its result with `-o`. No-argument machine form requires `--request` for the whole BindingInvocationInput envelope and prints one JSON result envelope; it rejects `-o`/`-F`. | Positional result may be rendered; machine envelope is fixed JSON. |
| Foreground process | 3 | Server/demo output belongs to the process, not a result file. | Both `-o` and `-F` are rejected and omitted from leaf help. |
| Generated code | 1 | Raw code by default; `--envelope` explicitly selects a language/code object; `-o` saves the selected result. | `-F` encodes the selected result and cannot select the envelope. |

`-F` accepts only `text`, `json`, or `yaml`; commands with narrower modes
reject inapplicable values. `show --full`, `resolve --with-origin`,
`codegen --envelope`, and `context get --reveal-secrets` select *content*;
`-F` only selects presentation. `--sample-output` is a hidden lab switch:
fixtures print a warning to stderr, never use the supplied locator, and reject
`-o` to avoid suggesting a file was written.
Configuration-edit and operation-stream leaf help displays only the applicable
`-F` flag. Foreground-process leaf help omits both result flags.

## Proposed exit contract

| Code | Meaning | Applicable cases |
| ---: | --- | --- |
| 0 | Completed action or positive report | Successful edit, complete invocation, conformant/compatible/identical result, no drift, or clean `--check`. |
| 1 | Negative result or unsuccessful invocation | Non-conformant OBI, incompatible comparison, diff found, drift under `--exit-code`, unsupported ID under `binding-specs check`, x-ob metadata found under `--check`, or an invocation's unsuccessful result. |
| 2 | Usage or execution error | Invalid arguments, unreadable locator, unavailable service, failed write, or process failure. Diagnostics go to stderr. |
| 3 | Incomplete or undetermined result | Conformance/compatibility cannot be concluded, or bounded invocation collection truncates. The result explains why it is incomplete. |

`--quiet` suppresses result output, not the verdict exit code. A stream can
already have printed values before a later failure; its nonzero exit and
stderr diagnostic signal that stdout is incomplete. The facade's parser
errors and placeholders currently exit 2 through `cmd/ob/main.go`, while
sample output exits 0. The 1/3 and runtime-failure cases are proposals that
cannot be exercised on this inert branch.

The `binding-specs check` verdict exit is a proposed contract. Its production
handler currently returns a report without a nonzero exit for an unsupported
ID; that handler is unchanged on this branch.

The command-name policy is one canonical spelling per action. Routine
abbreviations (`op`, `src`, `ctx`, `ls`, `rm`, and `mv`) are absent from this
facade. `describe` and `purify` remain migration aliases for renamed commands;
help examples use canonical spellings.
