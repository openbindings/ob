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
    for args in [
        vec![],
        vec!["--help"],
        vec!["help"],
        vec!["operation"],
        vec!["help", "source", "pull"],
        vec!["source", "pull", "--help"],
        vec!["invoke", "-h"],
    ] {
        let output = ob().args(&args).output().unwrap();
        assert!(output.status.success(), "{args:?}: {output:?}");
        assert!(output.stderr.is_empty(), "{args:?}: {output:?}");
        let help = String::from_utf8(output.stdout).unwrap();
        assert!(help.contains("COMMAND SURFACE ONLY"), "{args:?}: {help}");
        assert!(help.contains("Usage:"), "{args:?}: {help}");
    }
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

#[test]
fn every_frozen_help_example_reaches_the_placeholder_and_covers_all_domain_commands() {
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
    // The separate Go-derived inventory prevents a missing Rust command from
    // quietly disappearing from both the test inputs and the expected coverage.
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
    let reference: Value = serde_json::from_str(include_str!("fixtures/go-surface.json")).unwrap();
    let mut expected = BTreeSet::new();
    collect_paths(&reference, &mut expected);
    assert_eq!(reached, expected);
    assert_eq!(reached.len(), 73);
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
        assert!(
            !script.contains("createTask.http"),
            "sample names must not be real completions"
        );
    }
}

#[test]
fn argument_errors_are_distinct_and_do_not_echo_supplied_credentials() {
    for args in [
        vec!["show"],
        vec!["show", "x", "-F", "yaml"],
        vec!["source", "add", "x", "api"],
        vec!["init", "--version", "1.0"],
        vec!["validate", "-", "--operation", "op", "--input", "-"],
        vec![
            "context",
            "set",
            "scope",
            "--bearer-token",
            "do-not-echo-this",
        ],
        vec![
            "context",
            "set",
            "scope",
            "--header",
            "Authorization=do-not-echo-this",
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
        vec!["set", "-", "--description", "-"],
        vec!["synthesize", "-", "--kind", "example@1", "--out", "-"],
        vec!["operation", "add", "-", "new", "--tag", "-"],
    ] {
        placeholder(&ob().args(args).output().unwrap());
    }
}

#[test]
fn placeholders_do_not_wait_for_stdin_or_launch_a_service() {
    for args in [
        vec!["show", "-"],
        vec!["invoke", "absent.obi", "operation", "--input", "-"],
        vec!["context", "set", "scope", "--bearer-token", "-"],
        vec!["start", "--tls"],
        vec!["mcp", "absent.obi"],
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
        vec!["set", "existing", "--name", "Changed"],
        vec!["show", "existing"],
        vec![
            "schema",
            "add",
            "existing",
            "Thing",
            "--value",
            "@absent-schema",
        ],
        vec![
            "context",
            "set",
            "scope",
            "--bearer-token",
            "@absent-credentials",
        ],
        vec!["fetch", &url, "--out", "new-fetch"],
        vec![
            "codegen",
            "existing",
            "--lang",
            "typescript",
            "--out",
            "new-client",
            "--force",
        ],
    ] {
        let output = ob()
            .args(&args)
            .current_dir(&scratch.0)
            .env("HOME", &scratch.0)
            .env("XDG_CONFIG_HOME", &scratch.0)
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
