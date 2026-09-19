import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("./release-tag.mjs", import.meta.url));

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "nightseam-tag-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true, maxRetries: 10 }));
  const repo = join(directory, "repo");
  const remote = join(directory, "remote.git");
  mkdirSync(repo);
  const git = (...args) => execFileSync("git", ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", ...args], { cwd: repo, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  git("init", "--quiet");
  git("init", "--quiet", "--bare", remote);
  git("remote", "add", "origin", remote);
  writeFileSync(join(repo, "content"), "one");
  git("add", ".");
  git("commit", "--quiet", "-m", "fixture: create the release commit.");
  const check = (tag, ...args) => {
    try {
      return { code: 0, out: execFileSync(process.execPath, [script, tag, ...args], { cwd: repo, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) };
    } catch (error) {
      return { code: error.status, out: (error.stdout ?? "") + (error.stderr ?? "") };
    }
  };
  return { directory, repo, git, check };
}

test("a rehearsal permits an unpublished tag but a release requires it", t => {
  const { check } = fixture(t);
  assert.equal(check("v0.3.0").code, 0);
  const release = check("v0.3.0", "--require");
  assert.equal(release.code, 1);
  assert.match(release.out, /v0\.3\.0.*missing/);
});

test("annotated and lightweight remote tags are compared by commit", t => {
  const { git, check } = fixture(t);
  git("tag", "-a", "v0.3.0", "-m", "release fixture");
  git("tag", "v0.3.1");
  git("push", "--quiet", "origin", "refs/tags/v0.3.0", "refs/tags/v0.3.1");
  for (const tag of ["v0.3.0", "v0.3.1"]) {
    const result = check(tag, "--require");
    assert.equal(result.code, 0, result.out);
    assert.match(result.out, /matches the checked-out commit/);
  }
});

test("a tag already naming another commit refuses a release and a rehearsal", t => {
  const { repo, git, check } = fixture(t);
  git("tag", "-a", "v0.3.0", "-m", "release fixture");
  git("push", "--quiet", "origin", "refs/tags/v0.3.0");
  writeFileSync(join(repo, "content"), "two");
  git("add", ".");
  git("commit", "--quiet", "-m", "fixture: create a different commit.");
  for (const args of [[], ["--require"]]) {
    const result = check("v0.3.0", ...args);
    assert.equal(result.code, 1);
    assert.match(result.out, /v0\.3\.0.*already names.*checked-out commit/s);
  }
});

test("an unreadable remote is not treated as an unpublished tag", t => {
  const { directory, git, check } = fixture(t);
  git("remote", "set-url", "origin", join(directory, "missing.git"));
  assert.equal(check("v0.3.0").code, 1);
});
