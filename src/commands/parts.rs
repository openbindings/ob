// Command declarations migrated from the pinned default Go surface.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![
        cmd_operation(),
        cmd_source(),
        cmd_binding(),
        cmd_dependency(),
        cmd_schema(),
    ]
}

fn cmd_operation() -> Command {
    group(
        "operation",
        "Add, change, and remove operations",
        OPERATION_HELP,
        OPERATION_EXAMPLES,
    )
    .subcommand(cmd_operation_add())
    .subcommand(cmd_operation_set())
    .subcommand(cmd_operation_rename())
    .subcommand(cmd_operation_remove())
    .subcommand(cmd_operation_list())
    .subcommand(cmd_operation_show())
    .subcommand(cmd_operation_example())
}

const OPERATION_HELP: &str = r#"An operation is a protocol-independent contract: a name, and optional
schemas for each input and output value. Bindings say how to carry it out;
dependencies say where it is called."#;
const OPERATION_EXAMPLES: &str = "";

fn cmd_operation_add() -> Command {
    leaf(
        "add",
        "Add an operation",
        OPERATION_ADD_HELP,
        OPERATION_ADD_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(repeated(
        "alias",
        "another name the operation answers to (repeatable)",
    ))
    .arg(boolean("deprecated", "mark the operation deprecated"))
    .arg(text("description", "what the operation does"))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text(
        "input-schema",
        "schema for each input value: JSON, @file, or -",
    ))
    .arg(text(
        "output-schema",
        "schema for each output value: JSON, @file, or -",
    ))
    .arg(repeated("tag", "a documentation tag (repeatable)"))
}

const OPERATION_ADD_HELP: &str = r#"Add an operation: a protocol-independent contract, with an optional schema
for each input value and each output value. Leaving a schema out leaves that
side unspecified. Aliases are other names the operation answers to, such as a
shared contract's name for it.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_ADD_EXAMPLES: &str = r##"  ob operation add tasks.obi.json archiveTask --description "Archive a task." \
      --input-schema '{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}' \
      --output-schema '{"$ref":"#/schemas/Task"}'"##;

fn cmd_operation_set() -> Command {
    leaf("set","Change an operation",OPERATION_SET_HELP,OPERATION_SET_EXAMPLES)
.arg(position("obi", true, false))
.arg(position("name", true, false))
.arg(repeated("add-alias","add an alias (repeatable)"))
.arg(repeated("add-tag","add a tag (repeatable)"))
.arg(boolean("deprecated","mark deprecated; --deprecated=false marks it current"))
.arg(text("description","what the operation does"))
.arg(boolean("dry-run","show the change without writing it"))
.arg(text("input-schema","schema for each input value: JSON, @file, or -"))
.arg(text("output-schema","schema for each output value: JSON, @file, or -"))
.arg(repeated("remove-alias","remove an alias (repeatable)"))
.arg(repeated("remove-tag","remove a tag (repeatable)"))
.arg(repeated("unset","remove a member: input-schema, output-schema, description, deprecated, aliases, tags, examples"))
}

const OPERATION_SET_HELP: &str = r#"Change an operation's description, schemas, aliases, tags, or deprecation.
Only the flags you give change anything. --unset removes a member, named as
its flag is (input-schema, output-schema, description, deprecated) or as the
whole list (aliases, tags, examples); unsetting a schema leaves that side
unspecified. Removing an alias or tag the operation does not have is a
usage error (exit 2).

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_SET_EXAMPLES: &str = r#"  ob operation set tasks.obi.json createTask --add-alias acme.tasks.addTask
  ob operation set tasks.obi.json listTasks --deprecated
  ob operation set tasks.obi.json completeTask --unset output-schema"#;

