// Generated from the ob surface draft's help pages by gen_ob_surface.py; do not edit by hand.

pub(crate) const ROOT_HELP: &str = r####"ob works with OpenBindings interface documents (OBIs): read and check them,
compare them with shared contracts, pull operations in from their sources,
and call the operations they describe.

Usage: ob <command> [options]

Start here:
  ob init tasks.obi.json --name "Task Manager"
                                            start a document (ob fetch gets
                                            a published one)
  ob source add tasks.obi.json httpApi      add a source, such as an OpenAPI
                                            description
  ob source pull tasks.obi.json             pull its operations in
  ob show tasks.obi.json                    see what a document describes
  ob validate tasks.obi.json                check that it conforms
  ob invoke tasks.obi.json completeTask --input '{"id":"t_1"}'
                                            call one of its operations

Edit a document in any editor; ob validate checks the result.

Documents:
  init          Create a new document
  show          Show what a document describes
  validate      Check a document, values, or examples
  diff          Show what changed between two documents
  compat        Check a document against a shared contract
  merge         Bring operations in from another document
  patch         Apply a JSON Patch
  fmt           Format documents

Parts of a document:
  operation     Read, rename, and remove operations
  binding       Read bindings, and add one by hand
  dependency    Read dependencies
  schema        Read, rename, remove, and bundle schemas

Sources:
  source        Add sources and pull what they offer

Using a service:
  fetch         Download a service's OBI
  invoke        Call an operation
  context       Store what calls need beyond their input

This installation:
  kind          See which kinds this ob supports
  delegate      Let other tools do jobs for ob
  describe      Describe this ob: what it is and what it supports

Shell:
  completion    Print a shell completion script

Help topics:
  obi           What an OBI contains, and which command does what
  documents     Paths, standard input, and URLs
  exit-codes    What each exit status means
  output        Text and JSON output

Options:
  -h, --help       Print help (see a summary with '-h')
  -V, --version    Print this ob's version

Run 'ob <command> -h' for a summary, and 'ob help <command>' or
'ob <command> --help' for the full help."####;
pub(crate) const TOPIC_OBI: &str = r####"OBI: what a document contains, and which command does what

An OBI (OpenBindings interface document) is one JSON object that declares
the OpenBindings version it follows and describes a piece of software's
interface in five maps:

  operations     What it can do: each a named capability with optional
                 input and output schemas (JSON Schema 2020-12) for the
                 values a caller sends and receives, plus aliases and
                 examples
  sources        What its operations are realized through: each an exact
                 kind, which names how its data and its bindings' data
                 are read, and that data (an API description, a URL, a
                 command line, or nothing)
  bindings       Each a declaration that an operation is realized through
                 a source, with data read under the source's kind: the
                 author's claim, not a verified fact
  dependencies   Where the software described consumes an operation,
                 optionally limited to some kinds
  schemas        Named JSON Schemas that operations and other schemas
                 reference

The document also carries its own name, interface-version label and
description. Conformance (ob validate) is about the document's shape and
its cross-references, rules OBI-01 to OBI-14; whether a binding's claim
is true, or an example's, is outside it. What a source's or a binding's
data means is its kind's definition, which ob never interprets: it shows
the kind's schemas and records what the kind returns.

The jobs, and the command for each:

  see a document         ob show; ob operation, binding, dependency,
                         source and schema list and show
  check it               ob validate: the rules; --operation for values;
                         --examples; --kinds for source and binding data
  compare                ob diff, what changed; ob compat, whether it
                         serves a shared contract or a dependency
  start one              ob init; ob fetch, a published one; ob merge,
                         from a shared contract
  add a source and
  pull what it offers    ob source add; ob source pull (--check in CI)
  bind by hand           ob binding add
  edit                   your editor, then ob validate; ob patch in
                         scripts; ob operation, schema and source rename
                         and remove
  tidy and sign          ob fmt; ob fmt --canonical
  call an operation      ob invoke; ob context set for what a call needs
                         beyond its input
  this installation      ob kind list and describe; ob delegate; ob
                         describe

Each group's help (ob operation --help, ob source --help, and so on) says
what that part is, in the specification's words."####;
pub(crate) const TOPIC_DOCUMENTS: &str = r####"Documents

Commands that read a document take it as <obi>:

  a path        tasks.obi.json, ./api/tasks.json (any name; ob never
                looks for a document by name)
  -             standard input, read to its end
  a URL         https://api.example.com/tasks.obi.json, fetched as given
  an origin     https://api.example.com (a path of / or none, and no
                query or fragment), looked up at
                /.well-known/openbindings (HTTP Discovery)

Commands that change a document take a path, or - to read standard input
and write the result to standard output.

Sign-in: ob fetches with no credentials of its own. A URL that answers
401 or 403 is one ob cannot read (exit 2): fetch it with a tool that
holds the credential and give ob the result as a file or on standard
input. Context is for bindings, acquired through a kind's challenge and
stored per document and source (ob context --help); a document read from
standard input has no path, so no stored context applies to it, and
ob invoke, ob source add and ob source pull take --context instead.

URLs: ob fetches https and http, follows redirects, admits a response up
to the SDK's default size limit, and refuses no address by range or
name: it runs as the user, on the user's network.

Stored context lives in the OS keychain where there is one, otherwise in
files under ob's per-user config directory, readable only by you.
OB_CONFIG_DIR points ob at another directory for all its per-user state
(context and delegate registrations), kept there as plain files.

Versions: this ob reads OpenBindings 0.2. A document declaring another line,
or a prerelease such as 0.2.0-rc.1, is never checked against 0.2's rules:
ob show -F json prints its bytes, ob fetch saves them, and every other
command refuses it (exit 3).

