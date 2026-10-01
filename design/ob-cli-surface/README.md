# ob CLI surface review archive

This directory preserves rounds 1 through 5 of the CLI surface-design review.
It belongs to the `openbindings/ob` lab branch, `codex/cli-surface-lab`.
It records a playable proposal, not production implementation or a release.

The 59 original text records were copied byte for byte from the local
`/Users/matt/Code/ob-pj/design/ob-cli-surface/` archive on 2026-10-01.
Historical absolute paths in the frozen briefs, guides, and reports are
preserved as evidence. Use this directory for the versioned records going
forward. Keep frozen inputs and raw reviewer reports unchanged.

| Round | Recorded lab commit | Reports |
| --- | --- | --- |
| 1 | `1948e867328a760f3038719d2a6b1fee8a9bd5be` | [Astra](review-1/astra.md), [Opus](review-1/opus.md) |
| 2 | `f7a9d168f83d28666794b438807f9672bd6d2155` | [Astra](review-2/astra.md), [Opus](review-2/opus.md) |
| 3 | `e40c640e4e57ad0afa659aaf7f82a9c69188b22c` | [Astra](review-3/astra.md), [Opus](review-3/opus.md) |
| 4 | `29d7de26d5cd469f56ac40130f31d32968f73750` | [Astra](review-4/astra.md), [Opus](review-4/opus.md) |
| 5 | `4984a63d18fea62c079d65f1afab025e71923211` | [Codex A](review-5/reviewer-a.md), [Codex B](review-5/reviewer-b.md), [Codex C](review-5/reviewer-c.md) |

Each round retains its original pins, brief, guide, specification and supplied
interface texts, reports, and any recorded logs. The latest interpretation is
the [round-5 adjudication](review-5/adjudication.md); reviewer provenance is in
[RUNS.txt](review-5/RUNS.txt). Later lab changes do not change those verdicts.

## Frozen executables

The five original macOS arm64 executables total about 240 MiB. They remain
local build artifacts and are excluded from Git history. Their exact hashes
are recorded in [BINARY_SHA256SUMS.txt](BINARY_SHA256SUMS.txt), and their embedded
Go build metadata is in [BINARY_BUILD_INFO.txt](BINARY_BUILD_INFO.txt). Embedded
VCS metadata is retained as built, including any dirty-worktree marker; it is
not a replacement for the original round's recorded source selection.

The unchanged originals remain at
`/Users/matt/Code/ob-pj/design/ob-cli-surface/review-<N>/ob`. A local archive of
all original freezes, including executables, is also saved at
`/Users/matt/Code/ob-pj/openbindings/ob-cli-surface-lab/bin/cli-surface-review-freezes-2026-10-01.tar.gz`.
Neither that archive nor the executable copies is uploaded by the branch push.

Checking out a recorded lab commit lets another machine build and explore
that source's preview. A new build is not claimed to be byte-identical to the
original executable. The original hashes identify the exact reviewed binaries.

## Verification

From this directory, verify every versioned archive record with:

```sh
shasum -a 256 -c ARCHIVE_SHA256SUMS.txt
```

Round 5's original `SHA256SUMS.txt` also covers its omitted executable. To
verify that original full freeze, run it where the original binary is present.
Its `OUTPUT_SHA256SUMS.txt` can be checked directly in this versioned copy.

Current build and lab test commands are in [HANDOFF.md](../../HANDOFF.md).
Current proposals and Matt's rulings remain in
[COMMAND_SURFACE_LAB.md](../../COMMAND_SURFACE_LAB.md).
