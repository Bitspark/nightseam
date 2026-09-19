/**
 * The direction of the dependency, which is the whole reason this is a package
 * of its own: the adapter imports the runtime and OpenTelemetry, and the four
 * components the runtime is made of import neither this package nor anything
 * of OpenTelemetry — not in a manifest, not in a source file. A backend is the
 * consumer's choice, and a consumer that makes no such choice installs nothing
 * for it.
 */
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';

const root = path.join(import.meta.dirname, '..', '..', '..');
/** Every workspace package, found the way `scripts/packages.mjs` finds them. */
const packages = readdirSync(root, { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && entry.name !== 'node_modules')
  .map((entry) => path.join(root, entry.name, 'ts'))
  .filter((directory) => exists(path.join(directory, 'package.json')))
  .sort();

/** What a manifest may depend on, in the three ways one can. */
function dependencies(directory: string): string[] {
  const manifest = JSON.parse(readFileSync(path.join(directory, 'package.json'), 'utf8'));
  return [manifest.dependencies, manifest.devDependencies, manifest.peerDependencies].flatMap((named) =>
    Object.keys(named ?? {}),
  );
}

/** Every source file of a package, tests and checks among them. */
function sources(directory: string): string[] {
  const src = path.join(directory, 'src');
  return exists(src)
    ? readdirSync(src)
        .filter((name) => name.endsWith('.ts'))
        .map((name) => path.join(src, name))
    : [];
}

function exists(file: string): boolean {
  try {
    statSync(file);
    return true;
  } catch {
    return false;
  }
}

test('the components carry no OpenTelemetry dependency, and this package is the only one that does', () => {
  assert.equal(packages.length >= 5, true, `found ${packages.length} packages under */ts`);
  const carrying = packages.filter((directory) =>
    dependencies(directory).some((name) => name.startsWith('@opentelemetry/')),
  );
  assert.deepEqual(carrying, [path.join(root, 'otel', 'ts')]);
  // And nothing of the components names it in a source file either, which is
  // where a dependency would arrive before a manifest caught up with it.
  for (const directory of packages) {
    if (directory.endsWith(path.join('otel', 'ts'))) continue;
    for (const file of sources(directory)) {
      const source = readFileSync(file, 'utf8');
      assert.equal(source.includes('@opentelemetry/'), false, file);
      assert.equal(source.includes('@nightseam/otel'), false, file);
    }
  }
});

test('this package depends on the runtime and on OpenTelemetry, and on no other component', () => {
  const named = dependencies(path.join(root, 'otel', 'ts'));
  assert.equal(named.includes('@nightseam/runtime'), true);
  assert.equal(named.includes('@opentelemetry/api'), true);
  assert.equal(named.includes('@opentelemetry/core'), true);
  const manifest = JSON.parse(readFileSync(path.join(root, 'otel', 'ts', 'package.json'), 'utf8'));
  // The session, the tunnel and the seam are the suite's, which runs one call
  // through a relay; what the package ships is the runtime and the two.
  assert.deepEqual(Object.keys(manifest.dependencies), [
    '@nightseam/runtime',
    '@opentelemetry/api',
    '@opentelemetry/core',
  ]);
});
