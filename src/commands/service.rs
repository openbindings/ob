// Generated from the ob surface draft's help pages by gen_ob_surface.py; do not edit by hand.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![
        cmd_fetch(),
        cmd_invoke(),
        cmd_context(),
        cmd_kind(),
        cmd_delegate(),
        cmd_describe(),
    ]
}

pub(super) fn cmd_fetch() -> Command {
    leaf(
        "fetch",
        "Download a service's OBI",
        PAGE_FETCH,
        "ob fetch [options] <url>",
    )
    .arg(positional(
        "url",
        "An https:// or http:// URL, or a bare origin",
        true,
        false,
    ))
    .arg(
        text("out", "Save the document to <path> instead of printing it")
            .value_name("path")
            .short('o'),
    )
    .arg(boolean("force", "Replace an existing file at <path>"))
}
const PAGE_FETCH: &str = r####"Download a service's OBI

Usage: ob fetch [options] <url>

Download the OBI a service publishes and print it to standard output,
byte for byte as served, or save it with -o. A bare origin such as
https://api.example.com is looked up at /.well-known/openbindings (HTTP
Discovery 0.1); any other URL is fetched as given. ob follows redirects.

ob sends no credentials of its own: a URL that answers 401 or 403 is one
ob cannot read (exit 2). Fetch such a document with a tool that can sign
in and give ob the result on standard input, as every command that takes
a document accepts it (curl -H 'Authorization: Bearer ...' URL | ob show -).

ob saves any response that declares an OpenBindings version (§8.1),
whatever the line: one that declares another line unchanged, with a
warning that this ob reads 0.2, and a 0.2-line document that does not
conform, or whose conformance is undetermined, with a warning naming the
rules and pointing to ob validate. A response that is not JSON, or
declares no version, is not saved.

Commands that read a document, such as ob show and ob invoke, also take a
URL and fetch it the same way; fetch is for keeping a copy.

Arguments:
  <url>    An https:// or http:// URL, or a bare origin

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_invoke() -> Command {
    leaf("invoke", "Call an operation", PAGE_INVOKE, "ob invoke [options] <obi> <operation>")
        .arg(positional("obi", "The document: a path, - for standard input, or a URL (ob help documents)", true, false))
        .arg(positional("operation", "The operation's key or one of its aliases", true, false))
        .arg(repeated("input", "A value to write: JSON, @FILE, or - to stream from standard input (repeatable, in order)").value_name("value"))
        .arg(repeated("binding", "Use this binding; repeat for an ordered list").value_name("key"))
        .arg(text("context", "Context for this call, a JSON object, in place of the stored context").value_name("@FILE|-"))
        .arg(boolean("frames", "Print every frame the invocation returns"))
        .arg(boolean("preflight", "Print the binding's challenge, acquire what is missing at a terminal, call nothing"))
        .arg(duration("timeout", "A deadline for the call, such as 30s; none unless given"))
}
const PAGE_INVOKE: &str = r####"Call an operation

Usage: ob invoke [options] <obi> <operation>

Call an operation, named by its key or one of its aliases, through one of
its bindings, from a conformant document (ob help documents). ob writes
each --input value to the call and prints each output value, as one line
of JSON, as it arrives. What the binding needs beyond the input comes
from ob's context store, or from --context for this call alone.
--preflight calls nothing and prints what the binding knows it will ask
for.

An invocation is an exchange, whatever the operation's shape: ob opens it,
writes your input values to it, prints each output value as it arrives,
and finishes when the binding does. One value in and one out, a stream in,
a stream out, or both at once all work the same way.

Input: each --input writes, in order; then ob closes the input.
  --input VALUE   one value (JSON)
  --input @FILE   each JSON value in the file
  --input -       each JSON value read from standard input, as it
                  arrives, until it ends (Ctrl-D at a terminal)
  no --input      nothing; ob closes the input at once

