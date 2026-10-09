mod documents;
mod parts;
mod service;

use clap::{Arg, ArgAction, Command};

const NOTICE: &str = "COMMAND SURFACE ONLY: operational commands are not implemented.\nThe descriptions below specify intended behavior. No document, file, credential\nstore, or service is read or changed. Accepted operational requests exit 3.";

pub(crate) fn root() -> Command {
    let mut commands: Vec<_> = documents::commands()
        .into_iter()
        .chain(parts::commands())
        .chain(service::commands())
        .collect();
    commands.push(completion());
    let mut help = format!(
        "Work with OpenBindings interface documents\n\n{NOTICE}\n\nUsage: ob <COMMAND>\n\nStart here:\n  ob init --help\n  ob show --help\n  ob validate --help\n  ob invoke --help\n"
    );
    for heading in [
        "Documents",
        "Parts of a document",
        "Sources",
        "Using a service",
        "Advanced",
    ] {
        help.push_str(&format!("\n{heading}:\n"));
        for c in commands
            .iter()
            .filter(|c| root_heading(c.get_name()) == heading)
        {
            help.push_str(&format!(
                "  {:14} {}\n",
                c.get_name(),
                c.get_about().expect("command summary")
            ));
        }
    }
    help.push_str("\nOptions:\n  -h, --help       Print help\n  -V, --version    Print this build's version\n\nUse ob help <command> or ob <command> --help for command details.\nHelp, version, and completion generation work. Other commands exit 3 without\nrunning an operation; invalid arguments exit 2. No filename extension is assumed.\n");
    Command::new("ob")
        .version(concat!(
            env!("CARGO_PKG_VERSION"),
            " (command surface only)"
        ))
        .about("Work with OpenBindings interface documents")
        .override_help(help)
        .subcommands(commands)
}

fn root_heading(name: &str) -> &str {
    match name {
        "init" | "show" | "set" | "validate" | "diff" | "compat" | "fmt" | "patch" | "merge" => {
            "Documents"
        }
        "operation" | "source" | "binding" | "dependency" | "schema" => "Parts of a document",
        "synthesize" | "status" => "Sources",
        "fetch" | "invoke" | "context" => "Using a service",
        _ => "Advanced",
    }
}

fn leaf(
    name: &'static str,
    summary: &'static str,
    description: &'static str,
    examples: &'static str,
) -> Command {
    let command = Command::new(name)
        .about(summary)
        .long_about(format!("{NOTICE}\n\n{description}"))
        .after_help(NOTICE);
    if examples.is_empty() {
        command.after_long_help("")
    } else {
        command.after_long_help(format!("Examples (intended workflows):\n{examples}"))
    }
}

fn group(
    name: &'static str,
    summary: &'static str,
    description: &'static str,
    examples: &'static str,
) -> Command {
    leaf(name, summary, description, examples)
}

fn position(name: &'static str, required: bool, repeated: bool) -> Arg {
    let help = match name {
        "obi" => "OpenBindings document (see input/output rules above)",
        "before" => "Document before the change",
        "after" => "Document after the change",
        "contract" => "Contract document to check against",
        "from" => "Document to merge from",
        "patch" => "JSON Patch file, or - for stdin",
        "name" => "Entry name",
        "new-name" => "New entry name",
        "operation" => "Operation name or alias",
        "source" => "Source name (repeatable)",
        "artifact" => "Artifact path or URL",
        "uri" => "Schema URI (repeatable)",
        "url" => "Service or document URL",
        "scope" => "Context scope",
        "kind" => "Exact binding-kind identifier",
        "delegate-obi" => "Delegate's OpenBindings document",
        "id" => "Delegate registration ID",
        _ => name,
    };
    let arg = Arg::new(name)
        .value_name(name)
        .help(help)
        .required(required);
    if repeated {
        arg.num_args(1..)
    } else {
        arg.num_args(1)
    }
}

fn text(name: &'static str, help: &'static str) -> Arg {
    Arg::new(name).long(name).help(help).action(ArgAction::Set)
}

fn repeated(name: &'static str, help: &'static str) -> Arg {
    text(name, help).action(ArgAction::Append)
}

fn boolean(name: &'static str, help: &'static str) -> Arg {
    text(name, help)
        .num_args(0..=1)
        .require_equals(true)
        .default_missing_value("true")
        .default_value("false")
        .value_name("BOOL")
        .hide_default_value(true)
        .hide_possible_values(true)
        .value_parser(clap::value_parser!(bool))
}

fn integer(name: &'static str, help: &'static str) -> Arg {
    text(name, help)
        .allow_negative_numbers(true)
        .value_parser(clap::value_parser!(i64))
}

fn duration(name: &'static str, help: &'static str) -> Arg {
    text(name, help)
        .allow_negative_numbers(true)
        .value_parser(crate::syntax::duration)
}

fn format(allowed: &'static [&'static str]) -> Arg {
    text("format", "Output format")
        .short('F')
        .value_parser(clap::builder::PossibleValuesParser::new(
            allowed.iter().copied(),
        ))
        .default_value(allowed[0])
}

fn completion() -> Command {
    let mut command = Command::new("completion")
        .about("Print a shell completion script")
        .long_about("Print completion for command names, flags, and fixed choices.\nDocument-derived names await the operational implementation.\nRedirect stdout to save the script, then install it using your shell's instructions.");
    for shell in ["bash", "fish", "powershell", "zsh"] {
        command = command.subcommand(Command::new(shell).about("Print completion for this shell"));
    }
    command
}

#[cfg(test)]
mod tests;
