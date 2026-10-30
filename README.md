# OpenBindings CLI (`ob`)

Rust is the maintained CLI implementation on `release/0.2`, the repository
default and integration branch. It currently implements the designed command
surface only.
**Help (`ob help` and its topics), argument parsing, version output, and shell
completion generation work. The 44 operational commands are placeholders.**
They exit **3**, print an explicit message to stderr, and perform no
operation.

This is a source preview, not a released CLI or a working replacement for the
Go implementation. It does not yet parse OBI documents, invoke operations,
manage credentials, or start servers. Installing the existing Homebrew or Go
release does not install this Rust preview.

## Try the surface

With [Rust and Cargo](https://www.rust-lang.org/tools/install) installed:

```sh
cargo run --locked -- --help
cargo run --locked -- help obi
cargo run --locked -- source add --help
cargo run --locked -- source pull -h
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
| Documents | `init`, `show`, `validate`, `diff`, `compat`, `merge`, `patch`, `fmt` |
| Parts of a document | `operation` (`list`, `show`, `rename`, `remove`), `binding` (`list`, `show`, `add`), `dependency` (`list`, `show`), `schema` (`list`, `show`, `rename`, `remove`, `bundle`) |
| Sources | `source` (`list`, `show`, `add`, `pull`, `rename`, `remove`) |
| Using a service | `fetch`, `invoke`, `context` (`set`, `list`, `show`, `remove`) |
| This installation | `kind` (`list`, `describe`), `delegate` (`jobs`, `add`, `set`, `remove`, `list`, `show`, `resolve`), `describe` |
| Shell and help | `help`, `completion`; the topics `ob help obi`, `ob help documents`, `ob help exit-codes`, `ob help output` |

Use `ob <command> -h` (or `ob help <command> -h`) for a summary and
`ob help <command>` or `ob <command> --help` for the full page: the intended
workflow, options, examples and exit status. Descriptions of operational
behavior are the design target; they are not implementation claims. Commands
the design removed, such as `ob synthesize` and `ob kind check`, print the
hint that names their replacement. Every usage error is an `error:` line and
a `hint:` line that names a command to run.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for checks and branching, and the
[Rust surface notes](docs/rust-command-surface.md) for where the surface comes
from (the project's CLI design draft and its generator), the current checks,
and the path to handlers.

The complete Go implementation is preserved on
[`legacy/go-cli`](https://github.com/openbindings/ob/tree/legacy/go-cli), outside
the maintained source tree. Its commands and capability claims describe that
historical implementation. See [the preservation record](docs/legacy-go.md)
for exact revisions, original documentation, and recovery instructions.
The historical surface-lab branch also remains unchanged.
