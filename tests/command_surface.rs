use std::{
    collections::BTreeSet,
    fs,
    net::TcpListener,
    path::PathBuf,
    process::{Command, Output, Stdio},
    sync::atomic::{AtomicU64, Ordering},
    thread,
    time::{Duration, Instant},
};

use serde_json::Value;

fn ob() -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_ob"));
    command.env("NO_COLOR", "1");
    command
}

fn placeholder(output: &Output) -> String {
    assert_eq!(output.status.code(), Some(3), "{output:?}");
    assert!(
        output.stdout.is_empty(),
        "no success-shaped output: {output:?}"
    );
    let error = String::from_utf8(output.stderr.clone()).unwrap();
    assert!(error.contains("is not implemented"), "{error}");
    assert!(error.contains("no operation ran"), "{error}");
    error
}

#[test]
fn help_and_version_work_without_a_document() {
    // Command help carries a usage line; a help topic is prose and does not.
    for (args, usage) in [
        (vec![], true),
        (vec!["--help"], true),
        (vec!["help"], true),
        (vec!["operation"], true),
        (vec!["help", "source", "pull"], true),
        (vec!["source", "pull", "--help"], true),
        (vec!["invoke", "-h"], true),
        (vec!["source", "add", "-h"], true),
        (vec!["help", "help"], true),
        (vec!["help", "completion"], true),
        (vec!["completion", "--help"], true),
        (vec!["help", "-h"], true),
        (vec!["help", "obi"], false),
        (vec!["help", "documents"], false),
        (vec!["help", "exit-codes"], false),
        (vec!["help", "output"], false),
    ] {
        let output = ob().args(&args).output().unwrap();
        assert!(output.status.success(), "{args:?}: {output:?}");
        assert!(output.stderr.is_empty(), "{args:?}: {output:?}");
        let help = String::from_utf8(output.stdout).unwrap();
        assert!(help.contains("COMMAND SURFACE ONLY"), "{args:?}: {help}");
        assert_eq!(help.contains("Usage:"), usage, "{args:?}: {help}");
    }
    // -h is the summary cut; --help is the whole page.
    let short =
        String::from_utf8(ob().args(["source", "pull", "-h"]).output().unwrap().stdout).unwrap();
    let long = String::from_utf8(
        ob().args(["source", "pull", "--help"])
            .output()
            .unwrap()
            .stdout,
    )
    .unwrap();
    assert!(short.len() < long.len());
    assert!(short.contains("Exit status:") && long.contains("Exit status:"));
    assert!(long.contains("Changes are proposals") && !short.contains("Changes are proposals"));
    let output = ob().arg("--version").output().unwrap();
    assert!(output.status.success());
    assert!(
        String::from_utf8(output.stdout)
            .unwrap()
            .contains("command surface only")
    );
    let output = ob().args(["help", "primer"]).output().unwrap();
    assert_eq!(output.status.code(), Some(2));
    assert!(output.stdout.is_empty());
}

fn stdout(args: &[&str]) -> String {
    let output = ob().args(args).output().unwrap();
    assert!(output.status.success(), "{args:?}: {output:?}");
    assert!(output.stderr.is_empty(), "{args:?}: {output:?}");
    String::from_utf8(output.stdout).unwrap()
}

#[test]
fn help_prints_the_page_a_topic_or_with_dash_h_the_summary() {
    // `ob help <command>` is the page `ob <command> --help` prints.
    for words in [
        vec![],
        vec!["operation"],
        vec!["source", "pull"],
        vec!["delegate", "add"],
        vec!["completion"],
    ] {
        let mut long = words.clone();
        long.push("--help");
        let mut help = vec!["help"];
        help.extend(&words);
        assert_eq!(stdout(&help), stdout(&long), "{words:?}");
        if words.is_empty() {
            continue; // `ob help -h` is the help command's own summary.
        }
        // `ob help <command> -h` is the summary `ob <command> -h` prints.
        let mut short = words.clone();
        short.push("-h");
        help.push("-h");
        assert_eq!(stdout(&help), stdout(&short), "{words:?}");
    }
    let root = stdout(&["--help"]);
    let compat = stdout(&["help", "compat", "-h"]);
    assert_ne!(compat, root);
    assert!(compat.contains("Usage: ob compat"), "{compat}");
    let summary = stdout(&["help", "source", "pull", "-h"]);
    assert!(summary.contains("Usage: ob source pull"), "{summary}");
    assert!(!summary.contains("Changes are proposals"), "{summary}");
    // The help command's own page and summary.
    let page = stdout(&["help", "help"]);
    assert!(page.contains("Usage: ob help [<command>...]"), "{page}");
    assert!(
        page.contains("'ob help <command>\n-h' prints that summary too"),
        "{page}"
    );
    assert_eq!(stdout(&["help", "--help"]), page);
    let help_summary = stdout(&["help", "-h"]);
    assert!(help_summary.len() < page.len());
    assert!(help_summary.contains("Usage: ob help [<command>...]"));
    assert_eq!(stdout(&["help", "help", "-h"]), help_summary);
    // Topics carry the notice like every other help output.
    let obi = stdout(&["help", "obi"]);
    assert!(obi.starts_with("COMMAND SURFACE ONLY"), "{obi}");
    assert!(obi.contains("OBI: what a document contains"), "{obi}");
}

