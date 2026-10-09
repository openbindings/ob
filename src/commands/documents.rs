// Command declarations migrated from the pinned default Go surface.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![
        cmd_init(),
        cmd_show(),
        cmd_set(),
        cmd_validate(),
        cmd_diff(),
        cmd_compat(),
        cmd_fmt(),
        cmd_patch(),
        cmd_merge(),
        cmd_synthesize(),
        cmd_status(),
    ]
}

fn cmd_init() -> Command {
    leaf("init", "Create a new OBI", INIT_HELP, INIT_EXAMPLES)
        .arg(position("obi", false, false))
        .arg(text("description", "a human-readable description"))
        .arg(boolean("force", "replace an existing file"))
        .arg(text(
            "interface-version",
            "the document's own version label",
        ))
        .arg(text("name", "a human-readable name"))
        .arg(text("version", "").hide(true))
}

const INIT_HELP: &str = r#"Create a new OBI with an empty operations map, written to <obi>, or to
stdout when <obi> is omitted or -. It refuses to replace an existing file
unless --force is given.

--interface-version sets the document's own version label (its "version"
member). The OpenBindings version is always 0.2.0."#;
const INIT_EXAMPLES: &str = r#"  ob init tasks.obi.json --name "Task Manager"
  ob init --name "Task Manager" --interface-version 1.0.0 > tasks.obi.json"#;

fn cmd_show() -> Command {
    leaf("show", "Show a document", SHOW_HELP, SHOW_EXAMPLES)
        .arg(position("obi", true, false))
        .arg(format(&["text", "json"]))
}

const SHOW_HELP: &str = r#"Show an overview of a document: its operations, sources, bindings,
dependencies, and schemas. -F json prints the exact stored document; an OBI
is JSON, so there is no other document format.

<obi> is a path, - for stdin, or a URL. A bare origin such as
https://api.example.com is looked up at /.well-known/openbindings."#;
const SHOW_EXAMPLES: &str = r#"  ob show tasks.obi.json
  ob show https://api.example.com -F json"#;

fn cmd_set() -> Command {
    leaf(
        "set",
        "Change a document's name, version label, or description",
        SET_HELP,
        SET_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(text("description", "a human-readable description"))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text(
        "interface-version",
        "the document's own version label",
    ))
    .arg(text("name", "a human-readable name"))
    .arg(repeated(
        "unset",
        "remove a field: name, description, interface-version",
    ))
    .arg(text("version", "").hide(true))
}

const SET_HELP: &str = r#"Change the document's own fields: its name, its version label (the
document's "version" member, the interface's own label, not the
OpenBindings version), and its description. Only the flags you give change
anything. --unset removes a field, named as its flag is: name,
description, or interface-version.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SET_EXAMPLES: &str = r#"  ob set tasks.obi.json --interface-version 1.5.0
  ob set tasks.obi.json --unset description"#;

