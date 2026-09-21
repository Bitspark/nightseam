// Compare observations from the migrated runtimes with Bitwire's published
// independent cases. This repository never copies or rewrites their oracle.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const module = 'github.com/Bitspark/bitwire';
const revision = '9f45a2e0e9dc576db34237e5ad3aaaa0266a276b';
const run = (program, args, env = {}) => execFileSync(program, args, {
  cwd: root, encoding: 'utf8', timeout: 180_000, maxBuffer: 4 * 1024 * 1024,
  windowsHide: true, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'inherit'],
});
const dependency = JSON.parse(run('go', ['list', '-m', '-json', module]));
assert.equal(dependency.Version, 'v0.1.0');
assert.equal(dependency.Replace, undefined, 'Bitwire must resolve as a public versioned dependency');
const downloaded = JSON.parse(run('go', ['mod', 'download', '-json', `${module}@${dependency.Version}`]));
assert.equal(downloaded.Origin.Hash, revision);
assert.equal(downloaded.Sum, 'h1:kzUdpCWeepYLHkqmcBWCO33smBSajl2Z/qHl5vSAmsI=');
const casesPath = join(downloaded.Dir, 'conformance/cases/access.json');
const fixture = JSON.parse(readFileSync(casesPath, 'utf8'));
assert.equal(fixture.schemaVersion, 1);
assert.equal(fixture.cases.length, 10);
assert.equal(new Set(fixture.cases.map(test => test.id)).size, 10);
const physicalException = fixture.cases.filter(test => test.id === 'distinct-opaque-paths');
assert.equal(physicalException.length, 1);
assert.ok(physicalException[0].registrations.some(entry => entry.path.length === 0 && !entry.namespace));

function compare(label, carrier, output) {
  const observations = JSON.parse(output);
  const expected = fixture.cases.filter(test => carrier === 'local' || test.id !== 'distinct-opaque-paths');
  assert.ok(Array.isArray(observations), `${label}: observations must be an array`);
  assert.equal(observations.length, expected.length, `${label}: missing or extra cases`);
  const actual = new Map(observations.map(row => [row.id, row.observations]));
  assert.equal(actual.size, observations.length, `${label}: duplicate observations`);
  assert.deepEqual([...actual.keys()].sort(), expected.map(test => test.id).sort(), `${label}: case inventory`);
  for (const test of expected) assert.deepEqual(actual.get(test.id), test.expected, `${label}: ${test.id}`);
  console.log(`Bitwire ${label}: ${expected.length} independent cases passed`);
}

const scratch = mkdtempSync(join(tmpdir(), 'nightseam-bitwire-'));
try {
  mkdirSync(join(scratch, 'bin'));
  const driver = join(scratch, 'bin', process.platform === 'win32' ? 'driver.exe' : 'driver');
  run('go', ['build', '-o', driver, './conformance/bitwire/go']);
  run(process.execPath, [join(root, 'node_modules/typescript/bin/tsc'), '-p', 'conformance/ts/tsconfig.check.json']);
  for (const [carrier, reverse] of [['local', '0'], ['peer', '0'], ['peer', '1']]) {
    const env = { NIGHTSEAM_BITWIRE_CARRIER: carrier, NIGHTSEAM_BITWIRE_REVERSE: reverse };
    const role = carrier === 'local' ? 'local' : `WebSocket/${reverse === '0' ? 'client-to-server' : 'server-to-client'}`;
    compare(`Go/${role}`, carrier, run(driver, [casesPath], env));
    compare(`TypeScript/${role}`, carrier, run(process.execPath, ['--experimental-strip-types', 'conformance/ts/src/bitwire.ts', casesPath], env));
  }
  console.log('Physical applicability: distinct-opaque-paths is local-only because its exact [] receiver is refused at a peer root. All ten cases remain mandatory locally.');
} finally {
  assert.equal(dirname(resolve(scratch)), resolve(tmpdir()));
  rmSync(scratch, { recursive: true, force: true, maxRetries: 5 });
}