#[test]
fn help_names_no_command_or_topic_with_a_close_match() {
    for (words, hint) in [
        (
            vec!["primer"],
            "hint: ob help lists the commands and topics",
        ),
        (vec!["ouput"], "hint: did you mean ob help output?"),
        (
            vec!["sorce", "pull"],
            "hint: did you mean ob help source pull?",
        ),
        (
            vec!["source", "pul"],
            "hint: did you mean ob help source pull?",
        ),
        (vec!["op"], "hint: did you mean ob help operation?"),
    ] {
        let mut args = vec!["help"];
        args.extend(&words);
        let output = ob().args(&args).output().unwrap();
        assert_eq!(output.status.code(), Some(2), "{args:?}: {output:?}");
        assert!(output.stdout.is_empty(), "{args:?}: {output:?}");
        let error = String::from_utf8(output.stderr).unwrap();
        assert_eq!(
            error,
            format!(
                "error: no command or topic named \"{}\"\n{hint}\n",
                words.join(" ")
            )
        );
    }
}

#[test]
fn topics_are_not_commands_and_are_never_suggested_as_one() {
    for topic in ["obi", "documents", "exit-codes", "output"] {
        let output = ob().arg(topic).output().unwrap();
        assert_eq!(output.status.code(), Some(2), "{topic}: {output:?}");
    }
    let output = ob().arg("op").output().unwrap();
    assert_eq!(output.status.code(), Some(2));
    let error = String::from_utf8(output.stderr).unwrap();
    assert!(error.contains("did you mean ob operation"), "{error}");
    assert!(!error.contains("output"), "{error}");
}

#[test]
fn removed_commands_get_a_fixed_hint() {
    for (args, hint) in [
        (
            vec!["synthesize", "openapi.json"],
            "ob source add adds a source",
        ),
        (vec!["source", "status", "x"], "ob source pull --check"),
        (
            vec!["kind", "check", "example.openapi@1"],
            "ob kind list --supports",
        ),
        (
            vec!["operation", "add", "x", "op"],
            "edit the document in your editor",
        ),
        (vec!["version"], "ob --version prints the version"),
    ] {
        let output = ob().args(&args).output().unwrap();
        assert_eq!(output.status.code(), Some(2), "{args:?}: {output:?}");
        assert!(output.stdout.is_empty());
        let error = String::from_utf8(output.stderr).unwrap();
        assert!(error.starts_with("error: ob has no "), "{error}");
        assert!(error.contains(hint), "{error}");
    }
}

#[test]
fn every_help_example_reaches_the_placeholder_and_covers_all_domain_commands() {
    let examples: Value =
        serde_json::from_str(include_str!("fixtures/command-examples.json")).unwrap();
    let mut reached = BTreeSet::new();
    for example in examples.as_array().unwrap() {
        let args: Vec<_> = example["args"]
            .as_array()
            .unwrap()
            .iter()
            .map(|s| s.as_str().unwrap())
            .collect();
        let output = ob().args(&args).output().unwrap();
        assert_eq!(output.status.code(), Some(3), "{args:?}: {output:?}");
        let error = placeholder(&output);
        reached.insert(error.split('\'').nth(1).unwrap().to_owned());
    }
    // The frozen surface prevents a missing Rust command from quietly
    // disappearing from both the test inputs and the expected coverage.
    fn collect_paths(command: &Value, paths: &mut BTreeSet<String>) {
        if command["id"].as_str().is_some_and(|s| !s.is_empty()) {
            paths.insert(command["path"].as_str().unwrap().to_owned());
        }
        if let Some(children) = command["children"].as_array() {
            for child in children {
                collect_paths(child, paths);
            }
        }
    }
    let reference: Value = serde_json::from_str(include_str!("fixtures/surface.json")).unwrap();
    let mut expected = BTreeSet::new();
    collect_paths(&reference, &mut expected);
    assert_eq!(reached, expected);
    assert_eq!(reached.len(), 44);
}

