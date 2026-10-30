//! Argument grammar only. This module never opens files, reads stdin, or interprets an OBI.
use crate::command_id::CommandId;
use clap::{ArgMatches, Command, parser::ValueSource};

fn present(m: &ArgMatches, name: &str) -> bool {
    m.try_contains_id(name).unwrap_or(false)
        && m.value_source(name) == Some(ValueSource::CommandLine)
}
fn on(m: &ArgMatches, name: &str) -> bool {
    m.try_get_one::<bool>(name)
        .ok()
        .flatten()
        .copied()
        .unwrap_or(false)
}
fn values<'a>(m: &'a ArgMatches, name: &str) -> Vec<&'a str> {
    m.try_get_many::<String>(name)
        .ok()
        .flatten()
        .into_iter()
        .flatten()
        .map(String::as_str)
        .collect()
}
fn input_reference(value: &str) -> bool {
    value == "-" || value.strip_prefix('@').is_some_and(|path| !path.is_empty())
}

/// Commands that change a document: `-F json` needs `--dry-run` when the
/// document is read from standard input, since standard output then carries
/// the document (spine, R5).
fn edits_document(id: CommandId) -> bool {
    use CommandId::*;
    matches!(
        id,
        Merge
            | Patch
            | OperationRename
            | OperationRemove
            | BindingAdd
            | SchemaRename
            | SchemaRemove
            | SchemaBundle
            | SourceAdd
            | SourcePull
            | SourceRename
            | SourceRemove
    )
}

pub(crate) fn validate(id: CommandId, m: &ArgMatches, command: &Command) -> Result<(), String> {
    use CommandId::*;
    let usage = |message: &str| Err(message.to_owned());
    // Count input sources, not arbitrary '-' strings (a description, operation
    // name, or output path does not consume stdin).
    let mut stdin = 0;
    for arg in command.get_arguments() {
        let name = arg.get_id().as_str();
        if arg.get_index().is_none() && !present(m, name) {
            continue;
        }
        for value in values(m, name) {
            let source = if arg.get_index().is_some() {
                matches!(
                    name,
                    "obi"
                        | "before"
                        | "after"
                        | "shared-contract"
                        | "from"
                        | "patch"
                        | "delegate-obi"
                        | "value"
                ) && id != Init
                    && !matches!(id, ContextSet | ContextShow | ContextRemove if name == "obi")
            } else {
                matches!(name, "obi" | "input" | "output" | "data" | "context")
            };
            if source && value == "-" {
                stdin += 1;
            }
        }
    }
    if stdin > 1 {
        return usage("stdin (-) can supply only one input per command");
    }
    if edits_document(id)
        && values(m, "obi").first() == Some(&"-")
        && values(m, "format").first() == Some(&"json")
        && !on(m, "dry-run")
    {
        return usage(
            "-F json needs --dry-run when <obi> is -: standard output carries the document",
        );
    }
    match id {
        Validate => {
            let input = present(m, "input");
            let output = present(m, "output");
            let examples = on(m, "examples");
            if examples && (input || output) {
                return usage("--examples does not combine with --input or --output");
            }
            if (input || output) && !present(m, "operation") {
                return usage("--input and --output require --operation");
            }
            if present(m, "operation") && !(input || output || examples) {
                return usage("--operation requires --input, --output, or --examples");
            }
            if present(m, "operation") && values(m, "obi").len() > 1 {
                return usage("--operation takes one document");
            }
        }
        Compat => {
            if present(m, "dependency") && (present(m, "operation") || present(m, "check-as")) {
                return usage("--dependency does not combine with --operation or --check-as");
            }
        }
        Merge => {
            if on(m, "ours") && on(m, "theirs") {
                return usage("choose --ours or --theirs, not both");
            }
            if present(m, "onto") && values(m, "operation").len() != 1 {
                return usage("--onto takes exactly one --operation");
            }
        }
        Fmt => {
            if on(m, "canonical") && (on(m, "check") || on(m, "dry-run")) {
                return usage("--canonical does not combine with --check or --dry-run");
            }
            if on(m, "canonical") && values(m, "obi").len() != 1 {
                return usage("--canonical prints exactly one document");
            }
        }
        SourcePull => {
            let force = values(m, "force");
            if on(m, "check") && (!force.is_empty() || on(m, "prune") || on(m, "dry-run")) {
                return usage(
                    "--check writes nothing: it does not combine with --force, --prune, or --dry-run",
                );
            }
            if force.iter().any(|v| v.is_empty()) && force.iter().any(|v| !v.is_empty()) {
                return usage(
                    "--force applies every proposal; give --force=<entry> for some, not both",
                );
            }
        }
        Invoke => {
            if let Some(value) = values(m, "context").first()
                && !input_reference(value)
            {
                return usage(
                    "--context takes @FILE or -; context values stay off the command line",
                );
            }
            if on(m, "frames") && on(m, "preflight") {
                return usage("--preflight calls nothing, so it does not combine with --frames");
            }
        }
        ContextSet => {
            if let Some(value) = values(m, "value").first()
                && !input_reference(value)
            {
                return usage("<value> takes @FILE or -; context values stay off the command line");
            }
        }
        DelegateSet => {
            let added = values(m, "add-job");
            if values(m, "remove-job")
                .iter()
                .any(|job| added.contains(job))
            {
                return usage("--add-job and --remove-job name the same job");
            }
        }
        _ => {}
    }
    Ok(())
}

