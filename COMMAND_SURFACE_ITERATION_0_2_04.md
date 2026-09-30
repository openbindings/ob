# Iteration 0.2-04: the first document command

Spec pin: sibling `spec` at `ccfe0b6`. Four fresh first-command reviewers
in iteration 01/03 all tried `ob init` when asked to create an OBI; the
current command is `ob new`. Their full guesses also used a positional
destination and `--version 0.2`, revealing a second ambiguity: the Core
`version` member is an opaque author label, while `openbindings` pins the
specification version. The hypothesis is that `init <obi>` with an explicit
`--interface-version` label will make the first step easier without
misstating the document model.

## Task cards written before the change

1. Create a minimal 0.2 OBI at `api.obi.json`, named `Acme API`.
2. Create one at `-` for stdout/piping, with no interface-version label.
3. Create an OBI whose opaque interface-version label is `2026-09-alpha`.
4. Try `--version 0.2` as though choosing the OpenBindings spec; the CLI
   must not silently write that value into the interface's label.
5. Try both a positional destination and `-o`; reject the collision.
6. Explore help from the root without prior knowledge of either spelling.

## Competing shapes

| Existing `new` | Candidate `init` |
| --- | --- |
| `ob new --name 'Acme API' -o api.obi.json` | `ob init api.obi.json --name 'Acme API'` |
| `ob new --version 2026-09-alpha -o api.obi.json` | `ob init api.obi.json --interface-version 2026-09-alpha` |
| `ob new` prints JSON | `ob init -` prints JSON |

Both emit the same 0.2 OBI shape. `OB_SURFACE_VARIANT=init` selects the
candidate; the ordinary tree remains `new` until a blind comparison.
The first `init` candidate required one destination path or `-` for stdout.
The `--version` spelling is reserved on this command for a clear usage
error that points to `--interface-version` and explains that the 0.2
specification version is already fixed. As elsewhere in the facade, a
correct ordinary invocation parses and exits 2 with `no operation ran`;
an illustrative sample performs no write.

## Pilot outcome and revised contract

Two fresh blind agents each saw the six tasks with no help, then tried a
separate candidate. All ten document-creation first paths were `ob init`;
the only initial hit on the `new` candidate was the root CLI-version flag.
The initial `init <path>` candidate got first-path hits but still missed
both agents' `--output` full invocations. Both recovered the creation task
after one or two help hops; none reported an operational false success.

| Measure | `new -o` | Initial `init <path>` |
| --- | ---: | ---: |
| First path, six tasks including tool version | 1/6 | 6/6 |
| First full invocation | 1/6 | 1/6 |
| Creation tasks recovered with help | 5/5 | 5/5 |

**Choose `init` as the default command.** The revised form is
`ob init [<obi|->]` or `ob init -o <obi|->`; omission prints JSON to
stdout. A positional destination and `-o` together are rejected. This
uses the existing global output flag and preserves the discoverable
positional form. `--interface-version` maps to the opaque OBI `version`
member; `--version` gives an explicit ambiguity error on `init` while
root `ob --version` reports the CLI version. The old `new` shape remains
explorable through `OB_SURFACE_VARIANT=new`, not as an alias in the default
tree. The revised destination shape passed parser, help-example, and
schema gates but has not yet had a fresh blind round, so its expected
first-invocation gain is still a hypothesis.
