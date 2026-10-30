// Generated from the ob surface draft's help pages by gen_ob_surface.py; do not edit by hand.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![cmd_source()]
}

pub(super) fn cmd_source() -> Command {
    group(
        "source",
        "Add sources and pull what they offer",
        PAGE_SOURCE,
        "ob source <command> [options]",
    )
    .subcommand(cmd_source_list())
    .subcommand(cmd_source_show())
    .subcommand(cmd_source_add())
    .subcommand(cmd_source_pull())
    .subcommand(cmd_source_rename())
    .subcommand(cmd_source_remove())
}
const PAGE_SOURCE: &str = r####"Add sources and pull what they offer

Usage: ob source <command> [options]

A source is the document's name for something its bindings realize
operations through, read under one kind (§5.2): an entry under a key in
sources, with an exact kind, which names how the source's data and its
bindings' data are read (§6), and optional data, whatever the kind
defines for it: an embedded description such as an OpenAPI document, the
URL of one read on every use, the command line of a server, or nothing at
all. Bindings name a source and carry data of their own, read under the
same kind (§5.3).

add puts a source in a document the way its kind describes: ob shows the
kind's own schema for the data, fills it from your answers or --data, and
has the kind check and complete the entry before writing it. pull reads a
source under its kind and brings what it offers into the document: new
operations, the schemas they need and bindings naming the source, applied
directly, with proposals for what it would change and a report of what it
left out. list, show, rename and remove work with any kind, since they
read nothing under it.

What a source's data means, and what the kind consults to read it, is the
kind's definition. ob never interprets it: it shows the kind's schemas,
records what the kind returns, and keeps the data exactly as stored.
ob kind list shows which kinds this ob supports, and for which
operations: check (ob source add), pull (ob source pull) and invoke
(ob invoke), ob's names for the Kind Descriptor's checkSource, the Source
Puller's pullSource and the Binding Invoker's invokeBinding. The
descriptor ob consults for a kind, for its schemas and its check, is the
one of the provider ob would pull the kind with, or, where nothing pulls
it, invoke it with (ob kind --help).

Commands:
  list      List sources, with their kinds and what this ob supports
  show      Show one source
  add       Add a source, as its kind describes it
  pull      Pull what a source offers into the document
  rename    Rename a source and update the bindings that name it
  remove    Remove a source

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob source <command> --help' for details."####;

