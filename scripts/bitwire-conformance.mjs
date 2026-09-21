// Exercise actual Nightseam implementations against the released composition
// oracle, never the upstream test-only endpoint/router implementations.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const module = 'github.com/Bitspark/bitwire';
const revision = '616a2fc5e3a0972f67f40331a9d9ca102bc9698d';
const run = (program, args, env = {}) => execFileSync(program, args, {
  cwd: root, encoding: 'utf8', timeout: 180_000, maxBuffer: 4 * 1024 * 1024,
  windowsHide: true, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'inherit'],
});
const dependency = JSON.parse(run('go', ['list', '-m', '-json', module]));
assert.equal(dependency.Version, 'v0.2.0');
assert.equal(dependency.Replace, undefined, 'Bitwire must resolve as a public versioned dependency');
const downloaded = JSON.parse(run('go', ['mod', 'download', '-json', `${module}@${dependency.Version}`]));
assert.equal(downloaded.Origin.Hash, revision);
assert.equal(downloaded.Sum, 'h1:gGlNYgfzAHqZtxorw6HkCTWOoInil//uchOWhTA/d3k=');
const expected = JSON.parse(readFileSync(join(downloaded.Dir, 'conformance/reference/expected.json'), 'utf8'));
assert.deepEqual(Object.keys(expected).sort(), ['composition', 'opaquePaths', 'overlap', 'sameIDDelayedReplies', 'selectedEndpoints', 'siblings']);
// The reference's literal identifier is a placeholder outside Nightseam's
// canonical c:/s: decimal grammar. Instantiate only that input and its two
// echoed observations; no boolean, path, lifetime or routing oracle is changed.
assert.deepEqual(expected.sameIDDelayedReplies.replyIDs, ['same-id', 'same-id']);
expected.sameIDDelayedReplies.replyIDs = ['c:1', 'c:1'];
function compare(label, output) {
  assert.deepEqual(JSON.parse(output), expected, `${label}: composition observations differ from public Bitwire oracle`);
  console.log(`Bitwire ${label}: all six released composition groups passed`);
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
    compare(`Go/${role}`, run(driver, [], env));
    compare(`TypeScript/${role}`, run(process.execPath, ['--experimental-strip-types', 'conformance/ts/src/bitwire.ts'], env));
  }
  console.log('All six groups run on every carrier; repeated request-ID input is profile-valid c:1. Historical 0.1 registration cases are not 0.2 obligations.');
} finally {
  assert.equal(dirname(resolve(scratch)), resolve(tmpdir()));
  rmSync(scratch, { recursive: true, force: true, maxRetries: 5 });
}
