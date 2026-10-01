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
./bin/ob operation add tasks.obi.json x --input-schema '{"type":42}'   # refused: the result would be non-conformant
./bin/ob set tasks.obi.json --interface-version 2.0.0
./bin/ob operation example set tasks.obi.json createTask basic --input '{"title":"Plan"}'
./bin/ob operation remove tasks.obi.json createTask            # refused: bindings use it (exit 3)
./bin/ob operation remove tasks.obi.json acme.tasks.createTask  # refused: that is an alias
./bin/ob operation set tasks.obi.json completeTask --unset output-schema
./bin/ob binding set tasks.obi.json createTask.mcp --preference 20 --dry-run
./bin/ob schema rename tasks.obi.json Task Todo                # updates every $ref
./bin/ob validate tasks.obi.json
./bin/ob validate tasks.obi.json --operation createTask --input '{"title":5}'
printf '{"title":"a"}\n{"title":5}\n' | ./bin/ob validate tasks.obi.json --operation createTask --input -
./bin/ob source inspect tasks.obi.json httpApi
./bin/ob status tasks.obi.json                                 # drift per source; grpcApi cannot be checked (exit 4)
./bin/ob source pull tasks.obi.json --dry-run                  # adds new targets; never rewrites an operation unasked
./bin/ob source pull tasks.obi.json httpApi --update-operation listTasks
./bin/ob source pull tasks.obi.json --update-operation createTask   # refused: two sources disagree
./bin/ob source pull tasks.obi.json mcpServer --target tools/complete_task --operation completeTask
./bin/ob compat tasks.obi.json acme-tasks.obi.json             # what a contract needs, and the commands that add it
./bin/ob operation set tasks.obi.json listTasks --add-alias acme.tasks.listTasks
./bin/ob merge tasks.obi.json acme-tasks.obi.json --operation acme.tasks.deleteTask
./bin/ob merge tasks.obi.json acme-tasks.obi.json              # skips what already meets the contract
./bin/ob merge tasks.obi.json other.obi.json                   # refused: lists the conflicts
./bin/ob merge tasks.obi.json other.obi.json --theirs          # take the other side of each conflict
./bin/ob context list
./bin/ob mcp tasks.obi.json                                    # createTask is skipped: two bindings, none chosen
./bin/ob mcp tasks.obi.json --binding createTask.http
./bin/ob start --tls                                           # refused: installing the CA is its own step
./bin/ob describe
./bin/ob kind check example.openapi@2 --role invoke
./bin/ob delegate resolve --role invoke --kind acme.billing-rpc@1
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
| write an input value | `--input VALUE` (one value), or a stream of JSON values from `--input @FILE` or `--input -` |
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
JSON object.
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
| 0 | done, or yes |
| 1 | failed, or no (for `invoke`, the operation may have taken effect) |
| 2 | usage error |
| 3 | refused; nothing was written or sent (every refused edit, merge, remove, rename, and invoke) |
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

Decided on 2026-09-30: there is no `adopt` (formerly `conform`, then
`correspond`). A document meets a shared contract when its operations carry
the contract's names, so the plain commands do it: `compat` shows what is
missing and prints both remedies, `operation set --add-alias` gives an
operation you already have the contract's name, and `merge` adds a contract
operation you lack. `OB_SURFACE_VARIANT=adopt` restores the old command for
comparison.

Also decided on 2026-09-30:

- Edits refuse to write a document that breaks a document rule it did not
  already break, and name the rule (the preview checks OBI-D-02, D-04, D-05,
  D-07, D-08, D-10, D-11, and D-12).
- `rename` renames. Given an alias it refuses and says whose alias it is;
  renaming to the operation's own alias is refused until the alias is
  removed. There is no `--keep-alias`.
- The document argument is always explicit. ob does not look for a file by
  name; the spec defines no file extension.
- `merge` refuses when the two documents give the same name different
  definitions, and lists every conflict; `--ours` keeps this document's side
  and `--theirs` takes the other's. Identical entries are skipped, and names
  match by key or alias.
- `invoke --binding` is strict: a named binding that does not exist fails,
  and ob never falls back to a binding you did not name.
