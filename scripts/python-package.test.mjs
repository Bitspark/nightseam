import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const current = JSON.parse(readFileSync(join(root, "runtime/ts/package.json"), "utf8")).version;

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "nightseam-python-version-"));
  t.after(() => rmSync(directory, { recursive: true, force: true, maxRetries: 10 }));
  for (const path of ["scripts", "runtime/ts", "internal/targets/typescript", "conformance"]) {
    mkdirSync(join(directory, path), { recursive: true });
  }
  for (const name of readdirSync(join(root, "scripts")).filter(name => name.endsWith(".mjs") && !name.endsWith(".test.mjs"))) {
    copyFileSync(join(root, "scripts", name), join(directory, "scripts", name));
  }
  copyFileSync(join(root, "pyproject.toml"), join(directory, "pyproject.toml"));
  writeFileSync(join(directory, "runtime/ts/package.json"), JSON.stringify({ name: "@nightseam/runtime", version: current }));
  writeFileSync(join(directory, "internal/targets/typescript/target.go"), `package typescript\nconst DefaultRuntimeVersion = "${current}"\n`);
  writeFileSync(join(directory, "CHANGELOG.md"), `# Changes\n\n## ${current}\n\nFixture.\n\n## 99.7.3\n\nFixture.\n`);
  writeFileSync(join(directory, "conformance/profiles.json"), JSON.stringify({
    profiles: { core: { layers: ["peer"] } },
    tiers: { 4: { requires: ["core"], onFailure: "provisional" } },
    languages: { python: { tier: 4 } },
  }));
  writeFileSync(join(directory, "conformance/matrix.json"), JSON.stringify({
    profiles: ["core"],
    languages: { python: { tier: 4, cells: { core: { passed: 1, skipped: 0, failed: 0 } } } },
  }));
  const run = (script, ...args) => {
    const result = spawnSync(process.execPath, [join(directory, "scripts", script), ...args], { encoding: "utf8", cwd: directory });
    return { code: result.status, out: result.stdout + result.stderr };
  };
  return { directory, run };
}

test("Python's project version matches the current package release", () => {
  const source = readFileSync(join(root, "pyproject.toml"), "utf8");
  assert.equal(source.match(/^version = "([^"]+)"$/m)?.[1], current);
});

test("versioning moves the Python distribution and the resulting release passes", t => {
  const { directory, run } = fixture(t);
  const result = run("version.mjs", "99.7.3");
  assert.equal(result.code, 0, result.out);
  assert.match(readFileSync(join(directory, "pyproject.toml"), "utf8"), /^version = "99\.7\.3"$/m);
  const release = run("release-prepare.mjs", "v99.7.3", "--dry-run", "--no-previous");
  assert.equal(release.code, 0, release.out);
});

test("a Python-only version drift refuses release preparation", t => {
  const { directory, run } = fixture(t);
  const aligned = run("release-prepare.mjs", `v${current}`, "--dry-run", "--no-previous");
  assert.equal(aligned.code, 0, aligned.out);
  const file = join(directory, "pyproject.toml");
  writeFileSync(file, readFileSync(file, "utf8").replace(/^version = "[^"]+"$/m, 'version = "99.7.3"'));
  const result = run("release-prepare.mjs", `v${current}`, "--dry-run", "--no-previous");
  assert.equal(result.code, 1, result.out);
  assert.match(result.out, /pyproject\.toml is 99\.7\.3, the tag is/);
});

test("missing Python version metadata refuses both versioning and release", t => {
  const { directory, run } = fixture(t);
  const file = join(directory, "pyproject.toml");
  writeFileSync(file, readFileSync(file, "utf8").replace(/^version = "[^"]+"\r?\n/m, ""));
  for (const [script, args] of [["version.mjs", ["99.7.3"]], ["release-prepare.mjs", [`v${current}`, "--dry-run", "--no-previous"]]]) {
    const result = run(script, ...args);
    assert.equal(result.code, 1, result.out);
    assert.match(result.out, /pyproject\.toml/);
  }
});
