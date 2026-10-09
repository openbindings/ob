# Preserved Go CLI

Rust is the maintained CLI source on `release/0.2`. The maintainer approved
retiring the Go implementation from the active tree on 2026-10-09, while the
Rust version is still a command-surface preview. This is an implementation
direction decision, not a claim of operational parity.

The Go release-branch tip was saved remotely before the active tree was cleaned:

- Branch: [`legacy/go-cli`](https://github.com/openbindings/ob/tree/legacy/go-cli)
- Exact commit: [`103087368e1363bae66841487a038616ab7a8f85`](https://github.com/openbindings/ob/tree/103087368e1363bae66841487a038616ab7a8f85)
- Original [README](https://github.com/openbindings/ob/blob/103087368e1363bae66841487a038616ab7a8f85/README.md),
  [Go 0.2 draft changelog](https://github.com/openbindings/ob/blob/103087368e1363bae66841487a038616ab7a8f85/CHANGELOG.md),
  [release instructions](https://github.com/openbindings/ob/blob/103087368e1363bae66841487a038616ab7a8f85/RELEASING.md),
  and [CI](https://github.com/openbindings/ob/blob/103087368e1363bae66841487a038616ab7a8f85/.github/workflows/ci.yml)

That snapshot includes the complete Go sources, modules, tests, operational
schemas/descriptors, generated browser bundle, qualification material, and
release machinery. Its dependency pins and original limitations remain as they
were. Preservation does not establish that historical dependencies are still
fetchable or that the Go implementation meets the Rust roadmap.

To inspect or build the historical version, create a separate checkout:

```sh
git fetch origin legacy/go-cli
git worktree add --detach ../ob-go-reference 103087368e1363bae66841487a038616ab7a8f85
```

Consult its own source and prerequisites there. Do not restore its module files,
workflows, generated descriptors, or public capability claims into the maintained
Rust tree merely to reuse an example. Existing released tags and the historical
`main` branch are preserved without rewriting their histories.

The separate Go surface lab also remains at
[`codex/cli-surface-lab`](https://github.com/openbindings/ob/tree/codex/cli-surface-lab).
The Rust surface uses its default tree at exact commit
`11883f42ca92aa0f7743e499b3db644c3b0cb04a`, captured in
[the frozen metadata](../tests/fixtures/go-surface.json) and
[provenance](../tests/fixtures/PROVENANCE.json). It is distinct from the older
Go runtime's command tree.