#[test]
fn completion_scripts_are_real_and_do_not_offer_fixture_names() {
    for (shell, marker) in [
        ("bash", "complete"),
        ("zsh", "#compdef ob"),
        ("fish", "complete -c ob"),
        ("powershell", "Register-ArgumentCompleter"),
    ] {
        let output = ob().args(["completion", shell]).output().unwrap();
        assert!(output.status.success(), "{shell}: {output:?}");
        assert!(output.stderr.is_empty());
        let script = String::from_utf8(output.stdout).unwrap();
        for token in [marker, "source", "invoke", "interface-version"] {
            assert!(script.contains(token), "{shell}: missing {token}");
        }
        // clap_complete offers a positional's choices in bash and zsh only.
        if matches!(shell, "bash" | "zsh") {
            assert!(script.contains("exit-codes"), "{shell}: no help topics");
        }
        assert!(
            !script.contains("createTask.http"),
            "sample names must not be real completions"
        );
    }
}

#[test]
fn completion_needs_one_of_four_shells() {
    for args in [vec!["completion"], vec!["completion", "tcsh"]] {
        let output = ob().args(&args).output().unwrap();
        assert_eq!(output.status.code(), Some(2), "{args:?}: {output:?}");
        assert!(output.stdout.is_empty());
    }
}

#[test]
fn every_usage_error_has_one_shape() {
    for (args, hint) in [
        // Clap's own parse errors.
        (vec!["show"], "hint: ob show --help shows the syntax"),
        (
            vec!["show", "x", "-F", "yaml"],
            "hint: ob show --help shows the syntax",
        ),
        (
            vec!["show", "x", "-F", "jsn"],
            "hint: did you mean json? ob show --help",
        ),
        (
            vec!["show", "x", "-F", "json", "-F", "text"],
            "hint: ob show --help shows the syntax",
        ),
        (
            vec!["source", "pull", "x", "--aply"],
            "hint: did you mean --apply? ob source pull --help shows the syntax",
        ),
        (
            vec!["source", "pul"],
            "hint: did you mean ob source pull? ob source --help lists the commands",
        ),
        (vec!["op"], "hint: did you mean ob operation"),
        (
            vec!["binding", "add", "x", "b"],
            "hint: ob binding add --help",
        ),
        (
            vec!["source", "pull", "x", "--prune=maybe"],
            "hint: ob source pull --help",
        ),
        (
            vec!["invoke", "x", "op", "--timeout", "tomorrow"],
            "hint: ob invoke --help",
        ),
        (
            vec!["completion"],
            "hint: ob completion --help shows the syntax",
        ),
        (
            vec!["help", "--bogus"],
            "hint: ob help --help shows the syntax",
        ),
        // ob's own grammar.
        (
            vec!["validate", "x", "--input", "{}"],
            "hint: ob validate --help shows the syntax",
        ),
        (
            vec!["source", "pull", "x", "--apply="],
            "hint: ob source pull --help shows the syntax",
        ),
        (
            vec!["delegate", "add", "d", "--job", "invoke", "--job", "invoke"],
            "hint: ob delegate add --help shows the syntax",
        ),
        // A removed command.
        (vec!["version"], "hint: ob --version prints the version"),
    ] {
        let output = ob().args(&args).output().unwrap();
        assert_eq!(output.status.code(), Some(2), "{args:?}: {output:?}");
        assert!(output.stdout.is_empty(), "{args:?}: {output:?}");
        let error = String::from_utf8(output.stderr).unwrap();
        let lines: Vec<_> = error.lines().collect();
        assert!(lines[0].starts_with("error: "), "{args:?}: {error}");
        assert!(
            lines[lines.len() - 1].starts_with(hint),
            "{args:?}: {error}"
        );
        for line in &lines[1..lines.len() - 1] {
            assert!(line.starts_with("  "), "{args:?}: {error}");
        }
        for clap_extra in ["Usage:", "For more information", "tip:", "ob:"] {
            assert!(!error.contains(clap_extra), "{args:?}: {error}");
        }
    }
    // ob's value parsers say what the option takes.
    let output = ob()
        .args([
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
        ])
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(2));
    assert_eq!(
        String::from_utf8(output.stderr).unwrap(),
        "error: --preference takes an integer, such as 10 or -1\nhint: ob binding add --help shows the syntax\n"
    );
}

