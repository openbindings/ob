// Command declarations migrated from the pinned default Go surface.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![
        cmd_fetch(),
        cmd_invoke(),
        cmd_context(),
        cmd_codegen(),
        cmd_start(),
        cmd_mcp(),
        cmd_ca(),
        cmd_kind(),
        cmd_delegate(),
        cmd_describe(),
    ]
}

fn cmd_fetch() -> Command {
    leaf(
        "fetch",
        "Download a service's OBI",
        FETCH_HELP,
        FETCH_EXAMPLES,
    )
    .arg(position("url", true, false))
    .arg(boolean("force", "replace an existing file"))
    .arg(text("out", "save the document to a file").short('o'))
}

const FETCH_HELP: &str = r#"Download the OBI a service publishes. A bare origin, such as
https://api.example.com, is looked up at /.well-known/openbindings
(OpenBindings HTTP Discovery); any other URL is fetched as given. Prints the
document unless -o is given; -o refuses to replace an existing file unless
--force is given.

A service may ask for sign-in to read its OBI. ob then uses the context
stored for the URL's origin (ob context set <origin>), asks at a terminal,
and otherwise stops. fetch keeps a document written for an OpenBindings
version this ob does not read, and says so.

Commands that read a document, such as ob show and ob invoke, also accept a
URL directly and answer the same way; fetch is for keeping a copy.

Exit status: 0 fetched; 1 no OBI is published there; 3 refused: the
service asks for sign-in and nothing stored or given answers it."#;
const FETCH_EXAMPLES: &str = r#"  ob fetch https://api.example.com -o tasks.obi.json
  ob fetch https://api.example.com | jq '.operations | keys'"#;

fn cmd_invoke() -> Command {
    leaf("invoke","Call an operation",INVOKE_HELP,INVOKE_EXAMPLES)
.arg(position("obi", true, false))
.arg(position("operation", true, false))
.arg(repeated("binding","use this binding; repeat for an ordered list"))
.arg(text("context","context for this call only: a JSON object from @FILE or - (stdin)"))
.arg(boolean("frames","print the whole exchange as frames, not just output values"))
.arg(repeated("input","a value to write: JSON, @file, or - to stream from stdin (repeatable, in order)"))
.arg(boolean("preflight","print what the binding will ask for, and at a terminal get what is missing; call nothing"))
.arg(duration("timeout","a deadline for the call, such as 30s; ob cancels the exchange when it passes").default_value("0s"))
}

const INVOKE_HELP: &str = r#"Call an operation, by name or alias, through one of its bindings.

An invocation is an exchange, whatever the operation's shape: ob opens it,
writes your input values to it, prints each output value as it arrives, and
finishes when the binding does. One value in and one out, a stream in, a
stream out, or both at once all work the same way.

Input: each --input writes, in order; then ob closes the input.
  --input VALUE   one value (JSON)
  --input @FILE   each JSON value in the file
  --input -       each JSON value read from stdin, as it arrives, until
                  stdin ends (Ctrl-D at a terminal)
  no --input      nothing; ob closes the input at once

Output: each output value, as one line of JSON, as it arrives. --frames
prints the whole exchange instead, one frame per line, exactly as the
operation-invoker interface defines them: each output, the binding closing
its input early, and the final complete or error frame. A refusal ends with
its error frame too.

Choosing a binding: --binding names one; repeat it to give an ordered list,
and ob uses the first one it can invoke. A name that is not one of the
operation's bindings is refused, and so is a list with none ob can invoke;
ob never falls back to a binding you did not name. Without --binding, if
the operation has exactly one binding ob can invoke, ob uses it; if it has
several, ob stops and lists them. Preference and deprecation are shown,
never used to choose.

Checks: ob checks each input value against the operation's input schema
before sending it, and each output value against its output schema. Values
given with --input VALUE are checked before ob asks for any context. Where
the operation states no schema for a side, its values pass unchecked
(OBI-T-08). When a schema cannot be fully resolved, ob cannot check against
it and stops with ERR_SCHEMA_UNRESOLVED, before sending anything.

--timeout gives the call a deadline: ob passes it to the binding where the
binding takes one, and cancels the exchange if it has not finished.

