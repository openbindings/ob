# OpenBindings CLI (`ob`)

Rust is the maintained CLI implementation on `release/0.2`, the repository
default and integration branch. It currently implements the designed command
surface only.
**Help, argument parsing, version output, and shell completion generation work.
The 73 operational commands are placeholders.** They exit **3**, print an
explicit message to stderr, and perform no operation.

This is a source preview, not a released CLI or a working replacement for the
Go implementation. It does not yet parse OBI documents, invoke operations,
manage credentials, or start servers. Installing the existing Homebrew or Go
release does not install this Rust preview.

## Try the surface

With [Rust and Cargo](https://www.rust-lang.org/tools/install) installed:

```sh
cargo run --locked -- --help
cargo run --locked -- source pull --help
cargo run --locked -- invoke --help
cargo run --locked -- completion zsh
```

`rust-toolchain.toml` pins the toolchain. Go and sibling SDK checkouts are not
needed to build the Rust binary. To run it directly:

```sh
cargo build --locked --release
./target/release/ob --help
```

For example, `./target/release/ob init tasks.obi.json --name Tasks` checks the
command syntax and exits 3; it does **not** create `tasks.obi.json`.

## Command families

| Area | Commands |
| --- | --- |
| Documents | `init`, `show`, `set`, `validate`, `diff`, `compat`, `fmt`, `patch`, `merge` |
| Parts of a document | `operation` (including `example`), `source`, `binding`, `dependency`, `schema` |
| Sources | `synthesize`, `status` |
| Using a service | `fetch`, `invoke`, `context` |
| Advanced | `codegen`, `start`, `mcp`, `ca`, `kind`, `delegate`, `describe`, `completion` |

Use `ob help <command>` or `ob <command> --help` for the intended workflow,
options, and examples. Descriptions of operational behavior are the design
target; they are not implementation claims.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for checks and branching, and the
[Rust surface notes](docs/rust-command-surface.md) for the migration boundary,
source pins, current checks, and the path to handlers.

The complete Go implementation is preserved on
[`legacy/go-cli`](https://github.com/openbindings/ob/tree/legacy/go-cli), outside
the maintained source tree. Its commands and capability claims describe that
historical implementation. See [the preservation record](docs/legacy-go.md)
for exact revisions, original documentation, and recovery instructions.
The historical surface-lab branch also remains unchanged.