#[cfg(unix)]
#[test]
fn an_argument_that_is_not_utf8_is_a_usage_error() {
    use std::{ffi::OsStr, os::unix::ffi::OsStrExt};
    let output = ob()
        .arg("show")
        .arg(OsStr::from_bytes(b"\xff"))
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(2), "{output:?}");
    let error = String::from_utf8(output.stderr).unwrap();
    assert!(error.starts_with("error: "), "{error}");
    assert!(
        error.ends_with("hint: ob show --help shows the syntax\n"),
        "{error}"
    );
}

#[test]
fn value_options_take_negative_numbers() {
    for args in [
        vec!["validate", "x", "--operation", "op", "--input", "-1"],
        vec!["validate", "x", "--operation", "op", "--output", "-1"],
        vec!["validate", "x", "--operation", "op", "--input", "-0.5"],
        vec![
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "op",
            "--source",
            "s",
            "--data",
            "-1",
        ],
        vec![
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "op",
            "--source",
            "s",
            "--preference",
            "-1.0",
        ],
        vec!["invoke", "x", "op", "--input", "-1"],
    ] {
        let error = placeholder(&ob().args(&args).output().unwrap());
        assert!(!error.contains("tip:"), "{error}");
    }
}

#[test]
fn argument_only_rules_refuse_with_exit_2() {
    for args in [
        vec!["delegate", "add", "d", "--job", "invoke", "--job", "invoke"],
        vec![
            "delegate",
            "set",
            "d",
            "--add-job",
            "pull",
            "--add-job",
            "pull",
        ],
        vec![
            "delegate",
            "set",
            "d",
            "--remove-job",
            "pull",
            "--remove-job",
            "pull",
        ],
        vec!["init", "x", "--interface-version", ""],
        vec![
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=abc",
        ],
        vec![
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=inf",
        ],
        vec![
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=NaN",
        ],
        vec![
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=",
        ],
        vec![
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "pull=1",
        ],
        vec!["delegate", "set", "d", "--preference", "inspect=1"],
        vec!["delegate", "set", "d", "--unset", "preference.inspect"],
        vec![
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "o",
            "--source",
            "s",
            "--preference",
            "1.5",
        ],
        vec![
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "o",
            "--source",
            "s",
            "--preference",
            "abc",
        ],
        vec!["source", "pull", "x", "--apply="],
        vec!["source", "pull", "x", "--apply", "--apply=schemas/Task"],
        vec!["source", "pull", "x", "--add", "b.k"],
        vec!["source", "pull", "x", "k", "j", "--add", "b.k"],
        vec!["source", "pull", "x", "k", "--check", "--add", "b.k"],
        vec!["source", "pull", "x", "--check", "--apply"],
        vec!["source", "pull", "-", "-F", "json"],
        vec!["validate", "x", "--kinds", "--examples"],
        vec![
            "validate",
            "x",
            "--kinds",
            "--operation",
            "op",
            "--input",
            "1",
        ],
    ] {
        let output = ob().args(&args).output().unwrap();
        assert_eq!(output.status.code(), Some(2), "{args:?}: {output:?}");
        assert!(output.stdout.is_empty(), "{args:?}: {output:?}");
    }
    for args in [
        vec![
            "delegate",
            "add",
            "d",
            "--job",
            "invoke",
            "--preference",
            "invoke=-0.5",
        ],
        vec![
            "delegate",
            "set",
            "d",
            "--preference",
            "invoke=20",
            "--unset",
            "preference.pull",
        ],
        vec![
            "binding",
            "add",
            "x",
            "b",
            "--operation",
            "o",
            "--source",
            "s",
            "--preference",
            "1.0",
        ],
        vec!["source", "pull", "x", "k", "--add", "b.k"],
        vec!["source", "pull", "x", "--apply", "--apply"],
        vec!["source", "pull", "-", "--check", "-F", "json"],
        vec!["source", "pull", "-", "--dry-run", "-F", "json"],
        vec!["validate", "x", "--kinds"],
    ] {
        placeholder(&ob().args(&args).output().unwrap());
    }
}

