// Independent TypeScript consumer for the Delegate Manager.
// Required environment: OB_CANDIDATE_BINARY, OB_PROVIDER_BINARY.
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { after, before, test } from 'node:test';
import { OperationInvoker, USE_DEFAULT, operationSignature } from '@openbindings/invoke';
import { UsageInvoker } from '@openbindings/usage';
import { OpenAPIInvoker } from '@openbindings/openapi';
import { compareNumberTokens, parse as parseExact, stringify as stringifyExact } from '@openbindings/json';
import { createJSONataEvaluator } from '@openbindings/invoke/jsonata';
import { createJSONataExecutor } from '@openbindings/jsonata';

const transformEvaluator = createJSONataEvaluator(createJSONataExecutor({ timeout: 10000 }));

const shared = 'openbindings.delegate-manager.';
const ops = { listRoles: shared + 'listRoles', register: shared + 'registerDelegate', list: shared + 'listDelegates', prefer: shared + 'setDelegatePreference', unregister: shared + 'unregisterDelegate' };
const machineLane = new Set(['openbindings.ob.listDelegateRoles', 'openbindings.ob.registerDelegate', 'openbindings.ob.listDelegates', 'openbindings.ob.setDelegatePreference', 'openbindings.ob.unregisterDelegate', 'openbindings.ob.resolveRoleDelegate']);

function requireEnv(name) {
  const value = process.env[name];
  assert.ok(value, `${name} is required`);
  return value;
}
const binary = requireEnv('OB_CANDIDATE_BINARY');
const providerBinary = requireEnv('OB_PROVIDER_BINARY');
const workDir = mkdtempSync(join(tmpdir(), 'ob-ts-consumer-'));
const environment = {
  ...process.env,
  OB_CONFIG_DIR: join(workDir, 'config'), OB_CACHE_DIR: join(workDir, 'cache'),
  OB_CREDENTIALS_FILE: join(workDir, 'credentials.json'), OB_NO_UPDATE_CHECK: '1',
  PATH: dirname(binary) + (process.platform === 'win32' ? ';' : ':') + process.env.PATH,
};
function ob(...args) {
  const result = spawnSync(binary, args, { cwd: workDir, env: environment, encoding: 'utf8' });
  assert.equal(result.status, 0, `ob ${args.join(' ')}: ${result.stderr}`);
  return result.stdout;
}

let server, serverURL, provider, providerInfo, cliOBI, servedOBI, providerValue;
const token = 'ts-consumer-token';

before(async () => {
  // ob discovers an environment by walking up from the working directory
  // before consulting OB_CONFIG_DIR, and the developer's home holds one; the
  // SDK spawns commands in this process's working directory.
  process.chdir(workDir);
  ob('init');
  cliOBI = parseExact(ob('--openbindings'));
  const port = 20500 + Math.floor(Math.random() * 1000);
  server = spawn(binary, ['start', '--port', String(port), '--strict-port', '--token', token], { cwd: workDir, env: environment, stdio: ['ignore', 'ignore', 'pipe'] });
  serverURL = await new Promise((resolve, reject) => {
    let text = '';
    server.stderr.on('data', (chunk) => { text += chunk; const m = text.match(/http:\/\/127\.0\.0\.1:\d+/); if (m) resolve(m[0]); });
    server.on('exit', () => reject(new Error('ob start exited: ' + text)));
    setTimeout(() => reject(new Error('ob start not ready: ' + text)), 60000);
  });
  servedOBI = parseExact(await (await fetch(serverURL + '/.well-known/openbindings')).text());
  // The provider is built from the candidate's advertised accepted interfaces,
  // discovered through the served OpenAPI lane; the Usage-lane listRoles claim
  // is its own test below (F-06).
  const roles = (await http(ops.listRoles, undefined)).roles;
  const accepted = (id) => { const role = roles.find((r) => r.id === id); const path = join(workDir, id + '.accepted.json'); writeFileSync(path, stringifyExact(role.acceptedInterfaces[0])); return path; };
  const obiPath = join(workDir, 'provider.obi.json');
  provider = spawn(providerBinary, ['--accepted', accepted('invoke'), '--extra', accepted('synthesize'), '--obi', obiPath], { stdio: ['pipe', 'pipe', 'inherit'] });
  providerInfo = await new Promise((resolve, reject) => {
    let text = '';
    provider.stdout.on('data', (chunk) => { text += chunk; if (text.includes('\n')) resolve(JSON.parse(text.split('\n')[0])); });
    provider.on('exit', () => reject(new Error('provider exited')));
  });
  providerValue = parseExact(readFileSync(obiPath, 'utf8'));
});
after(() => { provider?.stdin.end(); provider?.kill(); server?.kill(); });

