import assert from "node:assert/strict";
import { execFileSync, execSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative } from "node:path";
import { test } from "node:test";
import { copyRegistryConsumer, examples, manifestsUnder, root } from "./packages.mjs";

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "nightseam-registry-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true, maxRetries: 10 }));
  const write = (path, data) => {
    const file = join(directory, path);
    mkdirSync(dirname(file), { recursive: true });
    writeFileSync(file, data);
  };
  return { directory, write };
}

test("the registry consumer installs inside an ancestor workspace without changing it", { timeout: 30000 }, t => {
  const { directory, write } = fixture(t);
  const packageManager = JSON.parse(readFileSync(join(root, "package.json"), "utf8")).packageManager;
  write("ancestor/package.json", JSON.stringify({ name: "parent-sentinel", private: true, packageManager, dependencies: { unrelated: "file:./unrelated" } }, null, 2) + "\n");
  write("ancestor/pnpm-workspace.yaml", "packages:\n  - 'scratch/*'\nautoInstallPeers: false\n");
  write("ancestor/pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: false\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n");
  write("ancestor/unrelated/package.json", '{"name":"unrelated","version":"1.0.0"}\n');
  write("example/package.json", '{"name":"release-consumer","private":true,"dependencies":{"fixture-release":"file:./release"}}\n');
  write("example/release/package.json", '{"name":"fixture-release","version":"0.4.0"}\n');
  write("example/node_modules/not-copied/sentinel", "not a release source\n");
  write("example/dist/sentinel", "not a release source\n");
  const sentinels = ["package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml", "unrelated/package.json"];
  const before = new Map(sentinels.map(name => [name, readFileSync(join(directory, "ancestor", name))]));
  const manifest = readFileSync(join(directory, "example/package.json"));
  const consumer = join(directory, "ancestor/scratch/consumer");
  copyRegistryConsumer(join(directory, "example"), consumer);
  assert.equal(existsSync(join(consumer, "node_modules")), false);
  assert.equal(existsSync(join(consumer, "dist")), false);
  const args = ["install", "--offline", "--ignore-scripts", "--no-frozen-lockfile"];
  const options = { cwd: consumer, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], env: { ...process.env, CI: "true" } };
  if (process.platform === "win32") execSync("pnpm.cmd " + args.join(" "), options);
  else execFileSync("pnpm", args, options);
  for (const [name, bytes] of before) assert.ok(readFileSync(join(directory, "ancestor", name)).equals(bytes), `ancestor ${name} changed`);
  assert.deepEqual(readFileSync(join(consumer, "package.json")), manifest, "release dependency declarations changed");
  assert.equal(JSON.parse(readFileSync(join(consumer, "node_modules/fixture-release/package.json"), "utf8")).version, "0.4.0");
  assert.ok(existsSync(join(consumer, "pnpm-lock.yaml")), "the install needs its own lockfile");
  assert.equal(existsSync(join(directory, "ancestor/node_modules")), false, "unrelated parent dependencies were installed");
});

test("registry setup preserves a consumer's existing workspace configuration", t => {
  const { directory, write } = fixture(t);
  const workspace = "packages:\n  - 'client'\nautoInstallPeers: false\n";
  write("example/package.json", '{"name":"existing-consumer","private":true}\n');
  write("example/pnpm-workspace.yaml", workspace);
  const consumer = join(directory, "consumer");
  copyRegistryConsumer(join(directory, "example"), consumer);
  assert.equal(readFileSync(join(consumer, "pnpm-workspace.yaml"), "utf8"), workspace);
});

test("registry setup preserves every actual release manifest and Go requirement", t => {
  const { directory } = fixture(t);
  assert.ok(examples.length > 0);
  for (const example of examples) {
    const source = join(root, example);
    const consumer = join(directory, example);
    copyRegistryConsumer(source, consumer);
    for (const manifest of manifestsUnder(example)) {
      const name = relative(source, join(root, manifest));
      assert.deepEqual(readFileSync(join(consumer, name)), readFileSync(join(source, name)), manifest);
    }
    assert.deepEqual(readFileSync(join(consumer, "go.mod")), readFileSync(join(source, "go.mod")));
  }
});