/// Recognize Go-style duration syntax retained by the designed --timeout flag.
/// The string remains input to a future handler; no clock or timer is created.
pub(crate) fn duration(value: &str) -> Result<String, String> {
    let invalid = || "use a duration such as 30s, 500ms, or 1m30s".to_owned();
    let negative = value.starts_with('-');
    let mut rest = value.strip_prefix(['+', '-']).unwrap_or(value);
    if rest == "0" {
        return Ok(value.to_owned());
    }
    let limit = i64::MAX as u128 + u128::from(negative);
    let mut total = 0u128;
    let mut count = 0;
    while !rest.is_empty() {
        let end = rest
            .find(|c: char| !c.is_ascii_digit() && c != '.')
            .unwrap_or(rest.len());
        let number = &rest[..end];
        rest = &rest[end..];
        let (whole, fraction) = number.split_once('.').unwrap_or((number, ""));
        if whole.is_empty() && fraction.is_empty()
            || !whole
                .bytes()
                .chain(fraction.bytes())
                .all(|c| c.is_ascii_digit())
        {
            return Err(invalid());
        }
        let units = [
            ("ns", 1u128),
            ("us", 1_000),
            ("µs", 1_000),
            ("μs", 1_000),
            ("ms", 1_000_000),
            ("s", 1_000_000_000),
            ("m", 60_000_000_000),
            ("h", 3_600_000_000_000),
        ];
        let Some((unit, scale)) = units.iter().find(|(unit, _)| rest.starts_with(unit)) else {
            return Err(invalid());
        };
        rest = &rest[unit.len()..];
        let mut integer = 0u128;
        for digit in whole.bytes() {
            integer = integer
                .checked_mul(10)
                .and_then(|n| n.checked_add(u128::from(digit - b'0')))
                .ok_or_else(invalid)?;
        }
        // Decimal multiplication from right to left retains exact nanoseconds,
        // even for a long fractional spelling, without a floating intermediate.
        let fractional = fraction.bytes().rev().fold(0u128, |carry, digit| {
            (u128::from(digit - b'0') * scale + carry) / 10
        });
        let part = integer
            .checked_mul(*scale)
            .and_then(|n| n.checked_add(fractional))
            .ok_or_else(invalid)?;
        total = total
            .checked_add(part)
            .filter(|n| *n <= limit)
            .ok_or_else(invalid)?;
        count += 1;
    }
    if count == 0 {
        return Err(invalid());
    }
    Ok(value.to_owned())
}