Context: what a binding needs beyond the input, such as a credential, comes
from ob's context store, looked up by the exact scope the binding asks for.
At a terminal ob gets anything missing: it asks for a value, or runs the
sign-in the binding names (such as an OAuth 2.0 flow in your browser), and
offers to store the result when the binding says it may be reused (durable).
Stored tokens are renewed as they expire.
Otherwise ob stops before sending anything and prints what supplies it: an
ob context set command, or, for a sign-in, this same invoke with
--preflight to run once at a terminal. --context gives context for this call
only, as a JSON object from @FILE or stdin. A delegate that invokes for ob resolves its own
context: ob never sends it stored context, and if it asks for something, ob
asks you or stops.

--preflight calls nothing. It prints what the binding already knows it will
ask for, and at a terminal it also gets what is missing, asking you or
running the sign-in, and stores it for later calls.

Without --frames, an error's code, and any data it carries, go to stderr.

Exit status: 0 completed; 1 the operation failed, an output did not fit,
or the deadline passed (any of these may have taken effect); 2 a usage
error, including a name that is not in the document; 3 refused before
anything was sent (a value that does not fit, missing context, an
unresolvable schema, or a binding's ERR_REFUSED); 130 cancelled."#;
const INVOKE_EXAMPLES: &str = r#"  ob invoke tasks.obi.json completeTask --input '{"id":"t_1"}'
  ob invoke tasks.obi.json createTask --binding createTask.http --input '{"title":"Ship it"}'
  printf '{"title":"a"}\n{"title":"b"}\n' | ob invoke tasks.obi.json importTasks --input -
  ob invoke tasks.obi.json watchTasks --frames
  ob invoke tasks.obi.json createTask --binding createTask.mcp --preflight"#;

fn cmd_context() -> Command {
    group(
        "context",
        "Store credentials and settings for calls",
        CONTEXT_HELP,
        CONTEXT_EXAMPLES,
    )
    .subcommand(cmd_context_set())
    .subcommand(cmd_context_list())
    .subcommand(cmd_context_show())
    .subcommand(cmd_context_remove())
}

const CONTEXT_HELP: &str = r#"A context holds what a binding needs beyond its input to call a service:
credentials, headers, and configuration values. ob stores it by scope and
uses it for its own invocations: only for the exact scope a binding asks
for, only when the binding says the value may be reused, and only the fields
that one request needs. Delegates resolve their own context; ob never sends
them stored context.

Well-known interface fields are bearerToken, apiKey, credentials (named
strings or Basic/OAuth objects), apiKeys (historical named API keys), basic,
accessToken, refreshToken, expiresAt, headers, cookies, environment,
metadata, and configuration. Context is opaque and may also hold custom
fields. context set --value takes any of these as a whole JSON object;
the individual flags cover the common fields. Pinned token-provider settings
are ob's local resolver configuration.

A scope is the exact string a binding names when it asks for context,
usually a service's origin such as https://api.example.com. ob prints it,
ready to copy, whenever a binding asks for something that is not stored."#;
const CONTEXT_EXAMPLES: &str = "";

fn cmd_context_set() -> Command {
    leaf("set","Store context for a scope",CONTEXT_SET_HELP,CONTEXT_SET_EXAMPLES)
.arg(position("scope", true, false))
.arg(text("access-token","an OAuth 2.0 access token: - (stdin, or asked for) or @FILE"))
.arg(text("api-key","an API key: - (stdin, or asked for) or @FILE"))
.arg(text("basic","HTTP Basic credentials as USER:PASSWORD: - (stdin, or asked for) or @FILE"))
.arg(text("bearer-token","a bearer token: - (stdin, or asked for) or @FILE"))
.arg(repeated("config","POINT=VALUE stores a string; POINT:=JSON stores a typed value; either accepts @FILE or - (repeatable)"))
.arg(repeated("cookie","a non-secret cookie: NAME=VALUE; for a secret use NAME=- or NAME=@FILE (repeatable)"))
.arg(repeated("credential","a named JSON credential: NAME=- or NAME=@FILE (repeatable)"))
.arg(repeated("header","a header to send: NAME=VALUE, or NAME=- or NAME=@FILE for a secret (repeatable)"))
.arg(text("refresh-token","an OAuth 2.0 refresh token: - (stdin, or asked for) or @FILE"))
.arg(repeated("token-binding","use this binding of the token service; repeat for an ordered list"))
.arg(text("token-credential","the credential to mint with: - (stdin, or asked for) or @FILE"))
.arg(text("token-provider","mint bearer tokens from this token service (its OBI: a path or URL)"))
.arg(repeated("unset","remove a field: bearer-token, access-token, refresh-token, api-key, basic, credential.NAME, cookie.NAME, token-provider, token-credential, header.NAME, config.POINT"))
.arg(text("value","replace the whole context with a JSON object: @FILE or -"))
}

const CONTEXT_SET_HELP: &str = r#"Store what bindings need beyond their input, for one scope: credentials,
headers, and configuration values. ob uses a stored context only for that
exact scope, and only when the binding says the value may be reused.

A secret never goes on the command line, where it would stay in your shell
history: --bearer-token, --access-token, --refresh-token, --api-key, --basic, and
--token-credential take - (read from stdin, or asked for at a terminal
without echo) or @FILE. --basic reads USER:PASSWORD. --header NAME=VALUE
takes a literal value, or NAME=- or NAME=@FILE for a secret one; the
headers that carry credentials (Authorization, Proxy-Authorization, Cookie)
take only those. --config POINT=VALUE answers a configuration point a binding
asks for, such as which server to use. POINT=VALUE always stores a string,
so code=001 and enabled=false keep their exact text. POINT:=JSON stores a
typed value, such as enabled:=false or server:={"url":"https://api.example.com"}.
Either operator accepts @FILE or -: = reads text and := parses JSON.
--value replaces the whole context with a JSON object, from @FILE or -.
Only one value may read stdin per command; use separate files for the others.

--credential NAME=- or NAME=@FILE stores a named credential in
credentials[NAME]. Read a JSON string for a bearer token or API key, a JSON
object {"username":...,"password":...} for Basic, or an OAuth object
beginning with accessToken. NAME is the scheme name the binding asks for,
an exact string. --cookie NAME=VALUE stores a non-secret cookie, or use
NAME=- or NAME=@FILE for a secret one.

--token-provider pins a token service: the OBI of a service that implements
the token-provider interface. ob keeps a copy of it, and when a binding asks
this scope for a bearer token, ob mints one from that provider (and only that
provider) and renews it before it expires. --token-credential is the
credential to mint with; leave it out for a provider that uses an identity
you are already signed in with. --token-binding chooses among the provider's
bindings, as ob invoke --binding does. A binding key names its operation, so
one list covers them all: give the mint binding and the refresh binding you
want, in any order, and each applies to its own operation.

--unset removes a field, named as its flag is: bearer-token, access-token,
refresh-token, api-key, basic, credential.NAME, cookie.NAME, token-provider,
token-credential, header.NAME, or config.POINT.
Text reports use these flag names; JSON reports keep the interface's field
names. A stored token provider also lets --token-credential rotate on its own.

A scope is the exact string a binding names when it asks for context,
usually a service's origin such as https://api.example.com. ob prints it,
ready to copy, whenever a binding asks for something that is not stored."#;
const CONTEXT_SET_EXAMPLES: &str = r#"  ob context set https://api.example.com --bearer-token -
  printf 'ada:s3cret' | ob context set https://legacy.example.com --basic -
  ob context set https://api.example.com/openapi.json --config 'server:={"url":"https://eu.example.com"}'
  ob context set https://api.example.com --credential primary=@primary.json --credential secondary=@secondary.json
  ob context set https://api.example.com --cookie locale=en --refresh-token -
  ob context set https://api.example.com --token-provider https://auth.example.com --token-credential @creds.txt
  ob context set https://api.example.com --unset header.X-Client"#;

fn cmd_context_list() -> Command {
    leaf(
        "list",
        "List stored contexts",
        CONTEXT_LIST_HELP,
        CONTEXT_LIST_EXAMPLES,
    )
    .arg(format(&["text", "json"]))
}

const CONTEXT_LIST_HELP: &str =
    "List the scopes that have stored context, and what each holds. Secrets are masked.";
const CONTEXT_LIST_EXAMPLES: &str = "  ob context list";

fn cmd_context_show() -> Command {
    leaf(
        "show",
        "Show a stored context",
        CONTEXT_SHOW_HELP,
        CONTEXT_SHOW_EXAMPLES,
    )
    .arg(position("scope", true, false))
    .arg(format(&["text", "json"]))
    .arg(boolean(
        "reveal",
        "print the values instead of masking them",
    ))
}

const CONTEXT_SHOW_HELP: &str = r#"Show what is stored for one scope. Every value is masked, since headers
and configuration can hold secrets too; --reveal prints them. Text names
match --unset; JSON contains scope, the native nested context object, and
ob-local resolver settings separately. Hidden values are {"masked":true}.
With --reveal, extract .context to reuse it with context set --value; resolver
settings are never forwarded as Context. Revealed values may contain secrets."#;
const CONTEXT_SHOW_EXAMPLES: &str = "  ob context show https://api.example.com";

fn cmd_context_remove() -> Command {
    leaf(
        "remove",
        "Remove a stored context",
        CONTEXT_REMOVE_HELP,
        CONTEXT_REMOVE_EXAMPLES,
    )
    .arg(position("scope", true, false))
}

const CONTEXT_REMOVE_HELP: &str = "Remove everything stored for one scope.";
const CONTEXT_REMOVE_EXAMPLES: &str = "  ob context remove https://api.example.com/openapi.json";

fn cmd_codegen() -> Command {
    leaf(
        "codegen",
        "Generate a typed client",
        CODEGEN_HELP,
        CODEGEN_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(boolean("force", "replace files already in the directory"))
    .arg(
        text("lang", "go or typescript (required)")
            .required(true)
            .value_parser(["go", "typescript"]),
    )
    .arg(
        text("out", "the directory to write (required)")
            .short('o')
            .required(true),
    )
    .arg(text("package", "the package or module name"))
}

const CODEGEN_HELP: &str = r#"Generate client code with one typed function per operation that has
bindings. Calls go through ob's invocation libraries and choose a binding
the way ob invoke does: each call takes an optional ordered list of binding
keys, like --binding; without one, a call uses the operation's only usable
binding and refuses when there are several.

-o names the output directory, and is required. It refuses to replace files
already there unless --force is given."#;
const CODEGEN_EXAMPLES: &str = r#"  ob codegen tasks.obi.json --lang go -o ./taskclient
  ob codegen https://api.example.com --lang typescript --package @acme/tasks -o ./tasks-client"#;

fn cmd_start() -> Command {
    leaf(
        "start",
        "Run ob as a local service",
        START_HELP,
        START_EXAMPLES,
    )
    .arg(repeated(
        "allow-origin",
        "let browser pages from this origin call ob (repeatable)",
    ))
    .arg(format(&["text", "json"]))
    .arg(
        integer("port", "the HTTP port; with --tls, HTTPS uses the next one")
            .default_value("20290"),
    )
    .arg(boolean(
        "tls",
        "also serve HTTPS, with a certificate from ob's local certificate authority",
    ))
}

const START_HELP: &str = r#"Serve ob's own operations over HTTP and WebSocket on this machine, for
editors, browsers, and other tools. ob describes itself with an OBI at
/.well-known/openbindings.

Who may call it: every request must carry this run's access token as a
bearer token. ob saves the addresses and token in a private run record,
readable only by this user (file mode 0600, directory 0700). Its own commands
read the record automatically for an exact matching service address.
Text output names the file; -F json prints the startup record, including
the token, as one line. Other tools can read that record to connect.
The record is removed when this run stops; simultaneous runs use separate
records by HTTP port.

ob answers only requests addressed
to 127.0.0.1 or localhost by name, which stops a web page from reaching it
through DNS rebinding, and a browser page only from an origin given with
--allow-origin.

--tls also serves HTTPS, on the next port, with a certificate from ob's local
certificate authority. ob never changes what this machine trusts on its own:
install the authority first with ob ca install."#;
const START_EXAMPLES: &str = r#"  ob start
  ob start --port 8080 --allow-origin https://editor.example.com
  ob start --tls"#;

fn cmd_mcp() -> Command {
    leaf(
        "mcp",
        "Serve a document's operations as MCP tools",
        MCP_HELP,
        MCP_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(repeated(
        "binding",
        "use this binding for its operation; repeat for an ordered list",
    ))
    .arg(repeated(
        "operation",
        "offer only this operation, by key or alias (repeatable)",
    ))
}

const MCP_HELP: &str = r#"Run a Model Context Protocol server on stdin and stdout that offers each
operation it can call as a tool, and calls it the way ob invoke does, with
the same context rules. <obi> may be a URL, including a running ob start.

Each tool call uses a binding chosen the way ob invoke chooses one.
--binding names bindings to use, in order, like ob invoke --binding; a
binding key belongs to one operation, so each key applies to its own
operation. An operation with several bindings ob can invoke, and none
named, is skipped and listed with the reason.

--operation offers only the named operations (a key or an alias). A name
that is not in the document is a usage error, and one ob cannot offer is
refused.

How operations appear as MCP tools (their names, input and output shapes,
operations whose bindings stream, and errors) will follow the MCP binding
specification, which is not yet written for OpenBindings 0.2. Until then
this command is a placeholder for that mapping."#;
const MCP_EXAMPLES: &str = r#"  ob mcp tasks.obi.json --binding createTask.http
  ob mcp https://api.example.com --operation createTask --operation listTasks --binding createTask.mcp"#;

fn cmd_ca() -> Command {
    group(
        "ca",
        "Manage the certificate authority ob start --tls uses",
        CA_HELP,
        CA_EXAMPLES,
    )
    .subcommand(cmd_ca_install())
    .subcommand(cmd_ca_remove())
    .subcommand(cmd_ca_show())
}

const CA_HELP: &str = r#"ob start --tls serves HTTPS with certificates from a local certificate
authority. These commands install it into this machine's trust store, show
it, and remove it."#;
const CA_EXAMPLES: &str = "";

fn cmd_ca_install() -> Command {
    leaf(
        "install",
        "Install ob's local certificate authority",
        CA_INSTALL_HELP,
        CA_INSTALL_EXAMPLES,
    )
}

const CA_INSTALL_HELP: &str = r#"Create ob's local certificate authority, if there is none yet, and install it
into this machine's trust store, so browsers and tools trust ob start --tls.
It asks for an administrator's password. Nothing else changes what this
machine trusts."#;
const CA_INSTALL_EXAMPLES: &str = "  ob ca install";

fn cmd_ca_remove() -> Command {
    leaf(
        "remove",
        "Remove ob's local certificate authority",
        CA_REMOVE_HELP,
        CA_REMOVE_EXAMPLES,
    )
}

const CA_REMOVE_HELP: &str = r#"Remove ob's local certificate authority from this machine's trust store and
delete its key. ob start --tls stops working until it is installed again."#;
const CA_REMOVE_EXAMPLES: &str = "  ob ca remove";

fn cmd_ca_show() -> Command {
    leaf(
        "show",
        "Show ob's local certificate authority",
        CA_SHOW_HELP,
        CA_SHOW_EXAMPLES,
    )
    .arg(format(&["text", "json"]))
}

const CA_SHOW_HELP: &str = "Say whether ob's local certificate authority is installed, and if so, its fingerprint and expiry.";
const CA_SHOW_EXAMPLES: &str = "  ob ca show";

fn cmd_kind() -> Command {
    group(
        "kind",
        "See which kinds this ob can handle",
        KIND_HELP,
        KIND_EXAMPLES,
    )
    .subcommand(cmd_kind_list())
    .subcommand(cmd_kind_check())
}

const KIND_HELP: &str = r#"A source's kind says how its content and bindings are read. ob handles a
kind with a built-in handler or through a delegate. These commands report
what this installation can do; they never judge a document."#;
const KIND_EXAMPLES: &str = "";

fn cmd_kind_list() -> Command {
    leaf(
        "list",
        "List the kinds this ob can handle",
        KIND_LIST_HELP,
        KIND_LIST_EXAMPLES,
    )
    .arg(format(&["text", "json"]))
    .arg(
        text(
            "role",
            "only kinds this ob can handle for: invoke, inspect, or synthesize",
        )
        .value_parser(["invoke", "inspect", "synthesize"]),
    )
}

const KIND_LIST_HELP: &str = r#"List the kinds this ob can handle, for each kind of work, and whether the
handler is built in or a delegate. A document can use any kind; this only
says what this installation can do with one. For delegates the list is what
they advertise and may be incomplete; ob kind check gives the authoritative
answer for one kind."#;
const KIND_LIST_EXAMPLES: &str = r#"  ob kind list
  ob kind list --role invoke"#;

fn cmd_kind_check() -> Command {
    leaf(
        "check",
        "Check whether this ob can handle a kind",
        KIND_CHECK_HELP,
        KIND_CHECK_EXAMPLES,
    )
    .arg(position("kind", true, false))
    .arg(format(&["text", "json"]))
    .arg(
        text("role", "the kind of work: invoke, inspect, or synthesize").value_parser([
            "invoke",
            "inspect",
            "synthesize",
        ]),
    )
}

const KIND_CHECK_HELP: &str = r#"Say whether this ob can handle an exact kind, for one kind of work (--role)
or for each. Kinds are compared exactly: example.openapi@1 and
example.openapi@2 are unrelated kinds. To see which handler ob would use,
and why, use ob delegate resolve.

Without --role, this is a report: exit 0 even if no work is supported.
With --role, it is a yes/no check: exit 0 supported, 1 unsupported."#;
const KIND_CHECK_EXAMPLES: &str = r#"  ob kind check example.openapi@1
  ob kind check acme.billing-rpc@1 --role invoke"#;

fn cmd_delegate() -> Command {
    group(
        "delegate",
        "Let other tools handle work for ob",
        DELEGATE_HELP,
        DELEGATE_EXAMPLES,
    )
    .subcommand(cmd_delegate_roles())
    .subcommand(cmd_delegate_add())
    .subcommand(cmd_delegate_set())
    .subcommand(cmd_delegate_remove())
    .subcommand(cmd_delegate_list())
    .subcommand(cmd_delegate_show())
    .subcommand(cmd_delegate_resolve())
}

const DELEGATE_HELP: &str = r#"A delegate is another tool, described by its own OBI, that ob hands work
to: invoking bindings, inspecting sources, or synthesizing documents, for
kinds ob does not handle itself. Each kind of work is a role."#;
const DELEGATE_EXAMPLES: &str = "";

fn cmd_delegate_roles() -> Command {
    leaf(
        "roles",
        "List the work ob can hand to delegates",
        DELEGATE_ROLES_HELP,
        DELEGATE_ROLES_EXAMPLES,
    )
    .arg(format(&["text", "json"]))
}

const DELEGATE_ROLES_HELP: &str = r#"List the roles ob accepts, their admission/use policy, and their complete
expected interfaces. -F json carries id, description, and acceptedInterfaces
as the delegate-manager contract defines. Each accepted interface includes
all required operations and their referenced schemas, without bindings or
dependencies."#;
const DELEGATE_ROLES_EXAMPLES: &str = "  ob delegate roles";

fn cmd_delegate_add() -> Command {
    leaf(
        "add",
        "Register a delegate",
        DELEGATE_ADD_HELP,
        DELEGATE_ADD_EXAMPLES,
    )
    .arg(position("delegate-obi", true, false))
    .arg(format(&["text", "json"]))
    .arg(repeated(
        "preference",
        "ROLE=NUMBER, including negative/fractional values; higher is preferred (repeatable)",
    ))
    .arg(
        repeated(
            "role",
            "a role to register for: invoke, inspect, or synthesize (repeatable)",
        )
        .required(true)
        .value_parser(["invoke", "inspect", "synthesize"]),
    )
}

const DELEGATE_ADD_HELP: &str = r#"Register another tool as a delegate for one or more roles. <delegate-obi>
is the tool's OBI (a path, - for stdin, or a URL); ob keeps a copy, checks
that it offers the operations each role needs, and gives the registration an
ID. --preference ROLE=NUMBER sets how strongly ob prefers it among eligible
delegates for that role; higher wins. Negative and fractional numbers are
allowed. Absence has effective preference zero; an explicit zero stays
explicit. Each role may be requested once. -F json prints the full retained
registration, including interface and rolePreferences."#;
const DELEGATE_ADD_EXAMPLES: &str = r#"  ob delegate add ./acme-rpc.obi.json --role invoke --preference invoke=10
  ob delegate add https://tools.example.com --role inspect --role synthesize"#;

fn cmd_delegate_set() -> Command {
    leaf(
        "set",
        "Change a delegate",
        DELEGATE_SET_HELP,
        DELEGATE_SET_EXAMPLES,
    )
    .arg(position("id", true, false))
    .arg(
        repeated("add-role", "register it for another role (repeatable)").value_parser([
            "invoke",
            "inspect",
            "synthesize",
        ]),
    )
    .arg(format(&["text", "json"]))
    .arg(text(
        "obi",
        "replace the delegate's OBI: a path, - for stdin, or a URL",
    ))
    .arg(repeated(
        "preference",
        "ROLE=NUMBER, including negative/fractional values; higher is preferred (repeatable)",
    ))
    .arg(
        repeated("remove-role", "stop using it for a role (repeatable)").value_parser([
            "invoke",
            "inspect",
            "synthesize",
        ]),
    )
    .arg(repeated("unset", "remove a preference: preference.ROLE"))
}

const DELEGATE_SET_HELP: &str = r#"Change a registered delegate: replace its OBI with --obi, change its roles,
or set its preference for a role. Only the flags you give change anything.
Numbers may be negative or fractional. --unset preference.ROLE removes an
explicit preference. Removing a role also removes its preference; at least
one role must remain. Repeated or contradictory role changes are usage errors.
-F json prints the resulting full registration."#;
const DELEGATE_SET_EXAMPLES: &str = r#"  ob delegate set d_91c2 --preference invoke=20
  ob delegate set d_91c2 --add-role synthesize
  ob delegate set d_91c2 --unset preference.invoke"#;

fn cmd_delegate_remove() -> Command {
    leaf(
        "remove",
        "Remove a delegate",
        DELEGATE_REMOVE_HELP,
        DELEGATE_REMOVE_EXAMPLES,
    )
    .arg(position("id", true, false))
}

const DELEGATE_REMOVE_HELP: &str = "Remove a registered delegate. ob stops handing it work and deletes its copy of the delegate's OBI.";
const DELEGATE_REMOVE_EXAMPLES: &str = "  ob delegate remove d_7f3a";

fn cmd_delegate_list() -> Command {
    leaf(
        "list",
        "List delegates",
        DELEGATE_LIST_HELP,
        DELEGATE_LIST_EXAMPLES,
    )
    .arg(format(&["text", "json"]))
    .arg(text("role", "only delegates for this role").value_parser([
        "invoke",
        "inspect",
        "synthesize",
    ]))
}

const DELEGATE_LIST_HELP: &str = "List registered delegates, their roles, and their preferences.";
const DELEGATE_LIST_EXAMPLES: &str = r#"  ob delegate list
  ob delegate list --role invoke"#;

fn cmd_delegate_show() -> Command {
    leaf(
        "show",
        "Show a delegate",
        DELEGATE_SHOW_HELP,
        DELEGATE_SHOW_EXAMPLES,
    )
    .arg(position("id", true, false))
    .arg(format(&["text", "json"]))
}

const DELEGATE_SHOW_HELP: &str = r#"Show a registered delegate, its roles, and its explicit role preferences.
-F json prints the full retained interface and rolePreferences, including
schemas, bindings, sources, and extension values. Extract .interface to reuse
it with delegate set --obi -."#;
const DELEGATE_SHOW_EXAMPLES: &str = "  ob delegate show d_91c2";

fn cmd_delegate_resolve() -> Command {
    leaf(
        "resolve",
        "Explain which handler ob would use",
        DELEGATE_RESOLVE_HELP,
        DELEGATE_RESOLVE_EXAMPLES,
    )
    .arg(format(&["text", "json"]))
    .arg(text("kind", "the exact kind (required)").required(true))
    .arg(
        text(
            "role",
            "the kind of work: invoke, inspect, or synthesize (required)",
        )
        .required(true)
        .value_parser(["invoke", "inspect", "synthesize"]),
    )
}

const DELEGATE_RESOLVE_HELP: &str = r#"Explain which handler ob would use for a kind of work on an exact kind: a
built-in handler or a delegate, and why. ob kind check answers only whether
this ob can handle the kind."#;
const DELEGATE_RESOLVE_EXAMPLES: &str =
    "  ob delegate resolve --role invoke --kind acme.billing-rpc@1";

fn cmd_describe() -> Command {
    leaf(
        "describe",
        "Describe this ob",
        DESCRIBE_HELP,
        DESCRIBE_EXAMPLES,
    )
    .arg(format(&["text", "json", "obi", "usage"]))
}

const DESCRIBE_HELP: &str = r#"Describe this ob: its version, the OpenBindings versions it reads, and what
it can handle. -F obi prints ob's own OBI, for tools that drive ob through
OpenBindings; -F usage prints its usage spec (usage.kdl)."#;
const DESCRIBE_EXAMPLES: &str = r#"  ob describe
  ob describe -F obi > ob.obi.json"#;
