mod command_id;
mod commands;
mod syntax;

use clap::error::ErrorKind;
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
            let _ = writeln!(io::stderr(), "ob: {error}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> io::Result<u8> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if let Some((path, hint)) = removed_command(&args) {
        writeln!(
            io::stderr(),
            "error: ob has no {path} command\nhint: {hint}"
        )?;
        return Ok(2);
    }
    let mut command = commands::root();
    if let Some(text) = short_help(&command, &args) {
        write!(io::stdout(), "{text}")?;
        return Ok(0);
    }
    let matches = match command.try_get_matches_from_mut(std::env::args_os()) {
        Ok(matches) => matches,
        Err(error) => {
            let code = if matches!(
                error.kind(),
                ErrorKind::DisplayHelp | ErrorKind::DisplayVersion
            ) {
                0
            } else {
                2
            };
            if error.use_stderr() {
                write!(io::stderr(), "{error}")?;
            } else {
                write!(io::stdout(), "{error}")?;
            }
            return Ok(code);
        }
    };
    if let Some(("completion", sub)) = matches.subcommand()
        && let Some((shell, _)) = sub.subcommand()
    {
        let shell = match shell {
            "bash" => clap_complete::Shell::Bash,
            "fish" => clap_complete::Shell::Fish,
            "powershell" => clap_complete::Shell::PowerShell,
            "zsh" => clap_complete::Shell::Zsh,
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
        // A group named without a subcommand, or a help topic: print its page.
        definition.print_long_help()?;
        writeln!(io::stdout())?;
        return Ok(0);
    };
    if let Err(message) = syntax::validate(id, leaf, definition) {
        writeln!(
            io::stderr(),
            "ob: {message}\nRun '{path} --help' for the intended syntax."
        )?;
        return Ok(2);
    }
    // This is the only domain dispatch boundary. Future handlers receive validated
    // command input here; parsing/help must remain independent of SDK or host setup.
    writeln!(
        io::stderr(),
        "ob: '{path}' is not implemented in this Rust command-surface build; no operation ran."
    )?;
    Ok(3)
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
/// `ob help <command>` print the whole page.
fn short_help(root: &clap::Command, args: &[String]) -> Option<String> {
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
            Some(sub) if !word.starts_with('-') => {
                path.push(' ');
                path.push_str(word);
                current = sub;
            }
            _ => break,
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
    Some(format!("{}\n\n{text}", commands::NOTICE))
}
