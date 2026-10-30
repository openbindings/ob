// Generated from the ob surface draft's help pages by gen_ob_surface.py; do not edit by hand.
use super::*;

pub(super) fn commands() -> Vec<Command> {
    vec![cmd_help(), cmd_completion()]
}

pub(super) fn cmd_help() -> Command {
    leaf(
        "help",
        "Print help for a command or a topic",
        PAGE_HELP,
        "ob help [<command>...]",
    )
    .arg(positional(
        "command",
        "A command's words, such as source pull, or a topic: obi, documents, exit-codes, output",
        false,
        true,
    ))
}
const PAGE_HELP: &str = r####"Print help for a command or a topic

Usage: ob help [<command>...]

Print the full help of a command, named by its words (ob help source
pull), or a help topic: obi (what an OBI contains, and which command does
what), documents (paths, standard input and URLs), exit-codes (what each
exit status means) and output (text and JSON output). With no argument,
print ob's root help, as ob --help does.

'ob <command> --help' prints the same full page, and 'ob <command> -h'
a summary cut from it: its usage, its first paragraph, its commands on a
group's page, its options, examples and exit status. 'ob help <command>
-h' prints that summary too.

Arguments:
  <command>...    A command's words, such as source pull, or a topic:
                  obi, documents, exit-codes, output

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob help
  ob help obi
  ob help source pull
  ob help source pull -h
  ob help exit-codes

Exit status:
  0    printed
  2    the words name no command or topic
  See 'ob help exit-codes'."####;

pub(super) fn cmd_completion() -> Command {
    leaf(
        "completion",
        "Print a shell completion script",
        PAGE_COMPLETION,
        "ob completion <shell>",
    )
    .arg(
        positional("shell", "bash, zsh, fish, or powershell", true, false).value_parser(
            clap::builder::PossibleValuesParser::new(["bash", "zsh", "fish", "powershell"]),
        ),
    )
}
const PAGE_COMPLETION: &str = r####"Print a shell completion script

Usage: ob completion <shell>

Print a completion script for bash, zsh, fish or powershell on standard
output. It completes command names, option names and fixed choices, such
as -F text|json and the names of help topics. It does not complete names
read from a document, such as operation keys, since completing them
would mean reading the document at each keystroke.

Save the script where your shell loads completions, as its own
instructions say, for example:

  ob completion zsh > "${fpath[1]}/_ob"
  ob completion bash > ~/.local/share/bash-completion/completions/ob
  ob completion fish > ~/.config/fish/completions/ob.fish

Arguments:
  <shell>    bash, zsh, fish, or powershell

Options:
  -h, --help    Print help (see a summary with '-h')

Examples:
  ob completion zsh
  ob completion bash > ~/.local/share/bash-completion/completions/ob

Exit status:
  0    printed
  2    the shell is not one of the four
  See 'ob help exit-codes'."####;
