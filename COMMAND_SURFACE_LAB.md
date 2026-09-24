# ob command-surface lab

This branch is an **explorable CLI proposal**, not a working `ob` build. The
`ob` executable loads `NewSurfaceRoot`; every operational command returns a
placeholder error before reading or changing an OBI, configuration, or a
service. `--help` and `--version` work.

From this worktree:

```sh
GOWORK=off go build -o ./bin/ob ./cmd/ob
./bin/ob --help
./bin/ob show --help
./bin/ob source add --help
./bin/ob operation --help
./bin/ob merge --help
```

`GOWORK=off` avoids the parent development workspace, which refers to sibling
modules absent from this checkout. The normal `NewRoot` and implementation
files remain intact for reference; this branch changes the executable entry
point and overlays proposed help/flags in `internal/cmd/surface.go`.

## Proposals visible in the shell

- Root help leads with example tasks and groups commands by user intent.
- `ob show <obi>` presents a document; `source show`, `operation show`, and
  `binding show` inspect one part. `-F json|yaml` is intended to emit the full
  value rather than a shortened human summary.
- `ob source add <obi> <source> --pull` is an atomic tracked-import path. A
  plain `source add` still registers without deriving.
- `-o` is intended to name the primary result: an OBI for edits and resolve,
  a report for checks, generated code for codegen. `-F` controls encoding,
  not the result's shape.
- `ob resolve` is intended to print the OBI by default. `--with-origin`
  requests a provenance envelope.
- `ob operation invoke --envelope` explicitly selects aggregated output;
  the default remains a stream of JSON values.
- `ob about` describes the CLI itself; `ob describe` remains an alias in this
  proposal. `ob show` describes an OBI.

The shell deliberately retains the other command names and flag shapes so the
whole surface can be judged in context. In particular, `meta`, `synthesize`,
`compat`, `conform`, `binding-specs`, and the scope of `status` remain open
design decisions. Nothing in this branch updates the published command
contract, `usage.kdl`, or the production handlers.
