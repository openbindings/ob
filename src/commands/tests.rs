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
                    .is_some_and(|names| names[0] == "BOOL");
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
    // --force alone and --force=<entry> are distinct values of one option.
    let (id, m, command) = parsed(&["ob", "source", "pull", "x", "--force=a.b", "--force=Task"]);
    assert_eq!(
        m.get_many::<String>("force")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        ["a.b", "Task"]
    );
    syntax::validate(id, &m, &command).unwrap();
    let (id, m, command) = parsed(&["ob", "source", "pull", "x", "httpApi", "--force"]);
    assert_eq!(
        m.get_many::<String>("force")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        [""]
    );
    assert_eq!(
        m.get_many::<String>("key")
            .unwrap()
            .map(String::as_str)
            .collect::<Vec<_>>(),
        ["httpApi"]
    );
    syntax::validate(id, &m, &command).unwrap();
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
        vec!["ob", "help", "primer"],
        vec!["ob", "operation", "wat", "--help"],
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
        vec!["ob", "source", "pull", "x", "--check", "--force"],
        vec!["ob", "source", "pull", "x", "--check", "--dry-run"],
        vec!["ob", "source", "pull", "x", "--force", "--force=entry"],
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
