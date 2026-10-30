use super::*;
use crate::{command_id::CommandId, syntax};
use serde_json::Value;

fn parsed(args: &[&str]) -> (CommandId, clap::ArgMatches, Command) {
    let mut command = root();
    let mut matches = command
        .try_get_matches_from_mut(args)
        .expect("valid parser input");
    let mut path = String::from("ob");
    while let Some((name, nested)) = matches.remove_subcommand() {
        path.push(' ');
        path.push_str(&name);
        command = command.find_subcommand(&name).unwrap().clone();
        matches = nested;
    }
    (CommandId::from_path(&path).unwrap(), matches, command)
}

#[test]
fn command_tree_is_well_formed() {
    root().debug_assert();
}

#[test]
fn every_domain_path_and_flag_matches_the_frozen_surface() {
    fn walk(reference: &Value, root: &Command, count: &mut usize) {
        if reference["id"].as_str().is_some_and(|id| !id.is_empty()) {
            let path = reference["path"].as_str().unwrap();
            let mut cmd = root;
            for name in path.split_whitespace().skip(1) {
                cmd = cmd
                    .find_subcommand(name)
                    .unwrap_or_else(|| panic!("missing {path}"));
            }
            assert!(
                CommandId::from_path(path).is_some(),
                "dispatch missing for {path}"
            );
            assert!(summary(path).is_some(), "no -h summary for {path}");
            let flags = reference["flags"].as_array().unwrap();
            let actual: Vec<_> = cmd
                .get_arguments()
                .filter(|a| a.get_long().is_some())
                .collect();
            assert_eq!(actual.len(), flags.len(), "flags for {path}");
            for f in flags {
                let name = f["name"].as_str().unwrap();
                let arg = actual
                    .iter()
                    .find(|a| a.get_long() == Some(name))
                    .unwrap_or_else(|| panic!("missing {path} --{name}"));
                assert_eq!(
                    arg.get_short().map(|c| c.to_string()),
                    f["short"].as_str().map(str::to_owned),
                    "{path} --{name} short form"
                );
                assert_eq!(
                    matches!(arg.get_action(), ArgAction::Append),
                    f["repeatable"].as_bool().unwrap(),
                    "{path} --{name} repeatable"
                );
                assert_eq!(
                    arg.is_required_set(),
                    f["required"].as_bool().unwrap(),
                    "{path} --{name} required"
                );
                let takes_value = f["takesValue"].as_bool().unwrap();
                let is_boolean = arg
                    .get_value_names()
                    .is_some_and(|names| names[0] == "true|false");
                assert_eq!(takes_value, !is_boolean, "{path} --{name} takes a value");
            }
            let arguments = reference["arguments"].as_array().unwrap();
            let positions: Vec<_> = cmd
                .get_arguments()
                .filter(|a| a.get_long().is_none())
                .collect();
            assert_eq!(positions.len(), arguments.len(), "positions for {path}");
            for (arg, expected) in positions.iter().zip(arguments) {
                assert_eq!(
                    arg.get_id().as_str(),
                    expected["name"].as_str().unwrap(),
                    "{path}"
                );
                assert_eq!(
                    arg.is_required_set(),
                    expected["required"].as_bool().unwrap(),
                    "{path} <{}>",
                    arg.get_id()
                );
            }
            *count += 1;
        }
        if let Some(children) = reference["children"].as_array() {
            for child in children {
                walk(child, root, count);
            }
        }
    }
    let reference: Value =
        serde_json::from_str(include_str!("../../tests/fixtures/surface.json")).unwrap();
    let mut count = 0;
    walk(&reference, &root(), &mut count);
    assert_eq!(count, 44);
}

#[test]
fn explicit_false_repetition_and_opaque_values_are_preserved() {
    let (id, matches, command) = parsed(&[
        "ob",
        "binding",
        "add",
        "any.filename",
        "odd.key",
        "--operation",
        "op",
        "--source",
        "src",
        "--idempotent=false",
        "--preference",
        "-7",
        "--data",
        "{\"n\":9007199254740993}",
    ]);
    assert_eq!(id, CommandId::BindingAdd);
    assert_eq!(matches.get_one::<bool>("idempotent"), Some(&false));
    assert_eq!(
        matches.value_source("idempotent"),
        Some(clap::parser::ValueSource::CommandLine)
    );
    assert_eq!(matches.get_one::<i64>("preference"), Some(&-7));
    assert_eq!(
        matches.get_one::<String>("data").unwrap(),
        "{\"n\":9007199254740993}"
    );
    syntax::validate(id, &matches, &command).unwrap();
    let (_, m, _) = parsed(&[
        "ob",
        "invoke",
        "-",
        "alias",
        "--input",
        "{\"n\":1.2300e+2}",
        "--input",
        "null",
        "--binding",
        "b2",
        "--binding",
        "b1",
    ]);
    assert_eq!(
        m.get_many::<String>("input")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        ["{\"n\":1.2300e+2}", "null"]
    );
    assert_eq!(
        m.get_many::<String>("binding")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        ["b2", "b1"]
    );
    // --apply alone and --apply=<entry> are distinct values of one option.
    let (id, m, command) = parsed(&["ob", "source", "pull", "x", "--apply=a.b", "--apply=Task"]);
    assert_eq!(
        m.get_many::<String>("apply")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        ["a.b", "Task"]
    );
    syntax::validate(id, &m, &command).unwrap();
    let (id, m, command) = parsed(&["ob", "source", "pull", "x", "httpApi", "--apply"]);
    assert_eq!(
        m.get_many::<String>("apply")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        [EVERY]
    );
    assert_eq!(
        m.get_many::<String>("key")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        ["httpApi"]
    );
    syntax::validate(id, &m, &command).unwrap();
    // An explicitly empty entry stays distinct from --apply alone.
    let (_, m, _) = parsed(&["ob", "source", "pull", "x", "--apply="]);
    assert_eq!(m.get_one::<String>("apply").unwrap(), "");
}

