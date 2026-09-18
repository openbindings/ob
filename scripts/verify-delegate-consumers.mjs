#!/usr/bin/env node
// Independent-consumer qualification for the Delegate Manager (B2 / W07).
// Builds the candidate ob and the synthetic provider, then runs the Go and
// TypeScript consumers, which import only the public SDKs and drive the
// candidate's published Usage and OpenAPI bindings plus its served streaming
// binding. The TypeScript SDK is installed from exact packed tarballs
// (OB_TS_SDK_PACKS), never from a workspace link; that is labeled as packed
// prerelease artifacts, not a public npm release.
//
//   node scripts/verify-delegate-consumers.mjs [--skip-ts] [--evidence <file>]
//
// Environment: OB_TS_SDK_PACKS (directory of *.tgz for @openbindings/core,
// invoke, synthesize, json, json-schema, usage, openapi and jsonata) unless
// --skip-ts. Go modules resolve with GOWORK=off from the public proxy.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const skipTS = args.includes('--skip-ts');
const evidenceIndex = args.indexOf('--evidence');
const evidencePath = evidenceIndex >= 0 ? path.resolve(args[evidenceIndex + 1]) : null;
const work = fs.mkdtempSync(path.join(os.tmpdir(), 'ob-delegate-consumers-'));
const record = {started: new Date().toISOString(), work, commands: []};
const sha256 = (file) => createHash('sha256').update(fs.readFileSync(file)).digest('hex');

function run(label, command, cmdArgs, options = {}) {
  const started = new Date().toISOString();
  const result = spawnSync(command, cmdArgs, {encoding: 'utf8', maxBuffer: 256 << 20, ...options});
  const row = {label, command: [command, ...cmdArgs].join(' '), cwd: options.cwd ?? process.cwd(), started, status: result.status, signal: result.signal};
  record.commands.push(row);
  if (result.status !== 0) {
    process.stderr.write(`${label} failed (status ${result.status})\n${result.stdout}\n${result.stderr}\n`);
    finish(1);
  }
  return result;
}
// runAllowingFailure records the command like run() but leaves the verdict to
// the per-claim outcome table (a consumer test run is expected to carry red claims
// while a recorded finding is open).
function runAllowingFailure(label, command, cmdArgs, options = {}) {
  const started = new Date().toISOString();
  const result = spawnSync(command, cmdArgs, {encoding: 'utf8', maxBuffer: 256 << 20, ...options});
  record.commands.push({label, command: [command, ...cmdArgs].join(' '), cwd: options.cwd ?? process.cwd(), started, status: result.status, signal: result.signal});
  return result;
}
function finish(code) {
  record.finished = new Date().toISOString();
  record.exitCode = code;
  if (evidencePath) {
    fs.mkdirSync(path.dirname(evidencePath), {recursive: true});
    fs.writeFileSync(evidencePath, JSON.stringify(record, null, 2) + '\n', {flag: 'wx'});
  }
  process.exit(code);
}

const goEnv = {...process.env, GOWORK: 'off', GOFLAGS: '-mod=readonly', OB_NO_UPDATE_CHECK: '1'};
const binary = path.join(work, process.platform === 'win32' ? 'ob.exe' : 'ob');
run('build-candidate', 'go', ['build', '-o', binary, './cmd/ob'], {cwd: root, env: goEnv});
const consumerGo = path.join(root, 'qualification', 'delegate-manager', 'go');
const provider = path.join(work, process.platform === 'win32' ? 'provider.exe' : 'provider');
run('build-provider', 'go', ['build', '-o', provider, './cmd/provider'], {cwd: consumerGo, env: goEnv});
record.candidateBinary = {path: binary, sha256: sha256(binary)};
record.providerBinary = {path: provider, sha256: sha256(provider)};
record.candidateRevision = run('candidate-revision', 'git', ['rev-parse', 'HEAD'], {cwd: root}).stdout.trim();
record.candidateDirty = run('candidate-status', 'git', ['status', '--porcelain', '--untracked-files=no'], {cwd: root}).stdout.trim() !== '';
const consumerEnv = {...goEnv, OB_CANDIDATE_BINARY: binary, OB_PROVIDER_BINARY: provider};

