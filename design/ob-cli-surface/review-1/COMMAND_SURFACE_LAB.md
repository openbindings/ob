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
./bin/ob operation rename tasks.obi.json createTask addTask --keep-alias
./bin/ob operation remove tasks.obi.json createTask            # refuses: bindings use it
./bin/ob binding set tasks.obi.json createTask.mcp --preference 20 --dry-run
./bin/ob schema rename tasks.obi.json Task Todo                # updates every $ref
./bin/ob validate tasks.obi.json
./bin/ob validate tasks.obi.json --operation createTask --input '{"title":5}'
./bin/ob invoke tasks.obi.json createTask --input '{"title":"Ship it"}'
./bin/ob invoke tasks.obi.json acme.tasks.createTask --input '{}'   # refused: input does not fit
./bin/ob invoke tasks.obi.json events.deliver                  # refused: no bindings
./bin/ob source inspect tasks.obi.json httpApi
./bin/ob status tasks.obi.json
./bin/ob compat tasks.obi.json acme-tasks.obi.json             # what a contract needs, and the commands that add it
./bin/ob operation set tasks.obi.json listTasks --add-alias acme.tasks.listTasks
./bin/ob merge tasks.obi.json acme-tasks.obi.json --operation acme.tasks.deleteTask
./bin/ob kind check example.openapi@2 --role invoke
./bin/ob delegate resolve --role invoke --kind acme.billing-rpc@1
./bin/ob operations list tasks.obi.json                        # near miss: suggests ob operation
```

Every command's `--help` has examples. `-F json` works wherever a command
prints a report, and `echo $?` shows the exit status.

## Open decisions, and how to feel each

The default tree takes the proposed side of each open decision. Set
`OB_SURFACE_VARIANT` to try the alternative.

| Decision | Proposed (default) | Alternative |
| --- | --- | --- |
| How edits write | Edit `<obi>` in place; `--dry-run` shows the diff; `-` as `<obi>` reads stdin and prints the result | `filter-edits`: every edit prints the whole resulting document and leaves the file alone (`-o` to save) |
| Changing and renaming | `set` and `rename` for every part; `rename` updates references; `operation rename --keep-alias` | none in this tree; `v02-lab` has only add, list, show, remove |
| What `invoke` takes | An operation name or alias; ob picks a binding (supported kind, not deprecated, highest preference, refuses on a tie); `--binding` overrides; prints output values one per line; `--events` for the whole exchange | `binding-invoke`: an exact binding key, always event envelopes |
| Which other areas return | All of them: serving (`start`, `mcp`), `codegen`, `context`, `fetch`, `delegate`, `compat`, `status`, `merge`, `fmt` | remove what should not be here |

Decided on 2026-09-30: there is no `adopt` (formerly `conform`, then
`correspond`). A document meets a shared contract when its operations carry
the contract's names, so the plain commands do it: `compat` shows what is
missing and prints both remedies, `operation set --add-alias` gives an
operation you already have the contract's name, and `merge` adds a contract
operation you lack. `OB_SURFACE_VARIANT=adopt` restores the old command for
comparison.

Other proposals in this tree worth a look:

- `show` prints a readable overview; `-F json` prints the exact stored document.
- `validate` has three modes: the document (conformant, non-conformant, or
  conformance undetermined, naming the spec text applied), one value against
  an operation (`--operation` with `--input` or `--output`), and every
  example (`--examples`).
- Commands that read a document also accept a URL, looked up with HTTP
  Discovery for a bare origin. `fetch` saves one.
- `-F` belongs to commands that print reports; there is no separate `--json`.
  `-o` belongs to commands that create a new file (`fetch`, `synthesize`,
  `codegen`), so `--input` and `--output` can name values.
- Exit statuses: 0 success; 1 a failed check or refusal of the request; 2 a
  usage error; 3 refused before anything was sent (`invoke`).
- `ob kind` answers what this installation can handle; `ob delegate` is the
  CLI face of the delegate-manager interface. Both use "role" (invoke,
  inspect, synthesize), the interface's word, which is a known leftover
  pending a rename.
- `--openbindings`, `--usage-spec`, and `--agent-primer` stay as root flags,
  as in production.

## What the preview does not model

- Nothing runs, and outputs are illustrative. Values given as `@file` or `-`
  are replaced by a marked placeholder.
- The sample's kinds (`example.openapi@1`, `example.mcp@1`) are the spec's
  illustrative ones; nothing here says how a published kind reads its content.
- Each command starts from the same sample, so edits do not accumulate.

## Tests

```sh
GOWORK=off go test ./internal/cmd -run 'TestNext' -count=1
```

They run every help example in every variant; check that every edit's
result, and every new document, passes the 0.2 schema (vendored from spec
`de2c20b` in `internal/cmd/testdata/`); and check near-miss hints, exit
statuses, and `-F json` output.

## History

- The previous 0.2 lab tree is `OB_SURFACE_VARIANT=v02-lab`, and its
  variants `v02-lab:<name>`. It is pinned to spec `ccfe0b6`. Its loop
  records are the other `COMMAND_SURFACE_*.md` files in this directory.
- The 0.1-era previews remain as `broad`, `compact`, and `hybrid`.
