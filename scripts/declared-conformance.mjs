// Independent upstream observations exercised through the production Declared API.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compareDeclared, declaredInputs } from './declared-results.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const upstream = JSON.parse(readFileSync(join(root, 'conformance/declared/upstream.json'), 'utf8'));
const bytes = readFileSync(join(root, 'conformance/declared/cases.json'));
assert.equal(createHash('sha256').update(bytes).digest('hex'), upstream.sha256, 'upstream fixture changed');
const fixture = JSON.parse(bytes);
const input = declaredInputs(fixture);
const run = (program, args, env = {}) => execFileSync(program, args, {
  cwd: root, encoding: 'utf8', timeout: 180_000, maxBuffer: 4 * 1024 * 1024,
  windowsHide: true, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'inherit'],
});
const scratch = mkdtempSync(join(tmpdir(), 'nightseam-declared-'));
try {
  const inputPath = join(scratch, 'input.json');
  writeFileSync(inputPath, JSON.stringify(input));
  const driver = join(scratch, process.platform === 'win32' ? 'driver.exe' : 'driver');
  run('go', ['build', '-o', driver, './conformance/declared/go']);
  run(process.execPath, [join(root, 'node_modules/typescript/bin/tsc'), '-p', 'conformance/ts/tsconfig.check.json']);
  for (const [carrier, reverse] of [['local', '0'], ['peer', '0'], ['peer', '1']]) {
    const env = { NIGHTSEAM_BITWIRE_CARRIER: carrier, NIGHTSEAM_BITWIRE_REVERSE: reverse };
    const role = carrier === 'local' ? 'local' : `WebSocket/${reverse === '0' ? 'client-to-server' : 'server-to-client'}`;
    for (const [language, program, args] of [
      ['Go', driver, [inputPath]],
      ['TypeScript', process.execPath, ['--experimental-strip-types', 'conformance/ts/src/declared.ts', inputPath]],
    ]) {
      compareDeclared(fixture, JSON.parse(run(program, args, env)));
      console.log(`Declared ${language}/${role}: ${fixture.cases.length}/${fixture.cases.length} upstream cases passed`);
    }
  }
  console.log(`Expectations: Bitwire ${upstream.revision}; production construction, parts and routing in both languages.`);
} finally {
  assert.equal(dirname(resolve(scratch)), resolve(tmpdir()));
  assert.ok(scratch.startsWith(join(tmpdir(), 'nightseam-declared-')));
  rmSync(scratch, { recursive: true, force: true, maxRetries: 5 });
}
