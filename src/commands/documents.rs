// Generated from the ob surface draft's help pages by gen_ob_surface.py; do not edit by hand.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![
        cmd_init(),
        cmd_show(),
        cmd_validate(),
        cmd_diff(),
        cmd_compat(),
        cmd_merge(),
        cmd_patch(),
        cmd_fmt(),
    ]
}

pub(super) fn cmd_init() -> Command {
    leaf(
        "init",
        "Create a new document",
        PAGE_INIT,
        "ob init [options] <obi>",
    )
    .arg(positional(
        "obi",
        "The file to create, or - for standard output",
        true,
        false,
    ))
    .arg(text("name", "The document's name").value_name("name"))
    .arg(text("description", "The document's description").value_name("text"))
    .arg(text("interface-version", "The interface-version label").value_name("label"))
    .arg(boolean("force", "Replace an existing file"))
}
const PAGE_INIT: &str = r####"Create a new document

Usage: ob init [options] <obi>

Create an OBI that declares OpenBindings 0.2.0 and has an empty
operations map, and write it to <obi>, laid out as ob fmt lays out a
document. Given - as <obi>, it writes the document to standard output. It
refuses to replace an existing file unless --force is given.

The options fill in the document's own fields. --interface-version sets
the interface-version label, the "version" field: the author's label for
the interface, which neither the specification nor ob interprets. It is
not the OpenBindings version the document declares, which is always
0.2.0. A label that is present must not be empty, so an empty
--interface-version is refused.

With --name "Task Manager" and --interface-version 1.0.0, ob init writes:

  {
    "openbindings": "0.2.0",
    "name": "Task Manager",
    "version": "1.0.0",
    "operations": {}
  }

From there, write operations in your editor and check the result with
ob validate (scripts use ob patch), or copy a shared contract's
operations in with ob merge. To start from an existing description such
as an OpenAPI document, add it as a source with ob source add and bring
its operations in with ob source pull.

Arguments:
  <obi>    The file to create, or - for standard output

Options:
      --name <name>                The document's name
      --description <text>         The document's description
      --interface-version <label>  The interface-version label
      --force                      Replace an existing file
  -h, --help                       Print help (see a summary with '-h')

Examples:
  # A document with a name and an interface-version label
  ob init tasks.obi.json --name "Task Manager" --interface-version 1.0.0

  # Print the new document instead of writing a file
  ob init - --name "Task Manager" \
    --description "Create, list and complete tasks."

  # Start from the operations of a shared contract
  ob init tasks.obi.json && ob merge tasks.obi.json acme-tasks.obi.json

  # Start from an existing OpenAPI description
  ob init tasks.obi.json --name "Task Manager" \
    && ob source add tasks.obi.json httpApi --kind example.openapi@1 \
         --data '{"fetch":"https://api.example.com/openapi.json"}' \
    && ob source pull tasks.obi.json httpApi

  # Start over, replacing an existing file
  ob init tasks.obi.json --name "Task Manager" --force

Exit status:
  0    the document was written
  2    bad arguments, such as a URL as <obi> or an empty
       --interface-version, or a file ob cannot write
  3    refused: the file exists and --force was not given; nothing was
       written
  See 'ob help exit-codes'."####;

