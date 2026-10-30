// Generated from the ob surface draft's help pages by gen_ob_surface.py; do not edit by hand.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![
        cmd_operation(),
        cmd_binding(),
        cmd_dependency(),
        cmd_schema(),
    ]
}

pub(super) fn cmd_operation() -> Command {
    group(
        "operation",
        "Read, rename, and remove operations",
        PAGE_OPERATION,
        "ob operation <command> [options]",
    )
    .subcommand(cmd_operation_list())
    .subcommand(cmd_operation_show())
    .subcommand(cmd_operation_rename())
    .subcommand(cmd_operation_remove())
}
const PAGE_OPERATION: &str = r####"Read, rename, and remove operations

Usage: ob operation <command> [options]

An operation is a named, protocol-independent capability under a key in
operations: what a service can do, with optional input and output
schemas that govern the values a caller sends it and receives from it
(§3, §5.1). Its key and its aliases are its identifiers, and a name
identifies the operation when it equals one of them (§5.1). Bindings
declare realizations of an operation (§5.3), and dependencies name
points where the software described consumes one (§5.4).

Commands here read operations, and rename or remove one while accounting
for every reference to it. You add an operation or change its fields
(description, aliases, tags, deprecation, input, output, examples) in your
editor, then run ob validate to check the result; scripts use ob patch,
which refuses a change that breaks a rule the document met.

Commands:
  list      List operations, and how each is realized and consumed
  show      Show one operation, named by its key or an alias
  rename    Change an operation's key and every reference to it
  remove    Remove an operation

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob operation <command> --help' for details."####;

