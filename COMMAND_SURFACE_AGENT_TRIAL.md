# Blinded internal first-command trial

Three Codex agents started without the conversation history and were told to
record each first guess before using the hybrid CLI. They could read CLI help
and run inert commands, but not source, tests, review documents, or Git history.
The task sets were phrased as user goals without supplying command paths. A
placeholder error marks a parsed route; it does not prove runtime behavior.
No files were edited. All 15 tasks reached a placeholder after help. The
agents rated discoverability 8/10, 8/10, and 7/10. They are model-generated
users, so these are internal diagnostic observations rather than user testing.

| Task | First guess before help | Help and wrong turn | Resolved route |
| --- | --- | --- | --- |
| Create a named OBI file | `ob obi create --name 'Shop API' --output shop.obi.json` | `obi` unknown; root and `new` help | `ob new --name 'Shop API' -o shop.obi.json` |
| Summarize and display full OBI | `ob obi show shop.obi.json`, then `ob obi show --full shop.obi.json` | `obi` unknown; root and `show` help | `ob show shop.obi.json`, then `ob show shop.obi.json --full -F json` |
| Add local OpenAPI source and derive operations | `ob obi add-openapi shop.obi.json ./shop.yaml --derive-operations` | `obi` unknown; root, `source`, and `source add` help | `ob source add shop.obi.json ./shop.yaml --pull` |
| Check source drift | `ob obi drift shop.obi.json` | `obi` unknown; root and `status` help | `ob status shop.obi.json` |
| Save a distribution copy without ob metadata | `ob obi export shop.obi.json --output shop.dist.json --strip-metadata` | `obi` unknown; root and `strip-ob-metadata` help | `ob strip-ob-metadata shop.obi.json -o shop.dist.json` |
| Define an unbound operation | `ob operation create --name NAME` | Guessed `create`; `operation` and `operation add` help | `ob operation add <obi> <key>` |
| Add two bindings through one source | `ob binding create --operation NAME --source SOURCE` twice | Guessed `create`; `binding`, `binding add`, and `operation bind` help | `ob binding add <obi> <key> --operation OP --source SRC --selector SELECTOR` twice |
| Accept either of two binding specification IDs | `ob consumption-point create --accepts SPEC_A --accepts SPEC_B` | Guessed another noun; `dependency` and `dependency add` help | `ob dependency add <obi> <key> --operation OP --binding-spec SPEC_A --binding-spec SPEC_B` |
| Invoke a binding with a complete machine request | `ob binding invoke BINDING --request REQUEST_JSON` | Guessed a positional binding; `binding invoke` and `invoke` help | `ob binding invoke --request '<BindingInvocationInput JSON>'` |
| Patch an extension field | `ob extension patch FIELD --patch PATCH_JSON` | Guessed another hierarchy; root and `patch` help | `ob patch <obi> <patch.json>` |
| Check replacement compatibility | `ob diff old.obi new.obi` | `diff` is structural; `diff` and `compat` help | `ob compat old.obi new.obi` |
| Set an endpoint bearer token | `ob auth set-token <endpoint> <token>` | `auth` unknown; `context` and `context set` help | `ob context set <endpoint> --bearer-token -` |
| Check support for a binding-spec ID | `ob spec supports <id>` | `spec` unknown; `about`, `source`, and `source binding-specs` help | `ob source binding-specs check <id>` |
| Start the local API and workbench | `ob serve` | `serve` is a group; `serve` and `serve api` help | `ob serve api` |
| Create an OBI using a positional filename | `ob new sample.obi` | Error says `unknown command "sample.obi" for "ob new"`; `new` help | `ob new -o sample.obi.json` |

The novice agent guessed a nonexistent `obi` group for all five of its tasks.
The power user found the correct subject several times but missed a verb or
the machine-lane syntax. The operator found `diff` and `serve` plausible but
needed help to choose `compat` and `serve api`. Help resolved every task; bare
unknown-command errors did not point toward the repair. The clearest fix is a
specific `new <filename>` diagnostic. A wrong noun should guide the user to
root help or likely task areas without creating a collection of routine
aliases.

## Claude Opus 5.5 follow-up

The installed Claude Code CLI was authenticated outside the workspace sandbox.
Its version 2.1.261 could run Opus 5 but not the requested Opus 5.5, so the
CLI was updated to 2.1.282. The follow-up used the reported
`claude-opus-5-5` model. Claude recorded its guesses with tools disabled,
then resumed the same session with access limited to hybrid `./bin/ob` calls.
Two calls that added `; echo` were denied by that restriction and retried as
plain CLI invocations. No files or review notes were read, and no files were
edited. All five final routes parsed. Claude rated discovery **8/10**.

| Task | First guess before help | Wrong turn and help | Resolved route |
| --- | --- | --- | --- |
| Create a named OBI file | `ob create --name "Shop API" -o shop.obi.json` | `create` unknown; root and `new` help | `ob new --name "Shop API" -o shop.obi.json` |
| Summarize and display full OBI | `ob show shop.obi.json`, then `ob show shop.obi.json --full` | No wrong turn; `show` help | Same as first guess |
| Add local OpenAPI source and derive operations | `ob source add shop.obi.json shop.yaml --derive` | `--derive` unknown; `source add` help | `ob source add shop.obi.json shop.yaml --pull` |
| Check source drift | `ob diff shop.obi.json` | Parsed despite lacking a second OBI or `--from-sources`; `diff` and `status` help | `ob status shop.obi.json` |
| Save a distribution copy without ob metadata | `ob export shop.obi.json -o shop.dist.obi.json` | `export` unknown; root and `strip-ob-metadata` help | `ob strip-ob-metadata shop.obi.json -o shop.dist.obi.json` |