async function single(invocation, input, writeInput = input !== undefined) {
  // Every CLI realization is unary: one input message (null for an
  // input-less operation) carries the binding's adaptation transform.
  if (writeInput) await invocation.write(input === undefined ? null : input);
  await invocation.close();
  let out; let count = 0;
  for await (const value of invocation.outputs) { out = value; count++; }
  assert.ok(count <= 1, 'unary operation emitted more than one output');
  return count === 0 ? null : out;
}

// Usage lane: the consumer's own transcription of the published recipe
// (docs/bound-cli-recipe.md) through the TypeScript Usage package's
// configuration seam: named decoders on the invoker, selected per invocation
// by context configuration (`decode`), and the registerDelegate interface
// field routed to stdin as the `-` operand (`route`). The generic operation
// invoker's outputDecoder/fieldRouter hooks are not this package's seam.
const usageInvoker = new UsageInvoker({
  authorizeExecAddress: (argv) => argv[0] === 'ob' || argv[0] === binary,
  decoders: { json: (bytes) => (bytes.length ? parseExact(new TextDecoder('utf-8', { fatal: true }).decode(bytes)) : null) },
});
const usageEngine = new OperationInvoker([usageInvoker], { transformEvaluator });
function cliContext(operation) {
  const configuration = { decode: 'json' };
  if (operation === ops.register) configuration.route = { interface: { kind: 'stdin', operand: 'dash' } };
  return {
    configuration,
    environment: { OB_CONFIG_DIR: environment.OB_CONFIG_DIR, OB_CACHE_DIR: environment.OB_CACHE_DIR, OB_CREDENTIALS_FILE: environment.OB_CREDENTIALS_FILE, OB_NO_UPDATE_CHECK: '1', PATH: environment.PATH },
  };
}
async function cli(operation, input) {
  const bound = boundOperation(operation);
  assert.ok(machineLane.has(bound), `${operation} is not a machine-lane operation`);
  // Every CLI realization is unary: one input message carries the binding's
  // adaptation transform. An operation that declares an input receives the
  // portable "nothing supplied" value {} when the caller passes nothing; an
  // input-less operation receives null (the transform still applies here).
  const value = input === undefined && cliOBI.operations[bound].input != null ? {} : input;
  const invocation = usageEngine.invoke(cliOBI, operationSignature(operation), { context: cliContext(operation) });
  return single(invocation, value, true);
}
// boundOperation resolves a shared operation key to the bound CLI OBI's own
// operation key through the document's declared correspondence (aliases).
function boundOperation(operation) {
  for (const [key, op] of Object.entries(cliOBI.operations)) {
    if (key === operation || (Array.isArray(op.aliases) && op.aliases.includes(operation))) return key;
  }
  return operation;
}
async function http(operation, input) {
  const engine = new OperationInvoker([new OpenAPIInvoker()], { transformEvaluator });
  const invocation = engine.invoke(servedOBI, operationSignature(operation), { context: { bearerToken: token, configuration: { security: { index: 1 } } } });
  return single(invocation, input);
}
async function counters() { return (await fetch(providerInfo.url + '/counters')).json(); }
const exact = (value) => stringifyExact(value);

// The Usage lane's listRoles claim. Expected red at the packed SDK selection
// only if the TypeScript Usage package discarded the caller's value; it reads
// the first input whenever the command declares a field, so the binding's
// `--format json` transform applies here (contrast F-06 for the Go SDK).
test('usage: listRoles carries the binding transform for an input-less operation', async () => {
  const roles = (await cli(ops.listRoles, undefined)).roles.map((r) => r.id).sort();
  assert.deepEqual(roles, ['inspect', 'invoke', 'synthesize']);
});