Changing a document: commands that change one write it in place (or
filter - to standard output), keep every byte they do not change, and
refuse to break a rule the document met (exit 3); a further violation of
a rule it already breaks is written, with a warning. They warn, without
refusing, when they make an example fail that passed before. --dry-run
prints the change instead of writing it: a unified diff, or with -F json
a report holding the RFC 6902 patch. An applied edit prints the same
report after writing, except when it reads - (exit 2).

Conformance: commands that display a document answer from any document they
can read and warn when it does not conform. Commands that rely on what a
document means (checking values or examples, compat, merge, source pull,
invoke, delegate add and set --obi) need a conformant document. Commands
that change a document never break a rule the document met."####;
pub(crate) const TOPIC_EXIT_CODES: &str = r####"Exit status

  0    done, or yes; an edit that is already true is "no change"
  1    a definite no: non-conformant, a value fails, incompatible, drift
       found, the operation failed, no OBI is published there
  2    ob could not do what was asked as given: bad arguments, a name or
       file that does not exist, input it cannot read
  3    refused: ob understood the request and declined it; nothing was
       written, and no operation took effect. Output a binding printed
       before ERR_REFUSED stays printed, and neither CONTEXT_REQUIRED nor
       ERR_REFUSED promises that no request was sent
  4    no verdict: a check could not decide
  130  cancelled

Scripts written for diff or grep read ob the same way: 0 yes, 1 no, 2
trouble. Statuses 3 and 4 separate a refusal and a check with no verdict
from both. Where a check gives some answers and some no verdict, a
definite no wins, except in ob compat, which follows its comparison
profile and warns when that hides a definite no. Each command's help
lists the statuses it uses."####;
pub(crate) const TOPIC_OUTPUT: &str = r####"Output

Results go to standard output; error:, warning: and hint: lines go to
standard error.

-F text (the default) is for people and may change between versions.

-F json is for programs. Each report is one JSON object with a
formatVersion. Within a format version, fields are only added; read the
fields you need and ignore the rest. Fields use the specification's and
interfaces' words. Absent, null, false and 0 stay distinct. Values copied
from a document keep their exact spelling; numbers are never rounded. JSON
carries every fact the text shows.

Some commands print a value rather than a report, whatever -F says:

  ob show -F json, and each part's show   the document, or the part, as
                                          stored
  ob invoke                               output values, or frames
  ob diff --patch                         an RFC 6902 patch
  ob fmt --canonical                      the RFC 8785 bytes
  ob fetch                                the document as served
  ob init -                               the new document

Locations in JSON: pointer (a JSON Pointer), line and column (from 1; the
column counts characters), and offset (bytes, from 0).

Edit reports hold the RFC 6902 patch an edit would apply (with --dry-run)
or applied, and the summary members its help names.

The report schemas, one JSON Schema file per report with ob's
diagnostic codes, are published with ob's source, by format version.

Presence: a member copied from the document keeps the document's
presence; a list ob computes is always present, possibly empty; a member
that does not apply is omitted. In a document that does not conform, an
entry whose shape is wrong appears with unreadable: true and its
location, in place of the facts ob cannot read."####;