Output: each output value, as one line of JSON, as it arrives. --frames
prints every frame the invocation returns instead, one per line, exactly as
the Operation Invoker interface defines them and with nothing added: each
output, the binding closing its input early, and the final complete or error
frame. A refusal ends with its error frame too; a CONTEXT_REQUIRED frame
carries the challenge as its data, and the binding the call went through is
on standard error, as for every call, so a script knows where to store the
answer. Without --frames, an error's message, and any data it carries, go to
standard error. The message ends with the error's code, which its error
frame carries too: an interface's code where one defines the failure, such
as CONTEXT_REQUIRED, and for ob's own refusals ob's own code for the class
of refusal, as Exit status lists them: ERR_DOCUMENT_NON_CONFORMANT,
ERR_DOCUMENT_UNDETERMINED, ERR_UNSUPPORTED_VERSION or ERR_EXTERNAL_SCHEMA.

Choosing a binding: --binding names one; repeat it to give an ordered
list, and ob uses the first one it can invoke (ob supports its source's
kind for invoke), saying on standard error which one carried the call.
A binding of another operation is refused, and so is a list with none ob
can invoke; ob never falls back to a binding you did not name. Without
--binding, ob uses the operation's only binding it can invoke; if there
are several, it stops and lists them. Preference and deprecation are
shown, never used to choose.

Checks: each input value is checked against the input schema before it
is sent, and each output value against the output schema as it arrives;
on a side with no schema, values neither satisfy nor fail one and pass
unchecked (§5.5). Inline values (--input VALUE) are all checked before
ob asks for any context or opens the call, so one that fails, or whose
check gives no verdict (an undefined result, §5.5), stops the call
before anything is sent (exit 3). A value from @FILE or - is checked as
it is read, after the call opened: one that fails ends the call with
ERR_OPERATION_VALIDATION_FAILED, one that gets no verdict with
ERR_SCHEMA_UNRESOLVED, and the call may have taken effect (exit 1), even
when it was the first value. An output value that fails or gets no
verdict ends the call the same way.

ob never fetches a schema to check a value. Before sending anything, it
refuses (ERR_EXTERNAL_SCHEMA) when the input or output schema references
a schema the document does not contain, directly or through another
schema, naming each reference, so that a call never takes effect and
then fails for want of a schema. A hint gives the ob schema bundle
command that embeds them; for a document given by URL, save a copy with
ob fetch -o first.

--timeout gives the call a deadline: ob passes it to the binding where
the binding takes one, and cancels the exchange if it has not finished.

Context: what a binding needs beyond the input, such as a credential,
comes from ob's context store, under the binding's document and source
(ob context --help). How the binding applies it during the call belongs
to its kind; ob defines no rule for that. A binding that lacks something
answers with a CONTEXT_REQUIRED challenge, before any output and before
any effect: a message saying why, and a JSON Schema of the object to
supply next time, as the context document published with the interfaces
defines. At a terminal ob acquires it: it asks for each value the schema
describes, or, for a property the schema annotates with oauth2, offers to
sign in through your browser, and offers to store the answer for the
source; then it makes the attempt again. Otherwise ob stops (exit 3),
prints the message, and gives the way to supply it: ob context set for
the source, with the schema shown by this invoke with --preflight, or
--context for one call.

--context gives this call its context, a JSON object from @FILE or
standard input, in place of the stored context; the two never merge, so
stored secrets never mix into a file a CI job supplies. Inline JSON is
refused: context holds secrets, which stay off the command line. A
document read from standard input has no stored context, since the
store is keyed by a document's path or URL: give --context.

Delegates: stored context and --context are never sent to a delegate; a
delegate acquires its own context. If it asks, ob names the delegate and
the source it asks about and, at a terminal, sends what you enter for
that attempt only; otherwise it refuses (exit 3). In CI, configure the
delegate's own context.

