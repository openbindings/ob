// The command surface: one clap Command per page of the surface draft. The
// family modules are generated from the draft's help pages; this module holds
// the helpers they call and assembles the root.
mod documents;
mod help;
mod parts;
mod service;
// The generator writes a positional's choices as a borrowed array.
mod shell;
mod sources;

pub(crate) use help::{ROOT_HELP, summary};

use clap::{
    Arg, ArgAction, Command,
    builder::{PossibleValue, TypedValueParser},
};

pub(crate) const NOTICE: &str = "COMMAND SURFACE ONLY: operational commands are not implemented.\nThe help below specifies intended behavior. No document, file, context\nstore, or service is read or changed. Accepted operational requests exit 3.";

/// The help topics `ob help obi`, `ob help documents`, `ob help exit-codes`
/// and `ob help output`. They are pages of `ob help`, not commands.
pub(crate) const TOPICS: [(&str, &str); 4] = [
    ("obi", help::TOPIC_OBI),
    ("documents", help::TOPIC_DOCUMENTS),
    ("exit-codes", help::TOPIC_EXIT_CODES),
    ("output", help::TOPIC_OUTPUT),
];

pub(crate) fn root() -> Command {
    let commands: Vec<_> = documents::commands()
        .into_iter()
        .chain(parts::commands())
        .chain(sources::commands())
        .chain(service::commands())
        .chain(shell::commands())
        .collect();
    // `ob help <TAB>` completes the topics and the commands; any words parse,
    // and main.rs resolves them.
    let mut words: Vec<_> = TOPICS
        .iter()
        .map(|(name, _)| PossibleValue::new(*name).help("Help topic"))
        .collect();
    words.extend(
        commands
            .iter()
            .map(|command| PossibleValue::new(command.get_name().to_owned())),
    );
    let commands = commands.into_iter().map(|command| {
        if command.get_name() == "help" {
            command.mut_arg("command", |arg| arg.value_parser(HelpWords(words.clone())))
        } else {
            command
        }
    });
    Command::new("ob")
        .version(concat!(
            env!("CARGO_PKG_VERSION"),
            " (command surface only)"
        ))
        .about("Work with OpenBindings interface documents")
        .override_usage("ob <command> [options]")
        .override_help(format!("{NOTICE}\n\n{ROOT_HELP}"))
        // `ob help` is ob's own command, with its own page.
        .disable_help_subcommand(true)
        .subcommands(commands)
}

/// The words of `ob help`: any text parses, and the topics and command names
/// are offered for completion.
#[derive(Clone)]
struct HelpWords(Vec<PossibleValue>);

impl TypedValueParser for HelpWords {
    type Value = String;

    fn parse_ref(
        &self,
        command: &Command,
        arg: Option<&Arg>,
        value: &std::ffi::OsStr,
    ) -> Result<String, clap::Error> {
        clap::builder::StringValueParser::new().parse_ref(command, arg, value)
    }

    fn possible_values(&self) -> Option<Box<dyn Iterator<Item = PossibleValue> + '_>> {
        Some(Box::new(self.0.iter().cloned()))
    }
}

/// A command whose `--help` is its drafted page, printed as written. Clap
/// parses the arguments declared on it; main.rs prints its usage errors in
/// ob's one style.
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

/// A value option. A negative number, such as `--input -1`, is its value.
fn text(name: &'static str, help: &'static str) -> Arg {
    Arg::new(name)
        .long(name)
        .help(help)
        .action(ArgAction::Set)
        .allow_negative_numbers(true)
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
        .value_name("true|false")
        .hide_default_value(true)
        .hide_possible_values(true)
        .value_parser(clap::value_parser!(bool))
}

/// An integer option, such as binding add's `--preference <number>`. A
/// decimal with no fractional part, such as `1.0`, is the same integer.
fn integer(name: &'static str, help: &'static str) -> Arg {
    text(name, help)
        .value_name("number")
        .value_parser(move |value: &str| {
            crate::syntax::integer(value).map_err(|problem| format!("--{name} {problem}"))
        })
}

fn duration(name: &'static str, help: &'static str) -> Arg {
    text(name, help)
        .value_name("duration")
        .value_parser(move |value: &str| {
            crate::syntax::duration(value).map_err(|problem| format!("--{name} {problem}"))
        })
}

fn choice(name: &'static str, help: &'static str, allowed: &'static [&'static str]) -> Arg {
    // The value names the pages give these options.
    let value = match name {
        "check-as" => "how",
        "supports" => "op",
        "job" | "add-job" | "remove-job" => "job",
        _ => name,
    };
    text(name, help)
        .value_name(value)
        .value_parser(clap::builder::PossibleValuesParser::new(
            allowed.iter().copied(),
        ))
}

/// The value an optional-value option takes when it is given alone. No
/// argument can contain a NUL character, so it cannot be typed, and an
/// explicitly empty `--apply=` stays distinct from `--apply`.
pub(crate) const EVERY: &str = "\0";

/// `--name` alone, or `--name=<value>`, repeatable: `--apply` applies every
/// proposal and `--apply=<entry>` a named one. Alone it takes the value
/// [`EVERY`]; the grammar refuses the two forms together and an empty entry.
fn optional_value(name: &'static str, help: &'static str, value: &'static str) -> Arg {
    Arg::new(name)
        .long(name)
        .help(help)
        .value_name(value)
        .num_args(0..=1)
        .require_equals(true)
        .default_missing_value(EVERY)
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

#[cfg(test)]
mod tests;