/// The -h summary of a command path, cut from its page: usage, first paragraph, commands, options, examples, exit status.
pub(crate) fn summary(path: &str) -> Option<&'static str> {
    match path {
        "ob init" => Some(
            r####"Create a new document

Usage: ob init [options] <obi>

Create an OBI that declares OpenBindings 0.2.0 and has an empty
operations map, and write it to <obi>, laid out as ob fmt lays out a
document. Given - as <obi>, it writes the document to standard output. It
refuses to replace an existing file unless --force is given.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob show" => Some(
            r####"Show what a document describes

Usage: ob show [options] <obi>

Print an overview of a document: its name, interface-version label,
declared OpenBindings version and description; its operations, each with
its identifiers (key and aliases), its bindings and the dependencies that
consume it; then its sources, bindings, dependencies and named schemas.
To see one part in full, use the show command of its part: ob operation
show, ob binding show, ob dependency show, ob source show or ob schema
show.

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
  1    no OBI is published at the origin given
  2    bad arguments, a file or URL that does not exist, input ob cannot
       read, or a network failure
  3    refused: the document declares a version outside the 0.2 line
       (without -F json)
  See 'ob help exit-codes'.
"####,
        ),
        "ob validate" => Some(
            r####"Check a document, values, or examples

Usage: ob validate [options] <obi>...

Check one of three things: each document against the rules of
OpenBindings 0.2, OBI-01 to OBI-14 (the default), and with --kinds also
each source's and binding's data against its kind's schemas; with
--operation and --input or --output, values against that operation's
input or output schema; or, with --examples, each value an example
gives against its operation's schema for that side. The document and
examples checks take several documents at once; --operation takes one.

Options:
      --operation <name>     The operation whose values or examples to
                             check, by key or alias
      --input <value>        An input value to check: JSON, @FILE, or -
                             (repeatable)
      --output <value>       An output value to check: JSON, @FILE, or -
                             (repeatable)
      --examples             Check examples instead of the document
      --kinds                Also check each source's and binding's data
                             against its kind's schemas
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

  # After editing a binding's data by hand: check it against its kind
  ob validate tasks.obi.json --kinds

Exit status:
  0    every document conforms, and with --kinds every entry's data
       satisfies its kind's schema; or every value checked satisfies its
       schema
  1    a document is non-conformant; with --kinds, some entry's data
       fails its kind's schema; some value fails its schema; or no OBI is
       published at an origin given
  2    bad arguments, such as --input without --operation or with
       --examples, --operation with nothing to check or with more than
       one document, --kinds with --operation or --examples, or a value
       that is not JSON; a name, file or URL that does not exist; a
       network failure
  3    refused: a document declares a version outside the 0.2 line; for
       values and examples, a document is non-conformant
  4    no verdict: a document's conformance is undetermined; with
       --kinds, nothing fails but some entry's kind could not be asked;
       for values and examples, nothing fails but a result is undefined
       or no-verdict; or, for values only, nothing fails but a side has
       no schema to check
  With several documents, the most serious status wins: 2, then 3, then
  1, then 4, then 0.
  See 'ob help exit-codes'.
"####,
        ),
        "ob diff" => Some(
            r####"Show what changed between two documents

Usage: ob diff [options] <before> <after>

List the differences between two documents, part by part: the
document's own fields, then operations, sources, bindings, dependencies
and schemas. Each change names the part and its key, says whether it was
added, removed, changed or reordered, and gives a JSON Pointer to what
changed.

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
       reordered; or no OBI is published at an origin given
  2    bad arguments, a file or URL that does not exist, input ob cannot
       read, or a network failure
  3    refused: a document declares a version outside the 0.2 line
  See 'ob help exit-codes'.
"####,
        ),
        "ob compat" => Some(
            r####"Check a document against a shared contract

Usage: ob compat [options] <obi> <shared-contract>

Check <obi> against a shared contract: for each operation of the shared
contract, which of <obi>'s operations its published names identify, and
whether their input and output schemas are compatible under the Schema
Comparison Profile OB-2020-12. With --dependency, check instead whether <obi> can
serve a dependency that <shared-contract> declares.

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
       published at an origin given
  2    bad arguments, such as --dependency with --operation or
       --check-as; a name, file or URL that does not exist, such as a
       dependency key <shared-contract> does not have; input ob cannot
       read; a network failure
  3    refused: a document is non-conformant or declares a version
       outside the 0.2 line
  4    indeterminate, a comparison that errored included, even when an
       operation is also incompatible or missing, which a warning names;
       with --dependency, no operation serves and some gives no verdict;
       or a document's conformance is undetermined
  See 'ob help exit-codes'.
"####,
        ),
        "ob merge" => Some(
            r####"Bring operations in from another document

Usage: ob merge [options] <obi> <from>

Copy operations from <from> into <obi> with the schemas, bindings and
sources they need: every named schema their input and output schemas
reach through references, transitively; their bindings; and the sources
those bindings go through. Dependencies are not copied: each is a
consumption point of the software <from> describes. Each operation keeps its key
and aliases, so an operation copied from a shared contract carries its
published names, and so claims correspondence with the operation they
identify there. --operation limits the merge to the operations it names,
by key or alias in <from>. --no-bindings copies operations and their
schemas without bindings or sources.

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
      --dry-run            Write nothing; print the change
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
  1    no OBI is published at the origin given as <from>
  2    bad arguments, such as a URL as <obi>, --ours with --theirs,
       --onto without exactly one --operation, or -F json without
       --dry-run when <obi> is -; a name, file or URL that does not
       exist; input ob cannot read; a network failure
  3    refused: a conflict without --ours or --theirs; a source key both
       documents hold with different data; an incoming operation whose
       identifiers identify more than one operation of <obi>; a copied
       operation's reference that reaches into an operation, or a named
       schema, that the result keeps from <obi>, where <obi> holds no
       identical schema; --theirs replacing a named schema that
       operations outside the merge use; --onto with schemas established
       as incompatible (a hint gives the ob patch command that adds the
       alias), or with a name that already identifies another operation;
       a result that would violate a rule; a document that is
       non-conformant or whose conformance is undetermined; a version
       outside the 0.2 line
  See 'ob help exit-codes'.
"####,
        ),
        "ob patch" => Some(
            r####"Apply a JSON Patch

Usage: ob patch [options] <obi> <patch>

Apply an RFC 6902 JSON Patch to a document. The patch is read from the
file <patch>, or from standard input when <patch> is -. Its paths are
JSON Pointers into the document (RFC 6901), such as
/bindings/createTask.http/preference; a / inside a key is written ~1.

Options:
      --dry-run            Write nothing; print the change
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
  See 'ob help exit-codes'.
"####,
        ),
        "ob fmt" => Some(
            r####"Format documents

Usage: ob fmt [options] <obi>...

Rewrite each document with two-space indentation and ob's field order.
The root object, and each of its top-level maps that is not empty
(operations, sources, bindings, dependencies, schemas), always puts its
members one to a line, so the first member added never reflows the file.
Below them, an object or array is written on one line when that line is
at most 80 columns, and otherwise has its members one to a line.

Options:
      --check              Change nothing; exit 1 if any file would change
      --canonical          Print the RFC 8785 form of one document
      --dry-run            Write nothing; print the change
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
  See 'ob help exit-codes'.
"####,
        ),
        "ob operation" => Some(
            r####"Read, rename, and remove operations

Usage: ob operation <command> [options]

An operation is a named, protocol-independent capability under a key in
operations: what a service can do, with optional input and output
schemas that govern the values a caller sends it and receives from it
(§3, §5.1). Its key and its aliases are its identifiers, and a name
identifies the operation when it equals one of them (§5.1). Bindings
declare realizations of an operation (§5.3), and dependencies name
points where the software described consumes one (§5.4).

Commands:
  list      List operations, and how each is realized and consumed
  show      Show one operation, named by its key or an alias
  rename    Change an operation's key and every reference to it
  remove    Remove an operation

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob operation <command> --help' for details.
"####,
        ),
        "ob operation list" => Some(
            r####"List operations, and how each is realized and consumed

Usage: ob operation list [options] <obi>

List each operation in document order: its key, its aliases, whether it
states an input schema and an output schema, the bindings that declare
realizations of it, the dependencies that consume it, and whether it is
deprecated.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob operation show" => Some(
            r####"Show one operation, named by its key or an alias

Usage: ob operation show [options] <obi> <name>

Show the operation that <name> identifies, by its key or any of its
aliases (§5.1): its description, identifiers, input and output schemas,
examples, bindings and dependencies. --example shows one of its examples
alone.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob operation rename" => Some(
            r####"Change an operation's key and every reference to it

Usage: ob operation rename [options] <obi> <key> <new-key>

Change the key of the operation <key> to <new-key>, and update every
reference to it in the document: the operation field of each binding
and dependency that names it, and each same-document reference in the
document resource that points into the operation, however it is
spelled, such as {"$ref": "#/operations/createTask/input"} (§7.3).
--keep-alias also keeps the old key as an alias, so it still identifies
the operation.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob operation remove" => Some(
            r####"Remove an operation

Usage: ob operation remove [options] <obi> <key>

Remove the operation <key>, with its aliases and examples. Its names
stop identifying anything, and any correspondence it claimed through them
goes with it.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob binding" => Some(
            r####"Read bindings, and add one by hand

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

Commands:
  list    List bindings, with their operation, source and kind
  show    Show one binding
  add     Add a binding, as its source's kind describes it

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob binding <command> --help' for details.
"####,
        ),
        "ob binding list" => Some(
            r####"List bindings, with their operation, source and kind

Usage: ob binding list [options] <obi>

List each binding in document order: its key, the operation it binds,
the source it names, that source's kind, and the author's claim and
signals about it: its idempotent claim, its preference, and whether it is
deprecated. A binding whose source is not in the document shows no kind.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob binding show" => Some(
            r####"Show one binding

Usage: ob binding show [options] <obi> <key>

Show the binding under <key>: the operation it binds, the source it
names and that source's kind, its data, its idempotent claim, its
preference, whether it is deprecated, and its description.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob binding add" => Some(
            r####"Add a binding, as its source's kind describes it

Usage: ob binding add [options] --operation <name> --source <key> <obi> <key>

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob dependency" => Some(
            r####"Read dependencies

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

Commands:
  list    List dependencies, with their operation and kinds constraint
  show    Show one dependency

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob dependency <command> --help' for details.
"####,
        ),
        "ob dependency list" => Some(
            r####"List dependencies, with their operation and kinds constraint

Usage: ob dependency list [options] <obi>

List each dependency in document order: its key, which identifies the
consumption point, the operation it consumes, and its kinds constraint.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob dependency show" => Some(
            r####"Show one dependency

Usage: ob dependency show [options] <obi> <key>

Show the dependency under <key>, the consumption point that key
identifies: the operation it consumes, its kinds constraint, and its
description.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob schema" => Some(
            r####"Read, rename, remove, and bundle schemas

Usage: ob schema <command> [options]

The schemas map holds named JSON Schemas, which operations and other
schemas reference, as in {"$ref": "#/schemas/Task"} (§5.5). Every schema
the document contains is read as JSON Schema 2020-12, and its references
resolve as §7 defines. An external schema is one a reference in the
document identifies, once resolved (against the enclosing $id, inside a
schema that declares one), that the document does not contain.

Commands:
  list      List named schemas, what references each, and external schemas
  show      Show one named schema
  rename    Rename a named schema and every same-document reference to it
  remove    Remove a named schema that nothing references
  bundle    Embed the external schemas the document references

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob schema <command> --help' for details.
"####,
        ),
        "ob schema list" => Some(
            r####"List named schemas, what references each, and external schemas

Usage: ob schema list [options] <obi>

List each named schema in document order, with its $id when it declares
one, and every reference to it: each $ref or $dynamicRef that identifies
the schema or a schema inside it, by JSON Pointer, plain name or $id,
shown with the reference's location.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob schema show" => Some(
            r####"Show one named schema

Usage: ob schema show [options] <obi> <key>

Show the schema under <key> in schemas, as stored, with its $id when it
declares one and the references to it, as ob schema list shows them.
References inside the schema are shown as written, not expanded.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob schema rename" => Some(
            r####"Rename a named schema and every same-document reference to it

Usage: ob schema rename [options] <obi> <key> <new-key>

Change the key of the schema <key> in schemas to <new-key>, and update
every same-document reference to it in the document resource: each $ref
and $dynamicRef whose JSON Pointer reaches the schema or a schema inside
it, such as #/schemas/Task or #/schemas/Task/properties/id. That includes
spellings that identify it once percent-decoded, such as
#%2Fschemas%2FTask (§7.3). An updated reference keeps its spelling except
for the name.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob schema remove" => Some(
            r####"Remove a named schema that nothing references

Usage: ob schema remove [options] <obi> <key>

Remove the schema <key> from schemas.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob schema bundle" => Some(
            r####"Embed the external schemas the document references

Usage: ob schema bundle [options] <obi> [<uri>...]

Fetch each external schema the document references (resolved against any
enclosing $id), by its URI without the fragment, and embed it under schemas
with that URI as its $id. References do not change: references to the URI
now resolve to the embedded copy, as JSON Schema's bundling defines (§7.2,
§7.4), so the document no longer needs that schema from outside. External
schemas that a copy references in turn are bundled too. Given <uri>
arguments, it bundles only those, and what they reference in turn.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob source" => Some(
            r####"Add sources and pull what they offer

Usage: ob source <command> [options]

A source is the document's name for something its bindings realize
operations through, read under one kind (§5.2): an entry under a key in
sources, with an exact kind, which names how the source's data and its
bindings' data are read (§6), and optional data, whatever the kind
defines for it: an embedded description such as an OpenAPI document, the
URL of one read on every use, the command line of a server, or nothing at
all. Bindings name a source and carry data of their own, read under the
same kind (§5.3).

Commands:
  list      List sources, with their kinds and what this ob supports
  show      Show one source
  add       Add a source, as its kind describes it
  pull      Pull what a source offers into the document
  rename    Rename a source and update the bindings that name it
  remove    Remove a source

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob source <command> --help' for details.
"####,
        ),
        "ob source list" => Some(
            r####"List sources, with their kinds and what this ob supports

Usage: ob source list [options] <obi>

List each source in document order: its key, its kind, its description,
the number of bindings that name it, and which of check, pull and invoke
this ob supports for its kind, built in or through a delegate, as ob kind
list reports it. A source whose kind this ob supports for nothing is still
listed; a document may use any kind.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob source show" => Some(
            r####"Show one source

Usage: ob source show [options] <obi> <key>

Show one source: its kind, its description, its data exactly as stored,
and the bindings that name it. ob does not read the data under the kind
here, so show works for any kind. -F json prints the stored source
exactly.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob source add" => Some(
            r####"Add a source, as its kind describes it

Usage: ob source add [options] <obi> <key>

Add a source under <key>, in the form its kind describes. ob asks the
Kind Descriptor for the kind's source-data schema (what ob kind describe
shows) and fills it: from --data, or at a terminal from what you enter
for the schema's titled branches and members. It then has the kind check the
entry (the Kind Descriptor's checkSource) and return the one the kind
stands behind, completed as the kind defines (a description fetched and
embedded, a server's name read, a default filled), and writes that. What
the data means is the kind's; ob records what the kind returns.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob source pull" => Some(
            r####"Pull what a source offers into the document

Usage: ob source pull [options] <obi> [<key>...]

Read each source named, under its kind, and bring what it offers into the
document: for each item the source offers, an operation, the schemas it
needs, and a binding naming the source. With no <key>, every source this
ob can pull is pulled, one at a time, and any other is skipped and named.
Pull reads the source as the document holds it: an embedded description
from the document, a referenced one from where its kind says, and
nothing else; what a kind consults is its definition, which ob kind
describe shows.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob source rename" => Some(
            r####"Rename a source and update the bindings that name it

Usage: ob source rename [options] <obi> <key> <new-key>

Change the key of the source <key> to <new-key>, and update every binding
that names it to name the new key. Bindings are the only parts of a
document that name a source. Context stored for the source (ob context
set) moves with it, so the renamed source keeps what was stored; a dry
run moves nothing.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob source remove" => Some(
            r####"Remove a source

Usage: ob source remove [options] <obi> <key>

Remove the source <key>. It refuses while bindings name the source, and
lists them; --cascade removes those bindings too. Operations stay: one
whose last binding is removed is no longer realized in this document.
Context stored for the source (ob context set) is removed with it, and
the summary says so; a dry run removes nothing.

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
  See 'ob help exit-codes'.
"####,
        ),
        "ob fetch" => Some(
            r####"Download a service's OBI

Usage: ob fetch [options] <url>

Download the OBI a service publishes and print it to standard output,
byte for byte as served, or save it with -o. A bare origin such as
https://api.example.com is looked up at /.well-known/openbindings (HTTP
Discovery 0.1); any other URL is fetched as given. ob follows redirects.

Options:
  -o, --out <path>    Save the document to <path> instead of printing it
      --force         Replace an existing file at <path>
  -h, --help          Print help (see a summary with '-h')

Examples:
  ob fetch https://api.example.com -o tasks.obi.json
  ob fetch https://api.example.com -o tasks.obi.json --force
  ob fetch https://api.example.com/tasks.obi.json | jq '.operations | keys'

Exit status:
  0    fetched
  1    no OBI is published at the origin (its well-known path answered
       404)
  2    the URL was not found (a 404 at any other URL), the server asked
       for sign-in (401 or 403) or answered with another error, the
       network failed, or the response declares no OpenBindings version
       (it is not JSON, or names no version)
  3    refused, nothing saved: -o names an existing file and --force is
       not given
  See 'ob help exit-codes'.
"####,
        ),
        "ob invoke" => Some(
            r####"Call an operation

Usage: ob invoke [options] <obi> <operation>

Call an operation, named by its key or one of its aliases, through one of
its bindings, from a conformant document (ob help documents). ob writes
each --input value to the call and prints each output value, as one line
of JSON, as it arrives. What the binding needs beyond the input comes
from ob's context store, or from --context for this call alone.
--preflight calls nothing and prints what the binding knows it will ask
for.

Options:
      --input <value>         A value to write: JSON, @FILE, or - to
                              stream from standard input (repeatable,
                              in order)
      --binding <key>         Use this binding; repeat for an ordered list
      --context <@FILE|->     Context for this call, a JSON object, in
                              place of the stored context
      --frames                Print every frame the invocation returns
      --preflight             Print the binding's challenge, acquire what
                              is missing at a terminal, call nothing
      --timeout <duration>    A deadline for the call, such as 30s; none
                              unless given
  -h, --help                  Print help (see a summary with '-h')

Examples:
  ob invoke tasks.obi.json completeTask --input '{"id":"t_1"}'
  ob invoke tasks.obi.json createTask --binding createTask.http \
      --input '{"title":"Plan the release"}'
  ob invoke tasks.obi.json acme.tasks.listTasks --frames
  ob invoke https://api.example.com completeTask --input '{"id":"t_1"}' \
      --timeout 30s
  ob invoke tasks.obi.json createTask --binding createTask.mcp --preflight
  ob invoke tasks.obi.json listTasks --context @ci-context.json

Exit status:
  0    the call completed; with --preflight, no requirement the binding
       knows of is missing
  1    the call began but did not complete: the operation reported an
       error, an output value failed or got no verdict, an input value
       from @FILE or - did either after the call opened, or the deadline
       passed (any of these may have taken effect); or no OBI is
       published at the origin given
  2    bad arguments (such as --frames with --preflight, or inline JSON
       as --context), an operation name or binding key that identifies
       nothing, a file that does not exist, or input ob cannot read
  3    refused, with no effect. ob refused before sending: the document
       does not conform (ERR_DOCUMENT_NON_CONFORMANT), its conformance is
       undetermined (ERR_DOCUMENT_UNDETERMINED), or it declares another
       OpenBindings line (ERR_UNSUPPORTED_VERSION); an input or output
       schema references a schema the document does not contain
       (ERR_EXTERNAL_SCHEMA); an inline input value failed or got no
       verdict;
       context the binding needs is missing and ob cannot ask for it; or
       the operation has no binding ob can invoke, or several and none was
       chosen. Or the binding answered CONTEXT_REQUIRED or ERR_REFUSED
       (output printed before ERR_REFUSED stays printed). With --preflight,
       also: a requirement the binding knows of is missing and ob cannot
       ask for it, or its preflight answered ERR_REFUSED
  4    with --preflight: the binding could not answer
  130  cancelled (Ctrl-C)
  See 'ob help exit-codes'.
"####,
        ),
        "ob context" => Some(
            r####"Store what calls need beyond their input

Usage: ob context <command> [options]

Context is what a realization needs that the operation is not about: a
credential, an approval, a configuration value; the specification calls
the values an operation is about caller-facing (§3), and the context
document published with the interfaces defines the rest. A binding that
lacks it asks with a CONTEXT_REQUIRED challenge: a message saying why,
and a JSON Schema of the object to supply next time. The kind describes;
ob acquires, at a terminal or from what you stored; the kind consumes.
The object's members are whatever that schema described; ob defines
none of them, and never writes context into a document.

Commands:
  set       Store the context for a source
  list      List the sources that have stored context
  show      Show the context stored for a source
  remove    Remove the context stored for a source

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob context <command> --help' for details.
"####,
        ),
        "ob context set" => Some(
            r####"Store the context for a source

Usage: ob context set <obi> <source> <value>

Store the context object for one source of a document: the JSON object a
challenge's schema described, read from <value>: - for standard input,
or @FILE. Secrets never go on the command line, where shell history
keeps them, so inline JSON is refused (exit 2). At a terminal, give -
and ob reads until the input ends. Storing what is already stored is
"no change"; storing replaces the whole object.

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  # From a secret in the environment, at a terminal or in a script
  jq -n '{accessToken: env.TASKS_TOKEN}' \
      | ob context set tasks.obi.json httpApi -

  # From a file a CI job provides
  ob context set tasks.obi.json httpApi @ci-context.json

  # Pasted at a terminal, ending with Ctrl-D
  ob context set tasks.obi.json httpApi -

Exit status:
  0    stored, or no change
  2    bad arguments, such as inline JSON as <value>, a file that does not
       exist, or input that is not a JSON object
  See 'ob help exit-codes'.
"####,
        ),
        "ob context list" => Some(
            r####"List the sources that have stored context

Usage: ob context list [options]

List each document and source that has stored context, and the members
of each object, with every value masked. -F json prints contexts, each
with document, source and context, the object with each value shown as
{"masked":true}. ob context show shows one.

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob context list
  ob context list -F json | jq -r '.contexts[] | "\(.document) \(.source)"'

Exit status:
  0    listed, including when nothing is stored
  2    bad arguments
  See 'ob help exit-codes'.
"####,
        ),
        "ob context show" => Some(
            r####"Show the context stored for a source

Usage: ob context show [options] <obi> <source>

Show the context object stored for one source of a document. Every value
is masked, since any member can hold a secret; --reveal prints them, and
what it prints may contain secrets. -F json prints document, source and
context, the object with each hidden value shown as {"masked":true};
with --reveal, extract .context to reuse it with ob context set.

Options:
      --reveal             Print the values instead of masking them
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob context show tasks.obi.json httpApi
  ob context show tasks.obi.json httpApi --reveal -F json \
      | jq .context > context.json
  ob context set staging.obi.json httpApi @context.json

Exit status:
  0    shown
  2    bad arguments, or nothing is stored for that document and source
  See 'ob help exit-codes'.
"####,
        ),
        "ob context remove" => Some(
            r####"Remove the context stored for a source

Usage: ob context remove <obi> <source>

Remove the context object stored for one source of a document. To change
a member, store the whole object again with ob context set.

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob context remove tasks.obi.json httpApi

Exit status:
  0    removed
  2    bad arguments, or nothing is stored for that document and source
  See 'ob help exit-codes'.
"####,
        ),
        "ob kind" => Some(
            r####"See which kinds this ob supports

Usage: ob kind <command> [options]

A kind is the exact string a source carries to say how its data and its
bindings' data are read (§6). Kinds are compared whole: example.openapi@1
and example.openapi@2 are unrelated kinds, and so are two strings that
differ only in case. Support is per exact kind and per operation, built
in or through a delegate: check, pull and invoke are ob's names for the Kind
Descriptor's checkSource (ob source add), the Source Puller's pullSource
(ob source pull) and the Binding Invoker's invokeBinding (ob invoke), so
ob may support a kind for invoke and not for pull. Where more than one
provider carries a kind, the descriptor ob consults for it, for the
schemas ob source add and ob binding add prompt from and for its check,
is the one of the provider ob would pull the kind with, or, where nothing
pulls it, invoke it with; ob delegate resolve shows which that is. These
commands report what this installation can do with a kind; they never
judge a document, which may use any kind.

Commands:
  list        List the kinds this ob supports, and the operations for
              each
  describe    Describe a kind: what it reads, its check, and the schemas
              of its data

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob kind <command> --help' for details.
"####,
        ),
        "ob kind list" => Some(
            r####"List the kinds this ob supports, and the operations for each

Usage: ob kind list [options]

List each kind this ob supports: the operations it supports it for
(check, pull, invoke) and whether that support is built in or comes from
a delegate, given by its registration ID. A kind supported by more than
one provider is listed once for each; ob delegate resolve says which one
ob would use. A document can use any kind; this only says what this
installation can do with one.

Options:
      --supports <op>      Only kinds supported for this operation:
                           check, pull, or invoke
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob kind list
  ob kind list --supports pull
  ob kind list -F json | jq -r '.kinds[]
      | select(.supports | index("openbindings.binding-invoker.invokeBinding"))
      | .kind'

Exit status:
  0    listed, including when no kind is supported
  2    bad arguments, such as an operation ob does not know
  See 'ob help exit-codes'.
"####,
        ),
        "ob kind describe" => Some(
            r####"Describe a kind: what it reads, its check, and the schemas of its data

Usage: ob kind describe [options] <kind>

Describe an exact kind as its Kind Descriptor describes it, from the
provider ob consults for the kind: built-in support, or the delegate ob
would pull the kind with, or, where nothing pulls it, invoke it with (ob
delegate resolve shows which): a description of what the kind
reads and how its bindings reach their targets, the operations the
provider supports it for, what its check does and whether that runs
anything (ob source add asks before a check that executes), its
source-data schema, and its binding-data schema. The schemas are what
ob source add and ob binding add prompt from and check against; an agent
fills them as it fills any JSON Schema. The kind is compared whole, so
example.openapi@1 says nothing about example.openapi@2.

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob kind describe example.openapi@1
  ob kind describe example.mcp@1 -F json | jq .sourceData
  ob kind describe example.openapi@1 -F json | jq .bindingData

Exit status:
  0    described
  1    nothing supports the kind
  2    bad arguments
  4    no verdict: nothing that could be asked supports the kind, and a
       registered delegate could not be asked
  See 'ob help exit-codes'.
"####,
        ),
        "ob delegate" => Some(
            r####"Let other tools do jobs for ob

Usage: ob delegate <command> [options]

A delegate is another tool, described by its own OBI, that does a job for
ob (pull or invoke), so that this ob supports more kinds. Registering one
checks that its operations correspond to an interface the job accepts,
keeps a copy of its OBI, and gives the registration an ID and a
preference per job, as the Delegate Manager interface describes. A
delegate for either job also implements the Kind Descriptor for its
kinds, which answers ob kind list and ob kind describe and checks a
source entry for ob source add; where several providers carry a kind,
ob consults the descriptor of the one it would pull the kind with, or,
where nothing pulls it, invoke it with.

Commands:
  jobs       List the jobs ob accepts delegates for
  add        Register a delegate
  set        Change a registration
  remove     Remove a registration
  list       List registrations
  show       Show a registration
  resolve    Explain what ob would use for a job on a kind

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob delegate <command> --help' for details.
"####,
        ),
        "ob delegate jobs" => Some(
            r####"List the jobs ob accepts delegates for

Usage: ob delegate jobs [options]

List the jobs ob accepts delegates for, each with its description and its
accepted interfaces. ob accepts Source Puller for pull and Binding
Invoker for invoke, each together with Kind Descriptor for the kinds the
delegate supports.

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob delegate jobs
  ob delegate jobs -F json \
      | jq '.jobs[] | select(.id == "invoke") | .acceptedInterfaces[0]' \
      > invoke-job.obi.json
  ob compat grpc-invoker.obi.json invoke-job.obi.json

Exit status:
  0    listed
  2    bad arguments
  See 'ob help exit-codes'.
"####,
        ),
        "ob delegate add" => Some(
            r####"Register a delegate

Usage: ob delegate add [options] --job <job>... <delegate-obi>

Register another tool as a delegate for one or more jobs. ob reads the
tool's OBI, which must be conformant (ob help documents), keeps a copy
exactly as given, and checks, for each job requested, that the OBI's
operations correspond to one of the job's accepted interfaces (ob
delegate jobs). Admission proceeds on the delegate's correspondence
claim: it is refused only when an accepted interface's operation key
identifies no operation of the OBI, or a comparison establishes that two
contracts are incompatible. A comparison that cannot decide does not
refuse; ob reports it in a warning. Every job requested is admitted, or
nothing is registered. Registering calls nothing.