Claude found a concrete facade parsing gap: `ob diff <obi>` reaches the inert
placeholder even though `diff --help` lists only two valid modes, with a
second OBI or `--from-sources`. The production handler checks this in `RunE`,
which the facade replaces. Its validation needs to move into the facade's
argument checks. Claude also flagged the overlap between `diff --from-sources`
and `status`; their help should cross-reference the distinct report types.
The `create`/`export` misses support better error guidance, but are not by
themselves a reason to add more canonical aliases.

## Focused discoverability pass: two fresh blind rounds

Each round used two new Codex agents and a fresh Claude Opus 5.5 session.
Each reviewer received five natural-language tasks, recorded a first guess
before help, and could then use only the hybrid preview CLI. They did not read
source, review notes, or one another's findings. A resolved route means that
the inert placeholder accepted the command shape, not that an OBI operation
worked. No outside human review was conducted.

### Round one: after the first repair set

All **15/15** routes were eventually found. Reviewer grades were **7/10,
8/10, and 7/10**. The first guesses included:

| Reviewer | First guesses across five tasks | Main observed wrong turn |
| --- | --- | --- |
| Codex author | `interface create --title`, `source add ./file --derive-operations`, `consumption add --binding`, `binding-spec show`, `export --distribution` | Wrong nouns with `--help` could show root help with exit 0. |
| Codex operator | `check`, `drift`, `auth set --bearer-token`, `compare`, `create` | An unknown flag could mask the unknown `auth` path. |
| Claude Opus 5.5 | `diff <obi>`, `drift`, `auth set --bearer`, `bindings supports`, `new Products -o` | `diff` needed a mode error; binding-spec guidance and `new`'s positional-name error were weak. |

The next repair added a preview-only command-path preflight before Cobra flag
and help parsing, as well as clearer `diff`, `new`, credential, and binding
specification recovery.

### Round two: after command-path preflight

All **15/15** routes were again found. Reviewer grades were **8/10, 8/10,
and 7/10**. The Codex reviewers reported no wrong path that looked like
success; Claude still found several correct-area/wrong-flag dead ends.

| Reviewer | First guesses across five tasks | Main observed wrong turn |
| --- | --- | --- |
| Codex paths | `obi init`, `bindings supports`, `auth token set/match`, `policy check`, `obi export` | `policy` and credential lookup guidance still needed specificity. |
| Codex workflows | `source attach`, `diff --sources`, `drift check`, `consume define`, `invoke --request` | `diff --sources` and `invoke --request` received generic flag errors. |
| Claude Opus 5.5 | `create Warehouse -o`, `binding supports`, `diff <obi>`, `auth set --bearer`, `binding add --accept` | The `--accept` route and credential URL scope rule were hard to find. |

The final small repair added targeted flag errors for `--sources`,
`--request`, `--accept`, `--bearer`, and `--title`, and moved credential URL
matching rules to the start of `context set` help. These exact cases passed
manual checks and focused regressions. There has been **no fresh blind round
after that last repair**, so its repeatability is unproven. The continuing
7/10 Claude rating and repeated wrong first nouns do not justify an S-tier
discoverability claim.

### Final blind validation: after the wrong-flag repairs

Two further Codex reviewers and a fresh Claude Opus 5.5 session each received
five tasks. First guesses were committed before using the CLI. All **15/15**
tasks reached an inert parsed route, but **0/15 first guesses parsed**. None
of the wrong paths looked like success. This is stronger evidence of an
information-architecture weakness than the earlier recovery ratings.

| Reviewer | Five first guesses | First-command / recovery / overall |
| --- | --- | --- |
| Codex task paths | `obi create --name`, `source add --file --derive-operations`, `binding-spec check`, `context bearer set` plus `context resolve`, `consumption-point create --accepts` | 1 / 8 / 5 |
| Codex workflows | `diff --tracked`, `source status`, `export --strip-metadata`, `invoke --request`, `replace check` | 0 / 9 / 5 |
| Claude Opus 5.5 | `init --name <name> <path>`, `bindings check`, `diff <obi>`, `consume add --accept`, `invoke --input <request-file>` | 3 / 7 / 5.5 |

`obi create`, `binding-spec check`, `bindings check`, `consume add`, and
`export --strip-metadata` recovered from task-aware unknown-command messages.
Other paths required one or more help hops. In particular, `source status`
opened source help without pointing to root `status`; `invoke --input` with no
OBI returned only an argument-count error, and top-level invoke help did not
mention the binding request lane. One reviewer could not complete the scope
inspection task at the interface level: `context get` shows the selected
context but does not report which stored URL key matched. The grades reflect
parser discovery, not runtime correctness. Claude's first tool attempt
included a shell separator and was denied by the restricted allow rule; it
retried plain `ob` commands without further denial.

The subsequent targeted repair added corrections for `source status`,
`replace check`, `diff --tracked`, `source add --file`, `init --name`, and
`invoke --input` without an OBI, plus a machine-lane cross-link in top-level
invoke help. Focused tests cover these cases. These final corrections were
not re-reviewed blindly. They improve recovery, while the first-command
failure pattern still calls for a competing command hierarchy.