--preflight calls nothing. It prints one JSON value on standard output:
an object with binding and source, the keys the answer is about, and
challenge, the binding's preflight result exactly as the Binding Invoker
returned it: the challenge it already knows it will make, message and
schema, or null when it knows of nothing it needs. ob gives preflight
the context the call would carry, the stored context for the source or
--context, so once that holds what the binding needs it answers null.
invoke takes no -F; its standard output is always JSON values. Readable
notes go to standard error. At a terminal preflight also acquires what
is missing and offers to store it. It is advisory: exit 0 means no
requirement the binding knows of is missing, not that the call will need
nothing. It exits 3 when one is missing and ob cannot ask for it, when
the binding answers ERR_REFUSED (the call would be refused), or when ob
would refuse the call before sending; 4 when the binding could not
answer. Preflight never dispatches the operation, so --frames with
--preflight is a usage error. In a CI job, with nothing stored:

  $ ob invoke tasks.obi.json listTasks --preflight
  {"binding":"listTasks.http","source":"httpApi","challenge":{"message":
  "The Tasks API needs an access token or an API key.","schema":{"oneOf":
  [...]}}}
  listTasks.http needs context that is not stored for httpApi
  hint: supply it with ob context set tasks.obi.json httpApi - (an object
      the schema above describes), or for one call with --context @FILE

The JSON value is one line on standard output, wrapped here; the notes
and the hint are standard error, and the exit status is 3.

Arguments:
  <obi>          The document: a path, - for standard input, or a URL
                 (ob help documents)
  <operation>    The operation's key or one of its aliases

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_context() -> Command {
    group(
        "context",
        "Store what calls need beyond their input",
        PAGE_CONTEXT,
        "ob context <command> [options]",
    )
    .subcommand(cmd_context_set())
    .subcommand(cmd_context_list())
    .subcommand(cmd_context_show())
    .subcommand(cmd_context_remove())
}
const PAGE_CONTEXT: &str = r####"Store what calls need beyond their input

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

ob stores context per document and source, since context is about
reaching the service a source stands for: the key is the document's path,
made absolute, or its URL, and the source's key in it. A document read
from standard input has no key and so no stored context. A binding gets
only the context stored for its own source, whole; a kind reading a
source for ob source add or ob source pull gets the same. ob source
rename moves the stored context with the source and ob source remove
removes it. --context on ob invoke, ob source add and ob source pull
gives one run its context in place of what is stored; the two never
merge.

Stored context lives in the OS keychain where there is one (macOS
Keychain, Windows Credential Manager, the Secret Service), otherwise in
files under ob's per-user config directory, with the directory mode 0700
and files 0600. OB_CONFIG_DIR points ob at another directory for all its
per-user state (stored context and delegate registrations), kept there as
plain files, so a CI job can use a throwaway store.

Stored context and --context are never sent to a delegate, which
acquires its own context (ob delegate --help).

Commands:
  set       Store the context for a source
  list      List the sources that have stored context
  show      Show the context stored for a source
  remove    Remove the context stored for a source

Options:
  -h, --help    Print help (see a summary with '-h')

Run 'ob context <command> --help' for details."####;

pub(super) fn cmd_context_set() -> Command {
    leaf(
        "set",
        "Store the context for a source",
        PAGE_CONTEXT_SET,
        "ob context set <obi> <source> <value>",
    )
    .arg(positional(
        "obi",
        "The document: a path or a URL",
        true,
        false,
    ))
    .arg(positional(
        "source",
        "The source's key in the document",
        true,
        false,
    ))
    .arg(positional(
        "value",
        "The context object: - for standard input, or @FILE",
        true,
        false,
    ))
}
const PAGE_CONTEXT_SET: &str = r####"Store the context for a source

Usage: ob context set <obi> <source> <value>

Store the context object for one source of a document: the JSON object a
challenge's schema described, read from <value>: - for standard input,
or @FILE. Secrets never go on the command line, where shell history
keeps them, so inline JSON is refused (exit 2). At a terminal, give -
and ob reads until the input ends. Storing what is already stored is
"no change"; storing replaces the whole object.

