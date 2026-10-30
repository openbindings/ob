mod command_id;
mod commands;
mod syntax;

use clap::{
    Command,
    error::{ContextKind, ContextValue, ErrorKind},
};
use command_id::CommandId;
use std::{
    io::{self, Write},
    process::ExitCode,
};

/// Commands the surface removed or renamed, each with the hint the draft
/// gives (spine, section 3, "Help and completion").
const REMOVED: &[(&str, &str)] = &[
    (
        "set",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "edit",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "operation add",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "operation set",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "operation example",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "source set",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "schema add",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "schema set",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "dependency add",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "dependency set",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "dependency rename",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "dependency remove",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "binding set",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "binding rename",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "binding remove",
        "edit the document in your editor, then run ob validate; scripts use ob patch",
    ),
    (
        "synthesize",
        "ob source add adds a source; ob source pull pulls its operations in",
    ),
    (
        "source import",
        "ob source add adds a source; ob source pull pulls its operations in",
    ),
    (
        "source inspect",
        "ob source pull --dry-run shows what a pull would add; ob kind describe shows what a kind reads",
    ),
    (
        "status",
        "ob source pull --check checks a document against its sources",
    ),
    (
        "source status",
        "ob source pull --check checks a document against its sources",
    ),
    (
        "kind check",
        "ob kind list --supports <op> lists what this ob supports; ob kind describe describes a kind",
    ),
    (
        "delegate roles",
        "ob delegate jobs lists the jobs and their rule",
    ),
    (
        "version",
        "ob --version prints the version; ob describe describes this ob",
    ),
];

