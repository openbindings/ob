#!/usr/bin/env node
// Durable Delegate Manager qualification entrypoint, shared by CI and the
// non-publishing release validation steps. It runs the mandatory manager,
// process-locking, conversion/recovery and public-surface tests explicitly
// (outside -short, with race detection where the host supports it), requires
// the exact conformance corpora, and refuses zero-match selectors, skipped
// mandatory tests and any failure. Usage:
//
//   node scripts/verify-delegate-manager.mjs [--no-race] [--evidence <file>]
//
// Required environment: OB_INTERFACES_CORPUS (the selected interfaces
// checkout's conformance directory) and OB_SPEC_CORPUS. OB_CORPUS_REQUIRED
// is forced to 1 so a missing corpus is a failure, never a skip.
import fs from 'node:fs';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const race = !args.includes('--no-race');
const evidenceIndex = args.indexOf('--evidence');
const evidencePath = evidenceIndex >= 0 ? path.resolve(args[evidenceIndex + 1]) : null;
const gates = JSON.parse(fs.readFileSync(path.join(root, 'scripts', 'delegate-manager-gates.json'), 'utf8'));
if (gates.format !== 'ob.delegate-manager-gates@1') throw new Error('unsupported gate manifest');

const failures = [];
for (const name of ['OB_INTERFACES_CORPUS', 'OB_SPEC_CORPUS']) {
  const value = process.env[name];
  if (!value || !fs.existsSync(value)) failures.push(`${name} must name an existing corpus directory (got ${JSON.stringify(value ?? '')})`);
}
const admission = process.env.OB_INTERFACES_CORPUS ? path.join(process.env.OB_INTERFACES_CORPUS, 'delegate-manager', 'admission.json') : '';
if (admission && !fs.existsSync(admission)) failures.push(`interfaces corpus lacks delegate-manager/admission.json: ${admission}`);
if (failures.length) {
  console.error(failures.join('\n'));
  process.exit(2);
}
const env = {
  ...process.env,
  OB_CORPUS_REQUIRED: '1',
  OB_NO_UPDATE_CHECK: '1',
  OB_TEST_NO_CREDENTIAL_STORE: process.env.OB_TEST_NO_CREDENTIAL_STORE ?? '1',
};
const record = {started: new Date().toISOString(), race, corpora: {interfaces: process.env.OB_INTERFACES_CORPUS, spec: process.env.OB_SPEC_CORPUS}, packages: {}};

function goList(pkg) {
  const list = spawnSync('go', ['test', '-list', '.', pkg], {cwd: root, env, encoding: 'utf8', maxBuffer: 64 << 20});
  if (list.status !== 0) throw new Error(`go test -list ${pkg} failed:\n${list.stderr}`);
  return new Set(list.stdout.split('\n').map((line) => line.trim()).filter((line) => line.startsWith('Test')));
}

for (const [pkg, tests] of Object.entries(gates.packages)) {
  const available = goList(pkg);
  const missing = tests.filter((name) => !available.has(name));
  if (missing.length) failures.push(`${pkg}: mandatory tests absent from the inventory: ${missing.join(', ')}`);
  const selector = '^(' + tests.join('|') + ')$';
  const goArgs = ['test', '-json', '-count=1', ...(race ? ['-race'] : []), '-run', selector, pkg];
  const run = spawnSync('go', goArgs, {cwd: root, env, encoding: 'utf8', maxBuffer: 512 << 20});
  const outcomes = new Map();
  // Output is retained per top-level test (subtests fold into their parent)
  // and at package level, so a red run is diagnosable from the evidence
  // record alone: a timeout or panic that aborts the package leaves every
  // later test not-run, and only the package output says why.
  const output = new Map();
  const packageOutput = [];
  for (const line of run.stdout.split('\n')) {
    if (!line.startsWith('{')) continue;
    let event;
    try { event = JSON.parse(line); } catch { continue; }
    if (event.Action === 'output') {
      if (event.Test) {
        const top = event.Test.split('/')[0];
        output.set(top, (output.get(top) ?? '') + event.Output);
      } else {
        packageOutput.push(event.Output);
      }
      continue;
    }
    if (!event.Test || event.Test.includes('/')) continue;
    if (['pass', 'fail', 'skip'].includes(event.Action)) outcomes.set(event.Test, event.Action);
  }
  const summary = {};
  const failedOutput = {};
  for (const name of tests) {
    const outcome = outcomes.get(name) ?? 'not-run';
    summary[name] = outcome;
    if (outcome !== 'pass') {
      failures.push(`${pkg}: ${name} ${outcome}`);
      if (output.has(name)) failedOutput[name] = output.get(name);
    }
  }
  const packageText = packageOutput.join('');
  record.packages[pkg] = {exitStatus: run.status, selector, outcomes: summary, failedOutput, packageOutput: packageText, stderr: run.stderr};
  if (run.status !== 0) {
    failures.push(`${pkg}: go test exited ${run.status}`);
    for (const [name, text] of Object.entries(failedOutput)) process.stderr.write(`--- ${pkg} ${name}\n${text}`);
    process.stderr.write(packageText);
    process.stderr.write(run.stderr);
  }
}
record.finished = new Date().toISOString();
record.failures = failures;
if (evidencePath) {
  fs.mkdirSync(path.dirname(evidencePath), {recursive: true});
  fs.writeFileSync(evidencePath, JSON.stringify(record, null, 2) + '\n', {flag: 'wx'});
}
const total = Object.values(gates.packages).reduce((n, tests) => n + tests.length, 0);
if (failures.length) {
  console.error(`delegate-manager gates: ${failures.length} problem(s) across ${total} mandatory tests\n` + failures.join('\n'));
  process.exit(1);
}
console.log(`delegate-manager gates: ${total} mandatory tests passed${race ? ' with race detection' : ''}`);
