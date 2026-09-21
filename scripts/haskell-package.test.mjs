import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { checkHaskellVersions, haskellManifests } from "./haskell-packages.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const current = JSON.parse(readFileSync(join(root, "runtime/ts/package.json"), "utf8")).version;

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "nightseam-haskell-version-"));
  t.after(() => rmSync(directory, { recursive: true, force: true, maxRetries: 10 }));
  for (const path of ["scripts", "runtime/ts", "internal/targets/typescript", "conformance/haskell", "duplex/hs", "runtime/hs"]) {
    mkdirSync(join(directory, path), { recursive: true });
  }
  for (const name of readdirSync(join(root, "scripts")).filter(name => name.endsWith(".mjs") && !name.endsWith(".test.mjs"))) {
    copyFileSync(join(root, "scripts", name), join(directory, "scripts", name));
  }
  for (const file of [...haskellManifests, "conformance/haskell/testee.json", "pyproject.toml"]) {
    copyFileSync(join(root, file), join(directory, file));
  }
  writeFileSync(join(directory, "runtime/ts/package.json"), JSON.stringify({ name: "@nightseam/runtime", version: current }));
  writeFileSync(join(directory, "internal/targets/typescript/target.go"), `package typescript\nconst DefaultRuntimeVersion = "${current}"\n`);
  writeFileSync(join(directory, "CHANGELOG.md"), `# Changes\n\n## ${current}\n\nFixture.\n\n## 99.7.3\n\nFixture.\n`);
  writeFileSync(join(directory, "conformance/profiles.json"), JSON.stringify({
    profiles: { core: { layers: ["peer"] } }, tiers: { 4: { requires: ["core"], onFailure: "provisional" } },
    languages: { haskell: { tier: 4 } },
  }));
  writeFileSync(join(directory, "conformance/matrix.json"), JSON.stringify({
    profiles: ["core"], languages: { haskell: { tier: 4, cells: { core: { passed: 1, skipped: 0, failed: 0 } } } },
  }));
  const run = (script, ...args) => {
    const result = spawnSync(process.execPath, [join(directory, "scripts", script), ...args], { encoding: "utf8", cwd: directory });
    return { code: result.status, out: result.stdout + result.stderr };
  };
  return { directory, run };
}

test("Haskell packages and local dependencies match the current release", () => {
  assert.deepEqual(checkHaskellVersions(root, current), []);
});

test("versioning moves every Haskell package and its local dependency before release", t => {
  const { directory, run } = fixture(t);
  const moved = run("version.mjs", "99.7.3");
  assert.equal(moved.code, 0, moved.out);
  for (const file of haskellManifests) assert.match(readFileSync(join(directory, file), "utf8"), /^version: 99\.7\.3$/m);
  assert.match(readFileSync(join(directory, haskellManifests[1]), "utf8"), /nightseam-duplex ==99\.7\.3/);
  const release = run("release-prepare.mjs", "v99.7.3", "--dry-run", "--no-previous");
  assert.equal(release.code, 0, release.out);
});

for (const drift of ["package", "dependency", "missing"]) {
  test(`release refuses Haskell ${drift} drift`, t => {
    const { directory, run } = fixture(t);
    const file = join(directory, haskellManifests[1]);
    const source = readFileSync(file, "utf8");
    writeFileSync(file, drift === "package" ? source.replace(/^version:.*$/m, "version: 99.7.3")
      : drift === "dependency" ? source.replace(/nightseam-duplex ==[^,\s]+/, "nightseam-duplex ==99.7.3")
      : source.replace(/^version:.*\r?\n/m, ""));
    const release = run("release-prepare.mjs", `v${current}`, "--dry-run", "--no-previous");
    assert.equal(release.code, 1, release.out);
    assert.match(release.out, /nightseam-runtime\.cabal/);
    if (drift === "missing") assert.equal(run("version.mjs", "99.7.3").code, 1);
  });
}