Options:
      --job <job>                  A job to register it for: pull or
                                   invoke (required, repeatable)
      --preference <JOB=NUMBER>    Its preference for a job (repeatable)
  -F, --format <format>            Output format: text or json
                                   [default: text]
  -h, --help                       Print help (see a summary with '-h')

Examples:
  ob delegate add grpc-invoker.obi.json --job invoke --preference invoke=10
  ob delegate add https://tools.example.com --job pull --job invoke
  ob delegate add slow-grpc.obi.json --job invoke --preference invoke=-0.5

Exit status:
  0    registered
  1    no OBI is published at the URL given
  2    bad arguments (no --job, a job requested twice, a preference for a
       job not requested, a preference that is not a finite number), a
       file that does not exist, or a document ob cannot read
  3    refused, nothing registered: the OBI declares another OpenBindings
       line (ERR_UNSUPPORTED_VERSION), does not conform
       (ERR_DOCUMENT_NON_CONFORMANT), or its conformance is undetermined
       (ERR_DOCUMENT_UNDETERMINED); or it does not correspond to an
       accepted interface of a requested job
  See 'ob help exit-codes'.
"####,
        ),
        "ob delegate set" => Some(
            r####"Change a registration

Usage: ob delegate set [options] <id>

Change a registered delegate: replace its OBI, change its jobs, or set
its preference for a job. Only the options you give change anything, and
a change that is already true is "no change". The whole change applies,
or nothing does.

Options:
      --obi <delegate-obi>         Replace the delegate's OBI: a path, -
                                   for standard input, or a URL
      --add-job <job>              Register it for another job (repeatable)
      --remove-job <job>           Stop using it for a job (repeatable)
      --preference <JOB=NUMBER>    Set its preference for a job
                                   (repeatable)
      --unset <preference.JOB>     Remove its explicit preference for a job
                                   (repeatable)
  -F, --format <format>            Output format: text or json
                                   [default: text]
  -h, --help                       Print help (see a summary with '-h')

Examples:
  ob delegate set d_91c2 --preference invoke=20
  ob delegate set d_91c2 --add-job pull
  ob delegate set d_91c2 --unset preference.invoke
  ob delegate set d_91c2 --obi grpc-invoker.obi.json

Exit status:
  0    changed, or no change
  1    no OBI is published at the --obi URL
  2    bad arguments, contradictory changes, removing the last job, no
       registration with that ID, a file that does not exist, or a
       document ob cannot read
  3    refused, nothing changed: the new OBI declares another OpenBindings
       line (ERR_UNSUPPORTED_VERSION), does not conform
       (ERR_DOCUMENT_NON_CONFORMANT), or its conformance is undetermined
       (ERR_DOCUMENT_UNDETERMINED); or it does not correspond to an
       accepted interface of a job
  See 'ob help exit-codes'.
"####,
        ),
        "ob delegate remove" => Some(
            r####"Remove a registration

Usage: ob delegate remove <id>

Remove a registered delegate. ob stops using it for every job and
deletes its copy of the delegate's OBI and its preferences. An ID that
no registration has is already absent: ob says so and exits 0, as the
Delegate Manager's own operation does, so a script that removes what it
registered can run twice.

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob delegate remove d_7f3a
  ob delegate list --job invoke -F json \
      | jq -r '.delegates[].id' | xargs -n1 ob delegate remove

Exit status:
  0    removed, or already absent
  2    bad arguments
  See 'ob help exit-codes'.
"####,
        ),
        "ob delegate list" => Some(
            r####"List registrations

Usage: ob delegate list [options]

List registered delegates: each registration's ID, the name and version
label of its OBI, its jobs, and its explicit preferences. --job lists
only the registrations for that job. -F json prints each registration in
full, with its retained OBI.

Options:
      --job <job>          Only registrations for this job: pull or invoke
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob delegate list
  ob delegate list --job invoke
  ob delegate list -F json | jq '.delegates[] | {id, jobPreferences}'

Exit status:
  0    listed, including when nothing is registered
  2    bad arguments
  See 'ob help exit-codes'.
"####,
        ),
        "ob delegate show" => Some(
            r####"Show a registration

Usage: ob delegate show [options] <id>

Show one registered delegate: its OBI's name, version label and
operations, its jobs, and its explicit preferences. -F json prints a
report whose delegate member is the registration: id, jobs,
jobPreferences, and interface, the retained OBI exactly as given, with
its schemas, sources, bindings and extension fields. Extract
.delegate.interface to reuse it with ob delegate set --obi -.

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob delegate show d_91c2
  ob delegate show d_91c2 -F json \
      | jq .delegate.interface > grpc-invoker.obi.json

Exit status:
  0    shown
  2    bad arguments, or no registration with that ID
  See 'ob help exit-codes'.
"####,
        ),
        "ob delegate resolve" => Some(
            r####"Explain what ob would use for a job on a kind

Usage: ob delegate resolve [options] --job <job> --kind <kind>

Explain what ob would use for a job on an exact kind, its built-in
support or a delegate (given by its registration ID), and why. ob asks
each delegate registered for the job whether it supports the kind for
the job's operation (its Kind Descriptor), then applies the job's rule,
which ob delegate jobs prints.

Options:
      --job <job>          The job: pull or invoke (required)
      --kind <kind>        The exact kind (required)
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob delegate resolve --job invoke --kind example.grpc@1
  ob delegate resolve --job pull --kind example.openapi@1 -F json

Exit status:
  0    explained, including when nothing supports the kind
  2    bad arguments, including a missing --job or --kind
  See 'ob help exit-codes'.
"####,
        ),
        "ob describe" => Some(
            r####"Describe this ob: what it is and what it supports

Usage: ob describe [options]

Describe this ob as the Software Descriptor interface's describe operation
does: its name, version, description, homepage, repository and
maintainer. Then what it supports: the OpenBindings line it reads (0.2)
and the specification release and revision it applies, the version of
HTTP Discovery it follows, the interfaces and shared documents it follows
or uses, each with its version (Operation Invoker, Binding Invoker,
Source Puller, Kind Descriptor, the context document, Delegate Manager
and Software Descriptor), the comparison profiles it claims, each with
its version (OB-2020-12 0.1, which ob compat stamps on its reports), and
the kinds it supports built in, with the operations for each.

Options:
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob describe
  ob describe -F json | jq '{name, version, repository}'
  ob describe -F json | jq .openbindings
  ob describe -F json | jq .profiles

Exit status:
  0    described
  2    bad arguments
  See 'ob help exit-codes'.
"####,
        ),
        "ob help" => Some(
            r####"Print help for a command or a topic

Usage: ob help [<command>...]

Print the full help of a command, named by its words (ob help source
pull), or a help topic: obi (what an OBI contains, and which command does
what), documents (paths, standard input and URLs), exit-codes (what each
exit status means) and output (text and JSON output). With no argument,
print ob's root help, as ob --help does.

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob help
  ob help obi
  ob help source pull
  ob help source pull -h
  ob help exit-codes

Exit status:
  0    printed
  2    the words name no command or topic
  See 'ob help exit-codes'.
"####,
        ),
        "ob completion" => Some(
            r####"Print a shell completion script

Usage: ob completion <shell>

Print a completion script for bash, zsh, fish or powershell on standard
output. It completes command names, option names and fixed choices, such
as -F text|json and the names of help topics. It does not complete names
read from a document, such as operation keys, since completing them
would mean reading the document at each keystroke.

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob completion zsh
  ob completion bash > ~/.local/share/bash-completion/completions/ob

Exit status:
  0    printed
  2    the shell is not one of the four
  See 'ob help exit-codes'.
"####,
        ),
        _ => None,
    }
}