for (const [name, call, discover] of [['usage', cli, http], ['openapi', http, http]]) {
  test(`${name}: lifecycle, value fidelity and resulting delegation`, async () => {
    const roles = (await discover(ops.listRoles, undefined)).roles.map((r) => r.id).sort();
    assert.deepEqual(roles, ['inspect', 'invoke', 'synthesize']);
    const prefs = parseExact('{"invoke": 9007199254740993}');
    const record = await call(ops.register, { interface: providerValue, roles: ['invoke'], rolePreferences: prefs });
    assert.ok(typeof record.id === 'string' && record.id.length > 0);
    assert.equal(exact(record.rolePreferences), '{"invoke":9007199254740993}');
    assert.equal(exact(record.roles), '["invoke"]');
    assert.equal(exact(record.interface), exact(providerValue), 'retained interface differs from the supplied value');
    const second = await call(ops.register, { interface: providerValue, roles: ['invoke'] });
    assert.notEqual(second.id, record.id);
    assert.equal(exact(second.rolePreferences), '{}');
    assert.equal((await call(ops.list, { role: 'invoke' })).delegates.length, 2);
    assert.equal((await call(ops.list, { role: 'synthesize' })).delegates.length, 0, 'unrequested capability enrolled');
    for (const value of ['0', '-1.25', '1e400']) {
      assert.equal(await call(ops.prefer, { id: record.id, role: 'invoke', preference: parseExact(value) }), null);
      const listed = (await call(ops.list, { role: 'invoke' })).delegates[0];
      // The interface promises the number "preserved without silent rounding";
      // the CLI lane spells tokens canonically through JSONata $string (1e400
      // lists as 1e+400), so compare exact numeric identity, not spelling.
      assert.deepEqual(Object.keys(listed.rolePreferences), ['invoke']);
      assert.equal(compareNumberTokens(String(listed.rolePreferences.invoke), value), 0, `preference ${value} not retained exactly: ${exact(listed.rolePreferences)}`);
    }
    assert.equal(await call(ops.prefer, { id: record.id, role: 'invoke', preference: null }), null);
    assert.equal(exact((await call(ops.list, undefined)).delegates[0].rolePreferences), '{}');
    const replaced = await call(ops.register, { id: record.id, interface: providerValue, roles: ['invoke'], rolePreferences: {} });
    assert.equal(replaced.id, record.id);
    assert.equal(await call(ops.unregister, { id: second.id }), null);
    assert.equal(await call(ops.unregister, { id: second.id }), null);
    assert.equal((await call(ops.list, undefined)).delegates.length, 1);
    // Resulting delegation: a support query reaches the enrolled provider and
    // the diagnostic selects it; the extra capability and decoys stay silent.
    const before = await counters();
    const resolved = await call('openbindings.ob.resolveRoleDelegate', { role: 'invoke', bindingSpec: providerInfo.token });
    assert.equal(resolved.available, true);
    assert.equal(resolved.registrationId, record.id);
    const after = await counters();
    assert.ok(after.support >= before.support + 1, 'support query not observed');
    assert.equal(after.extra, 0); assert.equal(after.decoyCalls, 0); assert.equal(after.credentialBearing, 0);
    assert.equal(await call(ops.unregister, { id: record.id }), null);
    assert.equal((await call(ops.list, undefined)).delegates.length, 0);
    const gone = await call('openbindings.ob.resolveRoleDelegate', { role: 'invoke', bindingSpec: providerInfo.token });
    assert.equal(gone.available, false, 'removed registration still routed');
  });
}

test('openapi: refusals leave state untouched and admission never contacts the provider', async () => {
  const engine = new OperationInvoker([new OpenAPIInvoker()], { transformEvaluator });
  const failing = async (input) => {
    const invocation = engine.invoke(servedOBI, operationSignature(ops.register), { context: { bearerToken: token, configuration: { security: { index: 1 } } } });
    await assert.rejects(single(invocation, input));
  };
  await failing({ interface: providerValue, roles: ['inspect'] });
  await failing({ interface: providerValue, roles: ['invoke'], rolePreferences: { inspect: 1 } });
  assert.equal((await http(ops.list, undefined)).delegates.length, 0, 'refusals mutated the registry');
  const c = await counters();
  assert.equal(c.support + c.work + c.extra, 0, 'admission contacted the provider');
});