pub(super) fn cmd_source_list() -> Command {
    leaf(
        "list",
        "List sources, with their kinds and what this ob supports",
        PAGE_SOURCE_LIST,
        "ob source list [options] <obi>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_SOURCE_LIST: &str = r####"List sources, with their kinds and what this ob supports

Usage: ob source list [options] <obi>

List each source in document order: its key, its kind, its description,
the number of bindings that name it, and which of check, pull and invoke
this ob supports for its kind, built in or through a delegate, as ob kind
list reports it. A source whose kind this ob supports for nothing is still
listed; a document may use any kind.

With -F json, the report's sources array lists the same facts in
document order: key, kind, description when stated, bindingCount, and
supports, the operations this ob supports for the kind, by their keys,
each with its provider (built-in, or a delegate's registration ID).

In a document that does not conform, ob warns once and lists a source
whose shape is wrong as unreadable, with its location (see
'ob help documents'). In JSON, such an entry is

  {"key": "<key>", "unreadable": true, "location": {...}}

and when the document's sources member is not an object, the report's
sources member, instead of an array, is

  {"unreadable": true, "location": {...}}

Arguments:
  <obi>    The document: a path, - for standard input, or a URL

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob source list tasks.obi.json
  ob source list tasks.obi.json -F json
  ob source list https://api.example.com

Exit status:
  0    listed
  1    no OBI is published at the origin given
  2    the arguments are wrong, or <obi> cannot be read or fetched (a URL
       that is not found included)
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_source_show() -> Command {
    leaf(
        "show",
        "Show one source",
        PAGE_SOURCE_SHOW,
        "ob source show [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(positional("key", "The source's key", true, false))
    .arg(format(&["text", "json"]))
}
const PAGE_SOURCE_SHOW: &str = r####"Show one source

Usage: ob source show [options] <obi> <key>

Show one source: its kind, its description, its data exactly as stored,
and the bindings that name it. ob does not read the data under the kind
here, so show works for any kind. -F json prints the stored source
exactly.

Arguments:
  <obi>    The document: a path, - for standard input, or a URL
  <key>    The source's key

Options:
  -F, --format <format>    Output format: text, or json for the source as
                           stored [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob source show tasks.obi.json httpApi
  ob source show tasks.obi.json httpApi -F json

Exit status:
  0    shown
  1    no OBI is published at the origin given
  2    the arguments are wrong, <obi> cannot be read or fetched (a URL
       that is not found included), or no source has the key <key>
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_source_add() -> Command {
    leaf(
        "add",
        "Add a source, as its kind describes it",
        PAGE_SOURCE_ADD,
        "ob source add [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional("key", "The source's key", true, false))
    .arg(text("kind", "The exact kind; asked at a terminal when omitted").value_name("kind"))
    .arg(
        text(
            "data",
            "The source's data, as the kind's schema describes it: JSON, @FILE, or -",
        )
        .value_name("value"),
    )
    .arg(
        text(
            "description",
            "The source's description, in place of the one the kind derives",
        )
        .value_name("text"),
    )
    .arg(boolean("replace", "Replace a source already under <key>"))
    .arg(boolean(
        "yes",
        "Run a check that executes something without asking",
    ))
    .arg(
        text(
            "context",
            "Context for the kind's check, a JSON object, in place of the stored context",
        )
        .value_name("@FILE|-"),
    )
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_SOURCE_ADD: &str = r####"Add a source, as its kind describes it

Usage: ob source add [options] <obi> <key>

Add a source under <key>, in the form its kind describes. ob asks the
Kind Descriptor for the kind's source-data schema (what ob kind describe
shows) and fills it: from --data, or at a terminal from what you enter
for the schema's titled branches and members. It then has the kind check the
entry (the Kind Descriptor's checkSource) and return the one the kind
stands behind, completed as the kind defines (a description fetched and
embedded, a server's name read, a default filled), and writes that. What
the data means is the kind's; ob records what the kind returns.

--kind names the kind; without it, at a terminal, ob lists the kinds it
supports for check and asks. --data gives the data as the schema
describes it: JSON, @FILE, or - for standard input; --data null is the
JSON value null. Without --data, at a terminal, ob prompts from the
schema; without a terminal, --data is required. A kind whose sources
carry no data has no source-data schema: the entry is the kind alone,
and --data is a usage error for it. --description sets the source's
description; without it, ob records the description the kind's check
derives, if any. A kind's check may need context, such as a token to
read a description behind sign-in: ob uses what ob context set stored
for the source, or --context for this attempt, asks at a terminal,
offers to store the answer for the source, and otherwise stops (exit 3)
and prints the ob context set command that supplies it. Before a check
that runs something, such as starting a server to read it, ob says what
the kind's check does and asks; --yes answers for a script.

A source already under <key> is replaced only when you confirm at a
terminal or give --replace; an identical one is no change. Replacing a
source is how a document takes a newer description: ob reads nothing
behind an embedded document on its own, so bringing a new version in is
your act, and ob source pull then brings in what changed.

The document gains one source and no operations or bindings; ob source
pull adds those. Add needs this ob to support the kind for check, built
in or through a delegate: the descriptor ob consults for the kind (ob
kind describe) must support it. ob kind list --supports check shows
which kinds qualify.

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run runs the check and prints the change
instead of writing it. With -F json, the report adds the array added
(the source's key), or replaced when a source was replaced.

Arguments:
  <obi>    The document to change: a path, or - for standard input
  <key>    The source's key

Options:
      --kind <kind>            The exact kind; asked at a terminal when
                               omitted
      --data <value>           The source's data, as the kind's schema
                               describes it: JSON, @FILE, or -
      --description <text>     The source's description, in place of the
                               one the kind derives
      --replace                Replace a source already under <key>
      --yes                    Run a check that executes something without
                               asking
      --context <@FILE|->      Context for the kind's check, a JSON
                               object, in place of the stored context
      --dry-run                Write nothing; print the change
  -F, --format <format>        Output format: text (with --dry-run, a
                               unified diff) or json (a report holding the
                               RFC 6902 patch that would apply, or that
                               was applied) [default: text]
  -h, --help                   Print help (see a summary with '-h')

Examples:
  # At a terminal: pick the kind, then answer its schema
  ob source add tasks.obi.json httpApi

  # In a script: the kind and its data, as the kind's schema describes it
  ob source add tasks.obi.json httpApi --kind example.openapi@1 \
      --data '{"fetch":"https://api.example.com/openapi.json"}'

  # A description you already have, embedded in the form the kind takes
  jq '{artifact: .}' openapi.json | ob source add tasks.obi.json httpApi \
      --kind example.openapi@1 --data -

  # Take a newer description, then pull what changed
  ob source add tasks.obi.json httpApi --kind example.openapi@1 \
      --data '{"fetch":"https://api.example.com/openapi.json"}' --replace
  ob source pull tasks.obi.json httpApi

  # A description behind sign-in, in CI: the context for this attempt
  ob source add tasks.obi.json httpApi --kind example.openapi@1 \
      --data '{"fetch":"https://api.example.com/openapi.json"}' \
      --context @ci-context.json

Exit status:
  0    the source was added or replaced, or an identical one was there
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -, no --data without a terminal, --data for a kind whose
       sources carry none, or data the kind's schema does not accept;
       <obi> is a URL or cannot be read; or the kind's check failed,
       saying what is wrong
  3    refused: <key> names a different source and --replace was not
       given, this ob does not support the kind for check, the check
       needs context ob cannot ask for, a check that executes was not
       confirmed, the result would break a rule, or the document declares
       a version outside the 0.2 line; nothing was written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_source_pull() -> Command {
    leaf("pull", "Pull what a source offers into the document", PAGE_SOURCE_PULL, "ob source pull [options] <obi> [<key>...]")
        .arg(positional("obi", "The document to change: a path, or - for standard input", true, false))
        .arg(positional("key", "A source to pull (repeatable); with none, every source", false, true))
        .arg(repeated("add", "Apply only this addition, with what it needs, named by its added binding's key (repeatable)").value_name("binding"))
        .arg(optional_value("apply", "Apply every proposal, or the named one: bindings/<key>, schemas/<key> or sources/<key> (repeatable)", "entry"))
        .arg(boolean("prune", "Remove unmatched bindings; needs a complete pull"))
        .arg(boolean("no-reuse", "Never bind to an existing operation; leave such items out, as withheld"))
        .arg(boolean("check", "Write nothing; exit 1 when anything is stale or unmatched"))
        .arg(text("context", "Context for this run, a JSON object, in place of the stored context").value_name("@FILE|-"))
        .arg(boolean("dry-run", "Write nothing; print the change"))
        .arg(format(&["text", "json"]))
}
const PAGE_SOURCE_PULL: &str = r####"Pull what a source offers into the document

Usage: ob source pull [options] <obi> [<key>...]

Read each source named, under its kind, and bring what it offers into the
document: for each item the source offers, an operation, the schemas it
needs, and a binding naming the source. With no <key>, every source this
ob can pull is pulled, one at a time, and any other is skipped and named.
Pull reads the source as the document holds it: an embedded description
from the document, a referenced one from where its kind says, and
nothing else; what a kind consults is its definition, which ob kind
describe shows.

What pull writes, and what it leaves to you:

  - Additions are applied: new operations, schemas and bindings, under
    keys new to the document, so nothing is overwritten. When the source
    offers an operation under a key the document already has, pull
    compares the offered input and output schemas with the existing
    ones: equal, it binds the source to the existing operation and says
    so (reused); different, it leaves the item out as a conflict and says
    why, and you bind it by hand with ob binding add, adapting in binding
    data as the kind defines, or add it under another key. --add applies
    only the additions it names, each by the key of its added binding as
    the report shows it, with the operation and schemas that addition
    needs; the rest are listed as what a pull would add. --add takes one
    <key>.
  - Changes are proposals: a binding the source still offers but would now
    write differently (stale), a schema it offers that the document holds
    differently, or, for a kind that may refresh its own source data, the
    source entry itself. A proposal carries only what the kind owns (a
    binding's data, an operation's input or output schema, a schema, the
    source's data), never your description, preference or idempotent
    claim, and the kind's judgment of the change: breaking, dangerous or
    non-breaking. --apply applies every proposal; --apply=<entry> applies
    the named one, and may repeat. An entry is named as the report labels
    it, by map and key: bindings/completeTask.http, schemas/Task,
    sources/httpApi. A proposal that carries an operation's input or
    output changes an operation that other bindings may realize and
    dependencies may consume; the report names them beside it, and
    --apply applies it with a warning naming them, except one the kind
    judges breaking, which --apply leaves and only --apply=<entry>
    applies. When two sources pulled together propose different values
    for one entry, neither is applied and the report names both; pull
    one source with --apply=<entry> to choose.
  - Removals are yours: a binding that nothing the puller read and would
    write corresponds to any more (unmatched) is listed and left; --prune
    removes unmatched bindings. ob never removes an operation; the hint
    names ob operation remove when one has no bindings left.

--prune removes only from a complete pull: the puller read the whole
source (exhaustive), and left nothing out for want of support in this
ob's implementation of the kind. Otherwise an unmatched binding may
correspond to something unread, so the pull is refused (exit 3), nothing
is written, and the message names the limitation; a run that wants the
additions anyway drops --prune.

The report names each item the source offers that pull did not represent
(omitted), with the reason: excluded by a rule of the kind, invalid in the
source, not supported by this ob's implementation of the kind, or a
conflict with an existing operation. It says whether the source was read
in full (exhaustive) and, if not, why. Pulling again changes nothing.

--check writes nothing: it runs the pull and exits 1 when anything is
stale or unmatched. An item the source offers that the document does not
bind is listed as what a pull would add and does not fail the check, so
a document that binds part of a source passes. In a pull that is not
complete, an unmatched entry gives no verdict (exit 4); a stale entry
still fails. For a source embedded in the document, --check detects a
document whose bindings no longer match what it holds; a job that wants
to catch the API itself drifting re-adds the source from its description
first. --dry-run shows what a pull would apply without writing it.
--no-reuse leaves out every item that would bind to an existing
operation; the report lists them as withheld, apart from the puller's
own conflicts.

A kind may need context to read a source, such as a token: ob uses what
ob context set stored for the source, or --context for this run, asks at
a terminal, and otherwise stops (exit 3) and prints the ob context set
command that supplies it. --context is a JSON object from @FILE or
standard input, in place of the stored context; the two never merge.

With no <key>, a source this ob cannot pull is skipped and named: its
kind is not supported for pull, its kind could not read it, or it needs
context ob cannot ask for. The rest are pulled, and the run exits 4
unless something failed. A source named by <key> is pulled or refused.

Pull relies on what the document means, so it refuses a document that is
non-conformant or whose conformance is undetermined (exit 3), and never
writes a result that violates a rule. Like every edit, it changes <obi>
in place, or filters - to standard output (see 'ob help documents'). With
-F json, the report holds what ob wrote: the patch applied and, per
source, the Source Puller's report exactly as returned (added, existing,
omitted, exhaustive, limitation), with ob's own members beside it:
appliedProposals, pruned, withheld, affected, and skipped with its reason
for a source that was not pulled. With --check, nothing was applied and
there is no patch.

Arguments:
  <obi>    The document to change: a path, or - for standard input
  <key>    A source to pull (repeatable); with none, every source

Options:
      --add <binding>          Apply only this addition, with what it
                               needs, named by its added binding's key
                               (repeatable)
      --apply[=<entry>]        Apply every proposal, or the named one:
                               bindings/<key>, schemas/<key> or
                               sources/<key> (repeatable)
      --prune                  Remove unmatched bindings; needs a complete
                               pull
      --no-reuse               Never bind to an existing operation; leave
                               such items out, as withheld
      --check                  Write nothing; exit 1 when anything is stale
                               or unmatched
      --context <@FILE|->      Context for this run, a JSON object, in
                               place of the stored context
      --dry-run                Write nothing; print the change
  -F, --format <format>        Output format: text (with --dry-run, a
                               unified diff) or json (a report holding the
                               RFC 6902 patch that would apply, or that
                               was applied, and each source's report)
                               [default: text]
  -h, --help                   Print help (see a summary with '-h')

Examples:
  ob source pull tasks.obi.json
  ob source pull tasks.obi.json httpApi
  ob source pull tasks.obi.json httpApi --apply
  ob source pull tasks.obi.json httpApi --apply=bindings/completeTask.http \
      --apply=schemas/Task
  ob source pull tasks.obi.json httpApi --add deleteTask.httpApi
  ob source pull tasks.obi.json httpApi --prune
  ob source pull tasks.obi.json --check
  ob source pull tasks.obi.json httpApi --dry-run
  ob source pull tasks.obi.json httpApi --context @ci-context.json
  ob source pull tasks.obi.json -F json | jq '.sources[].report.existing'

Exit status:
  0    pulled: additions applied, and the proposals and removals asked
       for, or no change; with --check, nothing is stale or unmatched
  1    with --check: something is stale, or unmatched in a complete pull
  2    the arguments are wrong, such as -F json without --dry-run or
       --check when <obi> is -, --check with --add, --apply, --prune or
       --dry-run, --apply beside --apply=<entry>, an --apply=<entry> that
       names no proposal, --add without exactly one <key> or naming no
       addition; a source key identifies nothing; <obi> is a URL or
       cannot be read; or a source named by <key> could not be read by
       its kind, saying why
  3    refused: the document is non-conformant or its conformance is
       undetermined, this ob does not support a named source's kind for
       pull, a named source needs context ob cannot ask for, --prune in a
       pull that is not complete, the result would violate a rule, or
       the document declares a version outside the 0.2 line; nothing was
       written
  4    with no <key>, a source was skipped and nothing failed; with
       --check, an unmatched entry in a pull that is not complete, and
       nothing is stale
  See 'ob help exit-codes'."####;

pub(super) fn cmd_source_rename() -> Command {
    leaf(
        "rename",
        "Rename a source and update the bindings that name it",
        PAGE_SOURCE_RENAME,
        "ob source rename [options] <obi> <key> <new-key>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional("key", "The source's key", true, false))
    .arg(positional("new-key", "Its new key", true, false))
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_SOURCE_RENAME: &str = r####"Rename a source and update the bindings that name it

Usage: ob source rename [options] <obi> <key> <new-key>

Change the key of the source <key> to <new-key>, and update every binding
that names it to name the new key. Bindings are the only parts of a
document that name a source. Context stored for the source (ob context
set) moves with it, so the renamed source keeps what was stored; a dry
run moves nothing.

It refuses a <new-key> that another source already has, or one that does
not match the name grammar (OBI-04).

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run prints the change instead of writing
it. With -F json, the report adds two arrays: renamed (the old key and
the new) and updated (the locations of the bindings it updated).

Arguments:
  <obi>        The document to change: a path, or - for standard input
  <key>        The source's key
  <new-key>    Its new key

Options:
      --dry-run            Write nothing; print the change
  -F, --format <format>    Output format: text (with --dry-run, a unified
                           diff) or json (a report holding the RFC 6902
                           patch that would apply, or that was applied)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob source rename tasks.obi.json httpApi restApi
  ob source rename tasks.obi.json mcpServer mcp --dry-run

Exit status:
  0    renamed, or no change (<new-key> is already its key)
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -; <obi> is a URL or cannot be read; or no source has
       the key <key>
  3    refused: <new-key> is taken or breaks the name grammar, the result
       would break another rule, or the document declares a version
       outside the 0.2 line; nothing was written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_source_remove() -> Command {
    leaf(
        "remove",
        "Remove a source",
        PAGE_SOURCE_REMOVE,
        "ob source remove [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional("key", "The source's key", true, false))
    .arg(boolean(
        "cascade",
        "Also remove the bindings that name the source",
    ))
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_SOURCE_REMOVE: &str = r####"Remove a source

Usage: ob source remove [options] <obi> <key>

Remove the source <key>. It refuses while bindings name the source, and
lists them; --cascade removes those bindings too. Operations stay: one
whose last binding is removed is no longer realized in this document.
Context stored for the source (ob context set) is removed with it, and
the summary says so; a dry run removes nothing.

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run prints the change instead of writing
it. With -F json, the report adds the array removed (the source's key)
and, with --cascade, the array cascaded (the locations of the bindings
removed with it); contextRemoved is true when stored context went with
the source.

Arguments:
  <obi>    The document to change: a path, or - for standard input
  <key>    The source's key

Options:
      --cascade            Also remove the bindings that name the source
      --dry-run            Write nothing; print the change
  -F, --format <format>    Output format: text (with --dry-run, a unified
                           diff) or json (a report holding the RFC 6902
                           patch that would apply, or that was applied)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  # Refused: the binding createTask.mcp names mcpServer
  ob source remove tasks.obi.json mcpServer
  ob source remove tasks.obi.json mcpServer --cascade --dry-run

Exit status:
  0    removed
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -; <obi> is a URL or cannot be read; or no source has
       the key <key>
  3    refused: bindings name the source and --cascade was not given, the
       result would break another rule, or the document declares a
       version outside the 0.2 line; nothing was written
  See 'ob help exit-codes'."####;
