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

pub(crate) fn validate(id: CommandId, m: &ArgMatches, command: &Command) -> Result<(), String> {
    use CommandId::*;
    let usage = |message: &str| Err(message.to_owned());
    if matches!(id, Init | Set) && present(m, "version") {
        return usage("use --interface-version for the document's version label");
    }
    // Count input sources, not arbitrary '-' strings (a description, operation
    // name, or output path does not consume stdin).
    let mut stdin = 0;
    for arg in command.get_arguments() {
        let name = arg.get_id().as_str();
        if arg.get_index().is_none() && !present(m, name) {
            continue;
        }
        for value in values(m, name) {
            let keyed =
                id == ContextSet && matches!(name, "credential" | "cookie" | "header" | "config");
            let source = if arg.get_index().is_some() {
                matches!(
                    name,
                    "obi"
                        | "before"
                        | "after"
                        | "contract"
                        | "from"
                        | "patch"
                        | "artifact"
                        | "delegate-obi"
                ) && id != Init
            } else {
                matches!(
                    name,
                    "obi"
                        | "input"
                        | "output"
                        | "input-schema"
                        | "output-schema"
                        | "content"
                        | "value"
                        | "context"
                        | "bearer-token"
                        | "access-token"
                        | "refresh-token"
                        | "api-key"
                        | "basic"
                        | "token-credential"
                )
            };
            if source && value == "-"
                || keyed && value.split_once('=').is_some_and(|(_, v)| v == "-")
            {
                stdin += 1;
            }
        }
    }
    if stdin > 1 {
        return usage("stdin (-) can supply only one input per command");
    }
    match id {
        Validate => {
            let input = present(m, "input");
            let output = present(m, "output");
            let examples = on(m, "examples");
            if input && output {
                return usage("check one side at a time: --input or --output");
            }
            if examples && (input || output) {
                return usage("--examples does not combine with --input or --output");
            }
            if (input || output) && !present(m, "operation") {
                return usage("--input and --output require --operation");
            }
            if present(m, "operation") && !(input || output || examples) {
                return usage("--operation requires --input, --output, or --examples");
            }
        }
        SourcePull => {
            if present(m, "target") {
                if values(m, "source").len() != 1 {
                    return usage("--target requires exactly one source argument");
                }
                if on(m, "all-targets") || present(m, "update-operation") {
                    return usage(
                        "--target does not combine with --all-targets or --update-operation",
                    );
                }
                if present(m, "operation") && present(m, "new-operation") {
                    return usage("choose --operation or --new-operation, not both");
                }
            } else if ["operation", "new-operation", "binding-key"]
                .iter()
                .any(|f| present(m, f))
            {
                return usage("--operation, --new-operation, and --binding-key require --target");
            }
        }
        Merge if on(m, "ours") && on(m, "theirs") => {
            return usage("choose --ours or --theirs, not both");
        }
        Fmt => {
            if on(m, "check") && (on(m, "canonical") || on(m, "dry-run")) {
                return usage("--check does not combine with --canonical or --dry-run");
            }
            if on(m, "canonical") && values(m, "obi").len() != 1 {
                return usage("--canonical prints exactly one document");
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
        }
        ContextSet => {
            let fields = [
                "bearer-token",
                "access-token",
                "refresh-token",
                "api-key",
                "basic",
                "credential",
                "cookie",
                "header",
                "config",
                "unset",
                "token-provider",
                "token-credential",
                "token-binding",
            ];
            if present(m, "value") && fields.iter().any(|f| present(m, f)) {
                return usage(
                    "--value replaces the whole context and cannot combine with field edits",
                );
            }
            for name in [
                "value",
                "bearer-token",
                "access-token",
                "refresh-token",
                "api-key",
                "basic",
                "token-credential",
            ] {
                if let Some(value) = values(m, name).first()
                    && !input_reference(value)
                {
                    return Err(format!(
                        "--{name} takes @FILE or -; credential values stay off the command line"
                    ));
                }
            }
            for value in values(m, "credential") {
                if !value
                    .split_once('=')
                    .is_some_and(|(name, source)| !name.is_empty() && input_reference(source))
                {
                    return usage("--credential takes NAME=@FILE or NAME=-");
                }
            }
            for value in values(m, "header") {
                let Some((name, value)) = value.split_once('=') else {
                    return usage("--header takes NAME=VALUE");
                };
                if ["authorization", "proxy-authorization", "cookie"]
                    .iter()
                    .any(|s| name.eq_ignore_ascii_case(s))
                    && !input_reference(value)
                {
                    return usage(
                        "credential headers take NAME=@FILE or NAME=-; their values stay off the command line",
                    );
                }
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
