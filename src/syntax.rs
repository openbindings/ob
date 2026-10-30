//! Argument grammar only. This module never opens files, reads stdin, or interprets an OBI.
use crate::{command_id::CommandId, commands::EVERY};
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
    // A run that writes nothing prints its report: --dry-run, and source
    // pull's --check (spine, R5).
    let checks = id == SourcePull && on(m, "check");
    if edits_document(id)
        && values(m, "obi").first() == Some(&"-")
        && values(m, "format").first() == Some(&"json")
        && !on(m, "dry-run")
        && !checks
    {
        return usage(if id == SourcePull {
            "-F json needs --dry-run or --check when <obi> is -: standard output carries the document"
        } else {
            "-F json needs --dry-run when <obi> is -: standard output carries the document"
        });
    }
    match id {
        Init => {
            if values(m, "interface-version").first() == Some(&"") {
                return usage("--interface-version takes a label that is not empty, such as 1.0.0");
            }
        }
        Validate => {
            let input = present(m, "input");
            let output = present(m, "output");
            let examples = on(m, "examples");
            if on(m, "kinds") && (present(m, "operation") || examples) {
                return usage("--kinds does not combine with --operation or --examples");
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
        SourceAdd => context_reference(m)?,
        SourcePull => {
            let apply = values(m, "apply");
            let add = present(m, "add");
            if on(m, "check") && (add || !apply.is_empty() || on(m, "prune") || on(m, "dry-run")) {
                return usage(
                    "--check writes nothing: it does not combine with --add, --apply, --prune, or --dry-run",
                );
            }
            if apply.contains(&"") {
                return usage(
                    "--apply=<entry> takes an entry, such as bindings/<key>; --apply alone applies every proposal",
                );
            }
            if apply.contains(&EVERY) && apply.iter().any(|entry| *entry != EVERY) {
                return usage(
                    "--apply applies every proposal; give --apply=<entry> for some, not both",
                );
            }
            if add && values(m, "key").len() != 1 {
                return usage("--add takes exactly one <key>: the source that offers the addition");
            }
            context_reference(m)?;
        }
        Invoke => {
            context_reference(m)?;
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
        DelegateAdd => {
            let jobs = values(m, "job");
            once("--job", &jobs)?;
            let preferred = job_preferences(m)?;
            if let Some(job) = preferred.iter().find(|job| !jobs.contains(job)) {
                return Err(format!(
                    "--preference names {job}, a job --job does not request"
                ));
            }
        }
        DelegateSet => {
            let added = values(m, "add-job");
            let removed = values(m, "remove-job");
            once("--add-job", &added)?;
            once("--remove-job", &removed)?;
            if removed.iter().any(|job| added.contains(job)) {
                return usage("--add-job and --remove-job name the same job");
            }
            let preferred = job_preferences(m)?;
            let mut unset = Vec::new();
            for value in values(m, "unset") {
                match value.strip_prefix("preference.") {
                    Some(job @ ("pull" | "invoke")) => unset.push(job),
                    _ => return usage("--unset takes preference.pull or preference.invoke"),
                }
            }
            once("--unset", &unset)?;
            if preferred.iter().any(|job| unset.contains(job)) {
                return usage("--preference and --unset name the same job");
            }
            if let Some(job) = preferred.iter().find(|job| removed.contains(job)) {
                return Err(format!(
                    "--preference names {job}, a job --remove-job removes"
                ));
            }
        }
        _ => {}
    }
    Ok(())
}

/// `--context` takes `@FILE` or `-`: context values, such as a token, stay
/// off the command line, and an inline value is never echoed back.
fn context_reference(m: &ArgMatches) -> Result<(), String> {
    match values(m, "context").first() {
        Some(value) if !input_reference(value) => {
            Err("--context takes @FILE or -; context values stay off the command line".to_owned())
        }
        _ => Ok(()),
    }
}

/// A repeatable option names each job once.
fn once(option: &str, jobs: &[&str]) -> Result<(), String> {
    for (index, job) in jobs.iter().enumerate() {
        if jobs[..index].contains(job) {
            return Err(format!("{option} names {job} twice"));
        }
    }
    Ok(())
}

/// The jobs of delegate add's and set's `--preference JOB=NUMBER` values, each
/// named once. JOB is pull or invoke; NUMBER is a JSON number, so it is
/// finite, and it is kept as typed for the handler.
fn job_preferences(m: &ArgMatches) -> Result<Vec<&str>, String> {
    let mut jobs = Vec::new();
    for value in values(m, "preference") {
        let Some((job, number)) = value
            .split_once('=')
            .filter(|(job, _)| matches!(*job, "pull" | "invoke"))
        else {
            return Err(
                "--preference takes JOB=NUMBER, such as invoke=10, where JOB is pull or invoke"
                    .to_owned(),
            );
        };
        if !json_number(number) {
            return Err(format!(
                "--preference takes a finite number for {job}, written as in JSON, such as {job}=10 or {job}=-0.5"
            ));
        }
        jobs.push(job);
    }
    once("--preference", &jobs)?;
    Ok(jobs)
}

/// The JSON number grammar (RFC 8259, section 6): an optional minus sign,
/// an integer part without a leading zero, then an optional fraction and
/// exponent. It has no spelling for an infinity or NaN.
fn json_number(text: &str) -> bool {
    let digits = |part: &str| !part.is_empty() && part.bytes().all(|b| b.is_ascii_digit());
    let unsigned = text.strip_prefix('-').unwrap_or(text);
    let (mantissa, exponent) = match unsigned.split_once(['e', 'E']) {
        Some((mantissa, exponent)) => (mantissa, Some(exponent)),
        None => (unsigned, None),
    };
    let (integer, fraction) = match mantissa.split_once('.') {
        Some((integer, fraction)) => (integer, Some(fraction)),
        None => (mantissa, None),
    };
    digits(integer)
        && (integer == "0" || !integer.starts_with('0'))
        && fraction.is_none_or(digits)
        && exponent
            .is_none_or(|exponent| digits(exponent.strip_prefix(['+', '-']).unwrap_or(exponent)))
}

/// An integer option's value: an optional sign and decimal digits, and
/// optionally a fraction of zeros, since 1.0 and 1 are the same integer.
/// The error completes a sentence that begins with the option's name.
pub(crate) fn integer(value: &str) -> Result<i64, String> {
    let invalid = || "takes an integer, such as 10 or -1".to_owned();
    let whole = match value.split_once('.') {
        Some((whole, zeros)) if !zeros.is_empty() && zeros.bytes().all(|b| b == b'0') => whole,
        Some(_) => return Err(invalid()),
        None => value,
    };
    let digits = whole.strip_prefix(['+', '-']).unwrap_or(whole);
    if digits.is_empty() || !digits.bytes().all(|b| b.is_ascii_digit()) {
        return Err(invalid());
    }
    whole
        .parse()
        .map_err(|_| format!("takes an integer from {} to {}", i64::MIN, i64::MAX))
}

/// Recognize Go-style duration syntax retained by the designed --timeout flag.
/// The string remains input to a future handler; no clock or timer is created.
/// The error completes a sentence that begins with the option's name.
pub(crate) fn duration(value: &str) -> Result<String, String> {
    let invalid = || "takes a duration, such as 30s, 500ms, or 1m30s".to_owned();
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
