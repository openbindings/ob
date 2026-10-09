mod command_id;
mod commands;
mod syntax;

use clap::error::ErrorKind;
use command_id::CommandId;
use std::{
    io::{self, Write},
    process::ExitCode,
};

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
    let mut command = commands::root();
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