- `source pull --target` binds one target of a source.
- Commands that derive a new file from other input take `-o, --out`.
- Contradictory flags on one edit (for example `--input-schema false` with
  `--unset input-schema`) are a usage error. Removing a dependency's last
  kind is refused, because a dependency without kinds declares no kind
  constraint (spec §5.5); `--unset kinds` says that on purpose.

Decided on 2026-10-01, after review round 2:

- Every refusal that writes or sends nothing exits 3. A lookup that finds
  nothing in a read-only command exits 1.
- `invoke` has no `--no-check`; checking values is part of what an operation
  invoker does.
- `source pull` adds targets no binding covers yet and refreshes binding
  content. It never changes an operation you have: schema differences are
  reported, `--update-operation` takes them, and two sources that disagree
  are refused. `status` reports the same. A source ob cannot read is named,
  and the exit status is 4.
- `source pull --target` accepts a target's label or its identifier, and
  `--operation` binds it to an operation you already have.
- `init <obi>` keeps its positional document. Every command that creates a
  file refuses to replace one without `--force` (`init`, `fetch -o`,
  `synthesize -o`, `codegen -o`).
- `start` serves HTTP. `--tls` adds HTTPS, and installing ob's local
  certificate authority is its own explicit step (`--install-ca`).
- `operation remove` refuses an alias, as `rename` does.
- No `migrate` command. `diff --help` and `compat --help` say that `compat
  new old` is the breaking-change check.
- `ob describe` (with `-F obi` and `-F usage`) and the help topic `ob help
  primer` replace the root flags `--openbindings`, `--usage-spec`, and
  `--agent-primer`.
- `show` has no YAML form; an OBI is JSON.
- `--unset` takes a flag's own name (`--unset input-schema` undoes
  `--input-schema`).
- `merge` skips operations that already meet `<from>`'s operation, and lists
  them as met.
- The overlapping pairs stay, each saying how it differs: `status` and
  `source pull --dry-run`, `kind check` and `delegate resolve`, `fetch` and
  `show -F json`. The three exit-status flags stay, each matching its peers:
  `-q` (validate, compat), `--exit-code` (diff, status), `--check` (fmt).

- When a binding needs a sign-in (OAuth 2.0) and ob is not at a terminal,
  the refusal prints the same invoke with `--preflight`, to run once at a
  terminal. `--preflight` calls nothing; at a terminal it gets what is
  missing, asking or running the sign-in, and stores it.

Other proposals in this tree worth a look:

- `show` prints a readable overview; `-F json` prints the exact stored document.
- `validate` has three modes: the document (conformant, non-conformant, or
  conformance undetermined, naming the spec text applied), one value against
  an operation (`--operation` with `--input` or `--output`), and every
  example (`--examples`).
- Commands that read a document also accept a URL, looked up with HTTP
  Discovery for a bare origin. `fetch` saves one.
- `-F` belongs to commands that print reports; there is no separate `--json`.
  `-o, --out` belongs to commands that create a new file (`fetch`,
  `synthesize`, `codegen`), so `--input` and `--output` can name values.
- `ob kind` answers what this installation can handle; `ob delegate` is the
  CLI face of the delegate-manager interface. Both use "role" (invoke,
  inspect, synthesize), the interface's word, which is a known leftover
  pending a rename.

## What the preview does not model

- Nothing runs, and outputs are illustrative. `invoke` reads real input
  values and checks them, but answers with sample results; the stream from
  `watchTasks` is paced only at a terminal. Values given as `@file` or `-` to
  other commands are replaced by a marked placeholder.
- The pretend installation stores context for `https://api.example.com` and
  `https://api.example.com/openapi.json`, and none for the MCP server. It
  can invoke gRPC but not inspect it, so `status` and `pull` cannot check
  `grpcApi`. It has not installed ob's local certificate authority.
- The sample's kinds (`example.openapi@1`, `example.mcp@1`) are the spec's
  illustrative ones; nothing here says how a published kind reads its content.
- Each command starts from the same sample, so edits do not accumulate.

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