const goTest = runAllowingFailure('go-consumer', 'go', ['test', '-race', '-count=1', '-json', './...'], {cwd: consumerGo, env: consumerEnv});
const goOutcomes = {};
for (const line of goTest.stdout.split('\n')) {
  if (!line.startsWith('{')) continue;
  let event; try { event = JSON.parse(line); } catch { continue; }
  if (event.Test && !event.Test.includes('/') && ['pass', 'fail', 'skip'].includes(event.Action)) goOutcomes[event.Test] = event.Action;
}
record.goConsumer = goOutcomes;
// Every claim is reported; a claim that is red at the selected SDK because of a
// recorded finding is named with that finding and still fails the script. The
// ledger (audit/delegate-manager-migration/2026-09-17-completion/findings.md)
// owns the attribution; nothing here skips or downgrades a claim.
const knownBlocked = {
  go: {TestConsumerListRolesOverUsage: 'F-06 (Go SDK no-input convention discards the caller value for an input-less operation)'},
  ts: {
    'usage: lifecycle, value fidelity and resulting delegation': 'F-08 (TypeScript core validates with the deprecated retained compiler; exact decimals throw)',
    'openapi: lifecycle, value fidelity and resulting delegation': 'F-08 (TypeScript core validates with the deprecated retained compiler; exact decimals throw)',
  },
};
let blocked = 0;
// Every top-level Go test the module actually ran is reported; a newly added
// consumer claim cannot be missed by a hard-coded list. The named claims must
// be present, so a silently dropped test is a failure rather than an absence.
const goExpected = ['TestConsumerLifecycleAndDelegation', 'TestConsumerListRolesOverUsage', 'TestConsumerRefusalsLeaveStateUntouched'];
const goRequired = [...new Set([...goExpected, ...Object.keys(goOutcomes)])].sort();
for (const name of goRequired) {
  const outcome = goOutcomes[name] ?? 'not-run';
  const note = outcome === 'pass' ? '' : knownBlocked.go[name] ? `  blocked by ${knownBlocked.go[name]}` : '  UNATTRIBUTED';
  process.stdout.write(`go consumer: ${name}: ${outcome}${note}\n`);
  if (outcome !== 'pass') blocked += 1;
}

if (!skipTS) {
  const packs = process.env.OB_TS_SDK_PACKS;
  if (!packs || !fs.existsSync(packs)) { process.stderr.write('OB_TS_SDK_PACKS must name a directory of packed SDK tarballs (or pass --skip-ts)\n'); finish(2); }
  const tarballs = fs.readdirSync(packs).filter((name) => name.endsWith('.tgz')).map((name) => path.join(packs, name));
  record.typescriptPacks = tarballs.map((file) => ({file: path.basename(file), sha256: sha256(file)}));
  const consumerTS = path.join(root, 'qualification', 'delegate-manager', 'ts');
  const install = path.join(work, 'ts-consumer');
  fs.mkdirSync(install, {recursive: true});
  for (const entry of fs.readdirSync(consumerTS)) {
    if (entry === 'node_modules') continue;
    fs.cpSync(path.join(consumerTS, entry), path.join(install, entry), {recursive: true});
  }
  // Every @openbindings package resolves to its exact tarball, including
  // transitive ranges inside the packs (npm would otherwise consult the
  // registry for an unpublished sibling or a workspace file: link).
  const overrides = {};
  for (const file of tarballs) {
    const manifest = JSON.parse(run('ts-pack-manifest', 'tar', ['-xOzf', file, 'package/package.json']).stdout);
    overrides[manifest.name] = `file:${file}`;
  }
  const manifestPath = path.join(install, 'package.json');
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  // npm refuses an override that conflicts with a direct dependency range, so
  // the direct devDependencies carry the identical exact tarball spec.
  for (const section of ['dependencies', 'devDependencies']) {
    for (const name of Object.keys(manifest[section] ?? {})) {
      if (overrides[name]) manifest[section][name] = overrides[name];
    }
  }
  manifest.overrides = overrides;
  fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + '\n');
  run('ts-install-packed-sdk', 'npm', ['install', '--no-audit', '--no-fund', '--no-package-lock', '--install-links'], {cwd: install});
  const tsFiles = fs.readdirSync(path.join(install, 'test')).filter((name) => name.endsWith('.test.mjs')).map((name) => path.join('test', name)).sort();
  const tsTest = runAllowingFailure('ts-consumer', process.execPath, ['--test', ...tsFiles], {cwd: install, env: consumerEnv});
  const tsOutcomes = {};
  for (const line of tsTest.stdout.split('\n')) {
    const m = line.match(/^(ok|not ok) \d+ - (.+)$/);
    if (m) tsOutcomes[m[2]] = m[1] === 'ok' ? 'pass' : 'fail';
  }
  record.typescriptConsumer = tsOutcomes;
  for (const [name, outcome] of Object.entries(tsOutcomes)) {
    const note = outcome === 'pass' ? '' : knownBlocked.ts[name] ? `  blocked by ${knownBlocked.ts[name]}` : '  UNATTRIBUTED';
    process.stdout.write(`ts consumer: ${name}: ${outcome}${note}\n`);
    if (outcome !== 'pass') blocked += 1;
  }
  if (Object.keys(tsOutcomes).length === 0) { process.stderr.write(tsTest.stdout); blocked += 1; }
}
if (blocked > 0) {
  process.stdout.write(`delegate-manager consumers: ${blocked} claim(s) not passing against candidate ${record.candidateRevision}${record.candidateDirty ? ' (dirty tree)' : ''}; see the attributions above\n`);
  finish(1);
}
console.log(`delegate-manager consumers: Go${skipTS ? '' : ' and TypeScript'} consumers passed against candidate ${record.candidateRevision}${record.candidateDirty ? ' (dirty tree)' : ''}`);
finish(0);
