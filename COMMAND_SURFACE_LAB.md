# ob command-surface lab

This branch is a playable preview of the proposed `ob` command surface for
OpenBindings 0.2. Every command answers from a built-in sample document (a
Task Manager API modeled on the spec's §4 example) and a pretend
installation. Nothing is read, written, or called. Edit commands show the
change they would make as a diff, and read commands answer about the sample,
so commands agree with each other the way a real `ob` would.

The tree is `NewNextSurfaceRoot` in `internal/cmd/surface_next*.go`. It
follows the spec's `release/0.2` draft at `de2c20b`.

## Build and play

```sh
GOWORK=off go build -o ./bin/ob ./cmd/ob
./bin/ob --help
```

Things to try:

```sh
./bin/ob show tasks.obi.json
./bin/ob operation add tasks.obi.json archiveTask --description "Archive a task." --output-schema '{"$ref":"#/schemas/Task"}'
./bin/ob operation rename tasks.obi.json createTask addTask   # renames; updates bindings; adds nothing
./bin/ob operation add tasks.obi.json x --input-schema '{"type":42}'   # refused: the result would be non-conformant (exit 3)
./bin/ob operation show tasks.obi.json craeteTask              # a name not in the document: exit 2, with a suggestion
./bin/ob set tasks.obi.json --interface-version 2.0.0
./bin/ob operation example set tasks.obi.json createTask basic --input '{"title":"Plan"}'
./bin/ob operation remove tasks.obi.json createTask            # refused: bindings use it (exit 3)
./bin/ob operation remove tasks.obi.json acme.tasks.createTask  # refused: that is an alias
./bin/ob operation set tasks.obi.json completeTask --unset output-schema
./bin/ob operation set tasks.obi.json createTask --add-alias acme.tasks.createTask   # already true: no change (exit 0)
./bin/ob binding set tasks.obi.json createTask.mcp --preference 20 --dry-run
./bin/ob schema rename tasks.obi.json Task Todo                # updates every $ref
./bin/ob schema list tasks.obi.json                            # and the external schemas still referenced
./bin/ob schema bundle tasks.obi.json                          # embeds them by $id; no $ref changes
./bin/ob validate tasks.obi.json
./bin/ob validate tasks.obi.json --operation createTask --input '{"title":"a"}' --input '{"title":5}'
./bin/ob validate tasks.obi.json --examples --operation createTask
./bin/ob source inspect tasks.obi.json httpApi
./bin/ob status tasks.obi.json --exit-code                     # drift, apart from targets left unbound
./bin/ob source pull tasks.obi.json httpApi                    # refreshes bindings; adds nothing unasked
./bin/ob source pull tasks.obi.json httpApi --update-operation listTasks
./bin/ob source pull tasks.obi.json --update-operation createTask   # refused: two sources disagree
./bin/ob source pull tasks.obi.json mcpServer --target tools/complete_task --operation completeTask
./bin/ob source pull tasks.obi.json httpApi --target 'POST /tasks/{id}/archive' --new-operation archive --binding-key archivalHttp
./bin/ob source pull tasks.obi.json httpApi --all-targets --dry-run   # names and choices before the edit
./bin/ob compat tasks.obi.json acme-tasks.obi.json             # each operation checked in its role
./bin/ob merge tasks.obi.json acme-tasks.obi.json --operation acme.tasks.deleteTask --no-bindings
./bin/ob merge tasks.obi.json other.obi.json                   # refused: lists the conflicts
./bin/ob merge tasks.obi.json other.obi.json --theirs          # take the other side of each conflict
./bin/ob show https://private.example.com < /dev/null          # refused: the service asks for sign-in
./bin/ob show https://nothing.example.com                      # no OBI published there (exit 1)
./bin/ob validate https://old.example.com                      # refused: written for OpenBindings 0.1
./bin/ob context set https://api.example.com --bearer-token abc   # usage error (exit 2): secrets take - or @FILE
./bin/ob context show https://api.example.com
./bin/ob context set https://tokens.example.com --token-credential -   # rotates the stored provider's credential
./bin/ob mcp tasks.obi.json --binding createTask.http
./bin/ob start --allow-origin https://editor.example.com
./bin/ob ca show
./bin/ob delegate show d_91c2
./bin/ob describe
./bin/ob kind check example.grpc@1 --role inspect
./bin/ob operations list tasks.obi.json                        # near miss: suggests ob operation
```

Every command's `--help` has examples. `-F json` works wherever a command
prints a report, and `echo $?` shows the exit status.

## Invoking: one run is one exchange

`ob invoke` follows the invocation pattern of the operation-invoker
interface: an invocation is opened, input values are written to it, output
values come back as they arrive, and it ends in one terminal state. The
cardinality is whatever the binding does; the command is the same for all of
them. As a Unix process:

| Invocation | `ob invoke` |
| --- | --- |
| write input values | each `--input`, in order: `VALUE` (one value), or a stream of JSON values from `@FILE` or `-` |
| close the input | the end of those values (Ctrl-D at a terminal); no `--input` writes nothing and closes |
| each output value | one line of JSON on stdout, printed as it arrives |
| the whole exchange | `--frames`: the interface's own frames, one per line, including `input_closed` and the final `complete` or `error` |
| cancel | Ctrl-C |
| the terminal state | the exit status |

```sh
./bin/ob invoke tasks.obi.json completeTask --input '{"id":"t_7"}'          # one binding, stored context
./bin/ob invoke tasks.obi.json createTask --input '{"title":"x"}'           # refused: two bindings, choose one
./bin/ob invoke tasks.obi.json createTask --binding createTask.http --input '{"title":"x"}'
printf '{"title":"a"}\n{"title":"b"}\n{"title":"c"}\n' | ./bin/ob invoke tasks.obi.json importTasks --input -
./bin/ob invoke tasks.obi.json importTasks --input '{"title":"a"}' --input '{"title":"b"}'   # each --input is a write
./bin/ob invoke tasks.obi.json watchTasks                                  # a stream out; Ctrl-C cancels
./bin/ob invoke tasks.obi.json watchTasks --frames
printf '{"title":"a"}\n{"title":"b"}\n' | ./bin/ob invoke tasks.obi.json createTask --binding createTask.http --input - --frames
printf '{"title":"a"}\n{"title":5}\n' | ./bin/ob invoke tasks.obi.json importTasks --input - --frames
./bin/ob invoke tasks.obi.json createTask --binding createTask.mcp --input '{"title":"x"}' < /dev/null
./bin/ob invoke tasks.obi.json createTask --binding createTask.mcp --preflight
```

The binding is chosen the way the operation-invoker contract says: the
first invocable binding in an explicit list (`--binding`, repeatable), or
the sole invocable binding, and otherwise a refusal that lists the
candidates with their kind, preference, and deprecation. Preference and
deprecation are shown but never used to choose. `--binding` is the
interface's ordered `selection` (one name is a list of one): a name that is
not one of the operation's bindings is refused, and so is a list with none
ob can invoke, never a fallback to a binding you did not name. The
`binding-invoke` variant's exact binding key is the interface's `binding`.
(The strict selection rule is openbindings/interfaces#36.)

With `--frames`, every run that reaches the invoker ends with exactly one
terminal frame, a refusal included. The interface's own error codes carry
no `data`; the reason is on stderr.

Checks follow the same boundary as the interface. A first input value that
does not fit is refused before anything is sent (exit 3); a later one ends
the exchange with `ERR_OPERATION_VALIDATION_FAILED` after earlier values
were sent (exit 1). An output value that does not fit ends it the same way.

Context follows the context resolution pattern. A binding asks for context
by an exact scope; ob looks up only that scope, uses stored values only when
the requirement says they may be reused (durable), and sends only the fields
that one request needs. With nothing stored, ob gets it at a terminal: it
asks for a value, or runs the sign-in the binding names (an OAuth 2.0 flow
for the MCP binding), stores the tokens, and renews them as they expire.
Otherwise it stops before sending anything and prints what supplies it:
an `ob context set` command, or for a sign-in, the same invoke with
`--preflight`, to run once at a terminal. `--context` supplies context for one call, as a
JSON object from a file or stdin. `--timeout` gives the call a deadline.
Registering a delegate as an invoker does not give it the context store: a
delegate resolves its own context, and a challenge from one is shown to you
(naming who asked) or refused.

The sample's shapes: `completeTask`, `createTask`, and `listTasks` take one
value and return one; `importTasks` takes a stream and returns one;
`watchTasks` takes one and returns a stream.

## Exit status

The same table for every command:

| Status | Meaning |
| --- | --- |
| 0 | done, or yes; an edit that is already true is "no change" |
| 1 | failed, or no (for `invoke`, the operation may have taken effect) |
| 2 | usage error, including a name that is not in the document or the store |
| 3 | refused: ob understood the request and declined it; nothing was written or sent |
| 4 | no verdict: a check could not decide (`validate`, `compat`, `status`, a `source pull` that skipped a source) |
| 130 | cancelled |

## Open decisions, and how to feel each

The default tree takes the proposed side of each open decision. Set
`OB_SURFACE_VARIANT` to try the alternative.

| Decision | Proposed (default) | Alternative |
| --- | --- | --- |
| How edits write | Edit `<obi>` in place; `--dry-run` shows the diff; `-` as `<obi>` reads stdin and prints the result | `filter-edits`: every edit prints the whole resulting document and leaves the file alone (`-o, --out` to save) |
| Changing and renaming | `set` and `rename` for every part, and `ob set` for the document's own fields; `rename` changes the key and the references to it, nothing else | none in this tree; `v02-lab` has only add, list, show, remove |
| What `invoke` takes | An operation name or alias, with the binding chosen as described above | `binding-invoke`: an exact binding key, always frames |
| Which other areas return | All of them: serving (`start`, `mcp`), `codegen`, `context`, `fetch`, `delegate`, `compat`, `status`, `merge`, `fmt` | remove what should not be here |

## Decided

Every point below is the maintainer's ruling (2026-09-30 and 2026-10-01).

Documents and edits:

- There is no `adopt`. A document meets a shared contract when its
  operations carry the contract's names: `compat` shows what is missing and
  prints both remedies (`operation set --add-alias`, and `merge ...
  --no-bindings`). `compat new.obi.json old.obi.json` is the breaking-change
  check; there is no `migrate`.
- `compat` checks each operation in the role the document gives it: with
  bindings, as a provider; called by a dependency, as a consumer (spec §5.5);
  both, both ways.
- The document argument is always explicit. ob does not look for a file by
  name; the spec defines no file extension.
- Edits refuse to write a document that breaks a document rule it did not
  already break, and name the rule. `patch` follows the same rule.
- `rename` renames. `rename` and `operation remove` refuse an alias and say
  whose it is.
- `--unset` takes a flag's own name (`--unset input-schema`), and keyed
  fields as `header.NAME`, `config.POINT`, `preference.ROLE`.
- `merge` refuses conflicting definitions unless `--ours` or `--theirs`
  says which side wins, and skips operations that already meet the other's.
- `init <obi>` takes the document as its argument; commands that derive a
  new file from other input take `-o, --out` (`codegen` requires it); every
  command that creates a file refuses to replace one without `--force`.
- `ob schema bundle` embeds the external schemas a document references, by
  `$id`, so no `$ref` changes (JSON Schema's own bundling, spec §7.4).
- `show` and every other command print text or JSON; there is no YAML.

Sources:

- `source pull` refreshes the bindings you have and adds nothing unasked.
  `--target` binds one target (by label or identifier, to a suggested or an
  existing operation with `--operation`), `--all-targets` binds every one,
  and `--update-operation` takes a source's schemas for an operation; two
  sources that disagree are refused. `status` reports drift apart from
  targets left unbound, and only drift fails `--exit-code`. A source ob
  cannot read is named, with exit 4.
- With `--target`, `--operation NAME` selects an existing operation by exact
  key or alias. An unknown name is exit 2, never implicit creation.
  `--new-operation NAME` explicitly creates an operation under that name;
  the two flags are mutually exclusive. `--binding-key KEY` names the
  binding. These flags apply to one target, never to `--all-targets`.
  Without an override, use the handler's suggestions; choosing an operation
  leaves the suggested binding key unchanged. No dot segments are parsed
  to derive a name. An existing operation keeps every field. A new operation
  uses the handler's framing where supplied; absent framing states no input
  or output schema. Missing name suggestions require explicit names.
- A pull is no change only when the requested result is already true.
  An explicit binding key with the requested source, content, and resolved
  operation is no change; a different binding at that key is refused
  (exit 3). An unused explicit key can add another binding for a target
  already covered. Without an explicit key, a covered target can satisfy
  the selected operation, but a different operation is refused with the
  command that adds or changes the requested binding. `--new-operation`
  accepts a retry only when its requested binding already exists for that
  exact operation key; otherwise an occupied operation name is refused.
- Naming conflicts refuse the whole pull before writing. Generated binding
  keys are never overwritten or silently numbered. Two targets suggesting
  the same new operation name require explicit choices in separate pulls.
  Suggested names matching existing aliases resolve to the existing key.
  `--all-targets --dry-run` lists each proposed target, binding, and new or
  existing operation, with single-target commands for choosing an existing
  operation before the edit. After the edit, guidance uses `binding set`
  and, if the new operation becomes unused, `operation remove`.

Invoking and context:

- `ob invoke` is one exchange, following the operation-invoker pattern
  (above). Each `--input` is the next write. Values are always checked;
  there is no `--no-check`. `--binding` never falls back to a binding you
  did not name. `--timeout` gives the call a deadline.
- Context follows the context resolution pattern. Registering a delegate
  does not give it the context store. Away from a terminal, a sign-in
  refusal prints the same invoke with `--preflight`.
- A secret never goes on the command line: credential flags take `-` (read
  from stdin, or asked for at a terminal) or `@FILE`; `--context` and
  `context set --value` take `@FILE` or `-`.
- A document URL behind sign-in uses the context stored for its origin, asks
  at a terminal, and otherwise is refused with the `ob context set` command
  that fixes it. No OBI published there exits 1. A document written for an
  OpenBindings version ob does not read is refused, except by `show -F json`
  and `fetch`.

Serving, delegates, and the rest:

- `ob start` serves HTTP and requires a per-run access token, answers only
  requests addressed to 127.0.0.1 or localhost, and lets browser pages in
  only from `--allow-origin`. `--tls` adds HTTPS; the certificate authority
  is installed, shown, and removed with `ob ca`.
- `ob mcp` is a placeholder: how operations appear as MCP tools waits for the
  MCP binding specification. It chooses bindings like `invoke` and refuses an
  operation it was asked for but cannot offer.
- `delegate` uses add, set, remove, list, and show, with IDs ob gives.
- `ob describe` (with `-F obi` and `-F usage`) and the help topic `ob help
  primer` replace the root flags `--openbindings`, `--usage-spec`, and
  `--agent-primer`.
- Root help starts with a short path, and groups `codegen`, `start`, `mcp`,
  `ca`, `kind`, `delegate`, and `describe` as Advanced.
- The overlapping pairs stay, each saying how it differs: `status` and
  `source pull --dry-run`, `kind check` and `delegate resolve`, `fetch` and
  `show -F json`. So do the three exit-status flags, each matching its
  peers: `-q` (validate, compat), `--exit-code` (diff, status), `--check`
  (fmt). `synthesize` and `source import` stay separate commands.

Proposals not ruled on, open to challenge:

- `show` prints a readable overview; `-F json` prints the exact stored
  document.
- `validate` has three modes: the document (conformant, non-conformant, or
  conformance undetermined, naming the spec text applied), values against an
  operation (`--operation` with `--input` or `--output`), and examples
  (`--examples`).
- `-F` belongs to commands that print reports; there is no separate `--json`.
- `ob kind` answers what this installation can handle; `ob delegate` is the
  CLI face of the delegate-manager interface. Both use "role" (invoke,
  inspect, synthesize), the interface's word, a known leftover pending a
  rename.

## What the preview does not model

- Nothing runs, and outputs are illustrative. `invoke` reads real input
  values and checks them, but answers with sample results; the stream from
  `watchTasks` is paced only at a terminal. Values given as `@file` or `-` to
  other commands are replaced by a marked placeholder.
- The pretend installation stores context for `https://api.example.com` and
  `https://api.example.com/openapi.json`, and a pinned token provider for
  `https://tokens.example.com`; none is stored for the MCP server. It
  can invoke gRPC but not inspect it, so `status` and `pull` cannot check
  `grpcApi`. It has installed ob's local certificate authority.
- The sample's `Task` references two published schemas
  (`https://schemas.example.com/...`), which a pretend store answers, so
  checks and `schema bundle` work offline. Three pretend services answer
  document URLs: `private.example.com` asks for sign-in,
  `nothing.example.com` publishes no OBI, and `old.example.com` publishes
  one written for OpenBindings 0.1.
- The sample's kinds (`example.openapi@1`, `example.mcp@1`) are the spec's
  illustrative ones; nothing here says how a published kind reads its content.
- Each command starts from the same sample, so edits do not accumulate.
- Matched schemas in the sample contract fit. The comparison engine is not
  implemented here; tests supply illustrative profile failures to check the
  report's issue order, reasons, and verdict dominance.

## Tests

```sh
GOWORK=off go test ./internal/cmd -run 'TestNext' -count=1
```

They run every help example in every variant; check that every edit's
result, and every new document, passes the 0.2 schema (vendored from spec
`de2c20b` in `internal/cmd/testdata/`); check the invocation pattern (streams
in and out, early close, both kinds of input failure, binding choice, and
context refusal); and check refusals, near-miss hints, exit statuses, and
`-F json` output.

The production tests elsewhere in this branch fail, because `cmd/ob` now
builds the preview tree and the sibling SDK and spec checkouts have moved.
The lab does not use them.

## History

- The previous 0.2 lab tree is `OB_SURFACE_VARIANT=v02-lab`, and its
  variants `v02-lab:<name>`. It is pinned to spec `ccfe0b6`. Its loop
  records are the other `COMMAND_SURFACE_*.md` files in this directory.
- The 0.1-era previews remain as `broad`, `compact`, and `hybrid`.
