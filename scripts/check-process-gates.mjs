#!/usr/bin/env node
// Reads a verbose `go test` log of the separate-process locking/recovery
// gates and refuses a run in which any mandatory test did not report PASS
// (a zero-match selector produces no PASS lines and therefore fails).
import fs from 'node:fs';

const mandatory = [
  'TestEnvConfigProcesses',
  'TestEnvConfigRecovery',
  'TestEnvConfigLockTimeoutAndReentrancy',
  'TestEnvConfigSyncAndRenameFailure',
  'TestRoleRegistryProcesses',
  'TestRoleRegistryRecovery',
  'TestRoleRegistryConcurrentReaders',
  'TestRoleRegistryUnrelatedConfigProcess',
  'TestDelegateMigrationRecovery',
  'TestDelegateMigrationRollbackRecovery',
];
const log = fs.readFileSync(process.argv[2], 'utf8');
const missing = mandatory.filter((name) => !new RegExp(`^--- PASS: ${name} `, 'm').test(log));
const skipped = mandatory.filter((name) => new RegExp(`^--- SKIP: ${name} `, 'm').test(log));
if (missing.length || skipped.length || !/^ok\s/m.test(log)) {
  console.error(`process gates: missing PASS for ${missing.join(', ') || 'none'}; skipped: ${skipped.join(', ') || 'none'}; package ok line ${/^ok\s/m.test(log) ? 'present' : 'absent'}`);
  process.exit(1);
}
console.log(`process gates: ${mandatory.length} separate-process tests passed on ${process.platform}`);
