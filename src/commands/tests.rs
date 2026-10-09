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
fn every_domain_path_and_flag_matches_the_frozen_go_surface() {
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
            let flags = reference["flags"]
                .as_array()
                .map(Vec::as_slice)
                .unwrap_or(&[]);
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
                    f["short"].as_str().map(str::to_owned)
                );
                assert_eq!(arg.is_hide_set(), f["hidden"].as_bool().unwrap_or(false));
                if f["kind"] == "stringArray" {
                    assert!(matches!(arg.get_action(), ArgAction::Append));
                }
            }
            let positions: Vec<_> = cmd
                .get_arguments()
                .filter(|a| a.get_long().is_none())
                .collect();
            assert_eq!(
                positions.iter().filter(|a| a.is_required_set()).count(),
                reference["min"].as_u64().unwrap() as usize,
                "required positions for {path}"
            );
            if reference["max"].as_i64().unwrap() >= 0 {
                assert_eq!(positions.len(), reference["max"].as_u64().unwrap() as usize);
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
        serde_json::from_str(include_str!("../../tests/fixtures/go-surface.json")).unwrap();
    let mut count = 0;
    walk(&reference, &root(), &mut count);
    assert_eq!(count, 73);
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
        "--content",
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
        matches.get_one::<String>("content").unwrap(),
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
}

#[test]
fn missing_required_flags_invalid_types_and_repeated_singletons_refuse() {
    for args in [
        vec!["ob", "source", "add", "x", "source"],
        vec!["ob", "binding", "add", "x", "b", "--operation", "op"],
        vec!["ob", "codegen", "x", "--lang", "go"],
        vec!["ob", "show", "x", "-F", "yaml"],
        vec!["ob", "show", "x", "-F", "json", "-F", "text"],
        vec!["ob", "binding", "set", "x", "b", "--preference", "1.5"],
        vec!["ob", "binding", "set", "x", "b", "--idempotent=maybe"],
        vec!["ob", "invoke", "x", "op", "--timeout", "tomorrow"],
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
            "--input",
            "0",
            "--output",
            "0",
        ],
        vec!["ob", "validate", "-", "--operation", "op", "--input", "-"],
        vec!["ob", "source", "pull", "x", "--target", "target"],
        vec!["ob", "source", "pull", "x", "s", "--operation", "op"],
        vec![
            "ob",
            "source",
            "pull",
            "x",
            "s",
            "--target",
            "t",
            "--operation",
            "op",
            "--new-operation",
            "new",
        ],
        vec!["ob", "merge", "x", "y", "--ours", "--theirs"],
        vec!["ob", "fmt", "x", "y", "--canonical"],
        vec![
            "ob", "context", "set", "scope", "--value", "@context", "--unset", "header.X",
        ],
        vec!["ob", "context", "set", "scope", "--bearer-token", "SECRET"],
        vec![
            "ob",
            "context",
            "set",
            "scope",
            "--header",
            "Authorization=SECRET",
        ],
        vec![
            "ob",
            "context",
            "set",
            "scope",
            "--credential",
            "primary=SECRET",
        ],
    ] {
        let (id, matches, command) = parsed(&args);
        let error =
            syntax::validate(id, &matches, &command).expect_err("invalid argument relationship");
        assert!(!error.contains("SECRET"));
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