fn cmd_validate() -> Command {
    leaf(
        "validate",
        "Check a document, a value, or examples",
        VALIDATE_HELP,
        VALIDATE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(boolean(
        "examples",
        "check every operation example against its schemas",
    ))
    .arg(format(&["text", "json"]))
    .arg(repeated(
        "input",
        "an input value to check: JSON, @file, or - (repeatable)",
    ))
    .arg(text(
        "operation",
        "check a value against this operation (name or alias)",
    ))
    .arg(repeated(
        "output",
        "an output value to check: JSON, @file, or - (repeatable)",
    ))
    .arg(
        boolean(
            "quiet",
            "print nothing; report through the exit status only",
        )
        .short('q'),
    )
}

const VALIDATE_HELP: &str = r#"Check a document against OpenBindings 0.2. The result is one of:

  conformant                 every rule was checked and none is violated
  non-conformant             at least one rule is violated
  conformance undetermined   nothing is violated, but some rule could not be checked

A document written for an OpenBindings version ob does not support is
refused rather than judged. The report names the spec text it applied.

--operation checks values against that operation's schemas instead: each
--input or --output is a value (JSON), or a stream of JSON values from @file
or - for stdin; every value is checked and reported in turn. --examples
checks every operation's examples against its schemas, or with --operation,
one operation's.

Exit status: 0 conformant, or every value fits; 1 non-conformant, or some
value does not fit; 2 a usage error, including a name that is not in the
document; 3 refused (an OpenBindings version ob does not support); 4 no
verdict (conformance undetermined, or no schema to check the value against)."#;
const VALIDATE_EXAMPLES: &str = r#"  ob validate tasks.obi.json
  ob validate tasks.obi.json --operation createTask --input '{"title":"Write the docs"}'
  ob validate tasks.obi.json --examples --operation createTask"#;

fn cmd_diff() -> Command {
    leaf("diff", "Compare two documents", DIFF_HELP, DIFF_EXAMPLES)
        .arg(position("before", true, false))
        .arg(position("after", true, false))
        .arg(boolean("exit-code", "exit 1 when the documents differ"))
        .arg(format(&["text", "json"]))
        .arg(boolean("patch", "print the changes as a JSON Patch"))
}

const DIFF_HELP: &str = r#"List what changed between two documents, part by part. It reports
differences only. To check whether <after> still serves the callers of
<before>, use ob compat <after> <before>, which treats <before> as the
contract; ob compat also checks a document against a shared contract.
--patch prints the changes as an RFC 6902 JSON Patch, which ob patch can
apply."#;
const DIFF_EXAMPLES: &str = r#"  ob diff tasks.obi.json tasks-next.obi.json
  ob diff tasks.obi.json tasks-next.obi.json --patch > changes.json"#;

fn cmd_compat() -> Command {
    leaf(
        "compat",
        "Check a document against a shared contract",
        COMPAT_HELP,
        COMPAT_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("contract", true, false))
    .arg(format(&["text", "json"]))
    .arg(
        boolean(
            "quiet",
            "print nothing; report through the exit status only",
        )
        .short('q'),
    )
}

const COMPAT_HELP: &str = r#"Check whether <obi> satisfies a shared contract: for each contract
operation, some operation must answer to its name, and fit the contract in
the role the document gives it:

  provider   it has bindings: it must accept every input the contract
             allows, and return only outputs the contract allows
  consumer   a dependency calls it (spec §5.5): it must send only inputs
             the contract accepts, and take every output the contract allows

An operation with both is checked both ways; one with neither, such as an
operation in a pure contract, is also checked both ways. Schemas are compared under the Schema Comparison Profile
OB-2020-12, published with the OpenBindings interfaces (schema-comparison).
Both arguments may be paths or URLs.

To check whether a new version of a document breaks callers of the old one,
compare it against the old one as the contract: ob compat new.obi.json
old.obi.json.

For each contract operation that nothing answers to, it prints the two ways
to meet it: give one of your operations the contract's name with ob
operation set --add-alias, or add the contract's operation with ob merge.

The result is compatible, incompatible, or indeterminate, and as the profile
defines, indeterminate outranks incompatible: one comparison outside the
profile makes the whole result indeterminate, while the report still lists
every operation found incompatible.

JSON reports list operations in sorted contract-key order. Each issue has
an operation, kind (missing, output_incompatible, or input_incompatible),
and detail carrying the profile's reason; output issues precede input issues
for a matched pair. Outside-profile comparisons are reported as
indeterminate with their reason.

Exit status: 0 compatible; 1 incompatible; 4 indeterminate."#;
const COMPAT_EXAMPLES: &str = r#"  ob compat tasks.obi.json acme-tasks.obi.json
  ob compat https://api.example.com https://contracts.example.com/acme-tasks.json -q"#;

fn cmd_fmt() -> Command {
    leaf("fmt", "Format documents", FMT_HELP, FMT_EXAMPLES)
        .arg(position("obi", true, true))
        .arg(boolean(
            "canonical",
            "print the RFC 8785 canonical form instead",
        ))
        .arg(boolean(
            "check",
            "report files that would change and exit 1 if any would",
        ))
        .arg(boolean(
            "dry-run",
            "show the formatting change without writing it",
        ))
}

const FMT_HELP: &str = r#"Rewrite each document with two-space indentation and the spec's member
order (openbindings, name, version, description, schemas, operations,
dependencies, sources, bindings, and the same within each part). Entries
keep the order you gave them.

--check changes nothing; it lists files that would change and exits 1 if
any would. --dry-run shows the change without writing it. Give - to format
a document from stdin to stdout. Values, including numbers, are kept exactly
as written. --canonical prints the JSON Canonicalization Scheme form
(RFC 8785), for hashing and signing, instead of rewriting."#;
const FMT_EXAMPLES: &str = r#"  ob fmt tasks.obi.json
  ob fmt --check *.obi.json
  ob fmt --canonical tasks.obi.json | sha256sum"#;

fn cmd_patch() -> Command {
    leaf("patch", "Apply a JSON Patch", PATCH_HELP, PATCH_EXAMPLES)
        .arg(position("obi", true, false))
        .arg(position("patch", true, false))
        .arg(boolean("dry-run", "show the change without writing it"))
}

const PATCH_HELP: &str = r#"Apply an RFC 6902 JSON Patch (a file, or - for stdin) for edits no other
command covers, such as extension fields. Like every edit, it refuses a
patch that would break a document rule the document did not already break,
so you can repair a document one problem at a time.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const PATCH_EXAMPLES: &str = r#"  ob patch tasks.obi.json changes.json
  ob diff old.obi.json new.obi.json --patch | ob patch tasks.obi.json -"#;

fn cmd_merge() -> Command {
    leaf(
        "merge",
        "Bring operations in from another document",
        MERGE_HELP,
        MERGE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("from", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(boolean(
        "no-bindings",
        "bring operations without their bindings",
    ))
    .arg(repeated(
        "operation",
        "merge only this operation of <from> (repeatable)",
    ))
    .arg(boolean(
        "ours",
        "where the documents differ, keep what <obi> has",
    ))
    .arg(boolean(
        "theirs",
        "where the documents differ, take what <from> has",
    ))
}

const MERGE_HELP: &str = r#"Copy operations from <from> (a path or URL) into <obi>, with the schemas,
bindings, and sources they need. --operation limits the merge to the named
operations of <from>.

An entry both documents have, by the same name (for an operation, its key or
an alias), is left alone when the two are identical, and an operation of
<obi> that already meets <from>'s operation, as ob compat checks it, is left
alone and listed as met. Otherwise, when they differ, ob lists every
difference and writes nothing, unless you say which side wins: --ours keeps
what <obi> has, --theirs takes what <from> has.

A shared contract is just another OBI, so merging from one adds the contract
operations you don't have yet, under the contract's names and with its
schemas. To give an operation you already have a contract's name instead,
use ob operation set --add-alias. ob compat shows which are missing.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const MERGE_EXAMPLES: &str = r#"  ob merge tasks.obi.json tasks-next.obi.json --operation archiveTask
  ob merge tasks.obi.json tasks-next.obi.json --theirs
  ob merge tasks.obi.json acme-tasks.obi.json --operation acme.tasks.deleteTask"#;

fn cmd_synthesize() -> Command {
    leaf(
        "synthesize",
        "Create a document from an artifact",
        SYNTHESIZE_HELP,
        SYNTHESIZE_EXAMPLES,
    )
    .arg(position("artifact", true, false))
    .arg(boolean("force", "replace an existing file"))
    .arg(text("kind", "the artifact's exact kind (required)").required(true))
    .arg(text("name", "a human-readable name for the document"))
    .arg(text("out", "write the document to a file").short('o'))
    .arg(text(
        "source",
        "name for the source in the new document (default \"api\")",
    ))
}

const SYNTHESIZE_HELP: &str = r#"Create a new document from an artifact, such as an OpenAPI document, using
a handler for its kind: a source for the artifact, and one operation and
binding for each target it offers. <artifact> is a path or URL. Prints the
document unless -o is given; -o refuses to replace an existing file unless
--force is given.

To add another artifact to an existing document, use ob source import and
then ob source pull <obi> <source> --target <target> for one target, or
--all-targets for all of them."#;
const SYNTHESIZE_EXAMPLES: &str = r#"  ob synthesize ./openapi.json --kind example.openapi@1 -o tasks.obi.json
  ob synthesize https://api.example.com/mcp --kind example.mcp@1"#;

fn cmd_status() -> Command {
    leaf(
        "status",
        "Show how a document has drifted from its sources",
        STATUS_HELP,
        STATUS_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(boolean(
        "exit-code",
        "exit 1 when something has drifted, for CI",
    ))
    .arg(format(&["text", "json"]))
}

const STATUS_HELP: &str = r#"For every source, show how the document has drifted from it: bindings whose
content or target changed, and operations whose schemas the source describes
differently. Targets no binding covers are listed apart: leaving one unbound
can be a choice, so it is not drift. Changes nothing; it reports what ob
source pull --dry-run would, for all sources, as a check.

A source whose kind this ob cannot read is listed as not checked, and
status exits 4: it cannot say whether that source has drifted.

Exit status: 0 checked every source; 4 some source could not be checked.
With --exit-code: 1 when something has drifted."#;
const STATUS_EXAMPLES: &str = r#"  ob status tasks.obi.json
  ob status tasks.obi.json --exit-code"#;