ob gives this object, whole, to a binding of <source> when it invokes,
and to the kind when it checks or pulls the source. Which members the
object needs is what the last challenge's schema said: ob invoke
--preflight prints it, and ob invoke, ob source add and ob source pull
print the same schema when they stop for want of context. ob does not
check the object against that schema here: the kind consumes it on the
next attempt and challenges again if something is missing.

The document is identified by its path, made absolute, or by its URL, as
ob context list shows it; <source> is the source's key in it. ob reads
the document only to warn when it cannot, or when it has no source under
<source>, and stores the object all the same, since a kind's check may
ask for context before the source is written; the document is never
changed.

Arguments:
  <obi>       The document: a path or a URL
  <source>    The source's key in the document
  <value>     The context object: - for standard input, or @FILE

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_context_list() -> Command {
    leaf(
        "list",
        "List the sources that have stored context",
        PAGE_CONTEXT_LIST,
        "ob context list [options]",
    )
    .arg(format(&["text", "json"]))
}
const PAGE_CONTEXT_LIST: &str = r####"List the sources that have stored context

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_context_show() -> Command {
    leaf(
        "show",
        "Show the context stored for a source",
        PAGE_CONTEXT_SHOW,
        "ob context show [options] <obi> <source>",
    )
    .arg(positional(
        "obi",
        "The document: a path or a URL, as ob context list shows it",
        true,
        false,
    ))
    .arg(positional(
        "source",
        "The source's key in the document",
        true,
        false,
    ))
    .arg(boolean(
        "reveal",
        "Print the values instead of masking them",
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_CONTEXT_SHOW: &str = r####"Show the context stored for a source

Usage: ob context show [options] <obi> <source>

Show the context object stored for one source of a document. Every value
is masked, since any member can hold a secret; --reveal prints them, and
what it prints may contain secrets. -F json prints document, source and
context, the object with each hidden value shown as {"masked":true};
with --reveal, extract .context to reuse it with ob context set.

Arguments:
  <obi>       The document: a path or a URL, as ob context list shows it
  <source>    The source's key in the document

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_context_remove() -> Command {
    leaf(
        "remove",
        "Remove the context stored for a source",
        PAGE_CONTEXT_REMOVE,
        "ob context remove <obi> <source>",
    )
    .arg(positional(
        "obi",
        "The document: a path or a URL, as ob context list shows it",
        true,
        false,
    ))
    .arg(positional(
        "source",
        "The source's key in the document",
        true,
        false,
    ))
}
const PAGE_CONTEXT_REMOVE: &str = r####"Remove the context stored for a source

Usage: ob context remove <obi> <source>

Remove the context object stored for one source of a document. To change
a member, store the whole object again with ob context set.

Arguments:
  <obi>       The document: a path or a URL, as ob context list shows it
  <source>    The source's key in the document

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob context remove tasks.obi.json httpApi

Exit status:
  0    removed
  2    bad arguments, or nothing is stored for that document and source
  See 'ob help exit-codes'."####;

pub(super) fn cmd_kind() -> Command {
    group(
        "kind",
        "See which kinds this ob supports",
        PAGE_KIND,
        "ob kind <command> [options]",
    )
    .subcommand(cmd_kind_list())
    .subcommand(cmd_kind_describe())
}
const PAGE_KIND: &str = r####"See which kinds this ob supports

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

Run 'ob kind <command> --help' for details."####;

pub(super) fn cmd_kind_list() -> Command {
    leaf(
        "list",
        "List the kinds this ob supports, and the operations for each",
        PAGE_KIND_LIST,
        "ob kind list [options]",
    )
    .arg(choice(
        "supports",
        "Only kinds supported for this operation: check, pull, or invoke",
        &["check", "pull", "invoke"],
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_KIND_LIST: &str = r####"List the kinds this ob supports, and the operations for each

Usage: ob kind list [options]

List each kind this ob supports: the operations it supports it for
(check, pull, invoke) and whether that support is built in or comes from
a delegate, given by its registration ID. A kind supported by more than
one provider is listed once for each; ob delegate resolve says which one
ob would use. A document can use any kind; this only says what this
installation can do with one.

For a delegate, the list is what the delegate advertises (its Kind
Descriptor's listKinds, with each kind's supports). When a delegate
cannot be asked, ob lists the rest and prints a warning naming it.
--supports limits the list to kinds supported for that operation.

-F json prints kinds (kind, supports as the operation keys the Kind
Descriptor names, and provider: built-in or a delegate's registration
ID) and unasked, the delegates that could not be asked (delegate,
reason).

Options:
      --supports <op>      Only kinds supported for this operation:
                           check, pull, or invoke
  -F, --format <format>    Output format: text or json [default: text]
  -h, --help               Print help (see a summary with '-h')

Examples:
  ob kind list
  ob kind list --supports pull
  ob kind list -F json | jq -r \
      --arg op openbindings.binding-invoker.invokeBinding \
      '.kinds[] | select(.supports | index($op)).kind'

Exit status:
  0    listed, including when no kind is supported
  2    bad arguments, such as an operation ob does not know
  See 'ob help exit-codes'."####;

pub(super) fn cmd_kind_describe() -> Command {
    leaf(
        "describe",
        "Describe a kind: what it reads, its check, and the schemas of its data",
        PAGE_KIND_DESCRIBE,
        "ob kind describe [options] <kind>",
    )
    .arg(positional(
        "kind",
        "The exact kind, such as example.openapi@1",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_KIND_DESCRIBE: &str = r####"Describe a kind: what it reads, its check, and the schemas of its data

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

-F json prints the Kind Descriptor's describeKind result exactly as
returned (kind, description, supports, check, sourceData, bindingData)
with provider added: built-in, or the delegate's registration ID.

Arguments:
  <kind>    The exact kind, such as example.openapi@1

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_delegate() -> Command {
    group(
        "delegate",
        "Let other tools do jobs for ob",
        PAGE_DELEGATE,
        "ob delegate <command> [options]",
    )
    .subcommand(cmd_delegate_jobs())
    .subcommand(cmd_delegate_add())
    .subcommand(cmd_delegate_set())
    .subcommand(cmd_delegate_remove())
    .subcommand(cmd_delegate_list())
    .subcommand(cmd_delegate_show())
    .subcommand(cmd_delegate_resolve())
}
const PAGE_DELEGATE: &str = r####"Let other tools do jobs for ob

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

ob keeps registrations, with its copy of each delegate's OBI, under its
per-user config directory. OB_CONFIG_DIR points ob at another directory
for all its per-user state, registrations and stored context alike, so
a CI job can register delegates in a throwaway store.

A registration makes a delegate a candidate, not a trusted party. Every
job chooses by the same rule, which ob delegate jobs prints: built-in
support and the delegates that support the kind rank by preference for
the job, higher first, with built-in support at 0 and a delegate with no
preference at 0; at equal preference, built-in support comes first, then
the earlier registration; ob uses the first. So a delegate registered
without a preference changes nothing ob already does. A preference
counts for every kind the delegate supports for the job: ob has no
per-kind choice of provider. ob delegate resolve shows the choice for
one kind.

Stored context and ob invoke --context never go to a delegate: they are
for ob's own calls only, because the interfaces forbid choosing a
secret's recipient by delegate preference or registration. A delegate
acquires its own context. If it asks, ob names the delegate and the
source it asks about and, at a terminal, sends what you enter for that
attempt only; otherwise it refuses (exit 3). In CI, configure the
delegate's own context.

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

Run 'ob delegate <command> --help' for details."####;

pub(super) fn cmd_delegate_jobs() -> Command {
    leaf(
        "jobs",
        "List the jobs ob accepts delegates for",
        PAGE_DELEGATE_JOBS,
        "ob delegate jobs [options]",
    )
    .arg(format(&["text", "json"]))
}
const PAGE_DELEGATE_JOBS: &str = r####"List the jobs ob accepts delegates for

Usage: ob delegate jobs [options]

List the jobs ob accepts delegates for, each with its description and its
accepted interfaces. ob accepts Source Puller for pull and Binding
Invoker for invoke, each together with Kind Descriptor for the kinds the
delegate supports.

The description gives the job's rule, which ob documents and follows, as
the Delegate Manager requires. Every job has the same rule, so the text
output prints it once, after the jobs; -F json carries it in each job's
description:

  eligible      Built-in support for the kind, and each delegate
                registered for the job whose Kind Descriptor says it
                supports the kind for the job's operation. Kinds enter
                only here, through the delegate's own descriptor: a
                registration names jobs, never kinds.
  rank          By preference for the job, higher first. Built-in
                support is a fixed candidate at preference 0, and a
                delegate with no preference counts as 0. At equal
                preference, built-in support comes first, then the
                earlier registration.
  unreachable   A delegate that cannot be asked is skipped, with a
                warning; ob delegate resolve says so when its preference
                would have ranked it above the provider chosen.
  use           ob uses the highest-ranked eligible provider. Once a
                call has begun, it never tries another, and a preference
                never pins a provider, however large: the Delegate
                Manager fixes these two.

So registering a delegate without a preference changes nothing ob
already does; a positive preference puts it ahead of built-in support,
and a negative one makes it a last resort. A preference counts for every
kind the delegate supports for the job: ob has no per-kind choice of
provider. Choosing by kind, or by other logic of your own, is for an
application built on the SDK, which sees the same candidates and decides
for itself.

Each accepted interface is a complete unbound OBI: the operations a
delegate must provide and the schemas they reference, with no bindings
or dependencies.

A delegate is admitted for a job when its operations correspond to every
operation of one accepted interface: each of that interface's operation
keys is a key or alias in the delegate's OBI, and no comparison
establishes that their contracts are incompatible.

-F json prints jobs, each with id, description (the job's rule) and
acceptedInterfaces, every accepted interface in full, so a tool's OBI
can be checked against one with ob compat before it is registered. ob
compat matches every published name, aliases included, while admission
matches the accepted interface's operation keys only, so compat can
report a correspondence that admission does not accept.

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_delegate_add() -> Command {
    leaf(
        "add",
        "Register a delegate",
        PAGE_DELEGATE_ADD,
        "ob delegate add [options] --job <job>... <delegate-obi>",
    )
    .arg(positional(
        "delegate-obi",
        "The delegate's OBI: a path, - for standard input, or a URL (ob help documents)",
        true,
        false,
    ))
    .arg(
        choice(
            "job",
            "A job to register it for: pull or invoke (required, repeatable)",
            &["pull", "invoke"],
        )
        .required(true)
        .action(ArgAction::Append),
    )
    .arg(repeated("preference", "Its preference for a job (repeatable)").value_name("JOB=NUMBER"))
    .arg(format(&["text", "json"]))
}
const PAGE_DELEGATE_ADD: &str = r####"Register a delegate

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

On success ob prints the ID it assigned to the registration. ob keeps
the registration, with its copy, under its per-user config directory,
or under OB_CONFIG_DIR when that is set (ob delegate --help). Adding the
same OBI again creates a second registration; to change one, use ob
delegate set.

--preference takes JOB=NUMBER and sets this delegate's preference for a
requested job: any finite number, negative and fractional included, kept
exactly as given (an explicit zero too). It ranks the delegate under the
job's rule (ob delegate jobs): at 0, the effective preference without one,
it comes after built-in support; a positive one puts it ahead of built-in
support, and a negative one makes it a last resort. It counts for every kind
the delegate supports for the job and never pins the delegate, however
large. It belongs to ob's registration and is unrelated to a binding's
preference in a document.

-F json prints a report whose delegate member is the registration: id,
interface (the retained OBI), jobs, and jobPreferences.

Arguments:
  <delegate-obi>    The delegate's OBI: a path, - for standard input, or a
                    URL (ob help documents)

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_delegate_set() -> Command {
    leaf(
        "set",
        "Change a registration",
        PAGE_DELEGATE_SET,
        "ob delegate set [options] <id>",
    )
    .arg(positional(
        "id",
        "The registration's ID, as ob delegate list shows it",
        true,
        false,
    ))
    .arg(
        text(
            "obi",
            "Replace the delegate's OBI: a path, - for standard input, or a URL",
        )
        .value_name("delegate-obi"),
    )
    .arg(
        choice(
            "add-job",
            "Register it for another job (repeatable)",
            &["pull", "invoke"],
        )
        .action(ArgAction::Append),
    )
    .arg(
        choice(
            "remove-job",
            "Stop using it for a job (repeatable)",
            &["pull", "invoke"],
        )
        .action(ArgAction::Append),
    )
    .arg(
        repeated("preference", "Set its preference for a job (repeatable)")
            .value_name("JOB=NUMBER"),
    )
    .arg(
        repeated(
            "unset",
            "Remove its explicit preference for a job (repeatable)",
        )
        .value_name("preference.JOB"),
    )
    .arg(format(&["text", "json"]))
}
const PAGE_DELEGATE_SET: &str = r####"Change a registration

Usage: ob delegate set [options] <id>

Change a registered delegate: replace its OBI, change its jobs, or set
its preference for a job. Only the options you give change anything, and
a change that is already true is "no change". The whole change applies,
or nothing does.

--obi replaces the retained OBI with a new one, read now and checked for
every job the registration keeps, as ob delegate add checks it. ob never
fetches a delegate's OBI again on its own; this is how its copy is
updated. --add-job registers it for another job, with the same check.
--remove-job stops using it for a job and removes that job's preference;
at least one job must remain (ob delegate remove removes the whole
registration). A job removed and added again starts with no preference.

--preference takes JOB=NUMBER and sets the preference for a job the
registration has, or gains in this change, which ranks it for that job
under the job's rule (ob delegate jobs): any finite number, negative and
fractional included, kept exactly as given. --unset takes preference.JOB
and removes the explicit preference, leaving an effective preference of
zero, which ranks it after built-in support.
Contradictory changes, such as --add-job and --remove-job for one job,
are a usage error.

-F json prints a report whose delegate member is the resulting
registration, as ob delegate show does.

Arguments:
  <id>    The registration's ID, as ob delegate list shows it

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_delegate_remove() -> Command {
    leaf(
        "remove",
        "Remove a registration",
        PAGE_DELEGATE_REMOVE,
        "ob delegate remove <id>",
    )
    .arg(positional(
        "id",
        "The registration's ID, as ob delegate list shows it",
        true,
        false,
    ))
}
const PAGE_DELEGATE_REMOVE: &str = r####"Remove a registration

Usage: ob delegate remove <id>

Remove a registered delegate. ob stops using it for every job and
deletes its copy of the delegate's OBI and its preferences. An ID that
no registration has is already absent: ob says so and exits 0, as the
Delegate Manager's own operation does, so a script that removes what it
registered can run twice.

Arguments:
  <id>    The registration's ID, as ob delegate list shows it

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob delegate remove d_7f3a
  ob delegate list --job invoke -F json \
      | jq -r '.delegates[].id' | xargs -n1 ob delegate remove

Exit status:
  0    removed, or already absent
  2    bad arguments
  See 'ob help exit-codes'."####;

pub(super) fn cmd_delegate_list() -> Command {
    leaf(
        "list",
        "List registrations",
        PAGE_DELEGATE_LIST,
        "ob delegate list [options]",
    )
    .arg(choice(
        "job",
        "Only registrations for this job: pull or invoke",
        &["pull", "invoke"],
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_DELEGATE_LIST: &str = r####"List registrations

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_delegate_show() -> Command {
    leaf(
        "show",
        "Show a registration",
        PAGE_DELEGATE_SHOW,
        "ob delegate show [options] <id>",
    )
    .arg(positional(
        "id",
        "The registration's ID, as ob delegate list shows it",
        true,
        false,
    ))
    .arg(format(&["text", "json"]))
}
const PAGE_DELEGATE_SHOW: &str = r####"Show a registration

Usage: ob delegate show [options] <id>

Show one registered delegate: its OBI's name, version label and
operations, its jobs, and its explicit preferences. -F json prints a
report whose delegate member is the registration: id, jobs,
jobPreferences, and interface, the retained OBI exactly as given, with
its schemas, sources, bindings and extension fields. Extract
.delegate.interface to reuse it with ob delegate set --obi -.

Arguments:
  <id>    The registration's ID, as ob delegate list shows it

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_delegate_resolve() -> Command {
    leaf(
        "resolve",
        "Explain what ob would use for a job on a kind",
        PAGE_DELEGATE_RESOLVE,
        "ob delegate resolve [options] --job <job> --kind <kind>",
    )
    .arg(
        choice(
            "job",
            "The job: pull or invoke (required)",
            &["pull", "invoke"],
        )
        .required(true),
    )
    .arg(
        text("kind", "The exact kind (required)")
            .value_name("kind")
            .required(true),
    )
    .arg(format(&["text", "json"]))
}
const PAGE_DELEGATE_RESOLVE: &str = r####"Explain what ob would use for a job on a kind

Usage: ob delegate resolve [options] --job <job> --kind <kind>

Explain what ob would use for a job on an exact kind, its built-in
support or a delegate (given by its registration ID), and why. ob asks
each delegate registered for the job whether it supports the kind for
the job's operation (its Kind Descriptor), then applies the job's rule,
which ob delegate jobs prints.

The report gives the provider ob would use, or says that nothing
supports the kind; the reason, the part of the rule that decided: the
only one that supports the kind, the higher preference, built-in support
first at equal preference, or the earlier registration; and, in rank
order, built-in support and each delegate registered for the job, each
with its effective preference (0 for built-in support) and whether it
supports the kind. A delegate that cannot be asked is skipped with a
warning, as it is whenever ob does the job; the report lists it with the
reason and says when it might have ranked higher than the provider
chosen.

-F json prints job, kind, provider (built-in or a delegate's
registration ID, absent when nothing supports the kind), reason
(only-supporter, higher-preference, built-in-at-tie or
earlier-registration), and candidates in rank order: built-in support
and each delegate registered for the job, each with provider, preference
(the effective preference: 0 for built-in) and supported, true or false;
or, for a delegate that could not be asked, answered false with a reason
in place of supported.

This explains a choice; it is not a check. For what a kind's support says
about it, use ob kind describe, whose exit status says whether anything
supports the kind.

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
  See 'ob help exit-codes'."####;

pub(super) fn cmd_describe() -> Command {
    leaf(
        "describe",
        "Describe this ob: what it is and what it supports",
        PAGE_DESCRIBE,
        "ob describe [options]",
    )
    .arg(format(&["text", "json"]))
}
const PAGE_DESCRIBE: &str = r####"Describe this ob: what it is and what it supports

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

The build fixes all of this, as the Software Descriptor requires: for a
given build, describe's output is stable, so it leaves out support
through delegates, which changes without a new build. ob kind list shows
current support, delegates included.

-F json prints the Software Descriptor's identity fields (name, version,
description, homepage, repository, maintainer) at the top level, followed
by the rest: openbindings (line, release, revision), httpDiscovery,
interfaces, profiles and kinds. A comparison profile is not an
interface, so profiles is a member of its own; the Software Descriptor
lets an implementation return fields beyond its own. For the version
line alone, use ob --version (or -V).

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
  See 'ob help exit-codes'."####;
