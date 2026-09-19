import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { prepareGoRehearsal } from "./rehearsal.mjs";

const module = "github.com/Bitspark/nightseam";

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "nightseam-rehearsal-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true, maxRetries: 10 }));
  const root = join(directory, "repo");
  const scratch = join(directory, "smoke");
  const consumer = join(scratch, "consumer");
  const shared = join(directory, "shared-cache");
  for (const path of [root, consumer, shared]) mkdirSync(path, { recursive: true });
  const git = (...args) => execFileSync("git", ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", ...args], { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  git("init", "--quiet");
  writeFileSync(join(root, "go.mod"), `module ${module}\n\ngo 1.22\n`);
  writeFileSync(join(root, "value.go"), 'package nightseam\n\nconst Value = "rehearsal"\n');
  git("add", ".");
  git("commit", "--quiet", "-m", "fixture: create a module.");
  const revision = git("rev-parse", "HEAD");
  const original = `module example.com/consumer\n\ngo 1.22\n\nrequire ${module} v0.3.0\n`;
  writeFileSync(join(consumer, "go.mod"), original);
  writeFileSync(join(consumer, "main.go"), `package main\n\nimport ("fmt"; "${module}")\n\nfunc main() { fmt.Println(nightseam.Value) }\n`);
  writeFileSync(join(shared, "sentinel"), "the consumer's existing cache\n");
  return { root, scratch, consumer, shared, revision, original };
}

test("a rehearsal uses a distinct version in its proxy and copied consumer", t => {
  const at = fixture(t);
  const source = readFileSync(join(at.root, "go.mod"), "utf8");
  const rehearsal = prepareGoRehearsal({ ...at, module, version: "0.3.0" });
  assert.equal(rehearsal.version, `v0.3.0-rehearsal.${at.revision}`);
  assert.ok(readFileSync(join(at.consumer, "go.mod"), "utf8").includes(`${module} ${rehearsal.version}`));
  assert.equal(readFileSync(join(at.root, "go.mod"), "utf8"), source, "only the copied consumer may change");
  const proxy = join(at.scratch, "goproxy", "github.com", "!bitspark", "nightseam", "@v");
  assert.equal(existsSync(join(proxy, "v0.3.0.zip")), false, "a rehearsal must never impersonate the release");
  assert.equal(JSON.parse(readFileSync(join(proxy, `${rehearsal.version}.info`), "utf8")).Version, rehearsal.version);
});

test("a real Go consumer leaves the inherited module cache untouched", t => {
  const at = fixture(t);
  const rehearsal = prepareGoRehearsal({ ...at, module, version: "0.3.0" });
  const env = { ...process.env, GOMODCACHE: at.shared, GOSUMDB: "unreachable.invalid", ...rehearsal.env };
  const go = (...args) => execFileSync("go", args, { cwd: at.consumer, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  assert.equal(resolve(go("env", "GOMODCACHE")), resolve(join(at.scratch, "gomodcache")));
  assert.equal(go("run", "."), "rehearsal");
  const downloaded = JSON.parse(go("mod", "download", "-json", `${module}@${rehearsal.version}`));
  assert.ok(resolve(downloaded.Dir).startsWith(resolve(join(at.scratch, "gomodcache"))));
  assert.deepEqual(readdirSync(at.shared), ["sentinel"]);
  assert.equal(readFileSync(join(at.shared, "sentinel"), "utf8"), "the consumer's existing cache\n");
  // A second tree at the same release number must build its own bytes too.
  writeFileSync(join(at.root, "value.go"), 'package nightseam\n\nconst Value = "second tree"\n');
  const secondScratch = join(at.scratch, "second");
  const secondConsumer = join(secondScratch, "consumer");
  mkdirSync(secondConsumer, { recursive: true });
  for (const name of ["go.mod", "main.go"]) writeFileSync(join(secondConsumer, name), name === "go.mod" ? at.original : readFileSync(join(at.consumer, name)));
  const second = prepareGoRehearsal({ ...at, scratch: secondScratch, consumer: secondConsumer, module, version: "0.3.0" });
  assert.equal(execFileSync("go", ["run", "."], { cwd: secondConsumer, env: { ...env, ...second.env }, encoding: "utf8" }).trim(), "second tree");
  assert.deepEqual(readdirSync(at.shared), ["sentinel"]);
});