#[test]
fn value_options_take_negative_numbers_and_integers_without_a_fraction() {
    let (id, m, command) = parsed(&[
        "ob",
        "validate",
        "x",
        "--operation",
        "op",
        "--input",
        "-1",
        "--output",
        "-2.5e3",
    ]);
    assert_eq!(m.get_one::<String>("input").unwrap(), "-1");
    assert_eq!(m.get_one::<String>("output").unwrap(), "-2.5e3");
    syntax::validate(id, &m, &command).unwrap();
    for (value, expected) in [("1", 1), ("1.0", 1), ("-3.00", -3), ("+7", 7), ("-0", 0)] {
        let (_, m, _) = parsed(&[
            "ob",
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "op",
            "--source",
            "s",
            "--preference",
            value,
        ]);
        assert_eq!(m.get_one::<i64>("preference"), Some(&expected), "{value}");
    }
    for value in [
        "1.5",
        "abc",
        "",
        "1.",
        ".0",
        "1e2",
        "--",
        "99999999999999999999",
    ] {
        let error = syntax::integer(value).expect_err(value);
        assert!(error.starts_with("takes an integer"), "{value}: {error}");
    }
}

#[test]
fn source_pull_check_and_dry_run_print_a_report_for_standard_input() {
    for args in [
        vec!["ob", "source", "pull", "-", "--check", "-F", "json"],
        vec!["ob", "source", "pull", "-", "--dry-run", "-F", "json"],
        vec![
            "ob",
            "binding",
            "add",
            "-",
            "b",
            "--operation",
            "o",
            "--source",
            "s",
            "--dry-run",
            "-F",
            "json",
        ],
        vec![
            "ob",
            "source",
            "pull",
            "x",
            "k",
            "--add",
            "b.k",
            "--context",
            "@ctx.json",
        ],
        vec!["ob", "source", "add", "x", "k", "--context", "-"],
        vec!["ob", "validate", "x", "y", "--kinds"],
        vec!["ob", "init", "x", "--interface-version", "1.0.0"],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--job",
            "pull",
            "--preference",
            "invoke=-0.5",
            "--preference",
            "pull=1e3",
        ],
        vec![
            "ob",
            "delegate",
            "set",
            "d",
            "--add-job",
            "pull",
            "--preference",
            "pull=0",
            "--unset",
            "preference.invoke",
        ],
    ] {
        let (id, matches, command) = parsed(&args);
        syntax::validate(id, &matches, &command)
            .unwrap_or_else(|error| panic!("{args:?}: {error}"));
    }
}

#[test]
fn missing_required_flags_invalid_types_and_repeated_singletons_refuse() {
    for args in [
        vec!["ob", "binding", "add", "x", "b", "--operation", "op"],
        vec!["ob", "delegate", "add", "x"],
        vec!["ob", "delegate", "resolve", "--job", "invoke"],
        vec!["ob", "show", "x", "-F", "yaml"],
        vec!["ob", "show", "x", "-F", "json", "-F", "text"],
        vec!["ob", "kind", "list", "--supports", "synthesize"],
        vec!["ob", "delegate", "add", "x", "--job", "inspect"],
        vec![
            "ob",
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "op",
            "--source",
            "s",
            "--preference",
            "1.5",
        ],
        vec![
            "ob",
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "op",
            "--source",
            "s",
            "--idempotent=maybe",
        ],
        vec!["ob", "invoke", "x", "op", "--timeout", "tomorrow"],
        vec!["ob", "context", "set", "x", "s"],
        vec!["ob", "operation", "wat", "--help"],
        vec!["ob", "completion"],
        vec!["ob", "completion", "tcsh"],
        vec![
            "ob",
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "op",
            "--source",
            "s",
            "--preference",
            "abc",
        ],
        vec!["ob", "source", "pull", "x", "--force"],
    ] {
        assert!(root().try_get_matches_from(&args).is_err(), "{args:?}");
    }
}