fn cmd_operation_rename() -> Command {
    leaf(
        "rename",
        "Rename an operation",
        OPERATION_RENAME_HELP,
        OPERATION_RENAME_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(position("new-name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const OPERATION_RENAME_HELP: &str = r#"Rename an operation's key, and update every binding and dependency that
uses it. Nothing else changes: its aliases stay as they are, and the old
name is gone. To keep answering to the old name, add it as an alias
afterwards with ob operation set --add-alias.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_RENAME_EXAMPLES: &str = "  ob operation rename tasks.obi.json createTask addTask";

fn cmd_operation_remove() -> Command {
    leaf(
        "remove",
        "Remove an operation",
        OPERATION_REMOVE_HELP,
        OPERATION_REMOVE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(boolean(
        "cascade",
        "also remove the bindings and dependencies that use it",
    ))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const OPERATION_REMOVE_HELP: &str = r#"Remove an operation, named by its key. It refuses while bindings or
dependencies use the operation; --cascade removes those too. Given an alias,
it refuses and says whose alias it is: an alias goes with ob operation set
--remove-alias.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_REMOVE_EXAMPLES: &str =
    "  ob operation remove tasks.obi.json completeTask --cascade";

fn cmd_operation_list() -> Command {
    leaf(
        "list",
        "List operations",
        OPERATION_LIST_HELP,
        OPERATION_LIST_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(format(&["text", "json"]))
}

const OPERATION_LIST_HELP: &str =
    "List operations with their aliases and how each is realized or consumed.";
const OPERATION_LIST_EXAMPLES: &str = "  ob operation list tasks.obi.json";

fn cmd_operation_show() -> Command {
    leaf(
        "show",
        "Show an operation",
        OPERATION_SHOW_HELP,
        OPERATION_SHOW_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(format(&["text", "json"]))
}

const OPERATION_SHOW_HELP: &str = r#"Show an operation, looked up by name or alias, with the bindings that
realize it. -F json prints the exact stored operation."#;
const OPERATION_SHOW_EXAMPLES: &str = r#"  ob operation show tasks.obi.json createTask
  ob operation show tasks.obi.json acme.tasks.createTask -F json"#;

fn cmd_operation_example() -> Command {
    group(
        "example",
        "Add, change, and remove operation examples",
        OPERATION_EXAMPLE_HELP,
        OPERATION_EXAMPLE_EXAMPLES,
    )
    .subcommand(cmd_operation_example_add())
    .subcommand(cmd_operation_example_set())
    .subcommand(cmd_operation_example_rename())
    .subcommand(cmd_operation_example_remove())
    .subcommand(cmd_operation_example_list())
    .subcommand(cmd_operation_example_show())
}

const OPERATION_EXAMPLE_HELP: &str = r#"An example is a named input value, output value, or both, that the
author claims fit the operation's schemas."#;
const OPERATION_EXAMPLE_EXAMPLES: &str = "";

fn cmd_operation_example_add() -> Command {
    leaf(
        "add",
        "Add an example to an operation",
        OPERATION_EXAMPLE_ADD_HELP,
        OPERATION_EXAMPLE_ADD_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("operation", true, false))
    .arg(position("name", true, false))
    .arg(text("description", "what the example shows"))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text(
        "input",
        "the example's input value: JSON, @file, or -",
    ))
    .arg(text(
        "output",
        "the example's output value: JSON, @file, or -",
    ))
}

const OPERATION_EXAMPLE_ADD_HELP: &str = r#"Add a named example: an input value, an output value, or both. ob checks
each value against the operation's schema and warns when it does not fit;
the schema always wins.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_EXAMPLE_ADD_EXAMPLES: &str =
    "  ob operation example add tasks.obi.json listTasks empty --input '{}' --output '[]'";

fn cmd_operation_example_set() -> Command {
    leaf(
        "set",
        "Change an example",
        OPERATION_EXAMPLE_SET_HELP,
        OPERATION_EXAMPLE_SET_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("operation", true, false))
    .arg(position("name", true, false))
    .arg(text("description", "what the example shows"))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text(
        "input",
        "the example's input value: JSON, @file, or -",
    ))
    .arg(text(
        "output",
        "the example's output value: JSON, @file, or -",
    ))
    .arg(repeated(
        "unset",
        "remove a member: input, output, description",
    ))
}

const OPERATION_EXAMPLE_SET_HELP: &str = r#"Change an example's input value, output value, or description. Only the
flags you give change anything; --unset removes one. ob warns when a value
does not fit the operation's schema; the schema always wins.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_EXAMPLE_SET_EXAMPLES: &str = "  ob operation example set tasks.obi.json createTask basic --input '{\"title\":\"Plan the release\"}'";

fn cmd_operation_example_rename() -> Command {
    leaf(
        "rename",
        "Rename an example",
        OPERATION_EXAMPLE_RENAME_HELP,
        OPERATION_EXAMPLE_RENAME_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("operation", true, false))
    .arg(position("name", true, false))
    .arg(position("new-name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const OPERATION_EXAMPLE_RENAME_HELP: &str = r#"Rename an example of an operation. Nothing else changes.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_EXAMPLE_RENAME_EXAMPLES: &str =
    "  ob operation example rename tasks.obi.json createTask basic minimal";

fn cmd_operation_example_remove() -> Command {
    leaf(
        "remove",
        "Remove an example",
        OPERATION_EXAMPLE_REMOVE_HELP,
        OPERATION_EXAMPLE_REMOVE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("operation", true, false))
    .arg(position("name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const OPERATION_EXAMPLE_REMOVE_HELP: &str = r#"Remove a named example from an operation.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const OPERATION_EXAMPLE_REMOVE_EXAMPLES: &str =
    "  ob operation example remove tasks.obi.json createTask basic";

fn cmd_operation_example_list() -> Command {
    leaf(
        "list",
        "List an operation's examples",
        OPERATION_EXAMPLE_LIST_HELP,
        OPERATION_EXAMPLE_LIST_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("operation", true, false))
    .arg(format(&["text", "json"]))
}

const OPERATION_EXAMPLE_LIST_HELP: &str = "List an operation's examples and what each includes.";
const OPERATION_EXAMPLE_LIST_EXAMPLES: &str =
    "  ob operation example list tasks.obi.json createTask";

fn cmd_operation_example_show() -> Command {
    leaf(
        "show",
        "Show an example",
        OPERATION_EXAMPLE_SHOW_HELP,
        OPERATION_EXAMPLE_SHOW_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("operation", true, false))
    .arg(position("name", true, false))
    .arg(format(&["text", "json"]))
}

const OPERATION_EXAMPLE_SHOW_HELP: &str =
    "Show an example. -F json prints the exact stored example.";
const OPERATION_EXAMPLE_SHOW_EXAMPLES: &str =
    "  ob operation example show tasks.obi.json createTask basic";

fn cmd_source() -> Command {
    group(
        "source",
        "Add, change, and remove sources, and read their artifacts",
        SOURCE_HELP,
        SOURCE_EXAMPLES,
    )
    .subcommand(cmd_source_add())
    .subcommand(cmd_source_set())
    .subcommand(cmd_source_rename())
    .subcommand(cmd_source_remove())
    .subcommand(cmd_source_list())
    .subcommand(cmd_source_show())
    .subcommand(cmd_source_import())
    .subcommand(cmd_source_inspect())
    .subcommand(cmd_source_pull())
}

const SOURCE_HELP: &str = r#"A source is a kind plus optional content the kind reads, such as an
artifact's address. Bindings realize operations through sources. import,
inspect, and pull use a handler for the source's kind."#;
const SOURCE_EXAMPLES: &str = "";

fn cmd_source_add() -> Command {
    leaf("add", "Add a source", SOURCE_ADD_HELP, SOURCE_ADD_EXAMPLES)
        .arg(position("obi", true, false))
        .arg(position("name", true, false))
        .arg(text("content", "content the kind reads: JSON, @file, or -"))
        .arg(text("description", "a human-readable description"))
        .arg(boolean("dry-run", "show the change without writing it"))
        .arg(text("kind", "the source's exact kind (required)").required(true))
}

const SOURCE_ADD_HELP: &str = r#"Add a source: a kind, and optional content that the kind reads, such as an
artifact's address or the artifact itself. ob stores both exactly as given,
and does not need to support the kind to do so. --content null stores an
explicit null, which is not the same as leaving content out.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SOURCE_ADD_EXAMPLES: &str = r#"  ob source add tasks.obi.json httpApi --kind example.openapi@1 \
      --content '{"location":"https://api.example.com/openapi.json"}'"#;

fn cmd_source_set() -> Command {
    leaf(
        "set",
        "Change a source",
        SOURCE_SET_HELP,
        SOURCE_SET_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(text("content", "content the kind reads: JSON, @file, or -"))
    .arg(text("description", "a human-readable description"))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text("kind", "the source's exact kind"))
    .arg(repeated("unset", "remove a member: content, description"))
}

const SOURCE_SET_HELP: &str = r#"Change a source's kind, content, or description. Only the flags you give
change anything; --unset removes content or the description. Changing the
kind changes how the source's bindings are read.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SOURCE_SET_EXAMPLES: &str = "  ob source set tasks.obi.json httpApi --content '{\"location\":\"https://api.example.com/v2/openapi.json\"}'";

fn cmd_source_rename() -> Command {
    leaf(
        "rename",
        "Rename a source",
        SOURCE_RENAME_HELP,
        SOURCE_RENAME_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(position("new-name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const SOURCE_RENAME_HELP: &str = r#"Rename a source, and update every binding that goes through it.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SOURCE_RENAME_EXAMPLES: &str = "  ob source rename tasks.obi.json httpApi restApi";

fn cmd_source_remove() -> Command {
    leaf(
        "remove",
        "Remove a source",
        SOURCE_REMOVE_HELP,
        SOURCE_REMOVE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(boolean("cascade", "also remove the bindings that use it"))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const SOURCE_REMOVE_HELP: &str = r#"Remove a source. It refuses while bindings use the source; --cascade
removes those bindings too.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SOURCE_REMOVE_EXAMPLES: &str = "  ob source remove tasks.obi.json mcpServer --cascade";

fn cmd_source_list() -> Command {
    leaf(
        "list",
        "List sources",
        SOURCE_LIST_HELP,
        SOURCE_LIST_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(format(&["text", "json"]))
}

const SOURCE_LIST_HELP: &str =
    "List sources with their kinds, and whether this ob can invoke through them.";
const SOURCE_LIST_EXAMPLES: &str = "  ob source list tasks.obi.json";

fn cmd_source_show() -> Command {
    leaf(
        "show",
        "Show a source",
        SOURCE_SHOW_HELP,
        SOURCE_SHOW_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(format(&["text", "json"]))
}

const SOURCE_SHOW_HELP: &str = r#"Show a source's kind, content, and the bindings that use it. -F json prints
the exact stored source; content is shown as stored, never interpreted."#;
const SOURCE_SHOW_EXAMPLES: &str = "  ob source show tasks.obi.json httpApi";

fn cmd_source_import() -> Command {
    leaf(
        "import",
        "Add a source built from an artifact",
        SOURCE_IMPORT_HELP,
        SOURCE_IMPORT_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(position("artifact", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text("kind", "the artifact's exact kind (required)").required(true))
}

const SOURCE_IMPORT_HELP: &str = r#"Add a source for an artifact (a path or URL), using a handler for its kind.
The handler decides what content to store, such as the artifact's address or
the artifact itself. To add operations and bindings from it, run ob source
pull with --target to bind one target or --all-targets to bind every target
afterwards.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SOURCE_IMPORT_EXAMPLES: &str = "  ob source import tasks.obi.json billing https://billing.example.com/openapi.json --kind example.openapi@1";

fn cmd_source_inspect() -> Command {
    leaf(
        "inspect",
        "List what a source offers",
        SOURCE_INSPECT_HELP,
        SOURCE_INSPECT_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(format(&["text", "json"]))
}

const SOURCE_INSPECT_HELP: &str = r#"List the targets a source offers, the things a binding could be written
for, using a handler for its kind. Each shows the operation name the
handler suggests and any binding that already uses it."#;
const SOURCE_INSPECT_EXAMPLES: &str = "  ob source inspect tasks.obi.json httpApi";

fn cmd_source_pull() -> Command {
    leaf("pull","Update a document from its sources",SOURCE_PULL_HELP,SOURCE_PULL_EXAMPLES)
.arg(position("obi", true, false))
.arg(position("source", false, true))
.arg(boolean("all-targets","bind every target no binding covers yet"))
.arg(text("binding-key","with --target: use this binding key; an identical binding is no change, a different one is refused"))
.arg(boolean("dry-run","show the change without writing it"))
.arg(text("new-operation","with --target: create the suggested operation under this new name"))
.arg(text("operation","with --target: bind it to this existing operation (key or alias)"))
.arg(text("target","bind this one target of the source, as ob source inspect lists it or by its identifier"))
.arg(repeated("update-operation","take the source's schemas for this operation (repeatable)"))
}

const SOURCE_PULL_HELP: &str = r#"Update the document from its sources, using a handler for each source's
kind. With no sources named, pull every source.

Pull refreshes the content of the bindings you have, so they keep up with
their sources. It adds nothing you did not ask for, and never changes an
operation you already have:

  - a target no binding covers is listed, not bound; --target binds one
    (and --all-targets binds every one), with the operation the handler
    suggests, or one you already have when you name it with --operation;
  - --new-operation names a new operation instead; it cannot combine with
    --operation. --binding-key names the binding. Without that flag, the
    handler's suggested binding key stays the same even when you choose a
    different operation. Existing operation fields are left unchanged;
  - where a source describes an operation's schemas differently, pull
    reports it, and --update-operation takes the source's schemas for that
    operation. When two sources being pulled describe it differently, pull
    refuses; pull one source to choose.

An unknown --operation is a usage error, never a request to create one.
An identical request is no change. An occupied binding key with different
source, content, or operation is refused, never overwritten or numbered.
To add another binding for an already bound target, give --binding-key;
to change a binding you have, use ob binding set. A repeated --new-operation
request is no change only when the requested binding already exists for it.
If the handler supplies no operation framing, a new operation states no
input or output schema. If it supplies no name, choose one explicitly.

Use --all-targets --dry-run to see every target's binding and whether its
operation would be created or reused, with commands for choosing an existing
operation instead. After an edit, the report gives binding set commands.
A naming collision refuses the whole edit, including any binding refreshes.

A source whose kind this ob cannot read is named and left alone, and pull
exits 4, since it cannot say the document is up to date with that source.
ob status reports the same, as a check, without changing anything.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SOURCE_PULL_EXAMPLES: &str = r#"  ob source pull tasks.obi.json
  ob source pull tasks.obi.json httpApi --update-operation listTasks
  ob source pull tasks.obi.json httpApi --target "POST /tasks/{id}/archive"
  ob source pull tasks.obi.json httpApi --target "POST /tasks/{id}/archive" --new-operation archive --binding-key archivalHttp
  ob source pull tasks.obi.json mcpServer --target tools/complete_task --operation completeTask
  ob source pull tasks.obi.json httpApi --all-targets --dry-run"#;

fn cmd_binding() -> Command {
    group(
        "binding",
        "Add, change, and remove bindings",
        BINDING_HELP,
        BINDING_EXAMPLES,
    )
    .subcommand(cmd_binding_add())
    .subcommand(cmd_binding_set())
    .subcommand(cmd_binding_rename())
    .subcommand(cmd_binding_remove())
    .subcommand(cmd_binding_list())
    .subcommand(cmd_binding_show())
}

const BINDING_HELP: &str = r#"A binding is a way to carry out an operation through a source. Its content
is read under the source's kind."#;
const BINDING_EXAMPLES: &str = "";

fn cmd_binding_add() -> Command {
    leaf("add","Add a binding",BINDING_ADD_HELP,BINDING_ADD_EXAMPLES)
.arg(position("obi", true, false))
.arg(position("name", true, false))
.arg(text("content","whatever the source's kind reads, such as which target to call: JSON, @file, or -"))
.arg(boolean("deprecated","mark deprecated; --deprecated=false marks it current"))
.arg(text("description","a human-readable description"))
.arg(boolean("dry-run","show the change without writing it"))
.arg(boolean("idempotent","claim that repeating a call adds no further intended effects; --idempotent=false claims it can"))
.arg(text("operation","the operation it carries out (name or alias)").required(true))
.arg(integer("preference","the author's preference among this operation's bindings (an integer; higher is stronger)").default_value("0"))
.arg(text("source","the source it goes through").required(true))
}

const BINDING_ADD_HELP: &str = r#"Add a binding: a way to carry out an operation through a source. --content
is whatever the source's kind needs, such as which endpoint or tool to call.
An operation can have several bindings, even through the same source.

--idempotent claims that repeating a call through this binding adds no
further intended effects (it says nothing about retry safety on its own);
--idempotent=false claims it can; leaving it out claims nothing.
--preference records the author's preference among the operation's
bindings; ob invoke shows it but does not choose by it.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const BINDING_ADD_EXAMPLES: &str = r#"  ob binding add tasks.obi.json completeTask.mcp --operation completeTask --source mcpServer \
      --content '{"target":"tools/complete_task"}' --idempotent"#;

fn cmd_binding_set() -> Command {
    leaf("set","Change a binding",BINDING_SET_HELP,BINDING_SET_EXAMPLES)
.arg(position("obi", true, false))
.arg(position("name", true, false))
.arg(text("content","whatever the source's kind reads, such as which target to call: JSON, @file, or -"))
.arg(boolean("deprecated","mark deprecated; --deprecated=false marks it current"))
.arg(text("description","a human-readable description"))
.arg(boolean("dry-run","show the change without writing it"))
.arg(boolean("idempotent","claim that repeating a call adds no further intended effects; --idempotent=false claims it can"))
.arg(text("operation","the operation it carries out (name or alias)"))
.arg(integer("preference","the author's preference among this operation's bindings (an integer; higher is stronger)").default_value("0"))
.arg(text("source","the source it goes through"))
.arg(repeated("unset","remove a member: content, idempotent, preference, description, deprecated"))
}

const BINDING_SET_HELP: &str = r#"Change a binding. Only the flags you give change anything; --unset removes
content, idempotent, preference, description, or deprecated.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const BINDING_SET_EXAMPLES: &str = r#"  ob binding set tasks.obi.json createTask.mcp --preference 5
  ob binding set tasks.obi.json listTasks.http --unset idempotent"#;

fn cmd_binding_rename() -> Command {
    leaf(
        "rename",
        "Rename a binding",
        BINDING_RENAME_HELP,
        BINDING_RENAME_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(position("new-name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const BINDING_RENAME_HELP: &str = r#"Rename a binding. Nothing else in a document refers to a binding by name,
so nothing else changes. Callers that name the old key, such as ob invoke
--binding, ob mcp --binding, or a generated client's binding list, stop
finding it.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const BINDING_RENAME_EXAMPLES: &str =
    "  ob binding rename tasks.obi.json createTask.http createTask.rest";

fn cmd_binding_remove() -> Command {
    leaf(
        "remove",
        "Remove a binding",
        BINDING_REMOVE_HELP,
        BINDING_REMOVE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const BINDING_REMOVE_HELP: &str = r#"Remove a binding. The operation and source stay.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const BINDING_REMOVE_EXAMPLES: &str = "  ob binding remove tasks.obi.json createTask.mcp";

fn cmd_binding_list() -> Command {
    leaf(
        "list",
        "List bindings",
        BINDING_LIST_HELP,
        BINDING_LIST_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(format(&["text", "json"]))
    .arg(text(
        "operation",
        "only this operation's bindings (name or alias)",
    ))
}

const BINDING_LIST_HELP: &str = "List bindings with their operation, source, and signals. --operation shows one operation's bindings.";
const BINDING_LIST_EXAMPLES: &str = r#"  ob binding list tasks.obi.json
  ob binding list tasks.obi.json --operation createTask"#;

fn cmd_binding_show() -> Command {
    leaf(
        "show",
        "Show a binding",
        BINDING_SHOW_HELP,
        BINDING_SHOW_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(format(&["text", "json"]))
}

const BINDING_SHOW_HELP: &str =
    "Show a binding. -F json prints the exact stored binding; content is shown as stored.";
const BINDING_SHOW_EXAMPLES: &str = "  ob binding show tasks.obi.json createTask.http";

fn cmd_dependency() -> Command {
    group(
        "dependency",
        "Add, change, and remove dependencies",
        DEPENDENCY_HELP,
        DEPENDENCY_EXAMPLES,
    )
    .subcommand(cmd_dependency_add())
    .subcommand(cmd_dependency_set())
    .subcommand(cmd_dependency_rename())
    .subcommand(cmd_dependency_remove())
    .subcommand(cmd_dependency_list())
    .subcommand(cmd_dependency_show())
}

const DEPENDENCY_HELP: &str = r#"A dependency is a point where the described software calls an operation
provided by something else. It names the operation, never a provider."#;
const DEPENDENCY_EXAMPLES: &str = "";

fn cmd_dependency_add() -> Command {
    leaf(
        "add",
        "Add a dependency",
        DEPENDENCY_ADD_HELP,
        DEPENDENCY_ADD_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(text("description", "a human-readable description"))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(repeated(
        "kind",
        "a kind of binding that can serve it (repeatable)",
    ))
    .arg(
        text(
            "operation",
            "the operation called at this point (name or alias)",
        )
        .required(true),
    )
}

const DEPENDENCY_ADD_HELP: &str = r#"Declare a dependency: a point where the described software calls an
operation that something else provides. --kind limits which kinds of
binding can serve it (any of those listed); leave it out to declare no kind
constraint (spec §5.5).

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const DEPENDENCY_ADD_EXAMPLES: &str =
    "  ob dependency add tasks.obi.json auditLog --operation events.deliver --kind example.grpc@1";

fn cmd_dependency_set() -> Command {
    leaf(
        "set",
        "Change a dependency",
        DEPENDENCY_SET_HELP,
        DEPENDENCY_SET_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(repeated("add-kind", "accept another kind (repeatable)"))
    .arg(text("description", "a human-readable description"))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text(
        "operation",
        "the operation called at this point (name or alias)",
    ))
    .arg(repeated(
        "remove-kind",
        "stop accepting a kind (repeatable)",
    ))
    .arg(repeated("unset", "remove a member: kinds, description"))
}

const DEPENDENCY_SET_HELP: &str = r#"Change a dependency. Only the flags you give change anything; --unset
removes kinds (declaring no kind constraint, spec §5.5) or the description.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const DEPENDENCY_SET_EXAMPLES: &str =
    "  ob dependency set tasks.obi.json notifier --add-kind example.mcp@1";

fn cmd_dependency_rename() -> Command {
    leaf(
        "rename",
        "Rename a dependency",
        DEPENDENCY_RENAME_HELP,
        DEPENDENCY_RENAME_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(position("new-name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const DEPENDENCY_RENAME_HELP: &str = r#"Rename a dependency. Nothing else in a document refers to a dependency by
name, so nothing else changes.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const DEPENDENCY_RENAME_EXAMPLES: &str = "  ob dependency rename tasks.obi.json notifier eventSink";

fn cmd_dependency_remove() -> Command {
    leaf(
        "remove",
        "Remove a dependency",
        DEPENDENCY_REMOVE_HELP,
        DEPENDENCY_REMOVE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const DEPENDENCY_REMOVE_HELP: &str = r#"Remove a dependency. The operation stays.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const DEPENDENCY_REMOVE_EXAMPLES: &str = "  ob dependency remove tasks.obi.json notifier";

fn cmd_dependency_list() -> Command {
    leaf(
        "list",
        "List dependencies",
        DEPENDENCY_LIST_HELP,
        DEPENDENCY_LIST_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(format(&["text", "json"]))
}

const DEPENDENCY_LIST_HELP: &str =
    "List dependencies with the operation each calls and the kinds it accepts.";
const DEPENDENCY_LIST_EXAMPLES: &str = "  ob dependency list tasks.obi.json";

fn cmd_dependency_show() -> Command {
    leaf(
        "show",
        "Show a dependency",
        DEPENDENCY_SHOW_HELP,
        DEPENDENCY_SHOW_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(format(&["text", "json"]))
}

const DEPENDENCY_SHOW_HELP: &str = "Show a dependency. -F json prints the exact stored dependency.";
const DEPENDENCY_SHOW_EXAMPLES: &str = "  ob dependency show tasks.obi.json notifier";

fn cmd_schema() -> Command {
    group(
        "schema",
        "Add, change, and remove reusable schemas",
        SCHEMA_HELP,
        SCHEMA_EXAMPLES,
    )
    .subcommand(cmd_schema_add())
    .subcommand(cmd_schema_set())
    .subcommand(cmd_schema_rename())
    .subcommand(cmd_schema_remove())
    .subcommand(cmd_schema_list())
    .subcommand(cmd_schema_show())
    .subcommand(cmd_schema_bundle())
}

const SCHEMA_HELP: &str = r##"Named JSON Schemas that operations share by reference, such as
{"$ref": "#/schemas/Task"}. bundle embeds the external schemas a document
references."##;
const SCHEMA_EXAMPLES: &str = "";

fn cmd_schema_add() -> Command {
    leaf(
        "add",
        "Add a reusable schema",
        SCHEMA_ADD_HELP,
        SCHEMA_ADD_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text("value", "the schema: JSON, @file, or - (required)").required(true))
}

const SCHEMA_ADD_HELP: &str = r##"Add a named JSON Schema that operations can reference as
{"$ref": "#/schemas/<name>"}.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."##;
const SCHEMA_ADD_EXAMPLES: &str = "  ob schema add tasks.obi.json TaskList --value '{\"type\":\"array\",\"items\":{\"$ref\":\"#/schemas/Task\"}}'";

fn cmd_schema_set() -> Command {
    leaf(
        "set",
        "Replace a reusable schema",
        SCHEMA_SET_HELP,
        SCHEMA_SET_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
    .arg(text("value", "the new schema: JSON, @file, or - (required)").required(true))
}

const SCHEMA_SET_HELP: &str = r#"Replace a named schema with a new one.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SCHEMA_SET_EXAMPLES: &str =
    "  ob schema set tasks.obi.json Problem --value @problem.schema.json";

fn cmd_schema_rename() -> Command {
    leaf(
        "rename",
        "Rename a reusable schema",
        SCHEMA_RENAME_HELP,
        SCHEMA_RENAME_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(position("new-name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const SCHEMA_RENAME_HELP: &str = r##"Rename a reusable schema, and update every reference to it, such as
{"$ref": "#/schemas/<name>"}.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."##;
const SCHEMA_RENAME_EXAMPLES: &str = "  ob schema rename tasks.obi.json Problem Error";

fn cmd_schema_remove() -> Command {
    leaf(
        "remove",
        "Remove a reusable schema",
        SCHEMA_REMOVE_HELP,
        SCHEMA_REMOVE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const SCHEMA_REMOVE_HELP: &str = r#"Remove a named schema. It refuses while anything references it.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SCHEMA_REMOVE_EXAMPLES: &str = "  ob schema remove tasks.obi.json Problem";

fn cmd_schema_list() -> Command {
    leaf(
        "list",
        "List reusable schemas",
        SCHEMA_LIST_HELP,
        SCHEMA_LIST_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(format(&["text", "json"]))
}

const SCHEMA_LIST_HELP: &str = r#"List named schemas and what references each, then the external schemas the
document references that no schema in it declares (ob schema bundle embeds
them)."#;
const SCHEMA_LIST_EXAMPLES: &str = "  ob schema list tasks.obi.json";

fn cmd_schema_show() -> Command {
    leaf(
        "show",
        "Show a reusable schema",
        SCHEMA_SHOW_HELP,
        SCHEMA_SHOW_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("name", true, false))
    .arg(format(&["text", "json"]))
}

const SCHEMA_SHOW_HELP: &str = "Print a named schema.";
const SCHEMA_SHOW_EXAMPLES: &str = "  ob schema show tasks.obi.json Task";

fn cmd_schema_bundle() -> Command {
    leaf(
        "bundle",
        "Embed external schemas",
        SCHEMA_BUNDLE_HELP,
        SCHEMA_BUNDLE_EXAMPLES,
    )
    .arg(position("obi", true, false))
    .arg(position("uri", false, true))
    .arg(boolean("dry-run", "show the change without writing it"))
}

const SCHEMA_BUNDLE_HELP: &str = r#"Embed the external schemas the document references, so it no longer needs
the network to resolve them. ob fetches each one and adds it to schemas
with its $id (which must match the URI it came from), along with
the external schemas it references in turn. References do not change: a
reference to that URI now resolves to the copy in the document, as JSON
Schema's bundling defines (spec §7.4). Named URIs limit it to those, and
what they reference. Each copy is named from its URI; ob schema rename
renames one. A different declared $id or a non-2020-12 $schema is refused
with the URI and reason; nothing is written and that URI stays external.
ob schema list shows what is still external.

Changes <obi> in place. Give - as <obi> to read a document from stdin and
print the result instead, or use --dry-run to see the change without
writing it."#;
const SCHEMA_BUNDLE_EXAMPLES: &str = r#"  ob schema bundle tasks.obi.json
  ob schema bundle tasks.obi.json https://schemas.example.com/time/date-time.json"#;