pub(super) fn cmd_operation_list() -> Command {
    leaf(
        "list",
        "List operations, and how each is realized and consumed",
        PAGE_OPERATION_LIST,
        "ob operation list [options] <obi>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_OPERATION_LIST: &str = r####"List operations, and how each is realized and consumed

Usage: ob operation list [options] <obi>

List each operation in document order: its key, its aliases, whether it
states an input schema and an output schema, the bindings that declare
realizations of it, the dependencies that consume it, and whether it is
deprecated.

An operation with bindings is realized, and one a dependency names is
consumed. An operation can be both, or neither.

With -F json, the report's operations array lists the same facts in
document order. A member copied from the document keeps its presence:
an operation with no aliases has no aliases member, and deprecated
appears only when the operation states it. bindings and dependencies
are always present, possibly empty.

In a document that does not conform, ob warns once and lists an operation
whose shape is wrong as unreadable, with its location (see
'ob help documents'). In JSON, such an entry is

  {"key": "<key>", "unreadable": true, "location": {...}}

and when the document's operations member is not an object, the
report's operations member, instead of an array, is

  {"unreadable": true, "location": {...}}

Arguments:
  <obi>         The document: a path, - for standard input, or a URL

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob operation list tasks.obi.json
  ob operation list acme-tasks.obi.json
  ob operation list https://api.example.com -F json

Exit status:
  0    listed
  1    no OBI is published at the origin given
  2    the arguments are wrong, or <obi> cannot be read or fetched (a URL
       that is not found included)
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_operation_show() -> Command {
    leaf(
        "show",
        "Show one operation, named by its key or an alias",
        PAGE_OPERATION_SHOW,
        "ob operation show [options] <obi> <name>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(positional(
        "name",
        "The operation's key or any of its aliases",
        true,
        false,
    ))
    .arg(text("example", "Show only this example of the operation").value_name("key"))
    .arg(format(&["text", "json"]))
}
const PAGE_OPERATION_SHOW: &str = r####"Show one operation, named by its key or an alias

Usage: ob operation show [options] <obi> <name>

Show the operation that <name> identifies, by its key or any of its
aliases (§5.1): its description, identifiers, input and output schemas,
examples, bindings and dependencies. --example shows one of its examples
alone.

The text shows:

  - its key, description and tags, and whether it is deprecated;
  - its aliases, the other names that identify it, such as the published
    names it carries. Carrying a shared contract's published name claims
    correspondence with that contract's operation (§5.1); ob compat
    checks such claims;
  - its input schema and output schema, with references shown as
    written, or that none is stated;
  - its examples, each with the input and output values it gives, which
    ob validate --examples checks;
  - its bindings, each with its source and kind;
  - the dependencies that consume it, each with its kinds constraint.

--example <key> shows only the example under <key> in the operation's
examples, with the input and output values it gives. A key the
operation has no example under is a usage error (exit 2), with a
suggestion when one is close.

-F json prints the operation as stored, every value spelled exactly, and
nothing else; with --example, it prints that example as stored. The
operation's bindings and dependencies are stored elsewhere in the
document, so they are not included; ob binding list --operation lists
its bindings.

Arguments:
  <obi>         The document: a path, - for standard input, or a URL
  <name>        The operation's key or any of its aliases

Options:
      --example <key>      Show only this example of the operation
  -F, --format <format>    Output format: text, or json for the operation
                           as stored (with --example, the example)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob operation show tasks.obi.json createTask
  ob operation show tasks.obi.json acme.tasks.listTasks
  ob operation show tasks.obi.json notifications.send -F json
  ob operation show tasks.obi.json createTask --example basic -F json

Exit status:
  0    shown
  1    no OBI is published at the origin given
  2    the arguments are wrong, <obi> cannot be read or fetched (a URL
       that is not found included), <name> identifies no operation, or
       the operation has no example under the --example key
  3    refused: <name> identifies two operations (OBI-05), the document
       declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_operation_rename() -> Command {
    leaf(
        "rename",
        "Change an operation's key and every reference to it",
        PAGE_OPERATION_RENAME,
        "ob operation rename [options] <obi> <key> <new-key>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional("key", "The operation's key", true, false))
    .arg(positional("new-key", "Its new key", true, false))
    .arg(boolean(
        "keep-alias",
        "Keep the old key as an alias of the operation",
    ))
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_OPERATION_RENAME: &str = r####"Change an operation's key and every reference to it

Usage: ob operation rename [options] <obi> <key> <new-key>

Change the key of the operation <key> to <new-key>, and update every
reference to it in the document: the operation field of each binding
and dependency that names it, and each same-document reference in the
document resource that points into the operation, however it is
spelled, such as {"$ref": "#/operations/createTask/input"} (§7.3).
--keep-alias also keeps the old key as an alias, so it still identifies
the operation.

Its other fields stay as they are, and so do its aliases unless
--keep-alias adds one: a plain rename only renames. ob changes only the
references the spec defines: data, descriptions and extension fields
are left alone, as is anything outside the document that uses the old
name, such as a script that runs ob invoke with it.

The old name then stops identifying the operation. If it was a shared
contract's published name, the operation no longer claims correspondence
through it. --keep-alias prevents that: it adds the old key to the
operation's aliases, creating the array when there is none, in the same
atomic write as the rename. This never breaks a rule the document met:
the old key was already unique and well-formed.

To keep the old name afterwards, or by hand, add it as an alias in your
editor or with ob patch. One RFC 6902 patch cannot both append to an
aliases array and create one, so the patch takes one of two shapes
(<old> is the old key, <new> the new one):

  when the operation has aliases:
    [{"op":"add","path":"/operations/<new>/aliases/-","value":"<old>"}]
  when it has none:
    [{"op":"add","path":"/operations/<new>/aliases","value":["<old>"]}]

<key> must be the key: given an alias, rename refuses and names the
operation whose alias it is. It refuses a <new-key> that already
identifies an operation, one of this operation's aliases included
(OBI-05), or that does not match the name grammar (OBI-04).

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run prints the change instead of writing
it. The summary on standard error names the alias --keep-alias added.
With -F json, the report adds the arrays renamed (the old key and the
new), updated (the locations of the references it updated) and, with
--keep-alias, aliasesAdded (the location of the alias added).

Arguments:
  <obi>         The document to change: a path, or - for standard input
  <key>         The operation's key
  <new-key>     Its new key

Options:
      --keep-alias         Keep the old key as an alias of the operation
      --dry-run            Write nothing; print the change
  -F, --format <format>    Output format: text (with --dry-run, a unified
                           diff) or json (a report holding the RFC 6902
                           patch that would apply, or that was applied)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob operation rename tasks.obi.json createTask addTask --keep-alias
  ob operation rename tasks.obi.json createTask addTask
  ob operation rename tasks.obi.json completeTask finishTask --dry-run
  ob operation rename tasks.obi.json listTasks findTasks --dry-run -F json
  ob operation rename - listTasks findTasks < tasks.obi.json > next.obi.json

Exit status:
  0    renamed, or no change (<new-key> is already its key)
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -; <obi> is a URL or cannot be read; or <key>
       identifies no operation
  3    refused: <key> is an alias or identifies two operations,
       <new-key> is taken or breaks the name grammar, the result would
       break another rule, or the document declares a version outside the
       0.2 line; nothing was written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_operation_remove() -> Command {
    leaf(
        "remove",
        "Remove an operation",
        PAGE_OPERATION_REMOVE,
        "ob operation remove [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional("key", "The operation's key", true, false))
    .arg(boolean(
        "cascade",
        "Also remove the bindings and dependencies that name the operation",
    ))
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_OPERATION_REMOVE: &str = r####"Remove an operation

Usage: ob operation remove [options] <obi> <key>

Remove the operation <key>, with its aliases and examples. Its names
stop identifying anything, and any correspondence it claimed through them
goes with it.

It refuses while bindings or dependencies name the operation, and lists
them; --cascade removes those bindings and dependencies too. Sources and
named schemas stay, even when nothing uses them any more.

It also refuses while a reference from outside the operation reaches
into it, and lists each one with its location: a same-document reference
that points into it, such as {"$ref": "#/operations/createTask/input"} in
another operation's input, or a reference to an $id declared at or inside
its input or output. --cascade does not remove these: change them first,
in your editor or with ob patch.

<key> must be the key: given an alias, remove refuses and names the
operation whose alias it is. To drop only an alias, edit the operation's
aliases.

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run prints the change instead of writing
it. With -F json, the report adds the array removed (the operation's
key) and, with --cascade, the array cascaded (the locations of the
bindings and dependencies removed with it).

Arguments:
  <obi>         The document to change: a path, or - for standard input
  <key>         The operation's key

Options:
      --cascade            Also remove the bindings and dependencies that
                           name the operation
      --dry-run            Write nothing; print the change
  -F, --format <format>    Output format: text (with --dry-run, a unified
                           diff) or json (a report holding the RFC 6902
                           patch that would apply, or that was applied)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob operation remove tasks.obi.json notifications.send --cascade
  ob operation remove tasks.obi.json completeTask --cascade --dry-run
  ob operation remove - createTask --cascade < tasks.obi.json

Exit status:
  0    removed
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -; <obi> is a URL or cannot be read; or <key>
       identifies no operation
  3    refused: <key> is an alias or identifies two operations, bindings
       or dependencies name it and --cascade was not given, a reference
       points into it or to an $id declared in it, the result would break
       another rule, or the document declares a version outside the 0.2
       line; nothing was written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_binding() -> Command {
    group(
        "binding",
        "Read bindings, and add one by hand",
        PAGE_BINDING,
        "ob binding <command> [options]",
    )
    .subcommand(cmd_binding_list())
    .subcommand(cmd_binding_show())
    .subcommand(cmd_binding_add())
}
const PAGE_BINDING: &str = r####"Read bindings, and add one by hand

Usage: ob binding <command> [options]

A binding declares a realization of an operation through a source, under a
key in bindings (§3, §5.3). It names the operation and the source by their
keys (OBI-06, OBI-07). Where a binding names its operation by an alias,
ob validate gives a hint with the key, and in a document that meets
OBI-06, an edit that makes that change is refused with the same hint. Its
data is read under the source's kind, which defines what the data holds:
a pointer into an API description, a method name, an adaptation of
values, or nothing, where the source alone identifies the realization
(§5.3, §6). Its idempotent field is an author claim, and its preference
and deprecated fields are author signals (§5.3).

ob source pull writes most bindings, with their operations. ob binding
add writes one by hand, in the form the source's kind describes, for an
operation you already have. You change, rename and remove bindings in
your editor, then run ob validate to check the result; scripts use
ob patch, which refuses a change that breaks a rule the document met.

Commands:
  list    List bindings, with their operation, source and kind
  show    Show one binding
  add     Add a binding, as its source's kind describes it

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob binding <command> --help' for details."####;

pub(super) fn cmd_binding_list() -> Command {
    leaf(
        "list",
        "List bindings, with their operation, source and kind",
        PAGE_BINDING_LIST,
        "ob binding list [options] <obi>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(
        text(
            "operation",
            "Only the bindings of this operation (its key or an alias)",
        )
        .value_name("name"),
    )
    .arg(format(&["text", "json"]))
}
const PAGE_BINDING_LIST: &str = r####"List bindings, with their operation, source and kind

Usage: ob binding list [options] <obi>

List each binding in document order: its key, the operation it binds,
the source it names, that source's kind, and the author's claim and
signals about it: its idempotent claim, its preference, and whether it is
deprecated. A binding whose source is not in the document shows no kind.

The idempotent claim is shown as stated: true, false, or absent, which
claims neither (§5.3). Preference and deprecation are author signals: a
higher preference states a stronger author preference among one
operation's bindings, and deprecated recommends migrating away (§5.3).
ob invoke shows them but does not choose a binding by them.

--operation lists only the bindings of the operation a name identifies,
given as its key or any alias.

With -F json, the report's bindings array lists the same facts in
document order. A member copied from the document keeps its presence:
idempotent, preference and deprecated appear only when the binding
states them, so an absent idempotent still claims neither. kind is
omitted when the binding's source is not in the document.

In a document that does not conform, ob warns once and lists a binding
whose shape is wrong as unreadable, with its location (see
'ob help documents'). In JSON, such an entry is

  {"key": "<key>", "unreadable": true, "location": {...}}

and when the document's bindings member is not an object, the
report's bindings member, instead of an array, is

  {"unreadable": true, "location": {...}}

Arguments:
  <obi>         The document: a path, - for standard input, or a URL

Options:
      --operation <name>   Only the bindings of this operation (its key or
                           an alias)
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob binding list tasks.obi.json
  ob binding list tasks.obi.json --operation createTask
  ob binding list tasks.obi.json --operation acme.tasks.listTasks -F json

Exit status:
  0    listed
  1    no OBI is published at the origin given
  2    the arguments are wrong, <obi> cannot be read or fetched (a URL
       that is not found included), or the --operation name identifies
       no operation
  3    refused: the --operation name identifies two operations (OBI-05),
       or the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_binding_show() -> Command {
    leaf(
        "show",
        "Show one binding",
        PAGE_BINDING_SHOW,
        "ob binding show [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(positional("key", "The binding's key", true, false))
    .arg(format(&["text", "json"]))
}
const PAGE_BINDING_SHOW: &str = r####"Show one binding

Usage: ob binding show [options] <obi> <key>

Show the binding under <key>: the operation it binds, the source it
names and that source's kind, its data, its idempotent claim, its
preference, whether it is deprecated, and its description.

Data is shown exactly as stored. Its meaning is read under the source's
kind, which this command does not consult (§5.3, §6).

-F json prints the binding as stored, every value spelled exactly, and
nothing else.

Arguments:
  <obi>         The document: a path, - for standard input, or a URL
  <key>         The binding's key

Options:
  -F, --format <format>    Output format: text, or json for the binding as
                           stored [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob binding show tasks.obi.json createTask.http
  ob binding show tasks.obi.json createTask.mcp -F json

Exit status:
  0    shown
  1    no OBI is published at the origin given
  2    the arguments are wrong, <obi> cannot be read or fetched (a URL
       that is not found included), or no binding has the key <key>
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_binding_add() -> Command {
    leaf("add", "Add a binding, as its source's kind describes it", PAGE_BINDING_ADD, "ob binding add [options] --operation <name> --source <key> <obi> <key>")
        .arg(positional("obi", "The document to change: a path, or - for standard input", true, false))
        .arg(positional("key", "The new binding's key", true, false))
        .arg(text("operation", "The operation it realizes, by key or alias (required)").value_name("name").required(true))
        .arg(text("source", "The source it names (required)").value_name("key").required(true))
        .arg(text("data", "The binding's data, as the kind's schema describes it: JSON, @FILE, or -").value_name("value"))
        .arg(boolean("idempotent", "Claim that repeating a call adds no further intended effects; --idempotent=false claims it can"))
        .arg(integer("preference", "The author's preference among the operation's bindings (an integer; higher is stronger)"))
        .arg(boolean("deprecated", "Recommend migrating away from this binding"))
        .arg(text("description", "The binding's description").value_name("text"))
        .arg(boolean("dry-run", "Write nothing; print the change"))
        .arg(format(&["text", "json"]))
}
const PAGE_BINDING_ADD: &str = r####"Add a binding, as its source's kind describes it

Usage: ob binding add [options] --operation <name> --source <key>
                      <obi> <key>

Add a binding under <key> that realizes the operation --operation names
through the source --source names, with data in the form the source's
kind describes. ob asks the Kind Descriptor for the kind's binding-data
schema (what ob kind describe shows) and fills it: from --data, or at a
terminal from what you enter for the schema's members. The data is what
the kind reads to carry out the operation: which target, and any
adaptation between the operation's input and output schemas and that
target, as the kind defines. ob records it as given; the claim that the
declared realization carries out the operation is yours, as for any
binding (§5.3). A kind whose bindings carry no data has no binding-data
schema: the binding is written without data, and --data is a usage
error for it; --data null is the JSON value null.

This is how a source realizes an operation you already have, including
one that ob source pull left out as a conflict because the source offers
it in a different shape; the hint pull printed gives this command. For
what a source offers under new keys, ob source pull adds operations and
bindings together.

--operation names the operation by its key or an alias; the binding is
written with the key, as bindings name operations (OBI-06). --idempotent
claims that repeating a call through this binding adds no further
intended effects; --idempotent=false claims it can; leaving it out claims
nothing. --preference records the author's preference among the
operation's bindings, which ob invoke shows and never chooses by.
--deprecated recommends migrating away.

Add needs the kind's descriptor, built in or through a delegate (the
one ob kind describe shows), for the schema it prompts from and checks
the data against. Without one, --data is recorded unchecked, with a
warning; or write the binding in your editor and run ob validate.

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run prints the change instead of writing it.
With -F json, the report adds the array added (the binding's key).

Arguments:
  <obi>    The document to change: a path, or - for standard input
  <key>    The new binding's key

Options:
      --operation <name>       The operation it realizes, by key or alias
                               (required)
      --source <key>           The source it names (required)
      --data <value>           The binding's data, as the kind's schema
                               describes it: JSON, @FILE, or -
      --idempotent             Claim that repeating a call adds no further
                               intended effects; --idempotent=false claims
                               it can
      --preference <number>    The author's preference among the
                               operation's bindings (an integer; higher is
                               stronger)
      --deprecated             Recommend migrating away from this binding
      --description <text>     The binding's description
      --dry-run                Write nothing; print the change
  -F, --format <format>        Output format: text (with --dry-run, a
                               unified diff) or json (a report holding the
                               RFC 6902 patch that would apply, or that
                               was applied) [default: text]
  -h, --help                   Print help (see a summary with '-h')

Examples:
  # At a terminal: answer the kind's binding schema
  ob binding add tasks.obi.json createTask.mcp --operation createTask \
      --source mcpServer

  # In a script: the data, as the kind's schema describes it
  ob binding add tasks.obi.json createTask.mcp --operation createTask \
      --source mcpServer --data '{"tool":"create_task"}'

  # By alias; the binding is written with the key, listTasks
  ob binding add tasks.obi.json listTasks.http \
      --operation acme.tasks.listTasks --source httpApi \
      --data '{"target":"#/paths/~1tasks/get"}' --idempotent

Exit status:
  0    added, or an identical binding was already under <key>
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -, no --data without a terminal, --data for a kind whose
       bindings carry none, or data the kind's schema does not accept;
       <obi> is a URL or cannot be read; --operation identifies no
       operation; or --source names no source
  3    refused: --operation identifies two operations (OBI-05), <key>
       already names a different binding, the result would break a rule,
       or the document declares a version outside the 0.2 line; nothing
       was written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_dependency() -> Command {
    group(
        "dependency",
        "Read dependencies",
        PAGE_DEPENDENCY,
        "ob dependency <command> [options]",
    )
    .subcommand(cmd_dependency_list())
    .subcommand(cmd_dependency_show())
}
const PAGE_DEPENDENCY: &str = r####"Read dependencies

Usage: ob dependency <command> [options]

A dependency is a named point where the software the document describes
consumes a realization of an operation, under a key in dependencies; the
key identifies the consumption point (§3, §5.4). It names the operation
by its key (OBI-08), and may limit the kinds acceptable there with
kinds, an any-of constraint. It names no realization to serve it, not
even one of the document's own bindings (§5.4). Where a dependency names its
operation by an alias, ob validate gives a hint with the key, and in a
document that meets OBI-08, an edit that makes that change is refused
with the same hint.

To check a candidate for a dependency, such as the OBI of a service whose
bindings might serve it, run:

  ob compat <candidate> <obi> --dependency <key>

It judges each operation of the candidate that a published name (key or
alias) of the dependency's operation identifies: whether it is
compatible as realized, and which of its bindings, the realizations it
declares, have a source whose kind meets the dependency's kinds. Whether
this ob can invoke one is a question for ob kind list. ob dependency
show prints the command for its dependency.

Commands here read dependencies. You add, change, rename and remove
dependencies in your editor, then run ob validate to check the result;
scripts use ob patch, which refuses a change that breaks a rule the
document met.

Commands:
  list    List dependencies, with their operation and kinds constraint
  show    Show one dependency

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob dependency <command> --help' for details."####;

pub(super) fn cmd_dependency_list() -> Command {
    leaf(
        "list",
        "List dependencies, with their operation and kinds constraint",
        PAGE_DEPENDENCY_LIST,
        "ob dependency list [options] <obi>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_DEPENDENCY_LIST: &str = r####"List dependencies, with their operation and kinds constraint

Usage: ob dependency list [options] <obi>

List each dependency in document order: its key, which identifies the
consumption point, the operation it consumes, and its kinds constraint.

The kinds constraint is an any-of constraint: a binding meets it when its
source's kind is the same as one of those listed (§5.4). A dependency
without kinds places no constraint on kind, and is shown as accepting
every kind.

With -F json, the report's dependencies array lists the same facts in
document order. A member copied from the document keeps its presence:
kinds appears only when the dependency states it.

In a document that does not conform, ob warns once and lists a dependency
whose shape is wrong as unreadable, with its location (see
'ob help documents'). In JSON, such an entry is

  {"key": "<key>", "unreadable": true, "location": {...}}

and when the document's dependencies member is not an object, the
report's dependencies member, instead of an array, is

  {"unreadable": true, "location": {...}}

Arguments:
  <obi>         The document: a path, - for standard input, or a URL

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob dependency list tasks.obi.json
  ob dependency list tasks.obi.json -F json

Exit status:
  0    listed
  1    no OBI is published at the origin given
  2    the arguments are wrong, or <obi> cannot be read or fetched (a URL
       that is not found included)
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_dependency_show() -> Command {
    leaf(
        "show",
        "Show one dependency",
        PAGE_DEPENDENCY_SHOW,
        "ob dependency show [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(positional("key", "The dependency's key", true, false))
    .arg(format(&["text", "json"]))
}
const PAGE_DEPENDENCY_SHOW: &str = r####"Show one dependency

Usage: ob dependency show [options] <obi> <key>

Show the dependency under <key>, the consumption point that key
identifies: the operation it consumes, its kinds constraint, and its
description.

Declaring a dependency claims that the software the document describes
sends only input values that satisfy the operation's input schema, and
takes any output value that satisfies its output schema (§5.4); ob
operation show shows those schemas. The kinds constraint is an any-of constraint: a
binding meets it when its source's kind is one of those listed. Without
kinds, the dependency places no constraint on kind. A dependency names no
realization to serve it, so none is shown.

Then ob prints a hint with the command that checks a candidate for the
dependency, such as the OBI of a service that might serve it, leaving
<candidate> for you to fill in. For assigneeNotifier in tasks.obi.json:

  hint: ob compat <candidate> tasks.obi.json --dependency assigneeNotifier

See 'ob help compat' for the check.

-F json prints the dependency as stored, every value spelled exactly, and
nothing else.

Arguments:
  <obi>         The document: a path, - for standard input, or a URL
  <key>         The dependency's key

Options:
  -F, --format <format>    Output format: text, or json for the dependency
                           as stored [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob dependency show tasks.obi.json assigneeNotifier
  ob dependency show tasks.obi.json assigneeNotifier -F json

Exit status:
  0    shown
  1    no OBI is published at the origin given
  2    the arguments are wrong, <obi> cannot be read or fetched (a URL
       that is not found included), or no dependency has the key <key>
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_schema() -> Command {
    group(
        "schema",
        "Read, rename, remove, and bundle schemas",
        PAGE_SCHEMA,
        "ob schema <command> [options]",
    )
    .subcommand(cmd_schema_list())
    .subcommand(cmd_schema_show())
    .subcommand(cmd_schema_rename())
    .subcommand(cmd_schema_remove())
    .subcommand(cmd_schema_bundle())
}
const PAGE_SCHEMA: &str = r####"Read, rename, remove, and bundle schemas

Usage: ob schema <command> [options]

The schemas map holds named JSON Schemas, which operations and other
schemas reference, as in {"$ref": "#/schemas/Task"} (§5.5). Every schema
the document contains is read as JSON Schema 2020-12, and its references
resolve as §7 defines. An external schema is one a reference in the
document identifies, once resolved (against the enclosing $id, inside a
schema that declares one), that the document does not contain.

Commands here read schemas, rename or remove one while accounting for
every reference to it, and bundle external schemas into the document. You
add a schema or change one in your editor, then run ob validate to check
the result; scripts use ob patch, which refuses a change that breaks a
rule the document met.

Commands:
  list      List named schemas, what references each, and external schemas
  show      Show one named schema
  rename    Rename a named schema and every same-document reference to it
  remove    Remove a named schema that nothing references
  bundle    Embed the external schemas the document references

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob schema <command> --help' for details."####;

pub(super) fn cmd_schema_list() -> Command {
    leaf(
        "list",
        "List named schemas, what references each, and external schemas",
        PAGE_SCHEMA_LIST,
        "ob schema list [options] <obi>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_SCHEMA_LIST: &str = r####"List named schemas, what references each, and external schemas

Usage: ob schema list [options] <obi>

List each named schema in document order, with its $id when it declares
one, and every reference to it: each $ref or $dynamicRef that identifies
the schema or a schema inside it, by JSON Pointer, plain name or $id,
shown with the reference's location.

Then it lists the external schemas: the resources that references identify,
once resolved against any enclosing $id and taken without their fragment,
that no schema the document contains declares as its $id. Each is shown with
the references to it. Resolving one needs that schema from outside the
document (§7.4). No check fetches it; ob schema bundle embeds it.

With -F json, the report holds a schemas array in document order and an
external array. A member copied from the document keeps its presence:
id appears only when the schema declares $id. references and external
are always present, possibly empty.

In a document that does not conform, ob warns once and lists a schema
whose shape is wrong as unreadable, with its location (see
'ob help documents'). In JSON, such an entry is

  {"key": "<key>", "unreadable": true, "location": {...}}

and when the document's schemas member is not an object, the
report's schemas member, instead of an array, is

  {"unreadable": true, "location": {...}}

Arguments:
  <obi>         The document: a path, - for standard input, or a URL

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob schema list tasks.obi.json
  ob schema list tasks.obi.json -F json

Exit status:
  0    listed
  1    no OBI is published at the origin given
  2    the arguments are wrong, or <obi> cannot be read or fetched (a URL
       that is not found included)
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_schema_show() -> Command {
    leaf(
        "show",
        "Show one named schema",
        PAGE_SCHEMA_SHOW,
        "ob schema show [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(positional(
        "key",
        "The schema's key in schemas",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_SCHEMA_SHOW: &str = r####"Show one named schema

Usage: ob schema show [options] <obi> <key>

Show the schema under <key> in schemas, as stored, with its $id when it
declares one and the references to it, as ob schema list shows them.
References inside the schema are shown as written, not expanded.

-F json prints the schema as stored, every value spelled exactly, and
nothing else.

Arguments:
  <obi>         The document: a path, - for standard input, or a URL
  <key>         The schema's key in schemas

Options:
  -F, --format <format>    Output format: text, or json for the schema as
                           stored [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob schema show tasks.obi.json Task
  ob schema show tasks.obi.json Problem -F json

Exit status:
  0    shown
  1    no OBI is published at the origin given
  2    the arguments are wrong, <obi> cannot be read or fetched (a URL
       that is not found included), or no schema has the key <key>
  3    refused: the document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_schema_rename() -> Command {
    leaf(
        "rename",
        "Rename a named schema and every same-document reference to it",
        PAGE_SCHEMA_RENAME,
        "ob schema rename [options] <obi> <key> <new-key>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional(
        "key",
        "The schema's key in schemas",
        true,
        false,
    ))
    .arg(positional("new-key", "Its new key", true, false))
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_SCHEMA_RENAME: &str = r####"Rename a named schema and every same-document reference to it

Usage: ob schema rename [options] <obi> <key> <new-key>

Change the key of the schema <key> in schemas to <new-key>, and update
every same-document reference to it in the document resource: each $ref
and $dynamicRef whose JSON Pointer reaches the schema or a schema inside
it, such as #/schemas/Task or #/schemas/Task/properties/id. That includes
spellings that identify it once percent-decoded, such as
#%2Fschemas%2FTask (§7.3). An updated reference keeps its spelling except
for the name.

References inside a schema that declares $id are untouched: they resolve
against that schema's own base, not against the document (§7.2, §7.4).
References through the schema's $id, or through a plain name it declares,
do not contain its key and keep working. Other documents reach a schema
only through an $id, which the rename does not change (§7.5). The
schema's own members, examples, descriptions and extension fields are
left alone.

It refuses a <new-key> that another schema already has, or one that does
not match the name grammar (OBI-04).

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run prints the change instead of writing
it. With -F json, the report adds two arrays: renamed (the old key and
the new) and updated (the locations of the references it updated).

Arguments:
  <obi>         The document to change: a path, or - for standard input
  <key>         The schema's key in schemas
  <new-key>     Its new key

Options:
      --dry-run            Write nothing; print the change
  -F, --format <format>    Output format: text (with --dry-run, a unified
                           diff) or json (a report holding the RFC 6902
                           patch that would apply, or that was applied)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob schema rename tasks.obi.json Problem Error
  ob schema rename tasks.obi.json Task TaskItem --dry-run

Exit status:
  0    renamed, or no change (<new-key> is already its key)
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -; <obi> is a URL or cannot be read; or no schema has
       the key <key>
  3    refused: <new-key> is taken or breaks the name grammar, the result
       would break another rule, or the document declares a version
       outside the 0.2 line; nothing was written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_schema_remove() -> Command {
    leaf(
        "remove",
        "Remove a named schema that nothing references",
        PAGE_SCHEMA_REMOVE,
        "ob schema remove [options] <obi> <key>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional(
        "key",
        "The schema's key in schemas",
        true,
        false,
    ))
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_SCHEMA_REMOVE: &str = r####"Remove a named schema that nothing references

Usage: ob schema remove [options] <obi> <key>

Remove the schema <key> from schemas.

It refuses while anything outside the schema references it, and lists
each reference with its location: a same-document reference that reaches
the schema or a schema inside it, by JSON Pointer or by a plain name it
declares, or a reference to its $id or to an $id declared inside it.
There is no cascade: change those references first, in your editor or
with ob patch.

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run prints the change instead of writing
it. With -F json, the report adds the array removed (the schema's
key).

Arguments:
  <obi>         The document to change: a path, or - for standard input
  <key>         The schema's key in schemas

Options:
      --dry-run            Write nothing; print the change
  -F, --format <format>    Output format: text (with --dry-run, a unified
                           diff) or json (a report holding the RFC 6902
                           patch that would apply, or that was applied)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  # Refused in the sample: three output schemas reference Problem
  ob schema remove tasks.obi.json Problem
  ob schema remove draft.obi.json Unused --dry-run -F json

Exit status:
  0    removed
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -; <obi> is a URL or cannot be read; or no schema has
       the key <key>
  3    refused: something references the schema, the result would break
       another rule, or the document declares a version outside the 0.2
       line; nothing was written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_schema_bundle() -> Command {
    leaf(
        "bundle",
        "Embed the external schemas the document references",
        PAGE_SCHEMA_BUNDLE,
        "ob schema bundle [options] <obi> [<uri>...]",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional(
        "uri",
        "Only these external schemas, by URI [default: every one]",
        false,
        true,
    ))
    .arg(boolean("dry-run", "Write nothing; print the change"))
    .arg(format(&["text", "json"]))
}
const PAGE_SCHEMA_BUNDLE: &str = r####"Embed the external schemas the document references

Usage: ob schema bundle [options] <obi> [<uri>...]

Fetch each external schema the document references (resolved against any
enclosing $id), by its URI without the fragment, and embed it under schemas
with that URI as its $id. References do not change: references to the URI
now resolve to the embedded copy, as JSON Schema's bundling defines (§7.2,
§7.4), so the document no longer needs that schema from outside. External
schemas that a copy references in turn are bundled too. Given <uri>
arguments, it bundles only those, and what they reference in turn.

This is the one command that fetches external schemas, and it embeds
what it fetches. Checks never fetch one, so bundling is what lets them
decide, and lets ob invoke call an operation whose input or output
schema references one.

ob fetches https and http URIs, sending no credentials of its own.

It refuses a fetched schema that declares an $id other than its URI, or a
$schema other than 2020-12 (OBI-09), and names the URI and the reason.
Nothing is written, so every schema stays external; name the other URIs
to bundle them without it. A URI that cannot be fetched stops the bundle
in the same way.

Each copy's key comes from its URI, such as date-time for
https://schemas.example.com/date-time.json, made unique among the
document's schemas. ob schema rename changes the key, and references keep
resolving through the $id.

Like every edit, it changes <obi> in place, or filters - to standard
output, keeps the bytes it does not change, refuses a result that breaks
a rule the document met, and warns about an example it makes fail (see
'ob help documents'). --dry-run still fetches, and prints the change
instead of writing it. With -F json, the report adds the array added
(the keys of the schemas embedded).

Arguments:
  <obi>         The document to change: a path, or - for standard input
  <uri>...      Only these external schemas, by URI [default: every one]

Options:
      --dry-run            Write nothing; print the change
  -F, --format <format>    Output format: text (with --dry-run, a unified
                           diff) or json (a report holding the RFC 6902
                           patch that would apply, or that was applied)
                           [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob schema bundle tasks.obi.json
  ob schema bundle tasks.obi.json --dry-run
  ob schema bundle tasks.obi.json https://schemas.example.com/date-time.json

Exit status:
  0    bundled, or no change (no external schema is left to embed)
  2    the arguments are wrong, such as -F json without --dry-run when
       <obi> is -; <obi> is a URL or cannot be read; a <uri> is not a
       schema the document references; or a schema cannot be fetched or
       read
  3    refused: a fetched schema declares another $id or $schema, the
       result would break another rule, or the document declares a
       version outside the 0.2 line;
       nothing was written
  130  cancelled; nothing was written
  See 'ob help exit-codes'."####;