pub(super) fn cmd_show() -> Command {
    leaf(
        "show",
        "Show what a document describes",
        PAGE_SHOW,
        "ob show [options] <obi>",
    )
    .arg(positional(
        "obi",
        "The document: a path, - for standard input, or a URL",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_SHOW: &str = r####"Show what a document describes

Usage: ob show [options] <obi>

Print an overview of a document: its name, interface-version label,
declared OpenBindings version and description; its operations, each with
its identifiers (key and aliases), its bindings and the dependencies that
consume it; then its sources, bindings, dependencies and named schemas.
To see one part in full, use the show command of its part: ob operation
show, ob binding show, ob dependency show, ob source show or ob schema
show.

<obi> is a path, - for standard input, or a URL. A bare origin such as
https://api.example.com is looked up at /.well-known/openbindings (HTTP
Discovery).

ob reads the declared version first. A text that declares a version
outside the 0.2 line is governed by that line, so ob never judges it by
0.2's rules: -F json prints its bytes, unchanged, and the overview
refuses it (exit 3), as other commands do (ob fetch still saves it).

Any other text is read under 0.2's rules, and ob show reads drafts: a
document that does not conform is still shown, with one warning naming
the rules involved and pointing to ob validate, and any part whose shape
is wrong is shown as unreadable, with its location; the exit status is
unaffected. Input that is not a JSON object meeting OBI-01 (UTF-8, with
no byte-order mark, repeated member name or unpaired-surrogate escape)
cannot be shown: ob prints its first findings and a hint to run
ob validate (exit 2). See 'ob help documents'.

-F json prints the document exactly as stored, rather than a report.

Arguments:
  <obi>    The document: a path, - for standard input, or a URL

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  # An overview of a local document
  ob show tasks.obi.json

  # The document a service publishes at /.well-known/openbindings
  ob show https://api.example.com

  # The stored document, for jq and other JSON tools
  ob show tasks.obi.json -F json | jq '.operations | keys'

  # An earlier revision, read from standard input
  git show HEAD~1:tasks.obi.json | ob show -

Exit status:
  0    shown
  1    no OBI is published at the URL given
  2    bad arguments, a file that does not exist, input ob cannot read, or
       a network failure
  3    refused: the document declares a version outside the 0.2 line
       (without -F json)
  See 'ob help exit-codes'."####;

pub(super) fn cmd_validate() -> Command {
    leaf(
        "validate",
        "Check a document, values, or examples",
        PAGE_VALIDATE,
        "ob validate [options] <obi>...",
    )
    .arg(positional(
        "obi",
        "The documents: paths, - for standard input, or URLs; with --operation, one document",
        true,
        true,
    ))
    .arg(
        text(
            "operation",
            "The operation whose values or examples to check, by key or alias",
        )
        .value_name("name"),
    )
    .arg(
        repeated(
            "input",
            "An input value to check: JSON, @FILE, or - (repeatable)",
        )
        .value_name("value"),
    )
    .arg(
        repeated(
            "output",
            "An output value to check: JSON, @FILE, or - (repeatable)",
        )
        .value_name("value"),
    )
    .arg(boolean(
        "examples",
        "Check examples instead of the document",
    ))
    .arg(boolean("quiet", "Print nothing; answer through the exit status").short('q'))
    .arg(format(&["text", "json"]))
}
const PAGE_VALIDATE: &str = r####"Check a document, values, or examples

Usage: ob validate [options] <obi>...

Check one of three things: each document against the rules of
OpenBindings 0.2, OBI-01 to OBI-14 (the default); with --operation and
--input or --output, values against that operation's input or output
contract; or, with --examples, each value an example gives against its
operation's value contract for that side. The document and examples
checks take several documents at once; --operation takes one.

Checking documents. The result for each document is one of:

  conformant                  every rule is met
  non-conformant              some rule is violated
  conformance undetermined    no rule is violated, but some rule could
                              not be decided

Each finding is one line on standard output:

  <document>:<line>:<column>: <rule>: <message> (at <pointer>)

<document> is the argument as given (<stdin> for standard input), so the
findings of several documents stay apart; line and column count from 1,
the column counts characters, and <pointer> is a JSON Pointer to the
place in the document. A last line for each document gives its result
and names the specification release and revision ob applied. A text that
is not JSON is non-conformant (OBI-01), not unreadable. A document that
declares another line, or a prerelease, is refused, not judged: see
'ob help documents'.

When an OBI-06 or OBI-08 finding is a binding or dependency that names
its operation by an alias, a hint follows it with the key that alias
identifies: "hint: acme.tasks.listTasks is an alias of listTasks;
bindings name operations by key".

When a document conforms, ob also checks its examples, and when any
example value fails its value contract it prints one warning pointing to
ob validate --examples, which lists each one. Examples are author claims,
which conformance does not judge (spec §5.1), so the warning never
changes the result or the exit status.

Checking values. --operation names the operation by key or alias. Each
--input is checked against its input contract and each --output against
its output contract, and one run may check both. A file or standard
input may hold several values separated by whitespace, each checked in
turn; only one argument may read standard input. Each result is labelled
by side and position (input 1, input 2, output 1) and is one of:

  satisfies      the value satisfies the value contract
  fails          the value fails the value contract; the problem is
                 given at a JSON Pointer into the value
  undefined      whether it satisfies the value contract is undefined (spec
                 §5.2), as when the answer rests on a pattern that is not
                 an ECMA-262 regular expression with Unicode semantics
  no-verdict     ob could not decide, and says why; for example, the
                 answer rests on an external schema, which ob names but
                 never fetches (ob schema bundle embeds it, so later
                 checks can decide)
  no-value-contract
                 the operation states no value contract for that side, so
                 the value neither satisfies nor fails one (spec §5.2)

Checking examples. --examples checks the examples of every operation of
each document, or with --operation, of that one. An example claims that
each value it gives satisfies the value contract for its side (spec
§5.1). A value given as null is a value, and is checked like any other.
Each value is reported in the words above, labelled by operation,
example and side. Where the operation states no value contract for a
side, the example makes no claim there: that value is listed as
no-value-contract and does not affect the exit status.

Values and examples need a conformant document: a non-conformant one is
refused (exit 3), and an undetermined one gets no verdict (exit 4). See
'ob help documents'.

For values and examples, one exit status covers each document: 1 if
anything fails; otherwise 4 if anything is undefined or no-verdict, or,
when checking values, no-value-contract (nothing was checked); otherwise
0.

Several documents. The document and examples checks take any number of
documents, at most one of them -, and check each in turn. --operation
takes one document (exit 2 otherwise), because values belong to one
operation. The exit status is the most serious across the documents: 2,
then 3, then 1, then 4, then 0, since a document that could not be read
or was refused leaves the run incomplete.

-q prints nothing and answers through the exit status alone. -F json
prints the report as JSON (see 'ob help output'): one report whose
documents array holds one entry per document, in the order given, even
for one, so a script reads the same shape however many documents a glob
matches. With --operation, it holds the one document's entry.

Arguments:
  <obi>...    The documents: paths, - for standard input, or URLs; with
              --operation, one document

Options:
      --operation <name>     The operation whose values or examples to
                             check, by key or alias
      --input <value>        An input value to check: JSON, @FILE, or -
                             (repeatable)
      --output <value>       An output value to check: JSON, @FILE, or -
                             (repeatable)
      --examples             Check examples instead of the document
  -q, --quiet                Print nothing; answer through the exit status
  -F, --format <format>      Output format: text or json [default: text]
  -h, --help                 Print help (see a summary with '-h')

Examples:
  # Check a document against the rules
  ob validate tasks.obi.json

  # Check the document a service publishes, in a script
  ob validate https://api.example.com -q && echo conformant

  # In CI: check every document in a directory
  ob validate api/*.json -q

  # Check an input and a recorded output in one run
  ob validate tasks.obi.json --operation createTask \
    --input '{"title":"Plan the release"}' --output @response.json

  # Check every output value in a file; the alias identifies listTasks
  ob validate tasks.obi.json --operation acme.tasks.listTasks \
    --output @responses.json

  # Check the examples of every operation
  ob validate tasks.obi.json --examples

Exit status:
  0    every document conforms; or every value checked satisfies its
       value contract
  1    a document is non-conformant; or some value fails its value
       contract; or no OBI is published at a URL given
  2    bad arguments, such as --input without --operation or with
       --examples, --operation with nothing to check or with more than
       one document, or a value that is not JSON; a name or file that
       does not exist; a network failure
  3    refused: a document declares a version outside the 0.2 line; for
       values and examples, a document is non-conformant
  4    no verdict: a document's conformance is undetermined; or, for
       values and examples, nothing fails but a result is undefined or
       no-verdict; or, for values only, nothing fails but a side has no
       value contract to check
  With several documents, the most serious status wins: 2, then 3, then
  1, then 4, then 0.
  See 'ob help exit-codes'."####;

pub(super) fn cmd_diff() -> Command {
    leaf(
        "diff",
        "Show what changed between two documents",
        PAGE_DIFF,
        "ob diff [options] <before> <after>",
    )
    .arg(positional(
        "before",
        "The earlier document: a path, -, or a URL",
        true,
        false,
    ))
    .arg(positional(
        "after",
        "The later document: a path, -, or a URL",
        true,
        false,
    ))
    .arg(boolean(
        "patch",
        "Print an RFC 6902 JSON Patch from <before> to <after>",
    ))
    .arg(boolean(
        "exit-code",
        "Exit 1 when the documents differ, other than in changes reported as reordered",
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_DIFF: &str = r####"Show what changed between two documents

Usage: ob diff [options] <before> <after>

List the differences between two documents, part by part: the
document's own fields, then operations, sources, bindings, dependencies
and schemas. Each change names the part and its key, says whether it was
added, removed, changed or reordered, and gives a JSON Pointer to what
changed.

The documents are compared as JSON values, so layout, field order and
the spelling of a number (1.0 and 1) are not differences. ob fmt settles
layout and the order of the fields the specification defines; it keeps
number spelling (ob fmt --canonical writes one spelling, for hashing).
Array order is a difference, except in aliases, tags and kinds, whose
order means nothing to the specification: there a reorder, or for tags a
repetition (a repeated tag adds nothing), is reported as reordered, not
changed. --patch still turns <before> into <after> exactly.

ob diff reports differences only, not whether they matter to anyone who
calls the operations. To check whether <after> still serves the callers
of <before>, run ob compat <after> <before>, which treats <before> as the
shared contract.

--patch prints the differences as an RFC 6902 JSON Patch that turns
<before> into <after>; ob patch applies it to this or another document.
--exit-code makes the run a predicate: exit 1 when the documents differ,
0 when they do not. A change reported as reordered does not count, so a
CI gate does not fail over an order the specification gives no meaning.

-F json prints the differences as a report. Each change also carries the
exact values as stored: oldValue when it was removed, changed or
reordered, and newValue when it was added, changed or reordered; each is
absent when that side is absent.

Each argument is a path, -, or a URL; at most one may be -. Like ob show,
ob diff reads drafts, with one warning for each document that does not
conform: see 'ob help documents'.

Arguments:
  <before>    The earlier document: a path, -, or a URL
  <after>     The later document: a path, -, or a URL

Options:
      --patch              Print an RFC 6902 JSON Patch from <before> to
                           <after>
      --exit-code          Exit 1 when the documents differ, other than
                           in changes reported as reordered
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  # What changed between two versions
  ob diff tasks.obi.json tasks-next.obi.json

  # What changed since the last commit
  git show HEAD:tasks.obi.json | ob diff - tasks.obi.json

  # The change as a patch, to apply elsewhere with ob patch
  ob diff tasks.obi.json tasks-next.obi.json --patch > changes.json

  # In CI: fail when the published document differs from yours
  ob diff https://api.example.com tasks.obi.json --exit-code

Exit status:
  0    done; with --exit-code, the documents do not differ, other than
       in changes reported as reordered
  1    with --exit-code, the documents differ in a change that is not
       reordered; or no OBI is published at a URL given
  2    bad arguments, a file that does not exist, input ob cannot read, or
       a network failure
  3    refused: a document declares a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_compat() -> Command {
    leaf("compat", "Check a document against a shared contract", PAGE_COMPAT, "ob compat [options] <obi> <shared-contract>")
        .arg(positional("obi", "The document to check: a path, -, or a URL; with --dependency, the candidate", true, false))
        .arg(positional("shared-contract", "The shared contract: a path, -, or a URL; with --dependency, the document that declares it", true, false))
        .arg(repeated("operation", "Check only this operation of the shared contract, by key or alias (repeatable)").value_name("name"))
        .arg(choice("check-as", "Check an operation with neither bindings nor dependencies as realized, consumed, or both", &["realized", "consumed", "both"]).default_value("both"))
        .arg(text("dependency", "Check <obi> as a candidate for this dependency of <shared-contract>").value_name("key"))
        .arg(boolean("quiet", "Print nothing; answer through the exit status").short('q'))
        .arg(format(&["text", "json"]))
}
const PAGE_COMPAT: &str = r####"Check a document against a shared contract

Usage: ob compat [options] <obi> <shared-contract>

Check <obi> against a shared contract: for each operation of the shared
contract, which of <obi>'s operations its published names identify, and
whether their value contracts are compatible under the Schema Comparison
Profile OB-2020-12. With --dependency, check instead whether <obi> can
serve a dependency that <shared-contract> declares.

A shared contract is a published set of operations offered for adoption,
such as another OBI. Its operations' keys and aliases are published
names. An operation that carries a published name, as its key or as an
alias, claims correspondence with the operation that name identifies.

For each operation of the shared contract, ob compat reports:

  - which of <obi>'s operations its published names (its key and its
    aliases) identify, and whether by key or by alias; when they identify
    more than one, each is checked and listed;
  - whether the two operations' value contracts are compatible under the
    Schema Comparison Profile OB-2020-12, checked the way <obi> uses its
    operation:

    realized   it has bindings: it must accept every input the shared
               contract allows, and return only outputs it allows
    consumed   a dependency names it: it must send only inputs the
               shared contract accepts, and take every output it allows

An operation with both bindings and dependencies is checked both ways.
One with neither is checked as --check-as says: realized, consumed, or
both (the default). Where either operation states no value contract for a
side, that side is not compared. Operations of <obi> that carry no
published name of the shared contract are not part of the check.
--operation limits the check to the shared contract's operations it
names.

An operation of the shared contract that no operation of <obi> carries is
missing, and counts as incompatible. For each one, when <obi> is a path,
ob prints the two ways to claim it as hints: copy the shared contract's
operation in without its bindings, or give one of your operations its
published name as an alias.

  ob merge <obi> <shared-contract> --operation <name> --no-bindings
  ob merge <obi> <shared-contract> --operation <name> --onto <operation>

With -F json, the missing operation's entry carries the same two
commands as its remedies, each in two forms: command, the display
string, quoted for the shell, and argv, an array of its arguments, ob
first, in which each placeholder, such as <operation>, is its own
element, so a program can fill one in without re-quoting.

The result is compatible, incompatible, or indeterminate, combined as the
profile combines its outcomes: any indeterminate comparison makes the
result indeterminate; otherwise any incompatibility, a missing operation
included, makes it incompatible; otherwise it is compatible. The exit
status follows the result, so an indeterminate comparison beside a
definite incompatibility exits 4, not 1. In that case ob prints a
warning naming each operation found incompatible and each one missing,
and -F json lists each operation's verdict.

A keyword outside the profile, such as pattern or not, at a position
where the two schemas are not identical makes a comparison indeterminate
(identical positions are compatible, whatever keywords they use). What
the profile treats as an error rather than a verdict (a reference cycle,
an external schema, which ob never fetches, or an allOf no value can
satisfy) ob also reports as indeterminate, naming the cause.

For each incompatibility the profile gives a reason, prefixed by the
keyword that decided it and, for a nested failure, the path to it, such
as:

  required: candidate requires "extra" but target does not

With -F json, an issue's detail carries that reason verbatim, its prefix
and path included; the text output may say it in plain words.

Any OBI can serve as the shared contract. To check whether a new version
of a document breaks the callers of the old one, compare the new against
the old: ob compat new.obi.json old.obi.json. An operation renamed with
its old key kept as an alias still corresponds.

--dependency checks a candidate for a dependency. <shared-contract> is
then the document that declares the dependency, and <obi> is the
candidate, such as the OBI of a service that might serve it:

  ob compat <candidate> <document> --dependency <key>

ob reads the dependency <key> from <shared-contract>: the operation it
consumes, and its kinds. It checks that operation as --operation would,
but always as realized, since the candidate's bindings would carry the
calls. For each operation of <obi> that a published name (key or alias)
of the dependency's operation identifies, it lists that operation's
bindings, each with its source's kind, and marks each binding that meets
the dependency's kinds: its source's kind is the same string as one of
the kinds listed (spec §5.5, §6). A dependency without kinds accepts
every kind.

Each such operation is judged on its own. It serves when it has a
binding that meets the kinds and its comparison is compatible. It cannot
serve when no binding meets the kinds, whatever the comparison, or when
the comparison is incompatible. Otherwise, with a binding that meets the
kinds beside an indeterminate comparison, it gives no verdict. A
dependency needs one realization, so the answer is yes (exit 0) when
some operation serves; no verdict (exit 4) when none serves and some
gives no verdict; and no (exit 1) otherwise, including when the names
identify no operation. This is a different question from a whole shared
contract, whose summary follows the profile.

With -F json, the report adds dependency (its key, its operation, its
kinds when stated, and answer: serves, cannot-serve or no-verdict, which
decides the exit status), and each identified operation's entry adds
serving (the same three values) and its bindings (key, source, kind,
meetsKinds). The top-level verdict stays the profile's summary of the
comparisons made, which is not the dependency's answer: scripts read
dependency.answer. ob prints no merge hints here, and the report carries
no remedies: they would edit the candidate's document.

The check reads only the two documents. Whether this ob can invoke such
a binding is a question for ob kind list, and nothing records the
wiring: a dependency names no realization (spec §5.5). --dependency does
not combine with --operation or --check-as.

Both documents must conform: see 'ob help documents'. Each argument is a
path, -, or a URL; at most one may be -. -q prints nothing and answers
through the exit status alone.

Arguments:
  <obi>                The document to check: a path, -, or a URL; with
                       --dependency, the candidate
  <shared-contract>    The shared contract: a path, -, or a URL; with
                       --dependency, the document that declares it

Options:
      --operation <name>   Check only this operation of the shared
                           contract, by key or alias (repeatable)
      --check-as <how>     Check an operation with neither bindings nor
                           dependencies as realized, consumed, or both
                           [default: both]
      --dependency <key>   Check <obi> as a candidate for this dependency
                           of <shared-contract>
  -q, --quiet              Print nothing; answer through the exit status
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  # Which of Acme's task operations does tasks.obi.json correspond to?
  ob compat tasks.obi.json acme-tasks.obi.json

  # The same against a published shared contract, in a script
  ob compat tasks.obi.json https://acme.example.com/tasks.obi.json -q

  # Does the next version break callers of this one?
  ob compat tasks-next.obi.json tasks.obi.json

  # A new version of an unbound shared contract, checked as realized
  ob compat acme-tasks-v3.obi.json acme-tasks.obi.json --check-as realized

  # The service's published document, as a JSON report
  ob compat https://api.example.com acme-tasks.obi.json -F json

  # Can the notification service serve the assigneeNotifier dependency?
  ob compat https://notify.example.com tasks.obi.json \
    --dependency assigneeNotifier

Exit status:
  0    compatible; with --dependency, some operation of <obi> serves
       the dependency: it corresponds, is compatible, and has a binding
       that meets the dependency's kinds
  1    incompatible, a missing operation included; with --dependency,
       no operation serves and none gives no verdict; or no OBI is
       published at a URL given
  2    bad arguments, such as --dependency with --operation or
       --check-as; a name or file that does not exist, such as a
       dependency key <shared-contract> does not have; input ob cannot
       read; a network failure
  3    refused: a document is non-conformant or declares a version
       outside the 0.2 line
  4    indeterminate, even when an operation is also incompatible or
       missing, which a warning names; with --dependency, no operation
       serves and some gives no verdict; or a document's conformance is
       undetermined
  See 'ob help exit-codes'."####;

pub(super) fn cmd_merge() -> Command {
    leaf("merge", "Bring operations in from another document", PAGE_MERGE, "ob merge [options] <obi> <from>")
        .arg(positional("obi", "The document to change: a path, or - for standard input", true, false))
        .arg(positional("from", "The document to copy from: a path, -, or a URL", true, false))
        .arg(repeated("operation", "Merge only this operation of <from>, named by key or alias (repeatable)").value_name("name"))
        .arg(text("onto", "Add the --operation name as an alias of this operation of <obi>, instead of copying").value_name("operation"))
        .arg(choice("check-as", "Check an operation of <obi> with neither bindings nor dependencies as realized, consumed, or both", &["realized", "consumed", "both"]).default_value("both"))
        .arg(boolean("no-bindings", "Bring in no bindings or sources"))
        .arg(boolean("ours", "Where the documents conflict, keep what <obi> has"))
        .arg(boolean("theirs", "Where the documents conflict, take what <from> has"))
        .arg(boolean("dry-run", "Show the change without writing it"))
        .arg(format(&["text", "json"]))
}
const PAGE_MERGE: &str = r####"Bring operations in from another document

Usage: ob merge [options] <obi> <from>

Copy operations from <from> into <obi> with the schemas, bindings and
sources they need: every named schema their value contracts reach
through references, transitively; their bindings; and the sources those
bindings go through. Dependencies are not copied: each is a consumption
point of <from>'s own described component. Each operation keeps its key
and aliases, so an operation copied from a shared contract carries its
published names, and so claims correspondence with the operation they
identify there. --operation limits the merge to the operations it names,
by key or alias in <from>. --no-bindings copies operations and their
schemas without bindings or sources.

ob never rebinds a copied reference silently, and never rewrites one. A
reference can reach into another operation of <from>, by JSON Pointer,
such as #/operations/listTasks/output, or through an $id declared there.
When the result keeps <obi>'s own version of that operation (it is not
being copied, it is skipped as already compatible, or --ours keeps it),
<obi> must hold an identical schema at that place, or under that $id.
Otherwise ob refuses (exit 3) and names the reference; when the operation
was not being copied, it hints --operation with that operation, which
then follows the conflict rules below.

Named schemas follow the same rule where <obi> already holds a key with
different content. --ours keeps <obi>'s schema, so a copied operation
that reaches it would reach a different contract than it does in
<from>: ob refuses (exit 3), names the reference, and hints ob schema
rename to make room. --theirs takes <from>'s schema, so it refuses
(exit 3) when operations outside the merge use the schema, and names
them: --operation limits the merge, and their contracts must not change
behind it. Its hint gives two ways on: ob schema rename one side's
schema first, or name those operations with --operation too, when
<from> has them. Identity decides, not compatibility: a compatible
schema is still a different contract.

Where <obi> already has an entry (the same key; for an operation, one
that the incoming operation's key or one of its aliases identifies):

  - an identical entry is left as it is;
  - an operation whose value contracts are already compatible with the
    incoming one's, checked as ob compat checks them (see --check-as
    below), is skipped as already compatible;
  - anything else, an indeterminate comparison included, is a conflict.
    ob lists every conflict and writes nothing, unless you choose a side:
    --ours keeps what <obi> has, --theirs takes what <from> has.

When an incoming operation's identifiers identify more than one
operation of <obi>, ob refuses (exit 3) and names them.

Where the match was by alias and the keys differ, --theirs keeps <obi>'s
key, so the bindings and dependencies that name it stay valid, and takes
<from>'s other fields. By key or by alias, the operation keeps its own
aliases and gains every identifier <from> gives it, its key and its
aliases, except the kept key, so each name that identified it in either
document still identifies it (spec §5.1). There is no option to replace
the aliases instead: like git's -X theirs, --theirs settles only what
conflicts, and dropping a name stays an edit of your own.

An operation <obi> already has, left as identical, or skipped as already
compatible when checked as realized, still brings the bindings <from>
has for it, and the sources they go through, unless --no-bindings: a
binding claims that its target realizes the operation as the document
describes it (spec §5.3), so the bindings of an operation compatible as
realized realize <obi>'s too. Binding and source keys that collide
follow the conflict rules above. Every binding merge brings in for an
operation matched by alias, copied with it under --theirs or brought in
this way, is written to name <obi>'s key, as ob operation rename
rewrites references.

--onto claims correspondence for an operation you already have, instead
of copying. It takes exactly one --operation, and adds the name given
there as an alias of the <obi> operation that --onto names, copying
nothing else. ob first compares the two operations' value contracts as ob
compat does. It refuses when the comparison establishes that they are
incompatible, and prints a hint with the ob patch command that adds the
alias directly, as your own claim (spec §5.1), which ob does not check;
there is no --force, so --onto always makes a checked claim. It goes
ahead with a warning when the comparison is indeterminate: carrying a
name is the author's claim, and only an established incompatibility
rules it out. It also refuses a name that already identifies another
operation of <obi>, since no name may identify two operations (OBI-05).

--check-as has ob compat's meaning (see 'ob help compat'), and applies
both to the skip check above and to --onto.

Both documents must conform, because merging relies on what they mean:
see 'ob help documents'. Like every edit, merge never writes a result
that violates a rule, warns about an example it makes fail, and changes
<obi> in place, or filters - to standard output (only one of <obi> and
<from> may be -); --dry-run prints the change instead of writing it.

A summary on standard error says what was added, what was skipped as
already compatible, which entries were replaced, and which aliases were
added. With -F json, the report adds them as four arrays: added (the
locations of the entries added, the bindings and sources brought for
operations <obi> already had included), skipped (the keys of the
operations skipped as already compatible), replaced (the locations of
the entries replaced) and aliasesAdded (the locations of the aliases
added to operations <obi> already had, by --onto or --theirs). When
there is nothing to bring in, the result is "no change".

Arguments:
  <obi>     The document to change: a path, or - for standard input
  <from>    The document to copy from: a path, -, or a URL

Options:
      --operation <name>   Merge only this operation of <from>, named by
                           key or alias (repeatable)
      --onto <operation>   Add the --operation name as an alias of this
                           operation of <obi>, instead of copying
      --check-as <how>     Check an operation of <obi> with neither
                           bindings nor dependencies as realized,
                           consumed, or both [default: both]
      --no-bindings        Bring in no bindings or sources
      --ours               Where the documents conflict, keep what <obi>
                           has
      --theirs             Where the documents conflict, take what
                           <from> has
      --dry-run            Show the change without writing it
  -F, --format <format>    Output format: text, or json for a report
                           holding the patch that would apply (with
                           --dry-run) or that was applied [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  # Copy in the operation that Acme's shared contract adds
  ob merge tasks.obi.json acme-tasks.obi.json \
    --operation acme.tasks.deleteTask --no-bindings

  # Or claim it for an operation you already have
  ob merge tasks.obi.json acme-tasks.obi.json \
    --operation acme.tasks.deleteTask --onto removeTask

  # Preview: add what is missing, keep your own where they conflict
  ob merge tasks.obi.json acme-tasks.obi.json --ours --dry-run

  # Operations you have not bound yet will be realized: check them so
  ob merge tasks.obi.json acme-tasks.obi.json --check-as realized \
    --dry-run

  # One operation from a newer version, without its bindings
  ob merge tasks.obi.json tasks-next.obi.json \
    --operation archiveTask --no-bindings

  # Start a new document from a shared contract
  ob init tasks.obi.json && ob merge tasks.obi.json acme-tasks.obi.json

Exit status:
  0    merged, or "no change"
  1    no OBI is published at the URL given as <from>
  2    bad arguments, such as a URL as <obi>, --ours with --theirs,
       --onto without exactly one --operation, or -F json without
       --dry-run when <obi> is -; a name or file that does not exist;
       input ob cannot read; a network failure
  3    refused: a conflict without --ours or --theirs; an incoming
       operation whose identifiers identify more than one operation of
       <obi>; a copied operation's reference that reaches into an
       operation, or a named schema, that the result keeps from <obi>,
       where <obi> holds no identical schema; --theirs replacing a named
       schema that operations outside the merge use; --onto with
       contracts established as incompatible (a hint gives the ob patch
       command that adds the alias), or with a name that already
       identifies another operation; a result that would violate a rule;
       a document that is non-conformant or whose conformance is
       undetermined; a version outside the 0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_patch() -> Command {
    leaf(
        "patch",
        "Apply a JSON Patch",
        PAGE_PATCH,
        "ob patch [options] <obi> <patch>",
    )
    .arg(positional(
        "obi",
        "The document to change: a path, or - for standard input",
        true,
        false,
    ))
    .arg(positional(
        "patch",
        "The RFC 6902 patch: a file, or - for standard input",
        true,
        false,
    ))
    .arg(boolean("dry-run", "Show the change without writing it"))
    .arg(format(&["text", "json"]))
}
const PAGE_PATCH: &str = r####"Apply a JSON Patch

Usage: ob patch [options] <obi> <patch>

Apply an RFC 6902 JSON Patch to a document. The patch is read from the
file <patch>, or from standard input when <patch> is -. Its paths are
JSON Pointers into the document (RFC 6901), such as
/bindings/createTask.http/preference; a / inside a key is written ~1.

The patch applies whole or not at all. If any of its operations fails,
nothing is written. A test operation that does not hold is a definite no
(exit 1), so a patch can carry its own precondition; any other operation
that cannot apply, such as one whose path does not exist, exits 2.

ob patch applies the guard every edit applies (see 'ob help documents'):
it refuses a result that breaks a rule the document met, naming the
rule and where, with the findings in ob validate's format. A document
that already breaks rules can be repaired one patch at a time: a further
break of a rule it already breaks is written, with a warning for each
new finding. When it refuses a binding or dependency that names its
operation by an alias, the hint gives the key that alias identifies:
"hint: acme.tasks.listTasks is an alias of listTasks; bindings name
operations by key". Like every edit, it also warns about an example it
makes fail, and writes the change anyway.

ob patch is for scripts, and for changes no other command makes: setting
the interface-version label in a release pipeline, deprecating a
binding, adding an extension field. By hand, edit the document in your
editor, then run ob validate to check the result.

Like every edit, ob patch changes <obi> in place, keeping every byte the
patch does not touch, or filters - to standard output (with <patch> then
a file); --dry-run prints the change instead of writing it. With
-F json, the report holds the patch that would apply or was applied, and
adds no summary members. A patch whose result is already true is "no
change".

Arguments:
  <obi>      The document to change: a path, or - for standard input
  <patch>    The RFC 6902 patch: a file, or - for standard input

Options:
      --dry-run            Show the change without writing it
  -F, --format <format>    Output format: text, or json for a report
                           holding the patch that would apply (with
                           --dry-run) or that was applied [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  # Apply a patch file
  ob patch tasks.obi.json changes.json

  # Apply it, and read back the patch that was applied
  ob patch tasks.obi.json changes.json -F json | jq '.patch'

  # Set the interface-version label in a release pipeline
  echo '[{"op":"add","path":"/version","value":"1.5.0"}]' |
    ob patch tasks.obi.json -

  # See what a patch would change, without writing it
  ob patch tasks.obi.json deprecate-mcp.json --dry-run

  # Apply the change between two versions to another document
  ob diff tasks.obi.json tasks-next.obi.json --patch |
    ob patch tasks-staging.obi.json -

  # As a filter
  ob patch - changes.json < tasks.obi.json > tasks-next.obi.json

Exit status:
  0    applied, or "no change"
  1    a test operation in the patch does not hold; nothing was written
  2    bad arguments, such as a URL as <obi>, or -F json without
       --dry-run when <obi> is -; a file that does not exist; a patch or
       document ob cannot read; an operation that cannot apply
  3    refused: the result would violate a rule the document did not
       already violate, or the document declares a version outside the
       0.2 line
  See 'ob help exit-codes'."####;

pub(super) fn cmd_fmt() -> Command {
    leaf(
        "fmt",
        "Format documents",
        PAGE_FMT,
        "ob fmt [options] <obi>...",
    )
    .arg(positional(
        "obi",
        "The documents to format: paths, or - for standard input",
        true,
        true,
    ))
    .arg(boolean(
        "check",
        "Change nothing; exit 1 if any file would change",
    ))
    .arg(boolean(
        "canonical",
        "Print the RFC 8785 form of one document",
    ))
    .arg(boolean("dry-run", "Show the change without writing it"))
}
const PAGE_FMT: &str = r####"Format documents

Usage: ob fmt [options] <obi>...

Rewrite each document with two-space indentation and ob's field order.
The root object, and each of its top-level maps that is not empty
(operations, sources, bindings, dependencies, schemas), always puts its
members one to a line, so the first member added never reflows the file.
Below them, an object or array is written on one line when that line is
at most 80 columns, and otherwise has its members one to a line.

The specification defines no layout; the layout is ob's own. The fields
of each object the specification defines come in the order its tables
list them: openbindings, name, version, description, operations,
sources, bindings, dependencies, schemas at the top; description,
deprecated, tags, aliases, input, output, examples in an operation;
kind, data, description in a source; operation, source, data,
idempotent, preference, description, deprecated in a binding; and so on
for dependencies and examples. Unknown fields, such as extensions,
follow in the order they had. Map entries (operations, sources,
bindings and the other maps) keep their order, and so do the members of
schemas, data and every other value.

Values are kept exactly: every number and string keeps its spelling, so
1.0 stays 1.0 and 1e3 stays 1e3. Formatting never changes a value.

--check changes nothing: it prints the name of each file that would
change and exits 1 if any would. --dry-run writes nothing and prints each
change as a unified diff. Together, --check --dry-run prints the diffs
and exits 1 if any file would change, as black --check --diff does. (fmt
takes no -F: formatting changes no value, so a JSON Patch of it would
always be empty.) Give - as <obi> to format standard input to standard
output; at most one <obi> may be -.

With several documents, ob formats each one it can, and the most serious
status wins: 2, then 3, then 1, then 0, since a document that could not
be read or was refused leaves the run incomplete.

--canonical prints the JSON Canonicalization Scheme form (RFC 8785) of
one document to standard output and writes nothing, for hashing and
signing (spec Appendix A), so it excludes --check and --dry-run
(exit 2). JCS sorts members, removes whitespace, and writes each number
from its binary64 value, so 1.0 becomes 1, the same number. ob refuses a
document that JCS cannot carry exactly (exit 3) rather than alter it:
one with a number whose value that rendering would change, such as
1000000000000000128, or other input RFC 8785 does not accept, such as a
number too large for binary64 or a noncharacter in a string.

Arguments:
  <obi>...    The documents to format: paths, or - for standard input

Options:
      --check              Change nothing; exit 1 if any file would change
      --canonical          Print the RFC 8785 form of one document
      --dry-run            Show the change without writing it
  -h, --help               Print help (see a summary with '-h')

Examples:
  # Format a document in place
  ob fmt tasks.obi.json

  # In CI: list documents that need formatting, and fail if any do
  ob fmt --check tasks.obi.json acme-tasks.obi.json

  # See what formatting would change
  ob fmt tasks.obi.json --dry-run

  # A stable digest of the document's value
  ob fmt --canonical tasks.obi.json | shasum -a 256

  # As a filter
  ob fmt - < tasks.obi.json > formatted.obi.json

Exit status:
  0    formatted, or already formatted; with --check, no file would
       change
  1    with --check, some file would change
  2    bad arguments, such as a URL, or --canonical with more than one
       document or with --check or --dry-run; a file that does not
       exist; input that is not JSON
  3    refused: --canonical on a document JCS cannot carry exactly, or a
       document that declares a version outside the 0.2 line
  With several documents, the most serious status wins: 2, then 3, then
  1, then 0.
  See 'ob help exit-codes'."####;