fn main() -> ExitCode {
    match run() {
        Ok(code) => ExitCode::from(code),
        Err(error) if error.kind() == io::ErrorKind::BrokenPipe => ExitCode::SUCCESS,
        Err(error) => {
            let _ = writeln!(io::stderr(), "error: {error}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> io::Result<u8> {
    // Words for matching commands; clap parses the arguments as given and
    // refuses one that is not UTF-8 with a usage error, not a panic.
    let args: Vec<String> = std::env::args_os()
        .skip(1)
        .map(|arg| arg.to_string_lossy().into_owned())
        .collect();
    if let Some((path, hint)) = removed_command(&args) {
        return usage_error(&format!("ob has no {path} command"), hint);
    }
    let mut command = commands::root();
    if args.first().is_some_and(|word| word == "help") {
        return help(&mut command, &args[1..]);
    }
    if let Some(text) = short_help(&command, &args) {
        write!(io::stdout(), "{text}")?;
        return Ok(0);
    }
    let matches = match command.try_get_matches_from_mut(std::env::args_os()) {
        Ok(matches) => matches,
        Err(error) => return parse_error(&error, &command, &args),
    };
    if let Some(("completion", sub)) = matches.subcommand() {
        let shell = match sub.get_one::<String>("shell").map(String::as_str) {
            Some("bash") => clap_complete::Shell::Bash,
            Some("fish") => clap_complete::Shell::Fish,
            Some("powershell") => clap_complete::Shell::PowerShell,
            Some("zsh") => clap_complete::Shell::Zsh,
            _ => unreachable!("parser limits completion shells"),
        };
        // Generate to memory so writing a closed pipe is handled without a generator panic.
        let mut output = Vec::new();
        clap_complete::generate(shell, &mut command, "ob", &mut output);
        io::stdout().write_all(&output)?;
        return Ok(0);
    }

    let mut path = String::from("ob");
    let mut leaf = &matches;
    let mut definition = &mut command;
    while let Some((name, nested)) = leaf.subcommand() {
        path.push(' ');
        path.push_str(name);
        leaf = nested;
        definition = definition
            .find_subcommand_mut(name)
            .expect("parsed command exists");
    }
    let Some(id) = CommandId::from_path(&path) else {
        // A group named without a subcommand: print its page, as --help does.
        write!(io::stdout(), "{}", definition.render_long_help())?;
        return Ok(0);
    };
    if let Err(message) = syntax::validate(id, leaf, definition) {
        return usage_error(&message, &format!("{path} --help shows the syntax"));
    }
    // This is the only domain dispatch boundary. Future handlers receive validated
    // command input here; parsing/help must remain independent of SDK or host setup.
    writeln!(
        io::stderr(),
        "ob: '{path}' is not implemented in this Rust command-surface build; no operation ran."
    )?;
    Ok(3)
}

/// Every usage error has one shape on standard error: an `error:` line,
/// optional indented detail lines, and one `hint:` line that gives a command
/// to run (spine, R7). It exits 2.
fn usage_error(message: &str, hint: &str) -> io::Result<u8> {
    writeln!(io::stderr(), "error: {message}\nhint: {hint}")?;
    Ok(2)
}

/// A clap parse error in ob's usage-error shape: clap's message and detail
/// lines, its suggestion, and the command whose help shows the syntax. Clap's
/// usage block, its "For more information" line and its tips are left out.
fn parse_error(error: &clap::Error, root: &Command, args: &[String]) -> io::Result<u8> {
    if matches!(
        error.kind(),
        ErrorKind::DisplayHelp | ErrorKind::DisplayVersion
    ) {
        write!(io::stdout(), "{error}")?;
        return Ok(0);
    }
    let (path, command) = command_path(root, args);
    let message = match std::error::Error::source(error) {
        // ob's own value parsers say what the option takes, without echoing it.
        Some(problem) if error.kind() == ErrorKind::ValueValidation => problem.to_string(),
        _ => {
            let rendered = error.render().to_string();
            let message = rendered.split("\n\n").next().unwrap_or_default();
            message
                .strip_prefix("error: ")
                .unwrap_or(message)
                .trim_end()
                .to_owned()
        }
    };
    let syntax = if command.has_subcommands() {
        format!("{path} --help lists the commands")
    } else {
        format!("{path} --help shows the syntax")
    };
    let hint = match suggestion(error, &path) {
        Some(suggestion) => format!("did you mean {suggestion}? {syntax}"),
        None => syntax,
    };
    usage_error(&message, &hint)
}

/// The command a command line names: its leading words that are commands.
/// The root and the groups take no positional arguments, so the walk is exact.
fn command_path<'a>(root: &'a Command, args: &[String]) -> (String, &'a Command) {
    let mut path = String::from("ob");
    let mut current = root;
    for word in args {
        let Some(sub) = current.find_subcommand(word) else {
            break;
        };
        path.push(' ');
        path.push_str(word);
        current = sub;
    }
    (path, current)
}

/// Clap's "did you mean" for a parse error: a similar command, option or
/// value. Its other tips, such as passing a value after `--`, are not
/// offered: a negative number is already a value.
fn suggestion(error: &clap::Error, path: &str) -> Option<String> {
    fn names(value: &ContextValue) -> Vec<String> {
        match value {
            ContextValue::String(name) => vec![name.clone()],
            ContextValue::Strings(names) => names.clone(),
            _ => Vec::new(),
        }
    }
    let names = if let Some(value) = error.get(ContextKind::SuggestedSubcommand) {
        // Clap's order, except that a command the typed word begins comes
        // first: `ob op` most likely meant `ob operation`.
        let mut names = names(value);
        if let Some(ContextValue::String(typed)) = error.get(ContextKind::InvalidSubcommand) {
            names.sort_by_key(|name| !name.starts_with(typed.as_str()));
        }
        names
            .into_iter()
            .map(|name| format!("{path} {name}"))
            .collect()
    } else if let Some(value) = error
        .get(ContextKind::SuggestedArg)
        .or_else(|| error.get(ContextKind::SuggestedValue))
    {
        names(value)
    } else if let Some(ContextValue::StyledStrs(tips)) = error.get(ContextKind::Suggested) {
        // "'pull --apply' exists": an option of one of this group's commands.
        tips.iter()
            .filter_map(|tip| {
                let tip = tip.to_string();
                let found = tip.strip_prefix('\'')?.strip_suffix("' exists")?;
                Some(format!("{path} {found}"))
            })
            .collect()
    } else {
        Vec::new()
    };
    match names.as_slice() {
        [] => None,
        [one] => Some(one.clone()),
        [rest @ .., last] => Some(format!("{} or {last}", rest.join(", "))),
    }
}

/// `ob help [<command>...]`: the root help, a command's full page or a topic;
/// with `-h`, the command's summary. `ob help --help` and `ob help -h` print
/// the help command's own page and summary.
fn help(root: &mut Command, args: &[String]) -> io::Result<u8> {
    let mut summary = false;
    let mut long = false;
    let mut escaped = false;
    let mut rest = Vec::new();
    for arg in args {
        match arg.as_str() {
            "-h" if !escaped => summary = true,
            "--help" if !escaped => long = true,
            word => {
                escaped |= word == "--";
                rest.push(word);
            }
        }
    }
    // Clap checks the rest as the help command's arguments, so an option
    // `ob help` does not take is a usage error like any other.
    let argv = ["ob", "help"].into_iter().chain(rest.iter().copied());
    let matches = match root.try_get_matches_from_mut(argv) {
        Ok(matches) => matches,
        Err(error) => return parse_error(&error, root, &["help".to_owned()]),
    };
    let words: Vec<&str> = matches
        .subcommand_matches("help")
        .and_then(|help| help.get_many::<String>("command"))
        .into_iter()
        .flatten()
        .map(String::as_str)
        .collect();
    let text = if words.is_empty() {
        if summary {
            surface(commands::summary("ob help").expect("ob help has a summary"))
        } else if long {
            let help = root.find_subcommand_mut("help").expect("ob help exists");
            help.render_long_help().to_string()
        } else {
            root.render_long_help().to_string()
        }
    } else if let [word] = words.as_slice()
        && let Some((_, topic)) = commands::TOPICS.iter().find(|(name, _)| name == word)
    {
        surface(topic)
    } else {
        let mut current: &Command = root;
        let mut path = String::from("ob");
        for (index, word) in words.iter().enumerate() {
            let Some(sub) = current.find_subcommand(word) else {
                return unknown_help(root, &words, index);
            };
            path.push(' ');
            path.push_str(word);
            current = sub;
        }
        if summary {
            surface(commands::summary(&path).expect("every page has a summary"))
        } else {
            current.clone().render_long_help().to_string()
        }
    };
    write!(io::stdout(), "{text}")?;
    Ok(0)
}

/// `ob help` given words that name no command or topic, the first unknown
/// one at `index`.
fn unknown_help(root: &Command, words: &[&str], index: usize) -> io::Result<u8> {
    let joined = words.join(" ");
    let typed: Vec<String> = words.iter().map(|word| (*word).to_owned()).collect();
    if let Some((path, hint)) = removed_command(&typed) {
        return usage_error(&format!("ob has no {path} command"), hint);
    }
    let parent = words[..index]
        .iter()
        .fold(root, |command, word| command.find_subcommand(word).unwrap());
    let mut candidates: Vec<&str> = parent
        .get_subcommands()
        .filter(|command| !command.is_hide_set())
        .map(Command::get_name)
        .collect();
    if index == 0 {
        candidates.extend(commands::TOPICS.iter().map(|(name, _)| *name));
    }
    let hint = match closest(words[index], &candidates) {
        Some(candidate) => {
            // Offer the corrected words when they name a page, else the
            // words up to the correction.
            let mut fixed: Vec<&str> = words.to_vec();
            fixed[index] = candidate;
            if !names_page(root, &fixed) {
                fixed.truncate(index + 1);
            }
            format!("did you mean ob help {}?", fixed.join(" "))
        }
        None => "ob help lists the commands and topics".to_owned(),
    };
    usage_error(&format!("no command or topic named \"{joined}\""), &hint)
}

fn names_page(root: &Command, words: &[&str]) -> bool {
    if let [word] = words
        && commands::TOPICS.iter().any(|(name, _)| name == word)
    {
        return true;
    }
    let mut current = root;
    for word in words {
        match current.find_subcommand(word) {
            Some(sub) => current = sub,
            None => return false,
        }
    }
    true
}

/// The candidate a mistyped word most likely meant: one it begins, else the
/// nearest within two edits.
fn closest<'a>(word: &str, candidates: &[&'a str]) -> Option<&'a str> {
    if let Some(candidate) = candidates
        .iter()
        .find(|candidate| !word.is_empty() && candidate.starts_with(word))
    {
        return Some(candidate);
    }
    candidates
        .iter()
        .map(|candidate| (edit_distance(word, candidate), *candidate))
        .filter(|(distance, _)| *distance <= 2 && *distance < word.chars().count())
        .min_by_key(|(distance, _)| *distance)
        .map(|(_, candidate)| candidate)
}