#[test]
fn argument_errors_are_distinct_and_do_not_echo_supplied_secrets() {
    for args in [
        vec!["show"],
        vec!["show", "x", "-F", "yaml"],
        vec!["binding", "add", "x", "b"],
        vec!["validate", "-", "--operation", "op", "--input", "-"],
        vec![
            "source",
            "add",
            "x",
            "k",
            "--context",
            "{\"token\":\"do-not-echo-this\"}",
        ],
        vec![
            "source",
            "pull",
            "x",
            "--context",
            "{\"token\":\"do-not-echo-this\"}",
        ],
        vec![
            "context",
            "set",
            "x",
            "s",
            "{\"token\":\"do-not-echo-this\"}",
        ],
        vec![
            "invoke",
            "x",
            "op",
            "--context",
            "{\"token\":\"do-not-echo-this\"}",
        ],
    ] {
        let output = ob().args(&args).output().unwrap();
        assert_eq!(output.status.code(), Some(2), "{args:?}: {output:?}");
        assert!(output.stdout.is_empty());
        let error = String::from_utf8(output.stderr).unwrap();
        assert!(!error.contains("do-not-echo-this"), "{error}");
    }
}

#[test]
fn a_literal_dash_outside_an_input_source_does_not_consume_stdin() {
    for args in [
        vec!["init", "-", "--description", "-"],
        vec![
            "source",
            "add",
            "-",
            "k",
            "--kind",
            "example@1",
            "--description",
            "-",
        ],
        vec![
            "binding",
            "add",
            "-",
            "b",
            "--operation",
            "op",
            "--source",
            "s",
            "--description",
            "-",
        ],
    ] {
        placeholder(&ob().args(args).output().unwrap());
    }
}

#[test]
fn placeholders_do_not_wait_for_stdin_or_launch_a_service() {
    for args in [
        vec!["show", "-"],
        vec!["invoke", "absent.obi", "operation", "--input", "-"],
        vec!["context", "set", "absent.obi", "s", "-"],
        vec![
            "source",
            "add",
            "absent.obi",
            "k",
            "--kind",
            "example@1",
            "--data",
            "-",
        ],
        vec!["source", "pull", "absent.obi"],
    ] {
        let mut child = ob()
            .args(&args)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        // Keep the writer open: a placeholder that tries to read stdin will
        // block rather than receive the convenient EOF provided by output().
        let input = child.stdin.take().unwrap();
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            if child.try_wait().unwrap().is_some() {
                break;
            }
            if Instant::now() >= deadline {
                let _ = child.kill();
                let _ = child.wait();
                panic!("{args:?} read stdin or started a service");
            }
            thread::sleep(Duration::from_millis(10));
        }
        drop(input);
        placeholder(&child.wait_with_output().unwrap());
    }
}

struct Scratch(PathBuf);
impl Scratch {
    fn new() -> Self {
        static COUNTER: AtomicU64 = AtomicU64::new(0);
        let path = std::env::temp_dir().join(format!(
            "ob-surface-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&path).unwrap();
        Self(path)
    }
}
impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

#[test]
fn placeholders_leave_files_unchanged_and_do_not_contact_a_supplied_url() {
    let scratch = Scratch::new();
    let original = b"not an OBI; must not be read, parsed, or replaced";
    fs::write(scratch.0.join("existing"), original).unwrap();
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    listener.set_nonblocking(true).unwrap();
    let url = format!("http://{}/interface", listener.local_addr().unwrap());
    for args in [
        vec!["init", "existing", "--force"],
        vec!["init", "new-document"],
        vec!["show", "existing"],
        vec![
            "source",
            "add",
            "existing",
            "api",
            "--kind",
            "example@1",
            "--data",
            "@absent-data",
        ],
        vec!["source", "pull", "existing", "--apply"],
        vec!["context", "set", "existing", "api", "@absent-context"],
        vec!["fetch", &url, "--out", "new-fetch"],
        vec!["schema", "bundle", "existing", &url],
    ] {
        let output = ob()
            .args(&args)
            .current_dir(&scratch.0)
            .env("HOME", &scratch.0)
            .env("XDG_CONFIG_HOME", &scratch.0)
            .env("OB_CONFIG_DIR", &scratch.0)
            .output()
            .unwrap();
        placeholder(&output);
        assert_eq!(fs::read(scratch.0.join("existing")).unwrap(), original);
        assert_eq!(
            fs::read_dir(&scratch.0).unwrap().count(),
            1,
            "{args:?} created files"
        );
        assert_eq!(
            listener.accept().unwrap_err().kind(),
            std::io::ErrorKind::WouldBlock
        );
    }
}
