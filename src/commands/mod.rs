// The command surface: one clap Command per page of the surface draft. The
// family modules are generated from the draft's help pages; this module holds
// the helpers they call and assembles the root.
mod documents;
mod help;
mod parts;
mod service;
mod sources;

pub(crate) use help::{ROOT_HELP, summary};

use clap::{Arg, ArgAction, Command};

pub(crate) const NOTICE: &str = "COMMAND SURFACE ONLY: operational commands are not implemented.\nThe help below specifies intended behavior. No document, file, context\nstore, or service is read or changed. Accepted operational requests exit 3.";

/// The help topics `ob help documents`, `ob help exit-codes` and `ob help output`.
pub(crate) const TOPICS: [(&str, &str); 3] = [
    ("documents", help::TOPIC_DOCUMENTS),
    ("exit-codes", help::TOPIC_EXIT_CODES),
    ("output", help::TOPIC_OUTPUT),
];

pub(crate) fn root() -> Command {
    let mut commands: Vec<_> = documents::commands()
        .into_iter()
        .chain(parts::commands())
        .chain(sources::commands())
        .chain(service::commands())
        .collect();
    commands.push(completion());
    for (name, text) in TOPICS {
        commands.push(
            Command::new(name)
                .about("Help topic")
                .hide(true)
                .override_usage(format!("ob help {name}"))
                .override_help(format!("{NOTICE}\n\n{text}")),
        );
    }
    Command::new("ob")
        .version(concat!(
            env!("CARGO_PKG_VERSION"),
            " (command surface only)"
        ))
        .about("Work with OpenBindings interface documents")
        .override_usage("ob <command> [options]")
        .override_help(format!("{NOTICE}\n\n{ROOT_HELP}"))
        .subcommands(commands)
}

/// A command whose `--help` is its drafted page, printed as written. Clap
/// still parses the arguments declared on it and prints its own usage errors.
fn page(
    name: &'static str,
    summary: &'static str,
    text: &'static str,
    usage: &'static str,
) -> Command {
    Command::new(name)
        .about(summary)
        .override_usage(usage)
        .override_help(format!("{NOTICE}\n\n{text}"))
}

fn leaf(
    name: &'static str,
    summary: &'static str,
    text: &'static str,
    usage: &'static str,
) -> Command {
    page(name, summary, text, usage)
}

fn group(
    name: &'static str,
    summary: &'static str,
    text: &'static str,
    usage: &'static str,
) -> Command {
    page(name, summary, text, usage)
}

fn positional(name: &'static str, help: &'static str, required: bool, repeated: bool) -> Arg {
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

fn choice(name: &'static str, help: &'static str, allowed: &'static [&'static str]) -> Arg {
    text(name, help).value_parser(clap::builder::PossibleValuesParser::new(
        allowed.iter().copied(),
    ))
}

/// `--name` alone, or `--name=<value>`, repeatable: `--force` applies every
/// proposal and `--force=<entry>` a named one. An empty value stands for
/// "every"; the grammar refuses the two forms together.
fn optional_value(name: &'static str, help: &'static str, value: &'static str) -> Arg {
    Arg::new(name)
        .long(name)
        .help(help)
        .value_name(value)
        .num_args(0..=1)
        .require_equals(true)
        .default_missing_value("")
        .action(ArgAction::Append)
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