#[test]
fn cross_argument_grammar_refuses_without_interpreting_documents() {
    for args in [
        vec!["ob", "validate", "x", "--input", "{}"],
        vec!["ob", "validate", "x", "--operation", "op"],
        vec![
            "ob",
            "validate",
            "x",
            "--operation",
            "op",
            "--examples",
            "--input",
            "0",
        ],
        vec![
            "ob",
            "validate",
            "x",
            "y",
            "--operation",
            "op",
            "--examples",
        ],
        vec!["ob", "validate", "-", "--operation", "op", "--input", "-"],
        vec![
            "ob",
            "compat",
            "x",
            "c",
            "--dependency",
            "d",
            "--operation",
            "o",
        ],
        vec!["ob", "merge", "x", "y", "--ours", "--theirs"],
        vec!["ob", "merge", "x", "y", "--onto", "o"],
        vec!["ob", "merge", "-", "y", "-F", "json"],
        vec!["ob", "fmt", "x", "y", "--canonical"],
        vec!["ob", "fmt", "x", "--canonical", "--check"],
        vec!["ob", "source", "pull", "x", "--check", "--apply"],
        vec!["ob", "source", "pull", "x", "--check", "--dry-run"],
        vec!["ob", "source", "pull", "x", "k", "--check", "--add", "b.k"],
        vec!["ob", "source", "pull", "x", "--check", "--prune"],
        vec!["ob", "source", "pull", "x", "--apply", "--apply=entry"],
        vec!["ob", "source", "pull", "x", "--apply="],
        vec!["ob", "source", "pull", "x", "--add", "b.k"],
        vec!["ob", "source", "pull", "x", "k", "j", "--add", "b.k"],
        vec!["ob", "source", "pull", "-", "-F", "json"],
        vec![
            "ob",
            "source",
            "pull",
            "-",
            "--check",
            "--dry-run",
            "-F",
            "json",
        ],
        vec![
            "ob",
            "source",
            "pull",
            "x",
            "--context",
            "{\"token\":\"SECRET\"}",
        ],
        vec![
            "ob",
            "source",
            "add",
            "x",
            "k",
            "--context",
            "{\"token\":\"SECRET\"}",
        ],
        vec!["ob", "validate", "x", "--kinds", "--examples"],
        vec![
            "ob",
            "validate",
            "x",
            "--kinds",
            "--operation",
            "op",
            "--input",
            "1",
        ],
        vec!["ob", "init", "x", "--interface-version", ""],
        vec![
            "ob", "delegate", "add", "d", "--job", "invoke", "--job", "invoke",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "pull=1",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=abc",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=inf",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=NaN",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "inspect=1",
        ],
        vec![
            "ob",
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=1",
            "--preference",
            "invoke=2",
        ],
        vec![
            "ob",
            "delegate",
            "set",
            "d",
            "--add-job",
            "pull",
            "--add-job",
            "pull",
        ],
        vec![
            "ob",
            "delegate",
            "set",
            "d",
            "--remove-job",
            "pull",
            "--remove-job",
            "pull",
        ],
        vec![
            "ob",
            "delegate",
            "set",
            "d",
            "--unset",
            "preference.inspect",
        ],
        vec!["ob", "delegate", "set", "d", "--unset", "invoke"],
        vec![
            "ob",
            "delegate",
            "set",
            "d",
            "--preference",
            "invoke=1",
            "--unset",
            "preference.invoke",
        ],
        vec![
            "ob",
            "delegate",
            "set",
            "d",
            "--preference",
            "invoke=1",
            "--remove-job",
            "invoke",
        ],
        vec!["ob", "source", "rename", "-", "a", "b", "-F", "json"],
        vec!["ob", "invoke", "x", "op", "--frames", "--preflight"],
        vec![
            "ob",
            "invoke",
            "x",
            "op",
            "--context",
            "{\"token\":\"SECRET\"}",
        ],
        vec!["ob", "context", "set", "x", "s", "{\"token\":\"SECRET\"}"],
        vec![
            "ob",
            "delegate",
            "set",
            "d",
            "--add-job",
            "pull",
            "--remove-job",
            "pull",
        ],
    ] {
        let (id, matches, command) = parsed(&args);
        let error =
            syntax::validate(id, &matches, &command).expect_err("invalid argument relationship");
        assert!(!error.contains("SECRET"), "{args:?}: {error}");
    }
}

#[test]
fn duration_syntax_is_exact_and_bounded() {
    for value in [
        "0",
        "+0",
        "-0",
        "30s",
        ".5s",
        "1m30.5s",
        "100µs",
        "-1ns",
        "2562047h47m16.854775807s",
        "-2562047h47m16.854775808s",
    ] {
        assert!(syntax::duration(value).is_ok(), "{value}");
    }
    for value in [
        "",
        "1",
        "1d",
        "NaNs",
        "1..2s",
        "2e3s",
        "2562047h47m16.854775808s",
        "-2562047h47m16.854775809s",
    ] {
        assert!(syntax::duration(value).is_err(), "{value}");
    }
}
