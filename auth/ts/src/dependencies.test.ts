/**
 * The direction of the dependency, which is the whole reason this is a package
 * of its own: the profile imports the runtime and Archon, and the components
 * the runtime is made of import neither this package nor anything of Archon —
 * not in a manifest, not in a source file. An identity layer is the
 * consumer's choice, and a consumer that makes no such choice installs
 * nothing for it.
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

const here = path.join(root, 'auth', 'ts');

test('the components carry no identity layer, and this package is the only one that may', () => {
  assert.equal(packages.length >= 6, true, `found ${packages.length} packages under */ts`);
  const carrying = packages.filter((directory) =>
    dependencies(directory).some((name) => name.startsWith('@bitspark/archon')),
  );
  assert.equal(
    carrying.every((directory) => directory === here),
    true,
    `an identity layer outside auth/ts: ${carrying.join(', ')}`,
  );
  // And nothing of the components names it in a source file either, which is
  // where a dependency would arrive before a manifest caught up with it.
  for (const directory of packages) {
    if (directory === here) continue;
    for (const file of sources(directory)) {
      const source = readFileSync(file, 'utf8');
      assert.equal(source.includes('@bitspark/archon'), false, file);
      assert.equal(source.includes('@nightseam/auth'), false, file);
    }
  }
});

test('this package depends on Archon and on the components it composes onto, and on nothing else', () => {
  const manifest = JSON.parse(readFileSync(path.join(here, 'package.json'), 'utf8'));
  // Archon, the components the profile composes onto, and the one hash the
  // grant's digest is — SHA-256, which Archon's TypeScript core does not
  // export and the platform offers only asynchronously.
  const allowed = new Set([
    '@bitspark/archon',
    '@bitspark/archon-sdk',
    '@noble/hashes',
    '@nightseam/runtime',
    '@nightseam/duplex',
    '@nightseam/live',
  ]);
  for (const name of Object.keys(manifest.dependencies ?? {})) {
    assert.equal(allowed.has(name), true, `${name} is not a dependency the profile may carry`);
  }
});
