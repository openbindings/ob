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

const goTest = run('go-consumer', 'go', ['test', '-race', '-count=1', '-json', './...'], {cwd: consumerGo, env: consumerEnv});
const goOutcomes = {};
for (const line of goTest.stdout.split('\n')) {
  if (!line.startsWith('{')) continue;
  let event; try { event = JSON.parse(line); } catch { continue; }
  if (event.Test && !event.Test.includes('/') && ['pass', 'fail', 'skip'].includes(event.Action)) goOutcomes[event.Test] = event.Action;
}
record.goConsumer = goOutcomes;
for (const name of ['TestConsumerLifecycleAndDelegation', 'TestConsumerRefusalsLeaveStateUntouched']) {
  if (goOutcomes[name] !== 'pass') { process.stderr.write(`Go consumer: ${name} ${goOutcomes[name] ?? 'not-run'}\n`); finish(1); }
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
  run('ts-install-packed-sdk', 'npm', ['install', '--no-audit', '--no-fund', '--no-package-lock', '--install-links', ...tarballs], {cwd: install});
  const tsTest = run('ts-consumer', process.execPath, ['--test', 'test/'], {cwd: install, env: consumerEnv});
  record.typescriptConsumer = {stdout: tsTest.stdout.split('\n').filter((line) => /^# (pass|fail|tests)/.test(line))};
  if (!/^# fail 0$/m.test(tsTest.stdout)) { process.stderr.write(tsTest.stdout); finish(1); }
}
console.log(`delegate-manager consumers: Go${skipTS ? '' : ' and TypeScript'} consumers passed against candidate ${record.candidateRevision}${record.candidateDirty ? ' (dirty tree)' : ''}`);
finish(0);
