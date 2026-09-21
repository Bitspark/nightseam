import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export const haskellManifests = [
  "duplex/hs/nightseam-duplex.cabal",
  "runtime/hs/nightseam-runtime.cabal",
  "conformance/haskell/nightseam-conformance.cabal",
];
const declaration = /^version:\s*(\S+)\s*$/m;
const dependency = /(nightseam-(?:duplex|runtime)\s*==)\s*([^,\s]+)/g;

function manifests(root) {
  if (!existsSync(join(root, "conformance/haskell/testee.json"))) return [];
  return haskellManifests.map(file => ({ file, source: readFileSync(join(root, file), "utf8") }));
}

export function setHaskellVersion(root, version) {
  const updates = manifests(root).map(({ file, source }) => {
    if (!declaration.test(source)) throw new Error(`${file} is missing its package version`);
    return { file, source: source.replace(declaration, `version: ${version}`).replace(dependency, `$1${version}`) };
  });
  for (const { file, source } of updates) writeFileSync(join(root, file), source);
}

export function checkHaskellVersions(root, version) {
  const problems = [];
  let files;
  try { files = manifests(root); }
  catch (error) { return [`Haskell package manifest: ${error.message}`]; }
  for (const { file, source } of files) {
    const actual = source.match(declaration)?.[1];
    if (actual !== version) problems.push(`${file} is ${actual ?? "missing its package version"}, the tag is ${version}`);
    for (const match of source.matchAll(dependency)) {
      if (match[2] !== version) problems.push(`${file} requires ${match[1]}${match[2]}, the tag is ${version}`);
    }
    if (file === "runtime/hs/nightseam-runtime.cabal" && !source.includes(`nightseam-duplex ==${version}`)) {
      problems.push(`${file} must require nightseam-duplex ==${version}`);
    }
  }
  return problems;
}