/// Levenshtein distance, counted in characters.
fn edit_distance(a: &str, b: &str) -> usize {
    let b: Vec<char> = b.chars().collect();
    let mut previous: Vec<usize> = (0..=b.len()).collect();
    for (i, x) in a.chars().enumerate() {
        let mut current = vec![i + 1];
        for (j, y) in b.iter().enumerate() {
            let cost = usize::from(x != *y);
            current.push(
                (previous[j] + cost)
                    .min(previous[j + 1] + 1)
                    .min(current[j] + 1),
            );
        }
        previous = current;
    }
    previous[b.len()]
}

/// A help text with the command-surface notice before it.
fn surface(text: &str) -> String {
    let text = text.trim_end_matches('\n');
    format!("{}\n\n{text}\n", commands::NOTICE)
}

/// A removed command typed as the leading words of the command line.
fn removed_command(args: &[String]) -> Option<(&'static str, &'static str)> {
    REMOVED.iter().copied().find(|(path, _)| {
        let words: Vec<_> = path.split(' ').collect();
        args.len() >= words.len() && args.iter().zip(&words).all(|(a, w)| a == w)
    })
}

/// `-h` prints the summary cut from the command's page (usage, first
/// paragraph, commands, options, examples, exit status); `--help` and
/// `ob help <command>` print the whole page. `ob help <command> -h` is
/// handled with the help command.
fn short_help(root: &Command, args: &[String]) -> Option<String> {
    let mut words = args.iter();
    let mut path = String::from("ob");
    let mut current = root;
    let mut seen = false;
    for word in words.by_ref() {
        if word == "-h" {
            seen = true;
            break;
        }
        if word == "--" {
            return None;
        }
        match current.find_subcommand(word) {
            Some(sub) => {
                path.push(' ');
                path.push_str(word);
                current = sub;
            }
            None => break,
        }
    }
    if !seen && !words.any(|word| word == "-h") {
        return None;
    }
    let text = if path == "ob" {
        commands::ROOT_HELP
    } else {
        commands::summary(&path)?
    };
    Some(surface(text))
}
